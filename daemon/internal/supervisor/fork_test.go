// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

const forkID = "0jUM5wEc1Hl"

// gitRepo makes a repository with one commit holding the given files.
func gitRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not installed, so the worktree path cannot be tested here")
	}
	root := t.TempDir()
	for name, body := range files {
		writeFile(t, filepath.Join(root, name), body)
	}
	for _, args := range [][]string{
		{"init", "-q"},
		{"config", "core.autocrlf", "false"},
		{"add", "-A"},
		{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "init"},
	} {
		if out, err := runGit(root, args...); err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
	}
	return root
}

// recordingProgress keeps what a launch showed.
type recordingProgress struct {
	mu      sync.Mutex
	notices []string
	out     bytes.Buffer
}

func (p *recordingProgress) Notice(text string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.notices = append(p.notices, text)
}

func (p *recordingProgress) Write(b []byte) (int, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.Write(b)
}

func (p *recordingProgress) noticed() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return slices.Clone(p.notices)
}

func (p *recordingProgress) printed() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.out.String()
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(b)
}

func TestAForkOfARepositoryIsAWorktreeOnItsOwnBranch(t *testing.T) {
	root := gitRepo(t, map[string]string{"app/main.go": "package main\n", "README": "hi\n"})
	home := t.TempDir()

	var shown recordingProgress
	dir, err := PrepareForkDir(t.Context(), home, filepath.Join(root, "app"), forkID, &shown)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	target := filepath.Join(home, ".agentrq", "forks", forkID)
	if dir != filepath.Join(target, "app") {
		t.Errorf("dir = %q, want the same subfolder of the worktree", dir)
	}
	if got, want := shown.noticed(), []string{
		CommandLine(root, []string{"git", "worktree", "add", "--no-checkout", "-b", ForkBranch(forkID), "--", target, "HEAD"}),
		CommandLine(target, []string{"git", "checkout", "--progress", "-f", ForkBranch(forkID)}),
	}; !slices.Equal(got, want) {
		t.Errorf("notices = %q, want %q", got, want)
	}
	if !strings.Contains(shown.printed(), "Preparing worktree") {
		t.Errorf("what git printed was not shown: %q", shown.printed())
	}
	if out, err := runGit(target, "status", "--porcelain"); err != nil || out != "" {
		t.Errorf("status = %q (%v), want a clean checkout", out, err)
	}
	if got := readFile(t, filepath.Join(dir, "main.go")); got != "package main\n" {
		t.Errorf("main.go = %q", got)
	}
	out, err := runGit(target, "branch", "--show-current")
	if err != nil || strings.TrimSpace(out) != ForkBranch(forkID) {
		t.Errorf("branch = %q (%v), want %s", out, err, ForkBranch(forkID))
	}
}

func TestAnExistingForkFolderIsReused(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	home := t.TempDir()
	first, err := PrepareForkDir(t.Context(), home, root, forkID, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first, "work-in-progress"), "keep me")

	var shown recordingProgress
	again, err := PrepareForkDir(t.Context(), home, root, forkID, &shown)
	if err != nil || again != first {
		t.Fatalf("relaunch = %q err=%v, want the same folder, reused", again, err)
	}
	if got, want := shown.noticed(), []string{"reusing this fork's folder " + quoteArg(first)}; !slices.Equal(got, want) {
		t.Errorf("notices = %q, want %q", got, want)
	}
	if readFile(t, filepath.Join(again, "work-in-progress")) != "keep me" {
		t.Error("a relaunch lost the fork's work")
	}
}

