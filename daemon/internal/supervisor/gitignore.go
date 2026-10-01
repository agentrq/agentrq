// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// GitIgnoreName is the file git reads its exclusions from.
const GitIgnoreName = ".gitignore"

// gitIgnoreBanner introduces the lines this daemon adds, so somebody reading
// the file later knows what put them there and can delete them knowingly.
const gitIgnoreBanner = "# Added by agentrqd: written per launch, and it holds this workspace's token"

// EnsureGitIgnored keeps a file this daemon writes out of a repository, and
// reports whether it changed anything.
//
// Used for the MCP config and **only** for it, because the MCP config is the
// one that carries the workspace's credential in a URL. A folder that is also
// a git checkout is one `git add -A` away from that token being committed and,
// once pushed, being somewhere it cannot be taken back from — so the exclusion
// goes in before the file does.
//
// The permissions file is deliberately not excluded: it holds no secret, only
// a list of servers to trust, and whether to check that in is the repository's
// decision rather than this daemon's.
//
// A folder that is not in a repository is left alone, and so is one whose
// .gitignore already names the path. Matching here is deliberately literal: a
// line equal to the path, or to a directory above it, counts as covered, and
// the full gitignore grammar — negations, globs, patterns in parent
// directories — is not interpreted. The cost of reading it too narrowly is a
// duplicate line that changes nothing; the cost of reading it too broadly is a
// token in a commit.
func EnsureGitIgnored(dir string, paths ...string) (bool, error) {
	root, ok := gitRoot(dir)
	if !ok {
		return false, nil
	}

	// Relative to the repository root, because that is what a .gitignore there
	// addresses: a workspace folder three directories down inside a checkout
	// needs "three/down/.mcp.json", not ".mcp.json".
	rel, err := filepath.Rel(root, dir)
	if err != nil {
		return false, fmt.Errorf("supervisor: locate %s inside %s: %w", dir, root, err)
	}
	prefix := ""
	if rel != "." {
		prefix = filepath.ToSlash(rel) + "/"
	}

	path := filepath.Join(root, GitIgnoreName)
	existing, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("supervisor: read %s: %w", path, err)
	}

	var missing []string
	for _, p := range paths {
		entry := prefix + filepath.ToSlash(p)
		if !gitIgnoreCovers(existing, entry) {
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}

	var out bytes.Buffer
	out.Write(existing)
	// A .gitignore whose last line has no newline would otherwise have our
	// first entry welded onto the end of it, quietly changing what that line
	// excludes.
	if len(existing) > 0 && !bytes.HasSuffix(existing, []byte("\n")) {
		out.WriteString("\n")
	}
	if len(existing) > 0 {
		out.WriteString("\n")
	}
	out.WriteString(gitIgnoreBanner + "\n")
	for _, entry := range missing {
		out.WriteString(entry + "\n")
	}

	// 0644 and not 0600: this one is meant to be read by everybody who checks
	// the repository out, and is the only file here with no secret in it.
	if err := os.WriteFile(path, out.Bytes(), 0o644); err != nil {
		return false, fmt.Errorf("supervisor: write %s: %w", path, err)
	}
	return true, nil
}

// KeepOutOfCommits stops git committing changes to name, a file in dir that
// the repository already tracks, and reports whether it had to. A .gitignore
// does nothing for a tracked file, so a fork of a repository that commits its
// .mcp.json would otherwise have its token in the next `git commit -a`.
//
// skip-worktree rather than assume-unchanged, which git may drop on its own;
// and it is set in the index of dir's own checkout, which for a fork is its
// worktree, so the repository it was forked from is not touched.
func KeepOutOfCommits(dir, name string) (bool, error) {
	if _, ok := gitRoot(dir); !ok {
		return false, nil
	}
	if _, err := runGit(dir, "ls-files", "--error-unmatch", "--", name); err != nil {
		return false, nil // not tracked, so a .gitignore covers it
	}
	if out, err := runGit(dir, "update-index", "--skip-worktree", "--", name); err != nil {
		return false, fmt.Errorf("supervisor: keep %s in %s out of commits: %w: %s", name, dir, err, strings.TrimSpace(out))
	}
	return true, nil
}

// gitRoot finds the repository a folder is in, or reports that it is in none.
//
// Walks up because a workspace folder is often a directory inside a checkout
// rather than the checkout itself, and the .gitignore that governs it lives at
// the top. `.git` is a directory in an ordinary clone and a file in a worktree
// or a submodule; both count.
func gitRoot(dir string) (string, bool) {
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir, true
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// gitIgnoreCovers reports whether a .gitignore already excludes an entry.
func gitIgnoreCovers(content []byte, entry string) bool {
	scan := bufio.NewScanner(bytes.NewReader(content))
	for scan.Scan() {
		line := strings.TrimSpace(scan.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "!") {
			continue
		}
		line = strings.TrimSuffix(strings.TrimPrefix(line, "/"), "/")
		if line == "" {
			continue
		}
		if line == entry || strings.HasPrefix(entry, line+"/") {
			return true
		}
	}
	return false
}
