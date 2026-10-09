// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package link

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/pty"
	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/internal/update"
	"github.com/agentrq/agentrq/daemon/wire"
)

// restartRig gives a link a restarter whose restart records rather than
// happens, and a supervisor that opens a fresh terminal per session, since a
// session started again must not inherit the one its predecessor closed.
type restartRig struct {
	state    string
	mu       sync.Mutex
	restarts int
	err      error
	// atRestart runs where the process would have been replaced.
	atRestart func()
}

func (r *restartRig) option(l *Link) {
	l.Supervisor = supervisor.New(func(_ context.Context, spec pty.Spec) (pty.Session, error) {
		if _, err := os.Stat(spec.Dir); err != nil {
			return nil, err
		}
		return newTTY(), nil
	}, 0, 0)
	l.Restarter = &Restarter{
		BinaryPath: "/usr/local/bin/agentrqd",
		Version:    "test",
		StateDir:   r.state,
		Supervisor: l.Supervisor,
		Log:        quietLog(),
		Mode:       update.ModeReexec,
		Restart: func(update.Mode, string, []string) error {
			// Counted after the hook, so a test that waits on the count
			// sees whatever the hook recorded.
			if r.atRestart != nil {
				r.atRestart()
			}
			r.mu.Lock()
			r.restarts++
			r.mu.Unlock()
			return r.err
		},
		Grace: time.Second,
	}
}

func (r *restartRig) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.restarts
}

func launch(t *testing.T, b *fakeBackend, h *harness, id uint64) {
	t.Helper()
	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: id, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a",
		MCPURL: "https://agentrq.example/mcp/ws?token=test", ServerName: "agentrq-workspace",
	}))
	waitFor(t, func() bool { return slices.Contains(h.link.Supervisor.Running(), id) }, "the session never started")
}

func states(t *testing.T, b *fakeBackend, id uint64) []wire.SessionState {
	t.Helper()
	var out []wire.SessionState
	for _, c := range b.controls(t, wire.OpSessionState) {
		var st wire.SessionState
		if err := json.Unmarshal(c.Body, &st); err != nil {
			t.Fatal(err)
		}
		if st.SessionID == id {
			out = append(out, st)
		}
	}
	return out
}

// The backend sends neither op to a daemon that does not say it can.
func TestTheHelloSaysWhatCanBeDoneFromThePanel(t *testing.T) {
	b := newBackend(t)
	rig := &restartRig{state: t.TempDir()}
	f := signedFeed(t, "0.7.0", "0.7.0")
	start(t, b, rig.option, func(l *Link) {
		l.Updater = &Updater{ManifestURL: "https://releases.example/agentrqd.json", Client: f, Log: quietLog(), Restarter: l.Restarter}
	})

	var hello wire.Hello
	if err := json.Unmarshal(b.controls(t, wire.OpHello)[0].Body, &hello); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{wire.CapabilityFork, wire.CapabilityRestart, wire.CapabilityUpdate} {
		if !slices.Contains(hello.Capabilities, want) {
			t.Errorf("capabilities = %v, want %q", hello.Capabilities, want)
		}
	}
}

func TestADaemonThatCannotRestartDoesNotSayItCan(t *testing.T) {
	b := newBackend(t)
	start(t, b)

	var hello wire.Hello
	if err := json.Unmarshal(b.controls(t, wire.OpHello)[0].Body, &hello); err != nil {
		t.Fatal(err)
	}
	if slices.Contains(hello.Capabilities, wire.CapabilityRestart) || slices.Contains(hello.Capabilities, wire.CapabilityUpdate) {
		t.Errorf("capabilities = %v", hello.Capabilities)
	}
}

