// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package hookrunner runs hooks: WebAssembly modules, sandboxed, that
// validate a request from an outside service and write the events it stands
// for. See docs/agents/hooks.md.
package hookrunner

import (
	"bytes"
	"container/list"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"sync"
	"time"

	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
	"github.com/tetratelabs/wazero/sys"
)

const (
	// MaxModuleSize is the largest module that may be registered. A hook
	// built by the standard Go toolchain is over 4 MiB.
	MaxModuleSize = 16 << 20
	// memoryLimitPages caps a hook's memory at 32 MiB (64 KiB pages).
	memoryLimitPages = 512
	// maxOutputSize caps what a hook may print; past it the run fails.
	maxOutputSize = 4 << 20
	// runTimeout bounds one run, compilation not included.
	runTimeout = 250 * time.Millisecond
	// compileTimeout bounds compiling a module, once per instance.
	compileTimeout = 30 * time.Second
	// cacheSize is how many compiled modules are kept.
	cacheSize = 64
)

// Module names a stored hook module by its digest; Load reads its bytes
// when the compiled module is not cached.
type Module struct {
	SHA256 [32]byte
	Load   func(ctx context.Context) ([]byte, error)
}

// Service runs hooks.
type Service interface {
	// Inspect compiles a module and returns its manifest, checked, with the
	// module's digest. An *InvalidModuleError means it cannot be registered.
	Inspect(ctx context.Context, wasm []byte) (Manifest, [32]byte, error)
	// Handle runs a hook on one request. An *OutputError is final: the hook
	// answered wrongly and would again. So is an *InvalidModuleError: the
	// stored module does not compile or is not the one registered. Any other
	// error may pass on a retry.
	Handle(ctx context.Context, mod Module, m Manifest, in HandleInput) (Result, error)
	Close(ctx context.Context) error
}

// Config sets the limits a hook's answer is held to.
type Config struct {
	MaxPayloadSize int
}

// InvalidModuleError is a module that cannot be registered.
type InvalidModuleError struct{ msg string }

func (e *InvalidModuleError) Error() string { return "invalid hook: " + e.msg }

func invalidModule(format string, a ...any) error {
	return &InvalidModuleError{msg: fmt.Sprintf(format, a...)}
}

var errOutputTooLarge = errors.New("output too large")

type service struct {
	rt             wazero.Runtime
	maxPayloadSize int
	// slots bounds the hooks running at once, and so their memory.
	slots chan struct{}

	mu       sync.Mutex
	lru      *list.List // of *entry, most recently used first
	entries  map[[32]byte]*list.Element
	inflight map[[32]byte]*compileCall
}

// entry is a compiled module. refs counts the runs using it: an evicted
// entry is closed by whichever of them finishes last.
type entry struct {
	sum      [32]byte
	compiled wazero.CompiledModule
	refs     int
	evicted  bool
}

// compileCall is one compilation that concurrent misses of the same module
// share.
type compileCall struct {
	done chan struct{}
	// waiters, ent and err are guarded by service.mu. The entry starts with
	// a ref for each waiter, so it cannot be evicted and closed before they
	// get to it.
	waiters int
	ent     *entry
	err     error
}

// New starts the runtime every hook runs in.
func New(ctx context.Context, cfg Config) Service {
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfig().
		WithCloseOnContextDone(true).
		WithMemoryLimitPages(memoryLimitPages))
	// A fresh runtime has no other WASI host module, the only way this fails.
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	return &service{
		rt:             rt,
		maxPayloadSize: cfg.MaxPayloadSize,
		slots:          make(chan struct{}, 2*runtime.GOMAXPROCS(0)),
		lru:            list.New(),
		entries:        make(map[[32]byte]*list.Element),
		inflight:       make(map[[32]byte]*compileCall),
	}
}

func (s *service) Close(ctx context.Context) error { return s.rt.Close(ctx) }

func (s *service) Inspect(ctx context.Context, wasm []byte) (Manifest, [32]byte, error) {
	sum := sha256.Sum256(wasm)
	if len(wasm) > MaxModuleSize {
		return Manifest{}, sum, invalidModule("%d bytes; at most %d", len(wasm), MaxModuleSize)
	}
	ent, err := s.acquire(ctx, Module{SHA256: sum, Load: func(context.Context) ([]byte, error) { return wasm, nil }})
	if err != nil {
		// An *InvalidModuleError, or the request's own deadline or
		// cancellation, which a retry may pass: never a 422.
		return Manifest{}, sum, err
	}
	defer s.release(ctx, ent)
	out, err := s.run(ctx, ent, "manifest", nil)
	if err != nil && ctx.Err() != nil {
		// The request gave up, not the module: it may pass on a retry.
		return Manifest{}, sum, ctx.Err()
	}
	if err != nil {
		return Manifest{}, sum, invalidModule("manifest: %v", err)
	}
	m, err := parseManifest(out)
	if err != nil {
		return Manifest{}, sum, invalidModule("%v", err)
	}
	return m, sum, nil
}

