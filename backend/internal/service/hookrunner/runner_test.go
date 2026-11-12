// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/tetratelabs/wazero"
)

// fixture is the testdata hook, built for wasip1 once.
var fixture []byte

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "hookrunner")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out := filepath.Join(dir, "fixture.wasm")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "./testdata/fixture")
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		fmt.Fprintf(os.Stderr, "build fixture: %v\n%s", err, b)
		os.Exit(1)
	}
	fixture, err = os.ReadFile(out)
	_ = os.RemoveAll(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

// tiny is the smallest WASI command: its _start pushes and drops n, so each n
// is a different module, then runs body.
func tiny(n int, body ...byte) []byte {
	code := append([]byte{0x00, 0x41, byte(n&0x7f) | 0x80, byte(n>>7) & 0x3f, 0x1a}, append(body, 0x0b)...)
	b := []byte{0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00, // type: func()
		0x03, 0x02, 0x01, 0x00, // one function of that type
		0x07, 0x0a, 0x01, 0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x00, // export _start
		0x0a, byte(len(code) + 2), 0x01, byte(len(code))}
	return append(b, code...)
}

const unreachable = 0x00

func newService(t *testing.T) *service {
	t.Helper()
	s := New(context.Background(), Config{MaxPayloadSize: 1024}).(*service)
	t.Cleanup(func() { _ = s.Close(context.Background()) })
	return s
}

func fixtureModule(loads *atomic.Int32) Module {
	return Module{SHA256: sha256.Sum256(fixture), Load: func(context.Context) ([]byte, error) {
		if loads != nil {
			loads.Add(1)
		}
		return fixture, nil
	}}
}

var fixtureManifest = Manifest{Events: []Event{{Name: "ci.failed"}, {Name: "pr.opened"}}}

func handle(t *testing.T, s Service, inputs map[string]any) (Result, error) {
	t.Helper()
	return s.Handle(context.Background(), fixtureModule(nil), fixtureManifest, HandleInput{Inputs: inputs})
}

func TestInspectReadsTheManifest(t *testing.T) {
	s := newService(t)
	m, sum, err := s.Inspect(context.Background(), fixture)
	if err != nil {
		t.Fatal(err)
	}
	if sum != sha256.Sum256(fixture) {
		t.Error("digest is not the module's")
	}
	if m.Name != "fixture" || m.Version != "1.0.0" || len(m.Inputs) != 3 || !m.Declares("pr.opened") {
		t.Errorf("manifest = %+v", m)
	}
}

func TestInspectRefusesWhatCannotBeRegistered(t *testing.T) {
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
		wasm []byte
		want string
	}{
		{"not wasm", context.Background(), []byte("hello"), "does not compile"},
		{"too large", context.Background(), make([]byte, MaxModuleSize+1), "at most"},
		{"prints no manifest", context.Background(), tiny(1), "manifest is not JSON"},
		{"traps on manifest", context.Background(), tiny(2, unreachable), "manifest:"},
		{"cancelled", cancelled, tiny(3), "context canceled"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := newService(t).Inspect(tc.ctx, tc.wasm)
			var invalid *InvalidModuleError
			if !errors.As(err, &invalid) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want an InvalidModuleError naming %q", err, tc.want)
			}
		})
	}
}

func TestHandleSendsTheRequestAndInputs(t *testing.T) {
	s := newService(t)
	s.maxPayloadSize = 1 << 20
	now := time.Date(2026, 11, 12, 3, 16, 59, 0, time.UTC)
	res, err := s.Handle(context.Background(), fixtureModule(nil), fixtureManifest, HandleInput{
		RegistrationID: "0ls4JilchmO",
		Request:        Request{Method: "POST", Path: "/p", Headers: map[string][]string{"x-a": {"1"}}, Body: []byte("{}")},
		Inputs:         map[string]any{"mode": "echo", "secret": "s"},
		Now:            now,
	})
	if err != nil {
		t.Fatal(err)
	}
	var got HandleInput
	if err := json.Unmarshal([]byte(res.Events[0].Payload), &got); err != nil {
		t.Fatal(err)
	}
	if got.ABIVersion != ABIVersion || got.RegistrationID != "0ls4JilchmO" || string(got.Request.Body) != "{}" ||
		got.Request.Headers["x-a"][0] != "1" || got.Inputs["secret"] != "s" || !got.Now.Equal(now) {
		t.Errorf("the hook read %+v", got)
	}
}

func TestHandleReturnsTheHooksAnswer(t *testing.T) {
	s := newService(t)
	res, err := handle(t, s, map[string]any{"mode": "reply", "reply": `{"accept":true,"events":[{"name":"pr.opened","dedupKey":"d1","payload":{"n": 1},"faq":[{"q":"Q","a":"A"}]}]}`})
	if err != nil {
		t.Fatal(err)
	}
	want := EventOut{Name: "pr.opened", DedupKey: "d1", Payload: `{"n":1}`, FAQ: []FAQ{{Q: "Q", A: "A"}}}
	if !res.Accept || len(res.Events) != 1 || fmt.Sprint(res.Events[0]) != fmt.Sprint(want) {
		t.Errorf("result = %+v", res)
	}
	res, err = handle(t, s, map[string]any{"mode": "reply", "reply": `{"accept":false,"reason":"signature mismatch"}`})
	if err != nil || res.Accept || res.Reason != "signature mismatch" {
		t.Errorf("rejected = %+v, %v", res, err)
	}
}

