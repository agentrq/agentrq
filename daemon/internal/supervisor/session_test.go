// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/wire"
)

// fakePTY stands in for a terminal so the supervisor's rules can be tested
// without spawning anything.
type fakePTY struct {
	mu       sync.Mutex
	closed   bool
	exitCode int
	waitErr  error
	written  []byte
	cols     uint16
	rows     uint16
	done     chan struct{}
	stuck    chan struct{}
}

func newFakePTY() *fakePTY { return &fakePTY{done: make(chan struct{})} }

func (f *fakePTY) Read([]byte) (int, error) { <-f.done; return 0, errors.New("closed") }

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.written = append(f.written, p...)
	return len(p), nil
}

func (f *fakePTY) Resize(cols, rows uint16) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cols, f.rows = cols, rows
	return nil
}

func (f *fakePTY) size() (uint16, uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cols, f.rows
}

func (f *fakePTY) Wait() (int, error) {
	<-f.done
	f.mu.Lock()
	stuck := f.stuck
	f.mu.Unlock()
	if stuck != nil {
		<-stuck
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exitCode, f.waitErr
}

// blockWait makes this terminal's process refuse to go: closing the terminal
// no longer ends it, and Wait does not return. Real processes do this — one
// ignoring the hang-up while it finishes a write, or wedged in a syscall — and
// a shutdown that waits for them has to give up rather than hold the machine.
func (f *fakePTY) blockWait(t *testing.T) {
	t.Helper()
	f.mu.Lock()
	f.stuck = make(chan struct{})
	stuck := f.stuck
	f.mu.Unlock()
	t.Cleanup(func() { close(stuck) })
}

func (f *fakePTY) Close() error {
	f.mu.Lock()
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	f.mu.Unlock()
	return nil
}

func (f *fakePTY) exit(code int, err error) {
	f.mu.Lock()
	f.exitCode, f.waitErr = code, err
	if !f.closed {
		f.closed = true
		close(f.done)
	}
	f.mu.Unlock()
}

// recordingStarter captures what the supervisor asked for.
type recordingStarter struct {
	mu    sync.Mutex
	specs []pty.Spec
	ctxs  []context.Context
	ptys  []*fakePTY
	err   error
}

func (r *recordingStarter) start(ctx context.Context, spec pty.Spec) (pty.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	r.specs = append(r.specs, spec)
	r.ctxs = append(r.ctxs, ctx)
	p := newFakePTY()
	r.ptys = append(r.ptys, p)
	return p, nil
}

// lastCtx is the context the process was actually started with, which decides
// how long it lives: the pty layer kills what it is given when that is done.
func (r *recordingStarter) lastCtx() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ctxs[len(r.ctxs)-1]
}

func (r *recordingStarter) last() (pty.Spec, *fakePTY) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.specs[len(r.specs)-1], r.ptys[len(r.ptys)-1]
}

// nth is the terminal of the n'th session started, so a test can end one that
// is not the most recent.
func (r *recordingStarter) nth(n int) *fakePTY {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ptys[n]
}

func claudeRequest(t *testing.T, id uint64) Request {
	t.Helper()
	return Request{
		ID:     id,
		Kind:   KindClaudeCode,
		Params: Params{Workspace: "agentrq-code", ServerName: "agentrq-workspace"},
		Dir:    t.TempDir(),
		MCPURL: testURL,
		Cols:   120, Rows: 40,
	}
}

func waitFor(t *testing.T, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal(msg)
}

func TestStartRunsTheResolvedCommandInTheWorkspaceFolder(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)

	req := claudeRequest(t, 1)
	sess, err := s.Start(t.Context(), "work", req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if state, _, _ := sess.State(); state != StateRunning {
		t.Errorf("state = %q, want running", state)
	}

	spec, _ := st.last()
	if spec.Dir != req.Dir {
		t.Errorf("dir = %q, want the workspace folder", spec.Dir)
	}
	if spec.Cols != 120 || spec.Rows != 40 {
		t.Errorf("size = %dx%d, want 120x40", spec.Cols, spec.Rows)
	}
	if len(spec.Argv) == 0 || spec.Argv[0] != "claude" {
		t.Errorf("argv = %v", spec.Argv)
	}
}

