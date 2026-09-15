// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package localstatus

import (
	"errors"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func report() File {
	return File{
		PID: 4242, Version: "0.7.1", StartedAt: time.Now(),
		Sessions: []Session{
			{ID: 9, Profile: "work", Kind: "claude-code", Dir: "/srv/app", Workspace: "Ops", Viewers: 1},
			{ID: 3, Profile: "work", Kind: "acp-gateway", Dir: "/srv/other"},
		},
	}
}

func TestWriteThenRead(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, report()); err != nil {
		t.Fatalf("Write: %v", err)
	}

	f, err := Read(dir, time.Now())
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	if f.PID != 4242 || f.Version != "0.7.1" {
		t.Errorf("read %+v", f)
	}
	// Sorted, so two readings of an unchanged machine look unchanged.
	if len(f.Sessions) != 2 || f.Sessions[0].ID != 3 {
		t.Errorf("sessions = %+v", f.Sessions)
	}
	if f.Sessions[1].Viewers != 1 {
		t.Errorf("the viewer count was lost: %+v", f.Sessions[1])
	}
	if f.UpdatedAt.IsZero() {
		t.Error("a report with no time on it cannot be judged stale")
	}
}

func TestNothingRunning(t *testing.T) {
	if _, err := Read(t.TempDir(), time.Now()); !errors.Is(err, ErrNoDaemon) {
		t.Errorf("error = %v, want ErrNoDaemon", err)
	}
}

// What a killed daemon was last doing is exactly what somebody investigating
// wants to see. Hiding it would answer "nothing is running" to the question
// "what was running".
func TestAStaleReportIsReturnedWithItsWarning(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, report()); err != nil {
		t.Fatal(err)
	}

	f, err := Read(dir, time.Now().Add(2*Stale))
	if !errors.Is(err, ErrStale) {
		t.Fatalf("error = %v, want ErrStale", err)
	}
	if len(f.Sessions) != 2 {
		t.Errorf("a stale report lost what it was reporting: %+v", f)
	}
}

func TestAnUnreadableReport(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(Path(dir), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Read(dir, time.Now()); err == nil {
		t.Error("nonsense was read as a report")
	}
}

// The list of workspaces and folders on a machine is not something every user
// on it needs.
func TestTheReportIsPrivateAndAtomic(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, report()); err != nil {
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
		t.Errorf("Write left %d files behind", len(entries))
	}
}

// Nothing that could identify a workspace's credentials or its terminal
// contents belongs in a file other processes can stat.
func TestTheReportCarriesNoSecrets(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, report()); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(Path(dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"token", "mcp", "authorization", "url"} {
		if strings.Contains(strings.ToLower(string(b)), forbidden) {
			t.Errorf("the report mentions %q:\n%s", forbidden, b)
		}
	}
}

func TestClear(t *testing.T) {
	dir := t.TempDir()
	if err := Write(dir, report()); err != nil {
		t.Fatal(err)
	}
	Clear(dir)
	if _, err := Read(dir, time.Now()); !errors.Is(err, ErrNoDaemon) {
		t.Errorf("after Clear, Read = %v", err)
	}
	Clear(dir)
}

func TestWriteReportsADirectoryItCannotUse(t *testing.T) {
	dir := t.TempDir()
	blocker := dir + "/blocker"
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Write(blocker+"/under", report()); err == nil {
		t.Error("Write claimed to write into a file")
	}
}
