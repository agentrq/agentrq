// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/internal/restore"
	"github.com/agentrq/agentrq/daemon/internal/supervisor"
	"github.com/agentrq/agentrq/daemon/wire"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestRestoredReadsTheNoteOnceAndRemovesIt(t *testing.T) {
	dir := t.TempDir()
	if err := restore.Write(dir, restore.File{
		Reason:   "update",
		Sessions: []restore.Session{{ID: 9, Profile: "work", Kind: "acp-gateway"}},
	}); err != nil {
		t.Fatal(err)
	}

	got := Restored(context.Background(), dir, nil, quietLog())
	if len(got) != 1 || got[0].ID != 9 {
		t.Fatalf("restored %+v", got)
	}
	// A note left behind starts somebody's agents again on every subsequent
	// start, forever.
	if again := Restored(context.Background(), dir, nil, quietLog()); len(again) != 0 {
		t.Errorf("a second start found %d sessions", len(again))
	}
}

func TestRestoredWithNoNote(t *testing.T) {
	if got := Restored(context.Background(), t.TempDir(), nil, quietLog()); got != nil {
		t.Errorf("found %+v in nothing", got)
	}
}

// An update takes seconds. Starting somebody's agents from an hour-old note is
// a surprise rather than a restoration.
func TestAStaleNoteIsNotActedOn(t *testing.T) {
	dir := t.TempDir()
	if err := restore.Write(dir, restore.File{
		WroteAt:  time.Now().Add(-2 * restore.MaxAge),
		Sessions: []restore.Session{{ID: 9, Profile: "work", Kind: "acp-gateway"}},
	}); err != nil {
		t.Fatal(err)
	}
	if got := Restored(context.Background(), dir, nil, quietLog()); len(got) != 0 {
		t.Errorf("a stale note restored %+v", got)
	}
}

func TestAnUnreadableNoteIsNotActedOn(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(restore.Path(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := Restored(context.Background(), dir, nil, quietLog()); len(got) != 0 {
		t.Errorf("nonsense restored %+v", got)
	}
}

// What comes back is intent, not state — and it is marked restored so nobody
// is left wondering why their terminal is empty.
func TestARestoredSessionSaysItWasRestored(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindACPGateway),
		Dir: t.TempDir(), Model: "m", Agent: "a", Cols: 120, Rows: 40,
	})

	var st wire.SessionState
	if err := json.Unmarshal(takeControl(t, conn, wire.OpSessionState).Body, &st); err != nil {
		t.Fatal(err)
	}
	if st.SessionID != 9 || st.State != string(supervisor.StateRunning) {
		t.Errorf("reported %+v", st)
	}
	if !st.Restored {
		t.Error("a restored session was not reported as restored")
	}
	if _, err := h.sup.Get(9); err != nil {
		t.Errorf("the session did not come back: %v", err)
	}
}

// A session that does not come back is reported failed, once, rather than
// retried until somebody notices. Saying so is the point: a session silently
// missing is worse than one that says why it is not there.
func TestASessionThatCannotComeBackSaysWhy(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: "something-this-daemon-does-not-run",
	})

	var st wire.SessionState
	if err := json.Unmarshal(takeControl(t, conn, wire.OpSessionState).Body, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != string(supervisor.StateFailed) || st.Error == "" {
		t.Errorf("reported %+v, want a failure with a reason", st)
	}
	if !st.Restored {
		t.Error("a failed restoration was not marked as one")
	}
}

// A session whose folder has gone is reported failed rather than started
// somewhere else.
func TestARestoredSessionWithNoFolderFails(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindACPGateway),
		Dir: "/definitely/not/a/directory", Model: "m", Agent: "a",
	})

	var st wire.SessionState
	if err := json.Unmarshal(takeControl(t, conn, wire.OpSessionState).Body, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != string(supervisor.StateFailed) {
		t.Errorf("reported %+v", st)
	}
}

// Each link restores only its own profile's sessions, or a machine enrolled
// with two accounts would start every session twice.
func TestAnotherProfilesSessionIsLeftAlone(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "somebody-else", Kind: string(supervisor.KindACPGateway),
		Dir: t.TempDir(), Model: "m", Agent: "a",
	})

	if _, err := h.sup.Get(9); err == nil {
		t.Error("another profile's session was started")
	}
	if len(conn.out) != 0 {
		t.Error("another profile's session was reported on")
	}
}

