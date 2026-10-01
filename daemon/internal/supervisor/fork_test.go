// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	dir, step, err := PrepareForkDir(home, filepath.Join(root, "app"), forkID)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	target := filepath.Join(home, ".agentrq", "forks", forkID)
	if dir != filepath.Join(target, "app") {
		t.Errorf("dir = %q, want the same subfolder of the worktree", dir)
	}
	if want := CommandLine(root, []string{"git", "worktree", "add", "-b", ForkBranch(forkID), "--", target, "HEAD"}); step != want {
		t.Errorf("step = %q, want %q", step, want)
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
	first, _, err := PrepareForkDir(home, root, forkID)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first, "work-in-progress"), "keep me")

	again, step, err := PrepareForkDir(home, root, forkID)
	if err != nil || step != "" || again != first {
		t.Fatalf("relaunch = %q step=%q err=%v, want the same folder, reused", again, step, err)
	}
	if readFile(t, filepath.Join(again, "work-in-progress")) != "keep me" {
		t.Error("a relaunch lost the fork's work")
	}
}

// The folder deleted by hand leaves the branch, and its worktree registered.
func TestAForkWhoseBranchIsLeftOverChecksItOutAgain(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	home := t.TempDir()
	dir, _, err := PrepareForkDir(home, root, forkID)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	again, step, err := PrepareForkDir(home, root, forkID)
	target := filepath.Join(home, ".agentrq", "forks", forkID)
	if err != nil || step != CommandLine(root, []string{"git", "worktree", "add", "-f", "--", target, ForkBranch(forkID)}) {
		t.Fatalf("PrepareForkDir after the folder went: step=%q err=%v", step, err)
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
		if _, _, err := PrepareForkDir(c.home, c.from, c.id); err == nil {
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

// A worktree of a repository that commits its .mcp.json: the entry it carries
// would connect the fork as the parent, so it is replaced in the fork's folder,
// and git is told to leave the token out of the fork's commits.
func TestAForkReplacesAnEntryThatPointsElsewhere(t *testing.T) {
	committed := `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://parent.example.com/mcp"},` +
		`"tools":{"command":"run-tools","args":["--fast"]}},"inputs":[1]}`
	root := gitRepo(t, map[string]string{MCPConfigName: committed})
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()

	sess, err := s.Start(t.Context(), "work", forkRequest(t, 1, root))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	cfg := readFile(t, filepath.Join(sess.Dir, MCPConfigName))
	if strings.Contains(cfg, "parent.example.com") || !strings.Contains(cfg, testURL) {
		t.Errorf("the fork's config still points at the parent: %s", cfg)
	}
	for _, kept := range []string{`"command": "run-tools"`, `"--fast"`, `"inputs"`} {
		if !strings.Contains(cfg, kept) {
			t.Errorf("the rest of the file was not kept: %s missing from %s", kept, cfg)
		}
	}
	if n := noticeWith(sess, "replaced in this fork's folder"); !strings.Contains(n, "out of this fork's commits") {
		t.Errorf("notices = %q, want the replacement and the git exclusion said", sess.Notices())
	}
	if out, err := runGit(sess.Dir, "diff", "--name-only"); err != nil || strings.Contains(out, MCPConfigName) {
		t.Errorf("git diff = %q (%v): the token is one commit away", out, err)
	}
	if readFile(t, filepath.Join(root, MCPConfigName)) != committed {
		t.Error("the parent's own .mcp.json was touched")
	}
	if out, _ := runGit(root, "ls-files", "-v", "--", MCPConfigName); !strings.HasPrefix(out, "H ") {
		t.Errorf("ls-files -v in the parent = %q: its index was changed", out)
	}
}

// Outside a repository there is nothing for git to leave out, and the notice
// does not claim it did.
func TestAForkOfAPlainFolderReplacesAnEntryWithoutGit(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	target := filepath.Join(s.Home, ".agentrq", "forks", forkID)
	// Made by hand, as a relaunch would find it.
	writeFile(t, filepath.Join(target, MCPConfigName), `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://elsewhere.example.com/mcp"}}}`)

	sess, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir()))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if n := noticeWith(sess, "replaced"); n == "" || strings.Contains(n, "commits") {
		t.Errorf("notices = %q, want the replacement said without a git claim", sess.Notices())
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

	dir, step, err := PrepareForkDir(home, from, forkID)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if dir != filepath.Join(home, ".agentrq", "forks", forkID) {
		t.Errorf("dir = %q", dir)
	}
	if want := "copied " + quoteArg(from) + " to " + quoteArg(dir) + ", which is not in a git repository"; step != want {
		t.Errorf("step = %q, want %q", step, want)
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
	dir, _, err := PrepareForkDir(home, home, forkID)
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
	dir, _, err := PrepareForkDir(home, from, forkID)
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

// noticeWith is the session's notice that says text, or "".
func noticeWith(sess *Session, text string) string {
	for _, n := range sess.Notices() {
		if strings.Contains(n, text) {
			return n
		}
	}
	return ""
}

func TestEndpointDiffers(t *testing.T) {
	const got = "https://a.example.com/mcp/1?token=old"
	for want, differs := range map[string]bool{
		"https://a.example.com/mcp/1?token=new": false,
		"https://a.example.com/mcp/2?token=new": true,
		"https://b.example.com/mcp/1?token=new": true,
		"http://a.example.com/mcp/1":            true,
		"://bad":                                true,
	} {
		if endpointDiffers(got, want) != differs {
			t.Errorf("%s: differs = %v, want %v", want, !differs, differs)
		}
	}
	if !endpointDiffers("://bad", "https://a.example.com/mcp/1") {
		t.Error("an unreadable URL was taken for the same endpoint")
	}
}

func TestRemovingAForkOfARepositoryKeepsItsBranch(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	home := t.TempDir()
	dir, _, err := PrepareForkDir(home, root, forkID)
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
	if _, _, err := PrepareForkDir(home, from, forkID); err != nil {
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
	if _, _, err := PrepareForkDir(home, from, forkID); err != nil {
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
	if _, _, err := PrepareForkDir(home, root, forkID); err != nil {
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
	s.PrepareDir = func(string, string, string) (string, string, error) {
		close(preparing)
		<-release
		return made, "made", nil
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

// A folder that was made but cannot be worked in is refused, and the slot is
// given back.
func TestAForkWhoseFolderIsUnusableIsRefused(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	s.PrepareDir = func(string, string, string) (string, string, error) {
		return filepath.Join(s.Home, "never-made"), "made", nil
	}
	if _, err := s.Start(t.Context(), "work", forkRequest(t, 1, t.TempDir())); err == nil {
		t.Fatal("a fork started in a folder that does not exist")
	}
	if len(st.specs) != 0 || s.Count() != 0 {
		t.Errorf("specs %d, count %d: the refused start left something behind", len(st.specs), s.Count())
	}
}