// Everything that can be refused is refused before a process exists, so a
// rejected request leaves nothing behind.
func TestAnUnknownKindNeverSpawnsAnything(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)

	req := claudeRequest(t, 1)
	req.Kind = "bash"
	if _, err := s.Start(t.Context(), "work", req); !errors.Is(err, ErrUnknownKind) {
		t.Fatalf("error = %v, want ErrUnknownKind", err)
	}
	if len(st.specs) != 0 {
		t.Error("a refused kind still reached the terminal layer")
	}
	if s.Count() != 0 {
		t.Error("a refused start left a session behind")
	}
}

// The whole-machine cap is the one that protects anything: two accounts that
// cannot see each other will otherwise exhaust a box neither believes it is
// overloading.
func TestCapsRefuseFurtherSessions(t *testing.T) {
	t.Run("whole machine", func(t *testing.T) {
		st := &recordingStarter{}
		s := New(st.start, 0, 2)
		for i := uint64(1); i <= 2; i++ {
			if _, err := s.Start(t.Context(), "work", claudeRequest(t, i)); err != nil {
				t.Fatalf("Start %d: %v", i, err)
			}
		}
		// A different profile must not get past the machine-wide cap.
		if _, err := s.Start(t.Context(), "other", claudeRequest(t, 3)); !errors.Is(err, ErrAtCapacity) {
			t.Errorf("error = %v, want ErrAtCapacity", err)
		}
	})

	t.Run("per profile", func(t *testing.T) {
		st := &recordingStarter{}
		s := New(st.start, 1, 10)
		if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Start(t.Context(), "work", claudeRequest(t, 2)); !errors.Is(err, ErrAtCapacity) {
			t.Errorf("error = %v, want ErrAtCapacity", err)
		}
		// Another profile still has room.
		if _, err := s.Start(t.Context(), "personal", claudeRequest(t, 3)); err != nil {
			t.Errorf("a different profile was refused: %v", err)
		}
	})
}

func TestDuplicateSessionIDIsRefused(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("error = %v, want ErrAlreadyExists", err)
	}
}

// A failure after the reservation must release it, or the caps leak and the
// machine slowly refuses everything.
func TestAFailedStartReleasesItsSlot(t *testing.T) {
	st := &recordingStarter{err: errors.New("no pty for you")}
	s := New(st.start, 0, 1)

	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err == nil {
		t.Fatal("expected the start to fail")
	}
	if s.Count() != 0 {
		t.Fatalf("the failed start held onto a slot: %d", s.Count())
	}
	// The one slot is still available.
	st.err = nil
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 2)); err != nil {
		t.Errorf("the cap leaked: %v", err)
	}
}

func TestExitIsRecordedWithItsCode(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	sess, err := s.Start(t.Context(), "work", claudeRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}

	_, p := st.last()
	p.exit(3, nil)

	waitFor(t, func() bool { st, _, _ := sess.State(); return st.Terminal() }, "the session never finished")
	state, code, _ := sess.State()
	if state != StateExited || code != 3 {
		t.Errorf("state = %q code = %d, want exited 3", state, code)
	}
}

// "No such session" for something somebody was watching a moment ago is a
// confusing answer, so a finished session stays until it is forgotten.
func TestAFinishedSessionIsStillReadable(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	_, p := st.last()
	p.exit(0, nil)

	waitFor(t, func() bool {
		sess, err := s.Get(1)
		if err != nil {
			return false
		}
		st, _, _ := sess.State()
		return st.Terminal()
	}, "the finished session became unreadable")
}