// A restart writes the note, stops the agents without reporting them ended —
// that report deletes the rows they come back to — and hands over.
func TestARestartHandsOverWithoutEndingTheSessions(t *testing.T) {
	b := newBackend(t)
	rig := &restartRig{state: t.TempDir()}
	var note restore.File
	rig.atRestart = func() {
		raw, err := os.ReadFile(restore.Path(rig.state))
		if err == nil {
			err = json.Unmarshal(raw, &note)
		}
		if err != nil {
			t.Errorf("no note at the handover: %v", err)
		}
	}
	h := start(t, b, rig.option)
	launch(t, b, h, 7)

	b.send(t, controlFrame(t, wire.OpRestart, struct{}{}))
	waitFor(t, func() bool { return rig.count() == 1 }, "the daemon never restarted")

	if note.Reason != "restart" || len(note.Sessions) != 1 || note.Sessions[0].ID != 7 {
		t.Errorf("the note said %+v", note)
	}
	if live := h.link.Supervisor.Live(); len(live) != 0 {
		t.Errorf("%d sessions still running at the handover", len(live))
	}
	if got := h.link.Supervisor.Running(); !slices.Equal(got, []uint64{7}) {
		t.Errorf("Running() = %v, want the session still named", got)
	}
	time.Sleep(50 * time.Millisecond)
	for _, st := range states(t, b, 7) {
		if st.State != string(supervisor.StateRunning) {
			t.Errorf("reported %q for a session that is coming back", st.State)
		}
	}
}

// With no next daemon to start them, this one does — including a session of
// its own profile, and not another's, which it has no connection to report on.
// Claude Code's chosen model and effort are written into the note, so the
// agent comes back on the same ones.
func TestARestartKeepsClaudeCodesModelAndEffort(t *testing.T) {
	b := newBackend(t)
	rig := &restartRig{state: t.TempDir()}
	var note restore.File
	rig.atRestart = func() {
		raw, err := os.ReadFile(restore.Path(rig.state))
		if err == nil {
			err = json.Unmarshal(raw, &note)
		}
		if err != nil {
			t.Errorf("no note at the handover: %v", err)
		}
	}
	h := start(t, b, rig.option)
	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "claude-code", Dir: t.TempDir(), Workspace: "demo", Model: "opus", Effort: "high",
		MCPURL: "https://agentrq.example/mcp/ws?token=test", ServerName: "agentrq-workspace",
	}))
	waitFor(t, func() bool { return slices.Contains(h.link.Supervisor.Running(), uint64(7)) }, "the session never started")

	b.send(t, controlFrame(t, wire.OpRestart, struct{}{}))
	waitFor(t, func() bool { return rig.count() == 1 }, "the daemon never restarted")

	if len(note.Sessions) != 1 || note.Sessions[0].Model != "opus" || note.Sessions[0].Effort != "high" {
		t.Errorf("the note said %+v, want one session on model opus with effort high", note)
	}
}

func TestARestartThatFailsBringsTheAgentsBack(t *testing.T) {
	b := newBackend(t)
	rig := &restartRig{state: t.TempDir(), err: errors.New("exec format error")}
	h := start(t, b, rig.option)
	launch(t, b, h, 7)
	if _, err := h.link.Supervisor.Start(context.Background(), "somebody-else", supervisor.Request{
		ID: 8, Kind: supervisor.KindACPGateway, Dir: t.TempDir(), MCPURL: "https://agentrq.example/mcp/ws?token=other",
		Params: supervisor.Params{Model: "m", Agent: "a", ServerName: "agentrq-workspace"},
	}); err != nil {
		t.Fatal(err)
	}

	b.send(t, controlFrame(t, wire.OpRestart, struct{}{}))

	waitFor(t, func() bool { return len(b.controls(t, wire.OpError)) > 0 }, "the failure was never reported")
	waitFor(t, func() bool {
		for _, st := range states(t, b, 7) {
			if st.Restored && st.State == string(supervisor.StateRunning) {
				return true
			}
		}
		return false
	}, "the session never came back")
	waitFor(t, func() bool { return slices.Equal(h.link.Supervisor.Running(), []uint64{7}) },
		"Running() still names the session that is not coming back")
	if _, err := os.Stat(restore.Path(rig.state)); !os.IsNotExist(err) {
		t.Error("the note was left to restore the sessions a second time")
	}
}

