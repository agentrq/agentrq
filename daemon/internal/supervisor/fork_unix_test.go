// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package supervisor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// These need Unix permissions, symlinks a user can make, or a FIFO.

func TestAWorktreeThatGitRefusesSaysWhy(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed")
	}
	if out, err := runGit(root, "init", "-q"); err != nil {
		t.Fatalf("git init: %v: %s", err, out)
	}
	// No commit, so there is no HEAD to check out.
	_, err := PrepareForkDir(t.Context(), t.TempDir(), root, forkID, nil)
	if err == nil || !strings.Contains(err.Error(), "git worktree add") {
		t.Fatalf("err = %v, want git's own reason", err)
	}

	// A leftover branch whose folder cannot be made fails the retry too.
	root = gitRepo(t, map[string]string{"a": "1"})
	if out, err := runGit(root, "branch", ForkBranch(forkID)); err != nil {
		t.Fatal(out)
	}
	home := t.TempDir()
	forks := filepath.Join(home, ".agentrq", "forks")
	if err := os.MkdirAll(forks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(forks, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(forks, 0o700) })
	_, err = PrepareForkDir(t.Context(), home, root, forkID, nil)
	if err == nil || !strings.Contains(err.Error(), "git worktree add") {
		t.Fatalf("err = %v, want git's own reason", err)
	}
}

func TestACopyKeepsModesAndSymlinks(t *testing.T) {
	from := t.TempDir()
	writeFile(t, filepath.Join(from, "src", "main.go"), "package main\n")
	writeFile(t, filepath.Join(from, "bin", "run"), "#!/bin/sh\n")
	if err := os.Chmod(filepath.Join(from, "bin", "run"), 0o775); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("src/main.go", filepath.Join(from, "link")); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Mkfifo(filepath.Join(from, "pipe"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(from, "src"), 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(from, "src"), 0o755) })

	dir, err := PrepareForkDir(t.Context(), t.TempDir(), from, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(dir, "src"), 0o755) })
	if info, err := os.Stat(filepath.Join(dir, "bin", "run")); err != nil || info.Mode().Perm() != 0o775 {
		t.Errorf("file mode = %v (%v), want 0775 whatever the umask", info.Mode(), err)
	}
	if info, err := os.Stat(filepath.Join(dir, "src")); err != nil || info.Mode().Perm() != 0o555 {
		t.Errorf("folder mode = %v (%v), want 0555", info.Mode(), err)
	}
	if link, err := os.Readlink(filepath.Join(dir, "link")); err != nil || link != "src/main.go" {
		t.Errorf("symlink = %q (%v), want it copied as a link", link, err)
	}
	if _, err := os.Lstat(filepath.Join(dir, "pipe")); !errors.Is(err, os.ErrNotExist) {
		t.Error("a FIFO was copied")
	}
}

// A folder named through a symlink is copied, not copied as a link.
func TestAForkFromASymlinkedFolderCopiesItsContent(t *testing.T) {
	real := t.TempDir()
	writeFile(t, filepath.Join(real, "a"), "1")
	from := filepath.Join(t.TempDir(), "via")
	if err := os.Symlink(real, from); err != nil {
		t.Fatal(err)
	}
	dir, err := PrepareForkDir(t.Context(), t.TempDir(), from, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if readFile(t, filepath.Join(dir, "a")) != "1" {
		t.Error("the content behind the symlink was not copied")
	}
}

func TestACopyThatFailsLeavesNothingToReuse(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is not stopped by a permission bit")
	}
	for _, name := range []string{"secret", "private"} {
		t.Run(name, func(t *testing.T) {
			from := t.TempDir()
			writeFile(t, filepath.Join(from, "secret"), "x")
			writeFile(t, filepath.Join(from, "private", "f"), "x")
			if err := os.Chmod(filepath.Join(from, name), 0); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(from, name), 0o700) })
			home := t.TempDir()
			if _, err := PrepareForkDir(t.Context(), home, from, forkID, nil); err == nil {
				t.Fatal("an unreadable " + name + " was copied")
			}
			entries, _ := os.ReadDir(filepath.Join(home, ".agentrq", "forks"))
			if len(entries) != 0 {
				t.Errorf("a failed copy left %d entries behind", len(entries))
			}
		})
	}
}

func TestAForkWithNowhereToGoIsRefused(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root is not stopped by a permission bit")
	}
	home := t.TempDir()
	forks := filepath.Join(home, ".agentrq", "forks")
	if err := os.MkdirAll(forks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(forks, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(forks, 0o700) })
	if _, err := PrepareForkDir(t.Context(), home, t.TempDir(), forkID, nil); err == nil {
		t.Fatal("a copy was made in a folder that cannot be written")
	}

	home = t.TempDir()
	if err := os.Chmod(home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o700) })
	if _, err := PrepareForkDir(t.Context(), home, t.TempDir(), forkID, nil); err == nil {
		t.Fatal("a forks folder was made where nothing can be written")
	}
}