func (s *service) Handle(ctx context.Context, mod Module, m Manifest, in HandleInput) (Result, error) {
	in.ABIVersion = ABIVersion
	stdin, err := json.Marshal(in)
	if err != nil {
		// Only an input that is not JSON-able, which the server built.
		return Result{}, fmt.Errorf("hookrunner: input: %w", err)
	}
	ent, err := s.acquire(ctx, mod)
	if err != nil {
		return Result{}, err
	}
	defer s.release(ctx, ent)
	out, err := s.run(ctx, ent, "handle", stdin)
	if errors.Is(err, errOutputTooLarge) {
		return Result{}, outputErr("more than %d bytes", maxOutputSize)
	}
	if err != nil {
		return Result{}, err
	}
	return parseResult(out, m, s.maxPayloadSize)
}

// run instantiates the module afresh, so nothing outlives one request, and
// runs it as a command with the one argument.
func (s *service) run(ctx context.Context, ent *entry, command string, stdin []byte) ([]byte, error) {
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-s.slots }()

	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	stdout := &cappedBuffer{max: maxOutputSize}
	cfg := wazero.NewModuleConfig().
		WithName("").
		WithArgs("hook", command).
		WithStdin(bytes.NewReader(stdin)).
		WithStdout(stdout).
		WithStderr(io.Discard).
		WithSysWalltime().
		WithSysNanotime().
		WithRandSource(rand.Reader)
	mod, err := s.rt.InstantiateModule(ctx, ent.compiled, cfg)
	if mod != nil {
		_ = mod.Close(ctx)
	}
	if stdout.over {
		return nil, errOutputTooLarge
	}
	var exit *sys.ExitError
	switch {
	case errors.As(err, &exit) && exit.ExitCode() == sys.ExitCodeDeadlineExceeded:
		return nil, fmt.Errorf("timed out after %s", runTimeout)
	case errors.As(err, &exit):
		return nil, fmt.Errorf("exited with code %d", exit.ExitCode())
	case err != nil:
		return nil, err
	}
	return stdout.Bytes(), nil
}

// acquire returns the module compiled, from the cache or compiling it once
// however many callers miss together, and holds it until release.
func (s *service) acquire(ctx context.Context, mod Module) (*entry, error) {
	s.mu.Lock()
	if el, ok := s.entries[mod.SHA256]; ok {
		s.lru.MoveToFront(el)
		ent := el.Value.(*entry)
		ent.refs++
		s.mu.Unlock()
		return ent, nil
	}
	call, ok := s.inflight[mod.SHA256]
	if !ok {
		call = &compileCall{done: make(chan struct{})}
		s.inflight[mod.SHA256] = call
		// Detached: one caller giving up must not fail the others.
		go s.compile(context.WithoutCancel(ctx), mod, call)
	}
	call.waiters++
	s.mu.Unlock()

	select {
	case <-call.done:
		return call.ent, call.err
	case <-ctx.Done():
	}
	s.mu.Lock()
	ent := call.ent
	if ent == nil {
		call.waiters--
	}
	s.mu.Unlock()
	if ent != nil {
		// It finished as this caller gave up: give back its ref.
		s.release(ctx, ent)
	}
	return nil, ctx.Err()
}

func (s *service) compile(ctx context.Context, mod Module, call *compileCall) {
	ctx, cancel := context.WithTimeout(ctx, compileTimeout)
	defer cancel()
	ent, err := s.load(ctx, mod)

	var evicted []*entry
	s.mu.Lock()
	delete(s.inflight, mod.SHA256)
	if err == nil {
		ent.refs = call.waiters
		s.entries[mod.SHA256] = s.lru.PushFront(ent)
		for s.lru.Len() > cacheSize {
			old := s.lru.Remove(s.lru.Back()).(*entry)
			delete(s.entries, old.sum)
			old.evicted = true
			if old.refs == 0 {
				evicted = append(evicted, old)
			}
		}
	}
	call.ent, call.err = ent, err
	s.mu.Unlock()
	close(call.done)
	for _, old := range evicted {
		_ = old.compiled.Close(ctx)
	}
}

func (s *service) load(ctx context.Context, mod Module) (*entry, error) {
	wasm, err := mod.Load(ctx)
	if err != nil {
		return nil, fmt.Errorf("hookrunner: load: %w", err)
	}
	// The digest names the module the hook was registered with; anything
	// else in its place is not run.
	if sha256.Sum256(wasm) != mod.SHA256 {
		return nil, invalidModule("the stored module does not match its digest")
	}
	compiled, err := s.rt.CompileModule(ctx, wasm)
	if err != nil {
		return nil, invalidModule("does not compile: %v", err)
	}
	return &entry{sum: mod.SHA256, compiled: compiled}, nil
}

func (s *service) release(ctx context.Context, ent *entry) {
	s.mu.Lock()
	ent.refs--
	closeIt := ent.evicted && ent.refs == 0
	s.mu.Unlock()
	if closeIt {
		_ = ent.compiled.Close(ctx)
	}
}

// cappedBuffer keeps what a hook prints up to max bytes and refuses the rest.
type cappedBuffer struct {
	bytes.Buffer
	max  int
	over bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	if b.Len()+len(p) > b.max {
		b.over = true
		return 0, errOutputTooLarge
	}
	return b.Buffer.Write(p)
}