func TestARecoveryWithAnUnreadableNoteStopsNamingTheSessions(t *testing.T) {
	b := newBackend(t)
	rig := &restartRig{state: t.TempDir(), err: errors.New("exec format error")}
	rig.atRestart = func() { _ = os.WriteFile(restore.Path(rig.state), []byte("{not json"), 0o600) }
	h := start(t, b, rig.option)
	launch(t, b, h, 7)

	b.send(t, controlFrame(t, wire.OpRestart, struct{}{}))
	waitFor(t, func() bool { return len(b.controls(t, wire.OpError)) > 0 }, "the failure was never reported")
	waitFor(t, func() bool { return len(h.link.Supervisor.Running()) == 0 }, "a session nothing will start is still named")
}

func TestADaemonThatCannotRestartSaysSo(t *testing.T) {
	b := newBackend(t)
	start(t, b)

	b.send(t, controlFrame(t, wire.OpRestart, struct{}{}))
	waitFor(t, func() bool { return len(b.controls(t, wire.OpError)) > 0 }, "the refusal never came back")
}

// An update refused before anything was stopped leaves the agents alone.
func TestARefusedUpdateLeavesTheSessionsRunning(t *testing.T) {
	b := newBackend(t)
	rig := &restartRig{state: t.TempDir()}
	f := signedFeed(t, "0.7.0", "0.7.0")
	h := start(t, b, rig.option, func(l *Link) {
		l.Restarter.Version = "0.7.0"
		l.Updater = &Updater{ManifestURL: "https://releases.example/agentrqd.json", Client: f, Log: quietLog(), Restarter: l.Restarter}
	})
	launch(t, b, h, 7)

	b.send(t, controlFrame(t, wire.OpUpdateNow, wire.UpdateNow{Version: "0.7.1"}))
	waitFor(t, func() bool { return len(b.controls(t, wire.OpError)) > 0 }, "the refusal never came back")
	if live := h.link.Supervisor.Live(); len(live) != 1 {
		t.Errorf("%d sessions running after a refused update, want 1", len(live))
	}
	if rig.count() != 0 {
		t.Error("a refused update restarted the daemon")
	}
}

func TestARestartWithAnUnwritableNoteStopsNothing(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.startSession(t, 9)
	blocked := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.r.StateDir = filepath.Join(blocked, "state")

	err := h.r.Now(context.Background())
	if err == nil || errors.Is(err, ErrNotRestarted) {
		t.Fatalf("error = %v, want a refusal before anything was stopped", err)
	}
	if state, _, _ := mustLive(t, h, 9).State(); state.Terminal() {
		t.Error("a session was stopped with nothing recording it")
	}
	if len(h.restarts) != 0 {
		t.Error("restarted without a note")
	}
}

// The sessions are gone once the swap is tried, so a swap that fails is one
// this process has to recover from.
func TestAnUpdateWhoseSwapFailsSaysTheAgentsWereStopped(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.startSession(t, 9)
	// A previous binary the swap cannot clear out of the way.
	if err := os.MkdirAll(filepath.Join(h.binary+update.OldSuffix, "busy"), 0o755); err != nil {
		t.Fatal(err)
	}

	if err := h.u.Apply(context.Background(), "0.7.1"); !errors.Is(err, ErrNotRestarted) {
		t.Fatalf("error = %v, want ErrNotRestarted", err)
	}
	if len(h.restarts) != 0 {
		t.Error("restarted into a binary that was never installed")
	}
	if b, _ := os.ReadFile(h.binary); string(b) != "v-old" {
		t.Errorf("the binary is %q", b)
	}
}

// A restored session reports its own end, or its row outlives the agent.
func TestARestoredSessionReportsItsEnd(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}
	dir := t.TempDir()
	if _, _, err := supervisor.WriteMCPConfig(dir, supervisor.MCPEntry{Name: "agentrq-workspace", URL: "https://agentrq.example/mcp/ws?token=test"}); err != nil {
		t.Fatal(err)
	}
	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindACPGateway),
		Dir: dir, ServerName: "agentrq-workspace", Model: "m", Agent: "a",
	})
	takeControl(t, conn, wire.OpSessionState) // running

	_ = h.tty.Close()
	waitFor(t, func() bool { return len(conn.out) > 0 }, "the end was never reported")
	var st wire.SessionState
	if err := json.Unmarshal(takeControl(t, conn, wire.OpSessionState).Body, &st); err != nil {
		t.Fatal(err)
	}
	if st.SessionID != 9 || !supervisor.State(st.State).Terminal() {
		t.Errorf("reported %+v", st)
	}
}