func TestHandleFailures(t *testing.T) {
	s := newService(t)
	for _, tc := range []struct {
		mode  string
		want  string
		final bool
	}{
		{"reply", "not JSON", true},
		{"flood", "more than", true},
		{"loop", "timed out", false},
		{"exit", "exited with code 3", false},
		{"memory", "", false},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			_, err := handle(t, s, map[string]any{"mode": tc.mode, "reply": "nope"})
			var final *OutputError
			if err == nil || errors.As(err, &final) != tc.final || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v (final %v), want %q, final %v", err, errors.As(err, &final), tc.want, tc.final)
			}
		})
	}
}

func TestHandleRefusesAnInputThatIsNotJSON(t *testing.T) {
	_, err := handle(t, newService(t), map[string]any{"mode": func() {}})
	if err == nil || !strings.Contains(err.Error(), "input") {
		t.Errorf("err = %v", err)
	}
}

func TestHandleRefusesAModuleThatIsNotTheRegisteredOne(t *testing.T) {
	s := newService(t)
	mod := Module{SHA256: sha256.Sum256([]byte("other")), Load: func(context.Context) ([]byte, error) { return fixture, nil }}
	if _, err := s.Handle(context.Background(), mod, fixtureManifest, HandleInput{}); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Errorf("err = %v", err)
	}
	mod.Load = func(context.Context) ([]byte, error) { return nil, errors.New("storage down") }
	if _, err := s.Handle(context.Background(), mod, fixtureManifest, HandleInput{}); err == nil || !strings.Contains(err.Error(), "storage down") {
		t.Errorf("err = %v", err)
	}
}

func TestConcurrentMissesCompileOnce(t *testing.T) {
	s := newService(t)
	var loads atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := s.Handle(context.Background(), fixtureModule(&loads), fixtureManifest, HandleInput{Inputs: map[string]any{"mode": "reply", "reply": `{"accept":true}`}}); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	if n := loads.Load(); n != 1 {
		t.Errorf("loaded %d times, want 1", n)
	}
}

func TestTheCacheEvictsTheLeastRecentlyUsed(t *testing.T) {
	s := newService(t)
	loads := map[int]int{}
	inspect := func(tag int) {
		t.Helper()
		mod := tiny(tag)
		_, err := s.acquire(context.Background(), Module{SHA256: sha256.Sum256(mod), Load: func(context.Context) ([]byte, error) {
			loads[tag]++
			return mod, nil
		}})
		if err != nil {
			t.Fatal(err)
		}
	}
	release := func(tag int) {
		t.Helper()
		s.release(context.Background(), s.entries[sha256.Sum256(tiny(tag))].Value.(*entry))
	}
	// "held" stays in use while the cache fills past it.
	const held = 1000
	inspect(held)
	heldEnt := s.entries[sha256.Sum256(tiny(held))].Value.(*entry)
	for i := range cacheSize {
		inspect(i)
		release(i)
	}
	if !heldEnt.evicted || s.lru.Len() != cacheSize {
		t.Fatalf("evicted %v, cached %d", heldEnt.evicted, s.lru.Len())
	}
	s.release(context.Background(), heldEnt)
	inspect(held)
	if loads[held] != 2 {
		t.Errorf("held loaded %d times, want 2 after its eviction", loads[held])
	}
	// 0 is now the least recently used, and went to make room.
	if _, ok := s.entries[sha256.Sum256(tiny(0))]; ok {
		t.Error("0 is still cached")
	}
}

func TestAWaiterThatGivesUpLeavesNoRef(t *testing.T) {
	s := newService(t)
	gate := make(chan struct{})
	mod := tiny(4)
	m := Module{SHA256: sha256.Sum256(mod), Load: func(context.Context) ([]byte, error) { <-gate; return mod, nil }}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := s.acquire(ctx, m); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	close(gate)
	ent, err := s.acquire(context.Background(), m)
	if err != nil {
		t.Fatal(err)
	}
	if ent.refs != 1 {
		t.Errorf("refs = %d, want only the second caller's", ent.refs)
	}
}

// A waiter that gives up just as the compile finishes holds a ref it must
// give back.
func TestAWaiterThatGivesUpAsTheCompileEndsGivesItsRefBack(t *testing.T) {
	s := newService(t)
	sum := sha256.Sum256([]byte("x"))
	call := &compileCall{done: make(chan struct{})}
	s.inflight[sum] = call
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error)
	go func() {
		_, err := s.acquire(ctx, Module{SHA256: sum})
		errc <- err
	}()
	for {
		s.mu.Lock()
		w := call.waiters
		s.mu.Unlock()
		if w == 1 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	ent := &entry{refs: 1, evicted: true, compiled: mustCompile(t, s, tiny(5))}
	s.mu.Lock()
	call.ent = ent
	s.mu.Unlock()
	cancel()
	if err := <-errc; !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if ent.refs != 0 {
		t.Errorf("refs = %d, want 0", ent.refs)
	}
}

func TestRunWaitsForASlot(t *testing.T) {
	s := newService(t)
	for range cap(s.slots) {
		s.slots <- struct{}{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	if _, err := s.run(ctx, &entry{}, "handle", nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func mustCompile(t *testing.T, s *service, wasm []byte) wazero.CompiledModule {
	t.Helper()
	c, err := s.rt.CompileModule(context.Background(), wasm)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
