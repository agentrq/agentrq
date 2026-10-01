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
	"slices"
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
// The line goes in the .gitignore of dir itself, the folder the agent runs
// in, and not the repository's top: a project in a subfolder of a repository
// keeps the change in its own folder, where whoever owns that folder sees it.
//
// A folder that is not in a repository is left alone, and so is one already
// excluded by a .gitignore anywhere from the repository's top down to dir —
// the one an earlier agentrqd wrote at the top included. Matching here is
// deliberately literal: a line equal to the path, or to a directory above it,
// counts as covered, and the full gitignore grammar — negations, globs — is
// not interpreted. The cost of reading it too narrowly is a duplicate line
// that changes nothing; the cost of reading it too broadly is a token in a
// commit.
func EnsureGitIgnored(dir string, paths ...string) (bool, error) {
	root, ok := gitRoot(dir)
	if !ok {
		return false, nil
	}
	// dir is absolute and clean (workspaceDir), so ".." can only be part of a
	// name; refusing it outright is what lets every folder from root down to
	// dir be read and written without a second look, as a fork's source is.
	if strings.Contains(dir, "..") || strings.Contains(root, "..") {
		return false, fmt.Errorf("%w: dir=%q contains \"..\"", ErrBadParameter, dir)
	}

	var missing []string
	var existing []byte // dir's own .gitignore, read last on the way down
	for _, p := range paths {
		entry := filepath.ToSlash(p)
		covered, own, err := ignoredOnTheWay(root, dir, entry)
		if err != nil {
			return false, err
		}
		if !covered {
			existing = own
			missing = append(missing, entry)
		}
	}
	if len(missing) == 0 {
		return false, nil
	}

	path := filepath.Join(dir, GitIgnoreName)

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

// ignoredOnTheWay reports whether entry, a path relative to dir, is excluded
// by the .gitignore of root or of any folder between it and dir. Each one is
// read with entry spelled relative to the folder it is in: the top's needs
// "three/down/.mcp.json", dir's own just ".mcp.json". own is dir's own
// .gitignore, read when the entry was not excluded above it.
func ignoredOnTheWay(root, dir, entry string) (covered bool, own []byte, err error) {
	rel, _ := filepath.Rel(root, dir) // root is dir or above it
	var below []string
	if rel != "." {
		below = strings.Split(filepath.ToSlash(rel), "/")
	}
	folder := root
	for i := 0; ; i++ {
		content, err := readGitIgnore(filepath.Join(folder, GitIgnoreName))
		if err != nil {
			return false, nil, err
		}
		if gitIgnoreCovers(content, strings.Join(append(slices.Clone(below[i:]), entry), "/")) {
			return true, nil, nil
		}
		if i == len(below) {
			return false, content, nil
		}
		folder = filepath.Join(folder, below[i])
	}
}

// readGitIgnore is a .gitignore's content, or none when there is no file.
func readGitIgnore(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("supervisor: read %s: %w", path, err)
	}
	return b, nil
}

// gitRoot finds the repository a folder is in, or reports that it is in none.
//
// Walks up because a workspace folder is often a directory inside a checkout
// rather than the checkout itself. `.git` is a directory in an ordinary clone and a file in a worktree
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