// ...unless it was handed over again, to come back once more.
func TestARestoredSessionHandedOverAgainReportsNothing(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}
	dir := t.TempDir()
	if _, _, err := supervisor.WriteMCPConfig(dir, supervisor.MCPEntry{Name: "agentrq-workspace", URL: "https://agentrq.example/mcp/ws?token=test"}); err != nil {
		t.Fatal(err)
	}
	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindACPGateway),
		Dir: dir, ServerName: "agentrq-workspace", Model: "m", Agent: "a",
	})
	takeControl(t, conn, wire.OpSessionState) // running

	h.sup.HandOver(context.Background(), time.Second)
	time.Sleep(50 * time.Millisecond)
	if len(conn.out) != 0 {
		t.Error("a handed-over session reported its end")
	}
}

// A session that cannot come back stops being named, so the backend can drop
// its row.
func TestASessionThatCannotComeBackIsNoLongerNamed(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}
	h.sup.Expect(9, 10)

	h.link.Restore(context.Background(), conn, restore.Session{ID: 9, Profile: "work", Kind: "nothing-runs-this"})
	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 10, Profile: "work", Kind: string(supervisor.KindACPGateway), Dir: "/definitely/not/a/directory", Model: "m", Agent: "a",
	})

	if got := h.sup.Running(); len(got) != 0 {
		t.Errorf("Running() = %v", got)
	}
}

// A restored claude-code agent picks its conversation back up rather than
// starting with no memory of the task it was part-way through.
func TestARestoredClaudeCodeAgentResumesItsConversation(t *testing.T) {
	claude := t.TempDir()
	project := filepath.Join(claude, "projects", "-work")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	conversation := supervisor.ConversationID(9)
	if err := os.WriteFile(filepath.Join(project, conversation+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_CONFIG_DIR", claude)

	var argv []string
	b := newBackend(t)
	h := start(t, b, func(l *Link) {
		l.Supervisor = supervisor.New(func(_ context.Context, spec pty.Spec) (pty.Session, error) {
			argv = spec.Argv
			return newTTY(), nil
		}, 0, 0)
	})
	dir := t.TempDir()
	if _, _, err := supervisor.WriteMCPConfig(dir, supervisor.MCPEntry{Name: "agentrq-workspace", URL: "https://agentrq.example/mcp/ws?token=abc"}); err != nil {
		t.Fatal(err)
	}
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}
	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindClaudeCode),
		Dir: dir, Workspace: "Ops", ServerName: "agentrq-workspace",
	})

	if !slices.Equal(argv[len(argv)-2:], []string{"--resume", conversation}) {
		t.Errorf("argv = %v, want it to resume %s", argv, conversation)
	}
}

// A restart with no grace configured waits the default, and with nothing
// running waits for nothing.
func TestARestartWithNothingRunningUsesTheDefaultGrace(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.r.Grace = 0
	if err := h.r.Now(context.Background()); err != nil {
		t.Fatalf("Now: %v", err)
	}
	if len(h.restarts) != 1 || h.restarts[0] != h.binary {
		t.Errorf("restarted %v", h.restarts)
	}
}

// Both failures are reported: the new binary would not run, and the old one
// could not be put back.
func TestAFailedRollbackIsReportedToo(t *testing.T) {
	h := newUpdater(t, signedFeed(t, "0.7.1", "0.7.1"), "0.7.0")
	h.r.Restart = func(update.Mode, string, []string) error {
		_ = os.Remove(h.binary + update.OldSuffix)
		return errors.New("exec format error")
	}

	err := h.u.Apply(context.Background(), "0.7.1")
	if !errors.Is(err, ErrNotRestarted) {
		t.Fatalf("error = %v, want ErrNotRestarted", err)
	}
	if !strings.Contains(err.Error(), "exec format error") || !strings.Contains(err.Error(), "nothing to roll back to") {
		t.Errorf("error = %v, want both failures", err)
	}
}
