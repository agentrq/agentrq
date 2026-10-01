// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ErrBadFork is a fork whose folder cannot be prepared from what was sent.
var ErrBadFork = errors.New("supervisor: fork is not acceptable")

// forkIDPattern is a base62 workspace id. It becomes a folder and a branch
// name, so nothing else gets through.
var forkIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{1,24}$`)

// gitTimeout bounds each quick git query. A variable so a test need not wait.
var gitTimeout = 60 * time.Second

// checkoutTimeout bounds making a fork's worktree, which writes out every file
// of the repository: a large one takes far longer than gitTimeout, and being
// cut off half way is what left a fork that could never start.
var checkoutTimeout = 30 * time.Minute

// gitWaitDelay is how long a killed git's output is waited for.
var gitWaitDelay = 5 * time.Second

// Progress is where a launch says what it is doing before its agent has a
// terminal: a fork's folder can take minutes to check out, and the person
// watching would otherwise see nothing until it was done.
type Progress interface {
	// Notice is a line from the daemon itself.
	Notice(text string)
	// Write takes what a command the launch runs prints.
	io.Writer
}

type discardProgress struct{}

func (discardProgress) Notice(string)               {}
func (discardProgress) Write(b []byte) (int, error) { return len(b), nil }

// ForkBranch is the branch a fork's worktree is checked out on.
func ForkBranch(id string) string { return "agentrq/fork-" + id }

// PrepareForkDir makes the folder a workspace fork runs in, under
// <home>/.agentrq/forks/<id>, and returns the directory the agent starts in.
//
// A git worktree on its own branch when from is inside a repository (the dir
// is then the same subfolder of it that from is of its repository), a copy of
// from otherwise. An existing folder is reused as it is, which is a relaunch.
//
// Each step is a notice on out before it runs, and what git prints goes to out
// as it prints it. Cancelling ctx stops the checkout; a worktree that was not
// finished is removed, so a relaunch does not take it for a finished fork.
func PrepareForkDir(ctx context.Context, home, from, id string, out Progress) (dir string, err error) {
	if out == nil {
		out = discardProgress{}
	}
	if !forkIDPattern.MatchString(id) {
		return "", fmt.Errorf("%w: id=%q", ErrBadFork, id)
	}
	if !filepath.IsAbs(home) {
		return "", fmt.Errorf("%w: home=%q is not an absolute path", ErrBadFork, home)
	}
	from, err = workspaceDir(from)
	if err != nil {
		return "", err
	}
	// Already absolute and clean; refusing ".." outright is what lets a
	// path from the server be walked and copied without a second look.
	if strings.Contains(from, "..") {
		return "", fmt.Errorf("%w: from=%q contains \"..\"", ErrBadFork, from)
	}

	target := filepath.Join(home, ".agentrq", "forks", id)
	root, inRepo := gitRoot(from)
	rel := "."
	if inRepo {
		rel, _ = filepath.Rel(root, from) // root is an ancestor of from
	}
	dir = filepath.Join(target, rel)

	if _, err := os.Lstat(target); err == nil {
		out.Notice("reusing this fork's folder " + quoteArg(dir))
		return dir, nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("supervisor: fork folder %s: %w", target, err)
	}
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("supervisor: fork folder: %w", err)
	}

	if inRepo {
		err = addWorktree(ctx, root, target, id, out)
	} else {
		out.Notice("copying " + quoteArg(from) + " to " + quoteArg(target) + ", which is not in a git repository")
		err = copyTree(from, target, filepath.Join(home, ".agentrq"))
	}
	if err != nil {
		return "", err
	}
	return dir, nil
}

// RemoveForkDir deletes the folder PrepareForkDir made for a fork, and nothing
// else. A worktree is removed through git, so the repository forgets it; its
// branch stays, holding the fork's commits. A folder already gone is done.
func RemoveForkDir(home, id string) error {
	if !forkIDPattern.MatchString(id) {
		return fmt.Errorf("%w: id=%q", ErrBadFork, id)
	}
	if !filepath.IsAbs(home) {
		return fmt.Errorf("%w: home=%q is not an absolute path", ErrBadFork, home)
	}
	target := filepath.Join(home, ".agentrq", "forks", id)
	if _, err := os.Lstat(target); errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	// The repository the worktree belongs to, read before the folder goes.
	// Only when the folder is the worktree's own top: a copy that sits inside
	// somebody's repository (a home folder under git) is not one.
	var repo string
	if out, err := runGit(target, "rev-parse", "--path-format=absolute", "--show-toplevel", "--git-common-dir"); err == nil {
		if top, common, ok := strings.Cut(strings.TrimSpace(out), "\n"); ok && sameFolder(top, target) {
			repo = filepath.Dir(common)
		}
	}
	if repo != "" {
		if _, err := runGit(repo, "worktree", "remove", "--force", "--", target); err == nil {
			return nil
		}
	}
	if err := os.RemoveAll(target); err != nil {
		return fmt.Errorf("supervisor: remove fork folder %s: %w", target, err)
	}
	if repo != "" {
		// The folder is gone; drop the worktree entry it left behind.
		_, _ = runGit(repo, "worktree", "prune")
	}
	return nil
}

// sameFolder reports whether two paths name one folder, links and all.
func sameFolder(a, b string) bool {
	ai, aerr := os.Stat(a)
	bi, berr := os.Stat(b)
	return aerr == nil && berr == nil && os.SameFile(ai, bi)
}

// addWorktree checks out HEAD of the repository at root into target on the
// fork's branch. A branch left by an earlier fork of the same id is checked
// out again rather than refused; -f covers its worktree still being
// registered after somebody deleted the folder.
//
// Two commands, because `git worktree add` has no --progress and shows none
// when it is not writing to a terminal: the checkout is what takes the time,
// and it is the step the person needs to see moving.
func addWorktree(ctx context.Context, root, target, id string, out Progress) error {
	ctx, cancel := context.WithTimeout(ctx, checkoutTimeout)
	defer cancel()

	branch := ForkBranch(id)
	add := []string{"worktree", "add", "--no-checkout", "-b", branch, "--", target, "HEAD"}
	if _, err := runGit(root, "rev-parse", "--verify", "--quiet", "refs/heads/"+branch); err == nil {
		add = []string{"worktree", "add", "--no-checkout", "-f", "--", target, branch}
	}
	steps := []struct {
		name, dir string
		args      []string
	}{
		{"worktree add", root, add},
		{"checkout", target, []string{"checkout", "--progress", "-f", branch}},
	}
	for _, step := range steps {
		out.Notice(CommandLine(step.dir, append([]string{"git"}, step.args...)))
		if text, err := runGitTo(ctx, step.dir, out, step.args...); err != nil {
			discardWorktree(root, target)
			if errors.Is(ctx.Err(), context.DeadlineExceeded) {
				err = fmt.Errorf("took longer than %s", checkoutTimeout)
			}
			return fmt.Errorf("supervisor: git %s in %s: %w: %s", step.name, step.dir, err, strings.TrimSpace(text))
		}
	}
	return nil
}

// discardWorktree removes a worktree that was not finished, with git's own
// record of it. Twice forced, because git locks a worktree while it is being
// added and a checkout that was killed leaves the lock behind.
func discardWorktree(root, target string) {
	if _, err := runGit(root, "worktree", "remove", "-f", "-f", "--", target); err == nil {
		return
	}
	_ = os.RemoveAll(target)
	_, _ = runGit(root, "worktree", "prune")
}

// runGit runs a quick git query in dir with a fixed argv and no shell,
// returning what it printed. dir is the working directory, never an argument,
// so a folder the server named cannot be read as a git option.
func runGit(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), gitTimeout)
	defer cancel()
	return runGitTo(ctx, dir, io.Discard, args...)
}

// runGitTo is [runGit] for a command that takes as long as ctx allows, with
// what it prints copied to out as it prints it.
func runGitTo(ctx context.Context, dir string, out io.Writer, args ...string) (string, error) {
	var text bytes.Buffer
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Stdout = io.MultiWriter(&text, out)
	cmd.Stderr = cmd.Stdout
	// A filter git runs, LFS's for one, can hold the output open after git
	// itself is killed; without this the wait would last as long as it does.
	cmd.WaitDelay = gitWaitDelay
	err := cmd.Run()
	return text.String(), err
}

// copyTree copies from into target, keeping modes and copying symlinks as
// symlinks. The top-level .mcp.json is left out — its entry points at the
// parent — as is the daemon's own folder, wherever it turns up.
//
// Built beside target and renamed into place, so a copy that fails half way
// leaves nothing a relaunch would take for a finished fork.
func copyTree(from, target, skip string) error {
	tmp, err := os.MkdirTemp(filepath.Dir(target), filepath.Base(target)+".tmp-")
	if err != nil {
		return fmt.Errorf("supervisor: fork folder: %w", err)
	}
	// Modes are set once everything is in, deepest first, so a read-only
	// folder can still be filled and umask has no say.
	type mode struct {
		path string
		perm fs.FileMode
	}
	var modes []mode
	// Compared by identity, not by path: the walk sees resolved paths, and
	// a spelling that differs (macOS /var, a Windows short name) would copy
	// the staging folder into itself.
	skipInfo, _ := os.Stat(skip)
	// Walk does not follow a symlink, the root included.
	src := from
	if real, err := filepath.EvalSymlinks(from); err == nil {
		src = real
	}
	walkErr := filepath.Walk(src, func(path string, info fs.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if rel == MCPConfigName || rel == ".agentrq" || os.SameFile(info, skipInfo) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		dst := filepath.Join(tmp, rel)
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err == nil {
				err = os.Symlink(link, dst)
			}
			return err
		case info.IsDir():
			modes = append(modes, mode{dst, info.Mode().Perm()})
			if rel == "." {
				return nil
			}
			return os.Mkdir(dst, 0o700)
		case info.Mode().IsRegular():
			modes = append(modes, mode{dst, info.Mode().Perm()})
			return copyFile(path, dst)
		}
		return nil // a socket or a device is not part of a project
	})
	for i := len(modes) - 1; walkErr == nil && i >= 0; i-- {
		walkErr = os.Chmod(modes[i].path, modes[i].perm)
	}
	if walkErr == nil {
		walkErr = os.Rename(tmp, target)
	}
	if walkErr != nil {
		_ = os.RemoveAll(tmp)
		return fmt.Errorf("supervisor: copy %s into a fork folder: %w", from, walkErr)
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err == nil {
		_, err = io.Copy(out, in)
		err = errors.Join(err, out.Close())
	}
	return err
}
