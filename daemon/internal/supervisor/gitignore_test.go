// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// repo makes a folder that looks like a git checkout to everything here.
func repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readIgnore(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, GitIgnoreName))
	if err != nil {
		t.Fatalf("read .gitignore: %v", err)
	}
	return string(b)
}

// The MCP config carries the workspace's token. A checkout is one `git add -A`
// away from that being committed and, once pushed, from being somewhere it
// cannot be taken back.
func TestTheCredentialFileIsExcludedFromARepository(t *testing.T) {
	dir := repo(t)
	changed, err := EnsureGitIgnored(dir, MCPConfigName)
	if err != nil {
		t.Fatalf("EnsureGitIgnored: %v", err)
	}
	if !changed {
		t.Error("nothing was excluded in a fresh repository")
	}
	if body := readIgnore(t, dir); !strings.Contains(body, ".mcp.json") {
		t.Errorf(".gitignore does not exclude the config:\n%s", body)
	}
}

// A folder that is not in a repository has no .gitignore to write, and making
// one would be leaving litter in somebody's directory.
func TestAFolderOutsideARepositoryIsLeftAlone(t *testing.T) {
	dir := t.TempDir()
	changed, err := EnsureGitIgnored(dir, MCPConfigName)
	if err != nil {
		t.Fatalf("EnsureGitIgnored: %v", err)
	}
	if changed {
		t.Error("it reported a change outside a repository")
	}
	if _, err := os.Stat(filepath.Join(dir, GitIgnoreName)); !os.IsNotExist(err) {
		t.Error("a .gitignore was created outside a repository")
	}
}

// A workspace folder is often a directory inside a checkout rather than the
// checkout itself. Its own folder gets the .gitignore, and the top is not
// touched.
func TestASubdirectoryIsExcludedInItsOwnFolder(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, "services", "api")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := EnsureGitIgnored(dir, MCPConfigName); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, GitIgnoreName)); !os.IsNotExist(err) {
		t.Error("a .gitignore was written at the top of the repository")
	}
	if body := readIgnore(t, dir); !strings.Contains(body, "\n.mcp.json\n") {
		t.Errorf("the entry is not relative to the workspace folder:\n%s", body)
	}
}

// A rule in any .gitignore from the top down to the folder counts, each
// spelled relative to where it is — the one an earlier agentrqd wrote at the
// top among them, so upgrading does not add a second.
func TestARuleAboveTheFolderCounts(t *testing.T) {
	for name, c := range map[string]struct{ at, rule string }{
		"at the top, as agentrqd used to write it": {".", "services/api/.mcp.json"},
		"a directory, at the top":                  {".", "services/"},
		"in a folder in between":                   {"services", "api/.mcp.json"},
	} {
		t.Run(name, func(t *testing.T) {
			root := repo(t)
			dir := filepath.Join(root, "services", "api")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			writeFile(t, filepath.Join(root, c.at, GitIgnoreName), c.rule+"\n")
			changed, err := EnsureGitIgnored(dir, MCPConfigName)
			if err != nil || changed {
				t.Errorf("changed=%v err=%v, want %q read as already excluding the file", changed, err, c.rule)
			}
		})
	}
}

// A .gitignore above the folder that cannot be read fails the launch, as the
// folder's own does: it may be the one that excludes the token.
func TestAnUnreadableGitIgnoreAboveTheFolderIsReported(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, "services")
	writeFile(t, filepath.Join(root, GitIgnoreName), "node_modules\n")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	unreadable(t, filepath.Join(root, GitIgnoreName))
	if _, err := EnsureGitIgnored(dir, MCPConfigName); err == nil {
		t.Error("an unreadable .gitignore at the top reported success")
	}
}

// Every launch calls this, so the second one must add nothing.
func TestAnAlreadyExcludedPathIsNotAddedAgain(t *testing.T) {
	dir := repo(t)
	if _, err := EnsureGitIgnored(dir, MCPConfigName); err != nil {
		t.Fatal(err)
	}
	first := readIgnore(t, dir)

	changed, err := EnsureGitIgnored(dir, MCPConfigName)
	if err != nil {
		t.Fatal(err)
	}
	if changed {
		t.Error("it reported a change when the path was already excluded")
	}
	if body := readIgnore(t, dir); body != first {
		t.Errorf(".gitignore changed on the second call:\n%s", body)
	}
}