func TestKill(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	sess, err := s.Start(t.Context(), "work", claudeRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}

	if err := s.Kill(1); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitFor(t, func() bool { st, _, _ := sess.State(); return st == StateKilled }, "state never became killed")

	_, p := st.last()
	p.mu.Lock()
	closed := p.closed
	p.mu.Unlock()
	if !closed {
		t.Error("the terminal was not closed")
	}

	// Killing it again is not an error: the caller's goal is already met.
	if err := s.Kill(1); err != nil {
		t.Errorf("second Kill: %v", err)
	}
	if err := s.Kill(999); !errors.Is(err, ErrNoSuchSession) {
		t.Errorf("Kill of an unknown session = %v, want ErrNoSuchSession", err)
	}
}

// A killed session's exit must not be re-reported as an ordinary exit — the
// kill is the cause, and the exit is its consequence.
func TestKillWinsOverTheExitItCauses(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	sess, err := s.Start(t.Context(), "work", claudeRequest(t, 1))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Kill(1); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if state, _, _ := sess.State(); state != StateKilled {
		t.Errorf("state = %q, want killed", state)
	}
}

// Forgetting a running session would leave a process nothing is watching and
// nothing can kill.
func TestForgetOnlyDropsFinishedSessions(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Forget(1); err == nil {
		t.Error("a running session was forgotten")
	}

	_, p := st.last()
	p.exit(0, nil)
	waitFor(t, func() bool {
		sess, _ := s.Get(1)
		st, _, _ := sess.State()
		return st.Terminal()
	}, "never finished")

	if err := s.Forget(1); err != nil {
		t.Errorf("Forget: %v", err)
	}
	if _, err := s.Get(1); !errors.Is(err, ErrNoSuchSession) {
		t.Errorf("still present after Forget: %v", err)
	}
}

// claude-code reads .mcp.json from its working directory, so it has to exist
// before the process does.
func TestMCPConfigIsWrittenBeforeTheProcessStarts(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	cfg := readConfig(t, req.Dir+"/"+MCPConfigName)
	if _, ok := cfg.Servers["agentrq-workspace"]; !ok {
		t.Error("the agent was started without its MCP configuration")
	}
}

// The gateway does not read .mcp.json, so writing one would leave a file — and
// a credential — in a directory for no reason.
// The gateway gets a config too, and it was the absence of this that made a
// launched gateway die a second after starting with "Could not find
// .mcp.json". This test used to assert the opposite.
func TestTheGatewayGetsItsMCPConfigToo(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := Request{
		ID: 1, Kind: KindACPGateway,
		Params: Params{
			Model: "gemini-3.8-flash-high", Agent: "antigravity-acp",
			ServerName: "agentrq-workspace",
		},
		Dir: t.TempDir(), MCPURL: testURL,
	}
	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	if _, err := readMCPConfigExists(req.Dir); err != nil {
		t.Errorf("no MCP config was written for the gateway: %v", err)
	}
}

func TestStateTerminal(t *testing.T) {
	for _, s := range []State{StateExited, StateKilled, StateFailed} {
		if !s.Terminal() {
			t.Errorf("%q should be terminal", s)
		}
	}
	for _, s := range []State{StateStarting, StateRunning} {
		if s.Terminal() {
			t.Errorf("%q should not be terminal", s)
		}
	}
}

// realStarter is pty.Start, for the tests that need the genuine working
// directory check rather than a stand-in.
func realStarter(ctx context.Context, spec pty.Spec) (pty.Session, error) {
	return pty.Start(ctx, spec)
}

