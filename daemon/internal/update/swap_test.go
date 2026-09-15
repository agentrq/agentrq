// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestSwapInstallsTheNewBinaryAndKeepsTheOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd")
	staged := filepath.Join(dir, "staged")
	write(t, path, "v1")
	write(t, staged, "v2")

	if err := Swap(path, staged); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if got := read(t, path); got != "v2" {
		t.Errorf("installed %q", got)
	}
	// An update that cannot be rolled back is an update that takes a machine
	// offline with no way in.
	if got := read(t, path+OldSuffix); got != "v1" {
		t.Errorf("retained %q", got)
	}
	if _, err := os.Stat(staged); !os.IsNotExist(err) {
		t.Error("the staged file is still there")
	}
}

// The second update must not trip over the first one's leftovers — which on
// Windows is the normal case, because the retained file could not be deleted
// while the process it replaced was still running.
func TestASecondUpdateClearsTheFirstOnesLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd")
	write(t, path, "v1")
	write(t, path+OldSuffix, "v0")

	staged := filepath.Join(dir, "staged")
	write(t, staged, "v2")
	if err := Swap(path, staged); err != nil {
		t.Fatalf("Swap: %v", err)
	}
	if got := read(t, path+OldSuffix); got != "v1" {
		t.Errorf("retained %q, want the version being replaced", got)
	}
}

// A failure here would otherwise leave the machine with no binary at that path
// at all, which is the one outcome worse than not updating.
func TestAFailedInstallPutsTheOldBinaryBack(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd")
	write(t, path, "v1")

	// A staged path that cannot be renamed into place: it does not exist.
	err := Swap(path, filepath.Join(dir, "nothing-here"))
	if err == nil {
		t.Fatal("Swap accepted a staged file that is not there")
	}
	if got := read(t, path); got != "v1" {
		t.Errorf("the binary is now %q; the old one was not put back", got)
	}
}

func TestRollback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd")
	write(t, path, "v2")
	write(t, path+OldSuffix, "v1")

	if err := Rollback(path); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	if got := read(t, path); got != "v1" {
		t.Errorf("after rollback the binary is %q", got)
	}
	// Kept for inspection rather than deleted: something that failed to start
	// is the one thing somebody will want to look at.
	if got := read(t, path+".failed"); got != "v2" {
		t.Errorf("the failed binary was not kept: %q", got)
	}
}

func TestRollbackWithNothingToRollBackTo(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd")
	write(t, path, "v2")

	if err := Rollback(path); err == nil {
		t.Error("Rollback claimed to restore a binary that was never kept")
	}
	if got := read(t, path); got != "v2" {
		t.Errorf("a failed rollback changed the binary to %q", got)
	}
}

// THE platform test the milestone asks to prove rather than assume.
//
// Windows will not let a running .exe be deleted or overwritten — but it will
// let it be *renamed*, and the whole swap depends on that being true. This
// holds the file open the way a running process does and then does exactly
// what Swap does.
func TestARunningBinaryCanBeRenamedButNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd.exe")
	write(t, path, "v1")

	// An open handle stands in for the running process holding its own image.
	held, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = held.Close() }()

	if runtime.GOOS == "windows" {
		// The thing that does *not* work, stated as a test so that the day it
		// starts working, somebody finds out here rather than in production.
		if err := os.Remove(path); err == nil {
			t.Error("Windows deleted a file that is open; the swap's premise has changed")
			write(t, path, "v1")
		}
	}

	staged := filepath.Join(dir, "staged.exe")
	write(t, staged, "v2")
	if err := Swap(path, staged); err != nil {
		t.Fatalf("Swap with the binary held open: %v", err)
	}
	if got := read(t, path); got != "v2" {
		t.Errorf("installed %q", got)
	}
	// And the process still reads its own original image through the handle it
	// already had, which is what lets it carry on until it restarts.
	b := make([]byte, 2)
	if _, err := held.ReadAt(b, 0); err != nil || string(b) != "v1" {
		t.Errorf("the held file reads %q, %v", b, err)
	}
}