// The folder deleted by hand leaves the branch, and its worktree registered.
func TestAForkWhoseBranchIsLeftOverChecksItOutAgain(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	home := t.TempDir()
	dir, err := PrepareForkDir(t.Context(), home, root, forkID, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	var shown recordingProgress
	again, err := PrepareForkDir(t.Context(), home, root, forkID, &shown)
	target := filepath.Join(home, ".agentrq", "forks", forkID)
	if err != nil {
		t.Fatalf("PrepareForkDir after the folder went: %v", err)
	}
	if got := shown.noticed(); len(got) == 0 || got[0] != CommandLine(root, []string{"git", "worktree", "add", "--no-checkout", "-f", "--", target, ForkBranch(forkID)}) {
		t.Errorf("notices = %q, want the leftover branch checked out with -f", got)
	}
	if readFile(t, filepath.Join(again, "a")) != "1" {
		t.Error("the leftover branch was not checked out")
	}
}

func TestAForkIsRefusedWhenItsNamesAreNotAcceptable(t *testing.T) {
	for name, c := range map[string]struct{ home, from, id string }{
		"id with a slash": {t.TempDir(), t.TempDir(), "../x"},
		"empty id":        {t.TempDir(), t.TempDir(), ""},
		"id too long":     {t.TempDir(), t.TempDir(), strings.Repeat("a", 25)},
		"relative home":   {"home", t.TempDir(), forkID},
		"relative from":   {t.TempDir(), "app", forkID},
		"missing from":    {t.TempDir(), "/no/such/folder", forkID},
		"from with ..":    {t.TempDir(), dotDotDir(t), forkID},
	} {
		if _, err := PrepareForkDir(t.Context(), c.home, c.from, c.id, nil); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// dotDotDir is a real folder whose name holds "..", which a fork refuses.
func dotDotDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "a..b")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func forkRequest(t *testing.T, id uint64, from string) Request {
	t.Helper()
	req := claudeRequest(t, id)
	req.Dir = from
	req.Fork = &wire.ForkSpec{ID: forkID, From: from}
	return req
}

func TestAForkStartsInItsOwnFolder(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	from := t.TempDir()
	writeFile(t, filepath.Join(from, MCPConfigName), `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://parent.example.com/mcp"}}}`)

	sess, err := s.Start(t.Context(), "work", forkRequest(t, 1, from))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := filepath.Join(s.Home, ".agentrq", "forks", forkID)
	if sess.Dir != want {
		t.Errorf("session dir = %q, want %q", sess.Dir, want)
	}
	if spec, _ := st.last(); spec.Dir != want {
		t.Errorf("process dir = %q, want the fork's", spec.Dir)
	}
	if cfg := readFile(t, filepath.Join(want, MCPConfigName)); strings.Contains(cfg, "parent.example.com") {
		t.Errorf("the fork's config points at the parent: %s", cfg)
	}
	if readFile(t, filepath.Join(from, MCPConfigName)) == "" {
		t.Error("the parent's config was touched")
	}
}

// A worktree of a repository that commits its .mcp.json: the kept entry
// would connect the fork as the parent.
func TestAForkWhoseFolderAlreadyConfiguresTheServerIsRefused(t *testing.T) {
	root := gitRepo(t, map[string]string{
		MCPConfigName: `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://parent.example.com/mcp"}}}`,
	})
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()

	_, err := s.Start(t.Context(), "work", forkRequest(t, 1, root))
	if !errors.Is(err, ErrForkConfigCollision) || !strings.Contains(err.Error(), MCPConfigName) {
		t.Fatalf("err = %v, want a collision naming the file", err)
	}
	if len(st.specs) != 0 {
		t.Error("a process was started anyway")
	}
	if s.Count() != 0 {
		t.Error("the refused start kept its slot")
	}
}

func TestAForkThatCannotBeMadeIsRefused(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	req := forkRequest(t, 1, t.TempDir())
	req.Fork.ID = "not/base62"
	if _, err := s.Start(t.Context(), "work", req); !errors.Is(err, ErrBadFork) {
		t.Fatalf("err = %v, want ErrBadFork", err)
	}
}

func TestAForkWithNoHomeIsRefused(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	s := New((&recordingStarter{}).start, 0, 0)
	if _, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir())); err == nil {
		t.Fatal("a fork started with nowhere to put its folder")
	}
}

func TestTheForkFolderIsReportedWithRunning(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	rep := &recordingReporter{}
	from := t.TempDir()

	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindClaudeCode), Dir: from, MCPURL: testURL,
		ServerName: "agentrq-workspace", Workspace: "fork",
		Fork: &wire.ForkSpec{ID: forkID, From: from},
	})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	got := rep.waitFor(t, string(StateRunning))
	if want := filepath.Join(s.Home, ".agentrq", "forks", forkID); got.Dir != want {
		t.Errorf("reported dir = %q, want %q", got.Dir, want)
	}
}

func TestAForkDefaultsToTheUsersHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	s := New((&recordingStarter{}).start, 0, 0)
	sess, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir()))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if want := filepath.Join(home, ".agentrq", "forks", forkID); sess.Dir != want {
		t.Errorf("dir = %q, want %q", sess.Dir, want)
	}
}

// A plain folder is copied, without the parent's .mcp.json or the daemon's own
// folder. Modes and symlinks are checked in fork_unix_test.go.
func TestAForkOfAPlainFolderIsACopy(t *testing.T) {
	from := t.TempDir()
	writeFile(t, filepath.Join(from, "src", "main.go"), "package main\n")
	writeFile(t, filepath.Join(from, MCPConfigName), `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://parent"}}}`)
	writeFile(t, filepath.Join(from, ".agentrq", "forks", "x", "f"), "no")
	home := t.TempDir()

	var shown recordingProgress
	dir, err := PrepareForkDir(t.Context(), home, from, forkID, &shown)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if dir != filepath.Join(home, ".agentrq", "forks", forkID) {
		t.Errorf("dir = %q", dir)
	}
	if got, want := shown.noticed(), []string{"copying " + quoteArg(from) + " to " + quoteArg(dir) + ", which is not in a git repository"}; !slices.Equal(got, want) {
		t.Errorf("notices = %q, want %q", got, want)
	}
	if readFile(t, filepath.Join(dir, "src", "main.go")) != "package main\n" {
		t.Error("the file was not copied")
	}
	for _, left := range []string{MCPConfigName, ".agentrq"} {
		if _, err := os.Stat(filepath.Join(dir, left)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s was copied into the fork", left)
		}
	}
	if matches, _ := filepath.Glob(filepath.Join(home, ".agentrq", "forks", "*.tmp-*")); len(matches) != 0 {
		t.Errorf("the staging folder was left behind: %v", matches)
	}
}

// The forks folder inside the folder being forked — somebody forking their
// home directory — is not copied into itself.
func TestAForkOfTheHomeFolderLeavesTheForksOut(t *testing.T) {
	home := t.TempDir()
	writeFile(t, filepath.Join(home, "notes"), "n")
	writeFile(t, filepath.Join(home, ".agentrq", "forks", "old", "f"), "no")
	dir, err := PrepareForkDir(t.Context(), home, home, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if readFile(t, filepath.Join(dir, "notes")) != "n" {
		t.Error("notes were not copied")
	}
	if _, err := os.Stat(filepath.Join(dir, ".agentrq")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the forks folder was copied into a fork")
	}
}

// The same when the daemon's folder is deeper inside the one being forked.
func TestAForkOfAnAncestorOfHomeLeavesTheForksOut(t *testing.T) {
	from := t.TempDir()
	home := filepath.Join(from, "users", "me")
	writeFile(t, filepath.Join(home, ".agentrq", "forks", "old", "f"), "no")
	writeFile(t, filepath.Join(from, "keep"), "k")
	dir, err := PrepareForkDir(t.Context(), home, from, forkID, nil)
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

// A relaunch finds the entry the first launch wrote. It is the fork's own —
// same endpoint, older token — and is kept like any other launch's.
func TestARelaunchedForkKeepsItsOwnEntry(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	from := t.TempDir()

	first, err := s.Start(t.Context(), "work", forkRequest(t, 1, from))
	if err != nil {
		t.Fatalf("first launch: %v", err)
	}
	st.nth(0).exit(0, nil)
	<-first.Ended()

	req := forkRequest(t, 2, from)
	req.MCPURL = "https://abc123.mcp.agentrq.com?token=a-newer-token"
	if _, err := s.Start(t.Context(), "work", req); err != nil {
		t.Fatalf("relaunch: %v", err)
	}
}

func TestConfigPointsElsewhere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, MCPConfigName)
	writeFile(t, path, `{"mcpServers":{"s":{"type":"http","url":"https://a.example.com/mcp/1?token=old"}}}`)
	for want, elsewhere := range map[string]bool{
		"https://a.example.com/mcp/1?token=new": false,
		"https://a.example.com/mcp/2?token=new": true,
		"https://b.example.com/mcp/1?token=new": true,
		"http://a.example.com/mcp/1":            true,
		"://bad":                                true,
	} {
		if got := configPointsElsewhere(path, "s", want); got != elsewhere {
			t.Errorf("%s: elsewhere = %v, want %v", want, got, elsewhere)
		}
	}
	writeFile(t, path, `{"mcpServers":{"s":{"type":"http","url":"://bad"}}}`)
	if !configPointsElsewhere(path, "s", "https://a.example.com/mcp/1") {
		t.Error("an unreadable URL was taken for this fork's")
	}
	writeFile(t, path, `not json`)
	if !configPointsElsewhere(path, "s", "https://a.example.com/mcp/1") {
		t.Error("an unreadable file was taken for this fork's")
	}
}