// The note carries no credential, so a restored claude-code session reads the
// config written into its folder when it was first launched. This is what a
// live run found: without it, every restored agent failed with "no MCP URL".
func TestARestoredAgentReadsTheConfigAlreadyInItsFolder(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	dir := t.TempDir()
	if _, err := supervisor.WriteMCPConfig(dir, "agentrq-workspace", "https://agentrq.example/mcp/ws?token=abc"); err != nil {
		t.Fatal(err)
	}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindClaudeCode),
		Dir: dir, Workspace: "Ops", ServerName: "agentrq-workspace",
	})

	var st wire.SessionState
	if err := json.Unmarshal(takeControl(t, conn, wire.OpSessionState).Body, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != string(supervisor.StateRunning) {
		t.Fatalf("reported %+v", st)
	}
	if !st.Restored {
		t.Error("a restored session was not reported as restored")
	}
}

// And a folder whose config has gone fails with a reason, rather than starting
// an agent that cannot reach its workspace and merely looks broken.
func TestARestoredAgentWithNoConfigLeftSaysSo(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindClaudeCode),
		Dir: t.TempDir(), Workspace: "Ops", ServerName: "agentrq-workspace",
	})

	var st wire.SessionState
	if err := json.Unmarshal(takeControl(t, conn, wire.OpSessionState).Body, &st); err != nil {
		t.Fatal(err)
	}
	if st.State != string(supervisor.StateFailed) || !strings.Contains(st.Error, ".mcp.json") {
		t.Errorf("reported %+v, want a failure naming the missing config", st)
	}
}

// Restoring writes no new secret: it reuses exactly the file that was already
// in the folder.
func TestRestoringWritesNoNewCredential(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)
	conn := &Conn{out: make(chan []byte, 8), done: make(chan struct{})}

	dir := t.TempDir()
	path, err := supervisor.WriteMCPConfig(dir, "agentrq-workspace", "https://agentrq.example/mcp/ws?token=original")
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	h.link.Restore(context.Background(), conn, restore.Session{
		ID: 9, Profile: "work", Kind: string(supervisor.KindClaudeCode),
		Dir: dir, Workspace: "Ops", ServerName: "agentrq-workspace",
	})

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Errorf("restoring rewrote the config:\nbefore %s\nafter  %s", before, after)
	}
}

// Somebody at this keyboard must be able to find out that a terminal here is
// being watched, without having to ask the account that is watching it. The
// backend's audit log is no help to them.
func TestAnAttachIsLoggedOnTheMachine(t *testing.T) {
	b := newBackend(t)

	var logged strings.Builder
	h := start(t, b)
	h.link.Log = slog.New(slog.NewTextHandler(&logged, nil))

	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a",
	}))
	waitFor(t, func() bool { _, err := h.sup.Get(7); return err == nil }, "the session never started")

	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 7}))
	waitFor(t, func() bool { return strings.Contains(logged.String(), "is watching a terminal") },
		"an attach was not logged on the machine")
	if h.link.Viewers(7) != 1 {
		t.Errorf("viewers = %d", h.link.Viewers(7))
	}

	b.send(t, controlFrame(t, wire.OpDetach, wire.KillSession{SessionID: 7}))
	waitFor(t, func() bool { return strings.Contains(logged.String(), "stopped watching") },
		"a detach was not logged on the machine")
	if h.link.Viewers(7) != 0 {
		t.Errorf("viewers after detach = %d", h.link.Viewers(7))
	}

	// The keystrokes are not logged: those carry secrets, and the fact of the
	// attach is what belongs in the record.
	f, err := wire.SessionFrame(wire.TypeInput, 7, []byte("hunter2-my-actual-password"))
	if err != nil {
		t.Fatal(err)
	}
	b.send(t, f)
	waitFor(t, func() bool { return len(h.tty.written()) > 0 }, "the keystrokes never arrived")
	if strings.Contains(logged.String(), "hunter2") {
		t.Errorf("a keystroke reached the machine's log:\n%s", logged.String())
	}
}

// Two viewers, then one leaving, is one viewer — not none.
func TestTheViewerCountTracksBothOfThem(t *testing.T) {
	b := newBackend(t)
	h := start(t, b)

	b.send(t, controlFrame(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: "acp-gateway", Dir: t.TempDir(), Model: "m", Agent: "a",
	}))
	waitFor(t, func() bool { _, err := h.sup.Get(7); return err == nil }, "the session never started")

	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 7}))
	b.send(t, controlFrame(t, wire.OpAttach, wire.KillSession{SessionID: 7}))
	waitFor(t, func() bool { return h.link.Viewers(7) == 2 }, "the second viewer was not counted")

	b.send(t, controlFrame(t, wire.OpDetach, wire.KillSession{SessionID: 7}))
	waitFor(t, func() bool { return h.link.Viewers(7) == 1 }, "one leaving took both counts with it")
}