// Running answers "what is this daemon actually supervising", which is what a
// reconnecting backend needs in order to correct rows it believes are alive.
func TestRunningListsOnlyLiveSessions(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	dir := t.TempDir()

	for _, id := range []uint64{9, 3, 5} {
		if _, err := s.Start(t.Context(), "work", Request{
			ID: id, Kind: KindACPGateway, Dir: dir, MCPURL: "https://agentrq.example/mcp/ws?token=test",
			Params: Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
		}); err != nil {
			t.Fatalf("start %d: %v", id, err)
		}
	}

	// Sorted, so a backend comparing two hellos is comparing like with like.
	if got := s.Running(); len(got) != 3 || got[0] != 3 || got[1] != 5 || got[2] != 9 {
		t.Fatalf("Running = %v", got)
	}

	if err := s.Kill(5); err != nil {
		t.Fatalf("kill: %v", err)
	}
	got := s.Running()
	for _, id := range got {
		if id == 5 {
			t.Errorf("a killed session is still reported as running: %v", got)
		}
	}
	if len(got) != 2 {
		t.Errorf("Running = %v, want two survivors", got)
	}
}

// The mount a workspace sits on is the one that fills up, and it is not always
// the root filesystem — so the machine reports free space for the folders its
// agents are actually working in.
func TestDirsAreTheFoldersOfLiveSessions(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	one, two := t.TempDir(), t.TempDir()

	// Two sessions in the same folder are one filesystem, not two entries.
	for id, dir := range map[uint64]string{9: one, 5: one, 3: two} {
		if _, err := s.Start(t.Context(), "work", Request{
			ID: id, Kind: KindACPGateway, Dir: dir, MCPURL: "https://agentrq.example/mcp/ws?token=test",
			Params: Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
		}); err != nil {
			t.Fatalf("start %d: %v", id, err)
		}
	}

	got := s.Dirs()
	if len(got) != 2 {
		t.Fatalf("Dirs = %v, want one entry per folder", got)
	}

	// A session that has ended is not working anywhere.
	if err := s.Kill(3); err != nil {
		t.Fatalf("kill: %v", err)
	}
	if got := s.Dirs(); len(got) != 1 || got[0] != one {
		t.Errorf("Dirs = %v after the second folder's session ended", got)
	}
}

// An agent that outlives its daemon is one nothing can reach: not listed, not
// stoppable, and not adopted by the next daemon — which starts believing
// nothing is running and says so, while the process keeps working against the
// workspace.
func TestStopAllEndsEverySession(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	dir := t.TempDir()

	for _, id := range []uint64{9, 3, 5} {
		if _, err := s.Start(t.Context(), "work", Request{
			ID: id, Kind: KindACPGateway, Dir: dir, MCPURL: testURL,
			Params: Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
		}); err != nil {
			t.Fatalf("start %d: %v", id, err)
		}
	}

	stopped := s.StopAll(t.Context(), 5*time.Second)
	if len(stopped) != 3 {
		t.Fatalf("stopped %v, want all three", stopped)
	}
	if live := s.Live(); len(live) != 0 {
		t.Errorf("%d sessions are still running", len(live))
	}
	for _, id := range []uint64{9, 3, 5} {
		sess, err := s.Get(id)
		if err != nil {
			t.Fatalf("session %d: %v", id, err)
		}
		if state, _, _ := sess.State(); state != StateKilled {
			t.Errorf("session %d is %q, want killed", id, state)
		}
	}
}

// Shutting down with nothing running is the ordinary case and must not wait.
func TestStopAllWithNothingRunning(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	start := time.Now()
	if stopped := s.StopAll(t.Context(), 5*time.Second); len(stopped) != 0 {
		t.Errorf("stopped %v", stopped)
	}
	if time.Since(start) > time.Second {
		t.Error("stopping nothing took a noticeable amount of time")
	}
}

// A shutdown that waits for ever is a machine somebody has to go and find.
func TestStopAllGivesUpRatherThanHanging(t *testing.T) {
	stubborn := &recordingStarter{}
	s := New(stubborn.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", Request{
		ID: 9, Kind: KindACPGateway, Dir: t.TempDir(), MCPURL: testURL,
		Params: Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
	}); err != nil {
		t.Fatal(err)
	}

	// A process that never finishes: Wait blocks until the test ends.
	_, p := stubborn.last()
	p.blockWait(t)

	start := time.Now()
	s.StopAll(t.Context(), 150*time.Millisecond)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("StopAll waited %v for a process that was never going to go", elapsed)
	}
}

