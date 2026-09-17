// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

type recordingReporter struct {
	mu     sync.Mutex
	states []wire.SessionState
	err    error
}

func (r *recordingReporter) ReportSessionState(s wire.SessionState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.states = append(r.states, s)
	return nil
}

func (r *recordingReporter) all() []wire.SessionState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]wire.SessionState(nil), r.states...)
}

func (r *recordingReporter) waitFor(t *testing.T, want string) wire.SessionState {
	t.Helper()
	var found wire.SessionState
	waitFor(t, func() bool {
		for _, s := range r.all() {
			if s.State == want {
				found = s
				return true
			}
		}
		return false
	}, "never reported state "+want)
	return found
}

func control(t *testing.T, op wire.Op, body any) wire.Control {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	return wire.Control{Op: op, Body: b}
}

func TestHandleStartRunsAndReports(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	rep := &recordingReporter{}

	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindClaudeCode),
		Dir: t.TempDir(), MCPURL: testURL,
		ServerName: "agentrq-workspace", Workspace: "agentrq-code",
	})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got := rep.waitFor(t, string(StateRunning))
	if got.SessionID != 7 {
		t.Errorf("reported session %d", got.SessionID)
	}
}

// A start that is rejected and says nothing leaves a row sitting in "starting"
// forever, blocking the workspace's next launch.
func TestARefusedStartIsReportedRatherThanSilent(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	rep := &recordingReporter{}

	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "bash", Dir: t.TempDir(),
	})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatalf("Handle returned an error for a refusal it reported: %v", err)
	}

	states := rep.all()
	if len(states) != 1 || states[0].State != string(StateFailed) {
		t.Fatalf("states = %+v, want one failure", states)
	}
	// The reason has to reach the person, not just the log.
	if !strings.Contains(states[0].Error, "unknown agent kind") {
		t.Errorf("failure does not say why: %q", states[0].Error)
	}
	if len(st.specs) != 0 {
		t.Error("a refused kind still reached the terminal layer")
	}
}

// "Working directory does not exist: /srv/app" is actionable; "failed to
// start" is not. The server cannot make this check — the agent runs elsewhere.
func TestAMissingWorkingDirectoryIsReportedWithItsPath(t *testing.T) {
	// The real starter, so the directory check is the real one.
	sup := New(realStarter, 0, 0)
	rep := &recordingReporter{}

	missing := t.TempDir() + "/not-checked-out"
	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindClaudeCode), Dir: missing,
		MCPURL: testURL, ServerName: "agentrq-workspace", Workspace: "agentrq-code",
	})
	if err := sup.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	states := rep.all()
	if len(states) != 1 || states[0].State != string(StateFailed) {
		t.Fatalf("states = %+v, want one failure", states)
	}
	if !strings.Contains(states[0].Error, missing) {
		t.Errorf("failure does not name the path: %q", states[0].Error)
	}
}

// Returning an error here would close the socket, turning one bad launch into
// every session on the machine losing its connection.
func TestABadLaunchDoesNotEndTheConnection(t *testing.T) {
	sup := New(realStarter, 0, 0)
	rep := &recordingReporter{}
	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindClaudeCode), Dir: "/definitely/not/here",
		MCPURL: testURL, ServerName: "s", Workspace: "w",
	})
	if err := sup.Handle(t.Context(), "work", c, rep); err != nil {
		t.Errorf("Handle = %v, want nil so the socket stays up", err)
	}
}

func TestHandleKillReportsEvenWhenAlreadyGone(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	rep := &recordingReporter{}

	// A session that never existed: the backend asked for it to be dead, and
	// it is.
	c := control(t, wire.OpKillSession, wire.KillSession{SessionID: 99})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	states := rep.all()
	if len(states) != 1 || states[0].State != string(StateKilled) {
		t.Errorf("states = %+v, want one killed", states)
	}
}

// Dropping the connection over an unrecognised message would make every new
// control op a breaking change for daemons already in the field.
func TestAnUnknownOpIsIgnoredRatherThanFatal(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	rep := &recordingReporter{}
	if err := s.Handle(t.Context(), "work", wire.Control{Op: "somethingNewer"}, rep); err != nil {
		t.Errorf("Handle = %v, want nil", err)
	}
	if len(rep.all()) != 0 {
		t.Error("an unknown op produced a state report")
	}
}

func TestMalformedControlBodiesAreErrors(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	rep := &recordingReporter{}
	for _, op := range []wire.Op{wire.OpStartSession, wire.OpKillSession} {
		c := wire.Control{Op: op, Body: json.RawMessage("{not json")}
		if err := s.Handle(t.Context(), "work", c, rep); err == nil {
			t.Errorf("Handle(%s) accepted a malformed body", op)
		}
	}
}

// A session that dies on its own must not sit in the UI as running forever.
func TestAnExitIsReportedWithoutBeingAsked(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	rep := &recordingReporter{}

	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindACPGateway),
		Dir: t.TempDir(), Model: "m", Agent: "a",
		// The gateway reads .mcp.json like claude-code does, so a start that
		// carries no URL is one the daemon correctly refuses.
		MCPURL: testURL, ServerName: "agentrq-workspace",
	})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatal(err)
	}
	rep.waitFor(t, string(StateRunning))

	_, p := st.last()
	p.exit(5, nil)

	got := rep.waitFor(t, string(StateExited))
	if got.ExitCode == nil || *got.ExitCode != 5 {
		t.Errorf("exit code = %v, want 5", got.ExitCode)
	}
}