func TestRemovingAForkOfARepositoryKeepsItsBranch(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	home := t.TempDir()
	dir, err := PrepareForkDir(t.Context(), home, root, forkID, nil)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "untracked"), "work")

	if err := RemoveForkDir(home, forkID); err != nil {
		t.Fatalf("RemoveForkDir: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agentrq", "forks", forkID)); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
	if out, _ := runGit(root, "worktree", "list"); strings.Contains(out, forkID) {
		t.Errorf("the repository still lists the worktree:\n%s", out)
	}
	if out, _ := runGit(root, "branch", "--list", ForkBranch(forkID)); !strings.Contains(out, ForkBranch(forkID)) {
		t.Error("the fork's branch went with its folder")
	}
}

func TestRemovingACopyDeletesIt(t *testing.T) {
	from := t.TempDir()
	writeFile(t, filepath.Join(from, "a"), "1")
	home := t.TempDir()
	if _, err := PrepareForkDir(t.Context(), home, from, forkID, nil); err != nil {
		t.Fatal(err)
	}
	if err := RemoveForkDir(home, forkID); err != nil {
		t.Fatalf("RemoveForkDir: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agentrq", "forks", forkID)); !os.IsNotExist(err) {
		t.Errorf("the copy is still there: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(from, "a")); err != nil {
		t.Errorf("the original was touched: %v", err)
	}
}

// A home folder under git must not be taken for the fork's repository.
func TestRemovingACopyInsideARepositoryLeavesTheRepositoryAlone(t *testing.T) {
	home := gitRepo(t, map[string]string{"dotfile": "x"})
	from := t.TempDir()
	writeFile(t, filepath.Join(from, "a"), "1")
	if _, err := PrepareForkDir(t.Context(), home, from, forkID, nil); err != nil {
		t.Fatal(err)
	}
	if err := RemoveForkDir(home, forkID); err != nil {
		t.Fatalf("RemoveForkDir: %v", err)
	}
	if readFile(t, filepath.Join(home, "dotfile")) != "x" {
		t.Error("the enclosing repository lost a file")
	}
}

func TestRemovingAForkWhoseFolderIsGoneIsDone(t *testing.T) {
	if err := RemoveForkDir(t.TempDir(), forkID); err != nil {
		t.Errorf("RemoveForkDir = %v, want nil", err)
	}
}

func TestRemovingAWorktreeGitCannotRemoveFallsBackToDeletingIt(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	home := t.TempDir()
	if _, err := PrepareForkDir(t.Context(), home, root, forkID, nil); err != nil {
		t.Fatal(err)
	}
	// A locked worktree is refused by `git worktree remove --force`.
	if out, err := runGit(root, "worktree", "lock", filepath.Join(home, ".agentrq", "forks", forkID)); err != nil {
		t.Fatalf("lock: %v %s", err, out)
	}
	if err := RemoveForkDir(home, forkID); err != nil {
		t.Fatalf("RemoveForkDir: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".agentrq", "forks", forkID)); !os.IsNotExist(err) {
		t.Errorf("the folder is still there: %v", err)
	}
}

func TestRemovingAForkIsRefusedWhenItsNamesAreNotAcceptable(t *testing.T) {
	for name, c := range map[string]struct{ home, id string }{
		"id with a slash": {t.TempDir(), "../x"},
		"empty id":        {t.TempDir(), ""},
		"relative home":   {"home", forkID},
	} {
		if err := RemoveForkDir(c.home, c.id); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// A kill that arrives while the fork's folder is still being made finds the
// session, and the agent is never started: the backend has been told it is
// dead, and may already have merged the fork away.
func TestAForkKilledWhileItsFolderIsMadeNeverStarts(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	made := t.TempDir()
	preparing, release := make(chan struct{}), make(chan struct{})
	s.PrepareDir = func(context.Context, string, string, string, Progress) (string, error) {
		close(preparing)
		<-release
		return made, nil
	}

	errc := make(chan error, 1)
	go func() {
		_, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir()))
		errc <- err
	}()
	<-preparing
	if err := s.Kill(1); err != nil {
		t.Fatalf("Kill while the folder is made: %v", err)
	}
	close(release)
	if err := <-errc; !errors.Is(err, ErrStoppedWhileStarting) {
		t.Fatalf("err = %v, want ErrStoppedWhileStarting", err)
	}
	if len(st.specs) != 0 {
		t.Error("the agent was started after its kill")
	}
	if s.Count() != 0 {
		t.Error("the stopped start kept its slot")
	}
}

// A kill cancels the checkout, and the git it kills fails: what is reported
// is the kill, not that failure.
func TestAForkKilledMidCheckoutIsStoppedNotFailed(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	preparing := make(chan struct{})
	s.PrepareDir = func(ctx context.Context, _, _, _ string, _ Progress) (string, error) {
		close(preparing)
		<-ctx.Done()
		return "", errors.New("supervisor: git checkout: signal: killed")
	}

	errc := make(chan error, 1)
	go func() {
		_, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir()))
		errc <- err
	}()
	<-preparing
	if err := s.Kill(1); err != nil {
		t.Fatalf("Kill mid-checkout: %v", err)
	}
	if err := <-errc; !errors.Is(err, ErrStoppedWhileStarting) {
		t.Fatalf("err = %v, want ErrStoppedWhileStarting", err)
	}
	if len(st.specs) != 0 || s.Count() != 0 {
		t.Errorf("specs %d, count %d: the stopped start left something behind", len(st.specs), s.Count())
	}
}