// A shutdown that is itself cut short stops waiting.
func TestStopAllStopsWhenItsContextDoes(t *testing.T) {
	stubborn := &recordingStarter{}
	s := New(stubborn.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", Request{
		ID: 9, Kind: KindACPGateway, Dir: t.TempDir(), MCPURL: testURL,
		Params: Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
	}); err != nil {
		t.Fatal(err)
	}
	_, p := stubborn.last()
	p.blockWait(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	start := time.Now()
	s.StopAll(ctx, time.Minute)
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Errorf("StopAll ignored its cancelled context for %v", elapsed)
	}
}

// The request that starts an agent must not own it.
//
// The context reaching Start is the daemon's connection to the backend, and
// that is cancelled every time the socket drops — a deploy, a network blink, a
// machine disabled and re-enabled. The pty layer kills the process bound to a
// cancelled context, so handing this one straight down killed every agent on
// the machine whenever the daemon reconnected.
func TestTheConnectionDroppingDoesNotKillTheAgents(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)

	connCtx, drop := context.WithCancel(context.Background())
	sess, err := s.Start(connCtx, "work", claudeRequest(t, 9))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	// The socket goes, as it does on every reconnect.
	drop()

	if err := st.lastCtx().Err(); err != nil {
		t.Fatalf("the process was started on a context that is now %v, so the pty layer will kill it", err)
	}
	if state, _, _ := sess.State(); state != StateRunning {
		t.Errorf("session is %q after a reconnect, want running", state)
	}
	if len(s.Live()) != 1 {
		t.Error("the session is no longer live after the connection dropped")
	}
}

// Whatever the connection carried is still there; only the cancellation is
// dropped. Using context.Background() instead would silently discard a logger
// or a trace the caller put in.
func TestTheStartContextKeepsItsValues(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)

	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "carried")
	if _, err := s.Start(ctx, "work", claudeRequest(t, 9)); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if got := st.lastCtx().Value(key{}); got != "carried" {
		t.Errorf("the process context carries %v, want the caller's value", got)
	}
}

// endAndWait ends a session's process and blocks until the supervisor has
// recorded the outcome — the capacity check reads the recorded state, so a
// test that raced it would pass for the wrong reason.
func endAndWait(t *testing.T, s *Supervisor, p *fakePTY, id uint64) {
	t.Helper()
	p.exit(0, nil)
	waitFor(t, func() bool {
		sess, err := s.Get(id)
		if err != nil {
			return false
		}
		state, _, _ := sess.State()
		return state.Terminal()
	}, "session never finished")
}

// The bug this is here for: a finished session stays in the map so its exit
// can still be reported, and the caps counted it. A machine that had run four
// agents refused the next one for ever, saying four were running when none
// were, and only a restart of the daemon cleared it.
func TestAFinishedSessionGivesItsSlotBack(t *testing.T) {
	t.Run("per profile", func(t *testing.T) {
		st := &recordingStarter{}
		s := New(st.start, 2, 0)
		for i := uint64(1); i <= 2; i++ {
			if _, err := s.Start(t.Context(), "work", claudeRequest(t, i)); err != nil {
				t.Fatalf("Start %d: %v", i, err)
			}
		}
		if _, err := s.Start(t.Context(), "work", claudeRequest(t, 3)); !errors.Is(err, ErrAtCapacity) {
			t.Fatalf("error = %v, want ErrAtCapacity", err)
		}

		endAndWait(t, s, st.nth(0), 1)

		if _, err := s.Start(t.Context(), "work", claudeRequest(t, 3)); err != nil {
			t.Errorf("a finished session still held its slot: %v", err)
		}
	})

	t.Run("whole machine", func(t *testing.T) {
		st := &recordingStarter{}
		s := New(st.start, 0, 2)
		if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Start(t.Context(), "other", claudeRequest(t, 2)); err != nil {
			t.Fatal(err)
		}
		if _, err := s.Start(t.Context(), "third", claudeRequest(t, 3)); !errors.Is(err, ErrAtCapacity) {
			t.Fatalf("error = %v, want ErrAtCapacity", err)
		}

		// The slot is freed for any profile, not only the one that used it.
		endAndWait(t, s, st.nth(0), 1)

		if _, err := s.Start(t.Context(), "third", claudeRequest(t, 3)); err != nil {
			t.Errorf("a finished session still held its slot: %v", err)
		}
	})
}