// Somebody else's exclusion counts. A directory rule covers what is under it,
// which is how most repositories already exclude the settings file.
func TestAnExistingRuleCounts(t *testing.T) {
	for name, rule := range map[string]string{
		"the path itself":      ".mcp.json",
		"an anchored path":     "/.mcp.json",
		"a directory above it": "secrets/",
	} {
		t.Run(name, func(t *testing.T) {
			dir := repo(t)
			if err := os.WriteFile(filepath.Join(dir, GitIgnoreName), []byte(rule+"\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			changed, err := EnsureGitIgnored(dir, coveredBy(rule))
			if err != nil {
				t.Fatal(err)
			}
			if changed {
				t.Errorf("%q was not recognised as already excluding the file", rule)
			}
		})
	}
}

// A .gitignore whose last line has no newline would otherwise have the first
// new entry welded onto the end of it, quietly changing what it excludes.
func TestAFileWithNoTrailingNewlineIsNotCorrupted(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, GitIgnoreName), []byte("node_modules"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureGitIgnored(dir, MCPConfigName); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(readIgnore(t, dir)), "\n")
	if lines[0] != "node_modules" {
		t.Errorf("their last line was changed to %q", lines[0])
	}
	if !strings.Contains(readIgnore(t, dir), "\n.mcp.json\n") {
		t.Errorf("the new entry is not on a line of its own:\n%s", readIgnore(t, dir))
	}
}

// A worktree and a submodule have a .git *file* rather than a directory.
func TestAWorktreeCountsAsARepository(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: /elsewhere/.git/worktrees/w\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureGitIgnored(dir, MCPConfigName)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("a worktree was not recognised as a repository")
	}
}

// It is meant to be read by everybody who checks the repository out, and is
// the only file written here with no secret in it.
func TestGitIgnoreIsNotOwnerOnly(t *testing.T) {
	dir := repo(t)
	if _, err := EnsureGitIgnored(dir, MCPConfigName); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, GitIgnoreName))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o044 == 0 {
		t.Errorf("mode = %o, want a file the repository's other users can read", info.Mode().Perm())
	}
}

