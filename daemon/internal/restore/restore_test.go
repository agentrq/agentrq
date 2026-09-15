// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package restore

import (
	"encoding/json"
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func note() File {
	return File{
		Reason:      "update",
		FromVersion: "0.7.0",
		Sessions: []Session{
			{ID: 9, Profile: "work", Kind: "claude-code", Dir: "/srv/app", Workspace: "Ops", ServerName: "agentrq-workspace", Cols: 100, Rows: 30},
		},
	}
}

func TestWriteThenTake(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, note()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	f, err := Take(dir, time.Now())
	if err != nil {
		t.Fatalf("Take: %v", err)
	}
	if len(f.Sessions) != 1 || f.Sessions[0].ID != 9 || f.Sessions[0].Dir != "/srv/app" {
		t.Errorf("read back %+v", f)
	}
	if f.FromVersion != "0.7.0" {
		t.Errorf("fromVersion = %q", f.FromVersion)
	}
}

// A note left behind starts somebody's agents again on every subsequent start,
// forever. Restoring is best effort and happens once.
func TestTakingANoteRemovesIt(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, note()); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(dir, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Error("the note is still there after being taken")
	}

	second, err := Take(dir, time.Now())
	if err != nil {
		t.Fatalf("a second Take errored: %v", err)
	}
	if len(second.Sessions) != 0 {
		t.Errorf("a second Take found %d sessions", len(second.Sessions))
	}
}

// A missing note is what an ordinary start looks like, and is not an error.
func TestNoNoteIsNotAnError(t *testing.T) {
	f, err := Take(t.TempDir(), time.Now())
	if err != nil {
		t.Fatalf("Take with no note: %v", err)
	}
	if len(f.Sessions) != 0 {
		t.Errorf("found %d sessions in nothing", len(f.Sessions))
	}
}

// An update takes seconds. Starting somebody's agents from a week-old note is
// a surprise, not a restoration — and the note is still removed, so it cannot
// go off later.
func TestAStaleNoteIsRefusedAndRemoved(t *testing.T) {
	dir := t.TempDir()
	old := note()
	old.WroteAt = time.Now().Add(-2 * MaxAge)
	if err := Write(dir, old); err != nil {
		t.Fatal(err)
	}

	_, err := Take(dir, time.Now())
	if !errors.Is(err, ErrStale) {
		t.Fatalf("error = %v, want ErrStale", err)
	}
	if _, statErr := os.Stat(Path(dir)); !os.IsNotExist(statErr) {
		t.Error("a stale note was left on disk to go off later")
	}
}

// The credential in an MCP URL is scoped and short-lived. Writing it to disk
// to survive a restart would turn a deliberate expiry into a file.
func TestTheNoteCarriesNoCredential(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, note()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "mcpUrl", "mcpurl", "Authorization"} {
		if strings.Contains(strings.ToLower(string(b)), strings.ToLower(forbidden)) {
			t.Errorf("the note mentions %q:\n%s", forbidden, b)
		}
	}

	// And the type itself has nowhere to put one, which is the stronger
	// guarantee: a future field cannot be added without this failing.
	var shape map[string]any
	if err := json.Unmarshal(b, &shape); err != nil {
		t.Fatal(err)
	}
	sessions := shape["sessions"].([]any)
	for key := range sessions[0].(map[string]any) {
		switch key {
		case "id", "profile", "kind", "dir", "workspace", "serverName", "model", "agent", "cols", "rows":
		default:
			t.Errorf("a session in the note carries an unexpected field %q", key)
		}
	}
}

// A crash midway must leave no half-written note that the next start misreads.
func TestTheNoteIsWrittenAtomicallyAndPrivately(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, note()); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600", info.Mode().Perm())
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("Write left %d files behind, want just the note", len(entries))
	}
}

func TestANoteFromAnotherVersionIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte(`{"version":99,"sessions":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(dir, time.Now()); err == nil {
		t.Error("a note from a future format was acted on")
	}
}

func TestAnUnreadableNoteIsRefused(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Take(dir, time.Now()); err == nil {
		t.Error("nonsense was acted on")
	}
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Error("an unreadable note was left to be misread again")
	}
}

// An update abandoned before anything was killed must not leave a note that
// restarts sessions nobody stopped.
func TestClear(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, note()); err != nil {
		t.Fatal(err)
	}
	Clear(dir)
	if _, err := os.Stat(Path(dir)); !os.IsNotExist(err) {
		t.Error("Clear left the note behind")
	}
	// And clearing nothing is not a problem.
	Clear(dir)
}

func TestWriteReportsADirectoryItCannotUse(t *testing.T) {
	// A path whose parent is a file, not a directory.
	dir := t.TempDir()
	blocker := dir + "/blocker"
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(blocker+"/under", note()); err == nil {
		t.Error("Write claimed to write into a file")
	}
}