// A refusal has to say how many are really running, or the log sends whoever
// reads it looking for agents that are not there.
func TestCapacityRefusalCountsOnlyLiveSessions(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 2, 0)
	for i := uint64(1); i <= 2; i++ {
		if _, err := s.Start(t.Context(), "work", claudeRequest(t, i)); err != nil {
			t.Fatalf("Start %d: %v", i, err)
		}
	}
	endAndWait(t, s, st.nth(0), 1)
	// Fill the freed slot so the cap is reached again, this time with a
	// finished session also in the map.
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 3)); err != nil {
		t.Fatal(err)
	}

	_, err := s.Start(t.Context(), "work", claudeRequest(t, 4))
	if !errors.Is(err, ErrAtCapacity) {
		t.Fatalf("error = %v, want ErrAtCapacity", err)
	}
	if want := `2 already running for profile "work", and the limit is 2`; !strings.Contains(err.Error(), want) {
		t.Errorf("error = %q, want it to say %q", err, want)
	}
}

// Nothing in the daemon calls Forget, so without a window the map is where
// every session a machine has ever run accumulates for the life of the
// process.
func TestFinishedSessionsAreDroppedAfterTheirWindow(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.finishedRetention = 0

	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	endAndWait(t, s, st.nth(0), 1)

	// The next start is what prunes.
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 2)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(1); !errors.Is(err, ErrNoSuchSession) {
		t.Errorf("a finished session outlived its window: %v", err)
	}
	// The running one is untouched, whatever the window says.
	if _, err := s.Get(2); err != nil {
		t.Errorf("a running session was pruned: %v", err)
	}
}

// Inside the window it must still answer, or "what happened to the agent I was
// just watching" becomes "no such session".
func TestAJustFinishedSessionCanStillBeAskedAbout(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 1)); err != nil {
		t.Fatal(err)
	}
	endAndWait(t, s, st.nth(0), 1)

	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 2)); err != nil {
		t.Fatal(err)
	}
	sess, err := s.Get(1)
	if err != nil {
		t.Fatalf("a session that finished a moment ago was dropped: %v", err)
	}
	if state, _, _ := sess.State(); state != StateExited {
		t.Errorf("state = %q, want exited", state)
	}
}

// An agent that has to ask a human before every MCP call stalls on a machine
// launched from the panel, because there is nobody at that terminal.
func TestClaudeCodeGetsItsPermissionsBeforeTheProcessStarts(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	settings := readSettings(t, filepath.Join(req.Dir, ClaudeSettingsDir, ClaudeSettingsName))
	if got := allowList(t, settings); len(got) != 1 || got[0] != "mcp__agentrq-workspace__*" {
		t.Errorf("allow = %v", got)
	}
}

// The gateway asks for permission over ACP, which the workspace answers. It
// never reads that file, so writing one would leave a folder claiming settings
// nothing applies.
func TestTheGatewayGetsNoPermissionsFile(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := Request{
		ID: 1, Kind: KindACPGateway,
		Params: Params{Agent: "antigravity-acp", ServerName: "agentrq-workspace"},
		Dir:    t.TempDir(), MCPURL: testURL,
	}
	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(req.Dir, ClaudeSettingsDir)); !os.IsNotExist(err) {
		t.Error("the gateway was given a .claude directory")
	}
}