// A commented-out or negated line excludes nothing, and reading one as cover
// would leave the token committable.
func TestACommentOrNegationDoesNotCount(t *testing.T) {
	for _, rule := range []string{"# .mcp.json", "!.mcp.json"} {
		dir := repo(t)
		if err := os.WriteFile(filepath.Join(dir, GitIgnoreName), []byte(rule+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		changed, err := EnsureGitIgnored(dir, MCPConfigName)
		if err != nil {
			t.Fatal(err)
		}
		if !changed {
			t.Errorf("%q was read as excluding the file", rule)
		}
	}
}

// A line that is only a slash excludes nothing and must not be read as cover.
func TestASlashOnlyLineIsIgnored(t *testing.T) {
	dir := repo(t)
	if err := os.WriteFile(filepath.Join(dir, GitIgnoreName), []byte("/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	changed, err := EnsureGitIgnored(dir, MCPConfigName)
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("a bare slash was read as excluding the file")
	}
}

// A repository whose .gitignore cannot be read or written fails the launch,
// rather than writing a token into a folder it could not protect.
func TestAnUnusableGitIgnoreIsReported(t *testing.T) {
	t.Run("it cannot be read", func(t *testing.T) {
		dir := repo(t)
		path := filepath.Join(dir, GitIgnoreName)
		if err := os.WriteFile(path, []byte("node_modules\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		unreadable(t, path)
		if _, err := EnsureGitIgnored(dir, MCPConfigName); err == nil {
			t.Error("an unreadable .gitignore reported success")
		}
	})

	t.Run("it cannot be written", func(t *testing.T) {
		dir := repo(t)
		unwritable(t, dir)
		if _, err := EnsureGitIgnored(dir, MCPConfigName); err == nil {
			t.Error("a read-only repository reported success")
		}
	})
}

// unreadable takes every permission off a file, or skips for the same reasons
// unwritable does.
func unreadable(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply")
	}
	if os.Geteuid() == 0 {
		t.Skip("root is not stopped by a permission bit")
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}

// coveredBy is the path a given rule should already exclude.
func coveredBy(rule string) string {
	if strings.HasSuffix(rule, "/") {
		return rule + MCPConfigName
	}
	return MCPConfigName
}

// The permissions file holds no secret — only a list of servers to trust — so
// whether it is checked in is the repository's decision, not this daemon's.
func TestThePermissionsFileIsNotExcluded(t *testing.T) {
	dir := repo(t)
	if _, err := EnsureGitIgnored(dir, MCPConfigName); err != nil {
		t.Fatal(err)
	}
	if body := readIgnore(t, dir); strings.Contains(body, ClaudeSettingsName) {
		t.Errorf("the settings file was excluded:\n%s", body)
	}
}

// A tracked file is kept out of commits in its own checkout; an untracked one,
// or one outside any repository, is left to the .gitignore.
func TestKeepOutOfCommits(t *testing.T) {
	root := gitRepo(t, map[string]string{MCPConfigName: "{}", "a": "1"})
	if hidden, err := KeepOutOfCommits(root, MCPConfigName); err != nil || !hidden {
		t.Fatalf("tracked: hidden=%v err=%v, want hidden", hidden, err)
	}
	writeFile(t, filepath.Join(root, MCPConfigName), `{"mcpServers":{}}`)
	if out, err := runGit(root, "status", "--porcelain"); err != nil || out != "" {
		t.Errorf("status = %q (%v), want the change left out", out, err)
	}
	if hidden, err := KeepOutOfCommits(root, "not-tracked"); err != nil || hidden {
		t.Errorf("untracked: hidden=%v err=%v, want nothing done", hidden, err)
	}
	if hidden, err := KeepOutOfCommits(t.TempDir(), MCPConfigName); err != nil || hidden {
		t.Errorf("no repository: hidden=%v err=%v, want nothing done", hidden, err)
	}
}

// Git refusing is a refused launch, never a token left committable.
func TestKeepOutOfCommitsSaysWhyGitRefused(t *testing.T) {
	root := gitRepo(t, map[string]string{MCPConfigName: "{}"})
	// Somebody else's git is mid-write, so the index cannot be updated.
	writeFile(t, filepath.Join(root, ".git", "index.lock"), "")
	_, err := KeepOutOfCommits(root, MCPConfigName)
	if err == nil || !strings.Contains(err.Error(), "index.lock") {
		t.Fatalf("err = %v, want git's own reason", err)
	}
}

// Of two paths, one already excluded above the folder: only the other is
// added, and the folder's own .gitignore keeps what it had.
func TestOnlyTheUncoveredPathIsAdded(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, "app")
	writeFile(t, filepath.Join(dir, GitIgnoreName), "node_modules\n")
	writeFile(t, filepath.Join(root, GitIgnoreName), "app/b\n")

	if _, err := EnsureGitIgnored(dir, "a", "b"); err != nil {
		t.Fatal(err)
	}
	body := readIgnore(t, dir)
	if !strings.HasPrefix(body, "node_modules\n") || !strings.Contains(body, "\na\n") || strings.Contains(body, "\nb\n") {
		t.Errorf("the folder's .gitignore is\n%s\nwant its own line kept, a added, b left to the top", body)
	}
}

// A folder whose path holds ".." is refused rather than walked: it can only be
// part of a name in a clean path, and refusing it is what keeps the walk from
// root to the folder inside the repository.
func TestAFolderWithDotDotInItsPathIsRefused(t *testing.T) {
	root := repo(t)
	dir := filepath.Join(root, "my..project")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureGitIgnored(dir, MCPConfigName); !errors.Is(err, ErrBadParameter) {
		t.Fatalf("err = %v, want ErrBadParameter", err)
	}
	if _, err := os.Stat(filepath.Join(dir, GitIgnoreName)); !os.IsNotExist(err) {
		t.Error("a .gitignore was written anyway")
	}
}