// A folder that was made but cannot be worked in is refused, and the slot is
// given back.
func TestAForkWhoseFolderIsUnusableIsRefused(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	s.PrepareDir = func(context.Context, string, string, string, Progress) (string, error) {
		return filepath.Join(s.Home, "never-made"), nil
	}
	if _, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir())); err == nil {
		t.Fatal("a fork started in a folder that does not exist")
	}
	if len(st.specs) != 0 || s.Count() != 0 {
		t.Errorf("specs %d, count %d: the refused start left something behind", len(st.specs), s.Count())
	}
}

// progressReporter is a reporter that also shows a launch's progress.
type progressReporter struct {
	recordingReporter
	shown *recordingProgress
}

func (r *progressReporter) Progress() Progress { return r.shown }

// A launch watched from the start sees each notice as it is added, before
// the agent runs, and what making the folder printed.
func TestAForkShowsItsProgressAsItHappens(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	rep := &progressReporter{shown: &recordingProgress{}}

	c := control(t, wire.OpStartSession, wire.StartSession{
		SessionID: 7, Kind: string(KindClaudeCode), Dir: root, MCPURL: testURL,
		ServerName: "agentrq-workspace", Workspace: "fork",
		Fork: &wire.ForkSpec{ID: forkID, From: root},
	})
	if err := s.Handle(t.Context(), "work", c, rep); err != nil {
		t.Fatalf("Handle: %v", err)
	}
	rep.waitFor(t, string(StateRunning))
	sess, err := s.Get(7)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := rep.shown.noticed(), sess.Notices(); !slices.Equal(got, want) || len(got) != 3 {
		t.Errorf("shown %q, want the session's notices %q: two git commands and the agent's", got, want)
	}
	if !strings.Contains(rep.shown.printed(), "Preparing worktree") {
		t.Errorf("what git printed was not shown: %q", rep.shown.printed())
	}
}