// The backend decides which workspace is the supervisor, by sending the
// account-wide server's URL. The daemon writes what it is given and works none
// of that out for itself.
func TestTheCoreServerIsWrittenWhenTheBackendSendsIt(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	req.CoreMCPURL = "https://mcp.agentrq.com/mcp"

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}

	cfg := readConfig(t, filepath.Join(req.Dir, MCPConfigName))
	if got := cfg.Servers[wire.CoreMCPServerName].URL; got != req.CoreMCPURL {
		t.Errorf("core entry = %q, want %q", got, req.CoreMCPURL)
	}
	settings := readSettings(t, filepath.Join(req.Dir, ClaudeSettingsDir, ClaudeSettingsName))
	if got := allowList(t, settings); len(got) != 2 || got[1] != "mcp__agentrq__*" {
		t.Errorf("allow = %v, want the core server pre-approved too", got)
	}
}

// Every other workspace gets one entry, and nothing about the folder tells the
// daemon otherwise.
func TestAnOrdinaryWorkspaceGetsNoCoreServer(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	cfg := readConfig(t, filepath.Join(req.Dir, MCPConfigName))
	if _, ok := cfg.Servers[wire.CoreMCPServerName]; ok {
		t.Errorf("servers = %+v, want only the workspace's own", cfg.Servers)
	}
}

// The exclusion goes in before the file does: a checkout holding a file with a
// live token in it, even for a moment, is a commit somebody can make.
//
// The MCP config only. The permissions file has no credential in it, and
// whether it is checked in belongs to the repository.
func TestALaunchIntoARepositoryExcludesTheCredentialFile(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	req.Dir = repo(t)

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	body := readIgnore(t, req.Dir)
	if !strings.Contains(body, MCPConfigName) {
		t.Errorf("the config is committable:\n%s", body)
	}
	if strings.Contains(body, ClaudeSettingsName) {
		t.Errorf("the permissions file was excluded:\n%s", body)
	}
}

// A restored session reuses the folder it was launched into. Its permissions
// file is already there, and rewriting one would be the daemon touching a
// folder it was not asked to touch.
func TestARestoredSessionWritesNothing(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	if _, _, err := WriteMCPConfig(req.Dir, ws(testURL)); err != nil {
		t.Fatal(err)
	}
	req.MCPURL = ""
	req.ReuseMCPConfig = true

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(req.Dir, ClaudeSettingsDir)); !os.IsNotExist(err) {
		t.Error("a restored session wrote a permissions file")
	}
}

// Everything that can refuse refuses before a process exists, so a rejected
// launch leaves nothing behind — including no session holding a slot.
func TestAFolderThatCannotBeSetUpRefusesTheLaunch(t *testing.T) {
	cases := map[string]func(t *testing.T, dir string){
		"the MCP config cannot be parsed": func(t *testing.T, dir string) {
			if err := os.WriteFile(filepath.Join(dir, MCPConfigName), []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"the permissions file cannot be parsed": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, ClaudeSettingsDir), 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ClaudeSettingsDir, ClaudeSettingsName)
			if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
				t.Fatal(err)
			}
		},
		"the .gitignore cannot be read": func(t *testing.T, dir string) {
			if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, GitIgnoreName)
			if err := os.WriteFile(path, []byte("node_modules\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			unreadable(t, path)
		},
	}

	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			st := &recordingStarter{}
			s := New(st.start, 0, 0)
			req := claudeRequest(t, 1)
			setup(t, req.Dir)

			if _, err := s.Start(t.Context(), "work", req); err == nil {
				t.Fatal("the launch was allowed")
			}
			if _, err := s.Get(1); !errors.Is(err, ErrNoSuchSession) {
				t.Error("a refused launch left a session holding a slot")
			}
		})
	}
}

