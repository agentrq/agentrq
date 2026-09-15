// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/pty"
)

// fakePTY stands in for a terminal so the supervisor's rules can be tested
// without spawning anything.
type fakePTY struct {
	mu       sync.Mutex
	closed   bool
	exitCode int
	waitErr  error
	done     chan struct{}
}

func newFakePTY() *fakePTY { return &fakePTY{done: make(chan struct{})} }

func (f *fakePTY) Read([]byte) (int, error)    { <-f.done; return 0, errors.New("closed") }
func (f *fakePTY) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakePTY) Resize(uint16, uint16) error { return nil }

func (f *fakePTY) Wait() (int, error) {
	<-f.done
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.exitCode, f.waitErr
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
	ptys  []*fakePTY
	err   error
}

func (r *recordingStarter) start(_ context.Context, spec pty.Spec) (pty.Session, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return nil, r.err
	}
	r.specs = append(r.specs, spec)
	p := newFakePTY()
	r.ptys = append(r.ptys, p)
	return p, nil
}

func (r *recordingStarter) last() (pty.Spec, *fakePTY) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.specs[len(r.specs)-1], r.ptys[len(r.ptys)-1]
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
func TestNoMCPConfigIsWrittenForTheGateway(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := Request{
		ID: 1, Kind: KindACPGateway,
		Params: Params{Model: "gemini-3.8-flash-high", Agent: "antigravity-acp"},
		Dir:    t.TempDir(), MCPURL: testURL,
	}
	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatal(err)
	}
	if _, err := readMCPConfigExists(req.Dir); err == nil {
		t.Error("an MCP config was written for a kind that does not read one")
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