func TestAForkThatExistsButCannotBeReadIsRefused(t *testing.T) {
	home := t.TempDir()
	forks := filepath.Join(home, ".agentrq", "forks")
	if err := os.MkdirAll(forks, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(forks, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(forks, 0o700) })
	if _, err := os.ReadDir(forks); err == nil {
		t.Skip("running as a user who can read anything")
	}
	if _, err := PrepareForkDir(t.Context(), home, t.TempDir(), forkID, nil); err == nil {
		t.Fatal("a fork folder that cannot be looked at was taken as missing")
	}
}

// The daemon's folder is recognised however its path is spelled. On macOS the
// temp dir is under /var, which is really /private/var, and a string compare
// copied the staging folder into itself until the name was too long.
func TestTheForksFolderIsLeftOutWhenNamedThroughASymlink(t *testing.T) {
	real := t.TempDir()
	via := filepath.Join(t.TempDir(), "via")
	if err := os.Symlink(real, via); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(real, "keep"), "k")
	home := filepath.Join(via, "users", "me")
	dir, err := PrepareForkDir(t.Context(), home, via, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if readFile(t, filepath.Join(dir, "keep")) != "k" {
		t.Error("keep was not copied")
	}
	if _, err := os.Stat(filepath.Join(dir, "users", "me", ".agentrq")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the daemon's own folder was copied into a fork")
	}
}

// slowRepo is a repository whose checkout takes a while: its file goes
// through a filter that sleeps, as an LFS download would. The filter outlives
// a killed git and holds its output open, so the wait for it is cut short.
func slowRepo(t *testing.T) string {
	t.Helper()
	root := gitRepo(t, map[string]string{"a": "1", ".gitattributes": "a filter=slow\n"})
	if out, err := runGit(root, "config", "filter.slow.smudge", "sleep 10; cat"); err != nil {
		t.Fatal(out)
	}
	was := gitWaitDelay
	gitWaitDelay = 100 * time.Millisecond
	t.Cleanup(func() { gitWaitDelay = was })
	return root
}

// A checkout that runs out of time leaves no folder and no worktree behind,
// so the next launch makes the fork afresh instead of reusing half of one.
func TestACheckoutThatTakesTooLongLeavesNothingBehind(t *testing.T) {
	root := slowRepo(t)
	home := t.TempDir()
	defer func(was time.Duration) { checkoutTimeout = was }(checkoutTimeout)
	checkoutTimeout = 200 * time.Millisecond

	_, err := PrepareForkDir(t.Context(), home, root, forkID, nil)
	if err == nil || !strings.Contains(err.Error(), "took longer than 200ms") {
		t.Fatalf("err = %v, want the checkout's time limit named", err)
	}
	target := filepath.Join(home, ".agentrq", "forks", forkID)
	if _, err := os.Lstat(target); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the unfinished folder was left behind: %v", err)
	}
	if out, _ := runGit(root, "worktree", "list", "--porcelain"); strings.Contains(out, target) {
		t.Errorf("git still lists the unfinished worktree:\n%s", out)
	}

	if out, err := runGit(root, "config", "--unset", "filter.slow.smudge"); err != nil {
		t.Fatal(out)
	}
	checkoutTimeout = time.Minute
	dir, err := PrepareForkDir(t.Context(), home, root, forkID, nil)
	if err != nil {
		t.Fatalf("the relaunch: %v", err)
	}
	if readFile(t, filepath.Join(dir, "a")) != "1" {
		t.Error("the relaunch did not check the fork out")
	}
}

// Stopping a fork while its folder is made stops git too, rather than leaving
// a checkout running for a session that is gone.
func TestAForkKilledWhileItChecksOutStopsTheCheckout(t *testing.T) {
	root := slowRepo(t)
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	shown := &recordingProgress{}
	req := forkRequest(t, 1, root)
	req.Progress = shown

	errc := make(chan error, 1)
	began := time.Now()
	go func() {
		_, err := s.Start(t.Context(), "work", req)
		errc <- err
	}()
	waitFor(t, func() bool { return len(shown.noticed()) == 2 }, "the checkout never began")
	if err := s.Kill(1); err != nil {
		t.Fatal(err)
	}
	if err := <-errc; err == nil {
		t.Fatal("the fork started after its kill")
	}
	if took := time.Since(began); took > 5*time.Second {
		t.Errorf("the start took %s to give up, so the checkout ran on", took)
	}
	if _, err := os.Lstat(filepath.Join(s.Home, ".agentrq", "forks", forkID)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the stopped checkout's folder was left behind: %v", err)
	}
	if len(st.specs) != 0 || s.Count() != 0 {
		t.Errorf("specs %d, count %d: the stopped start left something behind", len(st.specs), s.Count())
	}
}