// A folder that already names the server keeps what it has, and the launch
// says so rather than going quiet about which server the agent will reach.
func TestALaunchSaysWhatItKept(t *testing.T) {
	var logged strings.Builder
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Log = slog.New(slog.NewTextHandler(&logged, nil))

	req := claudeRequest(t, 1)
	theirs := `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://their-own.example/mcp"}}}`
	if err := os.WriteFile(filepath.Join(req.Dir, MCPConfigName), []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if !strings.Contains(logged.String(), "agentrq-workspace") {
		t.Errorf("the kept entry was not reported:\n%s", logged.String())
	}
	// And the token in the URL is never what gets logged.
	if strings.Contains(logged.String(), "token=") {
		t.Errorf("a credential reached the log:\n%s", logged.String())
	}
	cfg := readConfig(t, filepath.Join(req.Dir, MCPConfigName))
	if cfg.Servers["agentrq-workspace"].URL != "https://their-own.example/mcp" {
		t.Error("their entry was replaced")
	}
}

// A supervisor nobody gave a logger to still launches: the daemon's default
// logger is the fallback, not a nil dereference.
func TestALaunchWithNoLoggerConfiguredStillWorks(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	theirs := `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://their-own.example/mcp"}}}`
	if err := os.WriteFile(filepath.Join(req.Dir, MCPConfigName), []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatalf("Start: %v", err)
	}
}

// The folder is where three files get written before the process exists, so
// what it is allowed to be is decided before any of them.
func TestTheWorkspaceFolderIsCheckedBeforeAnythingIsWritten(t *testing.T) {
	cases := map[string]struct {
		dir  func(t *testing.T) string
		want error
	}{
		// A relative path resolves against the daemon's own working
		// directory, so it would put a workspace's config next to the daemon
		// rather than where somebody chose, and never say so.
		"relative": {
			dir:  func(*testing.T) string { return filepath.Join("projects", "app") },
			want: ErrBadParameter,
		},
		"empty": {
			dir:  func(*testing.T) string { return "" },
			want: ErrMissingParam,
		},
		// A missing folder is made; one that cannot be says the folder is
		// not set.
		"missing and cannot be made": {
			dir: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "afile")
				if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return filepath.Join(p, "nope")
			},
			want: ErrNoFolder,
		},
		"a file, not a folder": {
			dir: func(t *testing.T) string {
				p := filepath.Join(t.TempDir(), "afile")
				if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
					t.Fatal(err)
				}
				return p
			},
			want: pty.ErrDirNotDir,
		},
	}

	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			st := &recordingStarter{}
			s := New(st.start, 0, 0)
			req := claudeRequest(t, 1)
			req.Dir = tc.dir(t)

			if _, err := s.Start(t.Context(), "work", req); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
			if len(st.specs) != 0 {
				t.Error("a refused folder still reached the terminal layer")
			}
			if s.Count() != 0 {
				t.Error("a refused folder left a session holding a slot")
			}
		})
	}
	// And nothing was created next to the daemon on the way past.
	if _, err := os.Stat("projects"); err == nil {
		t.Error("the daemon created the relative folder next to itself")
	}
}

// One folder, one spelling, however the path was typed.
func TestTheWorkspaceFolderIsCleaned(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	clean := req.Dir
	req.Dir = filepath.Join(req.Dir, "sub", "..") + string(filepath.Separator)

	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if spec, _ := st.last(); spec.Dir != clean {
		t.Errorf("dir = %q, want the cleaned %q", spec.Dir, clean)
	}
}

// A folder that cannot even be looked at is refused with what the system said,
// rather than being treated as missing: "permission denied" and "not there"
// are different problems with different fixes.
func TestAFolderThatCannotBeStattedIsReported(t *testing.T) {
	parent := t.TempDir()
	dir := filepath.Join(parent, "workspace")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	unwritable(t, parent)
	if err := os.Chmod(parent, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(parent, 0o700) })

	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	req.Dir = dir

	_, err := s.Start(t.Context(), "work", req)
	if err == nil {
		t.Fatal("a folder that cannot be read was allowed")
	}
	if errors.Is(err, pty.ErrDirMissing) {
		t.Errorf("reported as missing rather than unreadable: %v", err)
	}
}
