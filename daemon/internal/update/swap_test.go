// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
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

// helperEnv makes the test binary act as a stand-in for a running daemon.
const helperEnv = "AGENTRQD_UPDATE_TEST_HELPER"

// TestHelperRunningBinary is not a test. It is the child process the swap test
// needs: something genuinely *running* from a file on disk.
//
// A copy of this test binary is started with this function selected, it
// announces itself by creating a file, and then it waits to be killed.
func TestHelperRunningBinary(t *testing.T) {
	marker := os.Getenv(helperEnv)
	if marker == "" {
		t.Skip("not the helper process")
	}
	if err := os.WriteFile(marker, []byte("running"), 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Minute)
}

// THE platform test the milestone asks to prove rather than assume.
//
// Windows will not let a running .exe be deleted or overwritten — but it will
// let it be *renamed*, and the whole swap depends on that being true.
//
// The first version of this test held the file open with os.Open and called
// that "running". It is not: Go's os.Open does not ask for FILE_SHARE_DELETE,
// so the rename failed for a reason that has nothing to do with the file being
// an executable, and the test said the design was broken when it was the model
// that was. A loaded image is a section object, and Windows gives it different
// rules from an ordinary handle — which is exactly the sort of thing that
// cannot be assumed, so this runs a real process from a real file instead.
func TestARunningBinaryCanBeRenamedButNotOverwritten(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Skipf("cannot locate the test binary: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "agentrqd"+exeSuffix())
	copyFile(t, self, path)

	marker := filepath.Join(dir, "running")
	cmd := exec.Command(path, "-test.run=TestHelperRunningBinary", "-test.timeout=5m")
	cmd.Env = append(os.Environ(), helperEnv+"="+marker)
	if err := cmd.Start(); err != nil {
		t.Fatalf("cannot start the stand-in daemon: %v", err)
	}

	// Waited on here rather than after the swap, so "did it die" is a question
	// with an answer instead of a guess. Registered before the temp directory
	// is created would be wrong: cleanups run last-registered-first, and the
	// directory cannot be removed on Windows while the child still holds a
	// file in it.
	exited := make(chan struct{})
	go func() { _, _ = cmd.Process.Wait(); close(exited) }()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		<-exited
	})

	// Wait for it to say it is up, so the image is genuinely mapped rather
	// than the process merely having been asked to start.
	deadline := time.Now().Add(30 * time.Second)
	for {
		if _, err := os.Stat(marker); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the stand-in daemon never started")
		}
		time.Sleep(20 * time.Millisecond)
	}

	if runtime.GOOS == "windows" {
		// The thing that does *not* work, stated as a test so that the day it
		// starts working, somebody finds out here rather than in production.
		if err := os.Remove(path); err == nil {
			t.Error("Windows deleted a running executable; the swap's premise has changed")
		}
	}

	staged := filepath.Join(dir, "staged"+exeSuffix())
	write(t, staged, "v2")
	if err := Swap(path, staged); err != nil {
		t.Fatalf("Swap while the binary is running: %v", err)
	}
	if got := read(t, path); got != "v2" {
		t.Errorf("installed %q", got)
	}

	// And the process it replaced is still running, which is what lets a
	// daemon carry on serving until it restarts.
	select {
	case <-exited:
		t.Error("the running process died when its binary was renamed out from under it")
	case <-time.After(200 * time.Millisecond):
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	b, err := os.ReadFile(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, b, 0o755); err != nil {
		t.Fatal(err)
	}
}
