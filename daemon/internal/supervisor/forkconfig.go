// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
)

// claudeDirName is the folder claude-code keeps a project's settings, skills
// and commands in.
const claudeDirName = ".claude"

// parentServerName matches the entry that connects an agent to its workspace:
// agentrq-workspace, or agentrq-<workspace id> in a file from before it.
var parentServerName = regexp.MustCompile(`^agentrq-(workspace|[A-Za-z0-9]{11})$`)

// copyAgentConfig gives a new fork's folder the parent's agent configuration
// that a worktree leaves behind, being untracked: its .mcp.json and .claude
// folder. Nothing already in the fork is overwritten, since a tracked file is
// the fork's branch's.
//
// The parent's workspace entry is dropped from .mcp.json, or the fork's agent
// would also connect as the parent; the launch writes the fork's own.
//
// Best effort: a fork without them still works, so a failure is a notice.
func copyAgentConfig(from, dir string, out Progress) {
	err := copyMCPConfig(from, dir)
	if src := filepath.Join(from, claudeDirName); !isHomeClaudeDir(src) {
		// Its worktrees are whole checkouts other agents made.
		err = errors.Join(err, copyMissing(src, filepath.Join(dir, claudeDirName), "worktrees"))
	}
	if err != nil {
		out.Notice("could not copy all of the parent's agent configuration: " + err.Error())
	}
}

// copyMCPConfig writes the parent's .mcp.json, less its workspace entry, into
// a fork folder that has none. A file with no other server is left out.
func copyMCPConfig(from, dir string) error {
	dst := filepath.Join(dir, MCPConfigName)
	if _, err := os.Lstat(dst); err == nil {
		return nil
	}
	_, raw, existed, err := loadMCPConfig(filepath.Join(from, MCPConfigName))
	if err != nil || !existed {
		return err
	}
	var order []string
	for _, name := range raw.order {
		if parentServerName.MatchString(name) {
			delete(raw.servers, name)
			continue
		}
		order = append(order, name)
	}
	if len(order) == 0 {
		return nil
	}
	raw.order = order
	raw.top["mcpServers"] = raw.marshalServers()
	body, _ := json.MarshalIndent(raw.top, "", "  ") // JSON just read: cannot fail
	return writePrivate(dir, dst, ".mcp-*.json", append(body, '\n'))
}

// isHomeClaudeDir reports whether dir is the user's own ~/.claude, which holds
// every project's history and the login, and is no project's to copy.
func isHomeClaudeDir(dir string) bool {
	home, err := os.UserHomeDir()
	return err == nil && sameFolder(dir, filepath.Join(home, claudeDirName))
}

// copyMissing copies from into target what target does not have yet, keeping
// modes and copying symlinks as symlinks. A top-level entry named skip is
// left out. A from that does not exist, or is no folder, is nothing to copy.
// What cannot be copied is passed over, and every such error returned.
func copyMissing(from, target, skip string) error {
	if info, err := os.Lstat(from); err != nil || !info.IsDir() {
		return nil
	}
	var errs []error
	_ = filepath.Walk(from, func(path string, info fs.FileInfo, err error) error {
		if err == nil {
			err = copyMissingOne(from, target, skip, path, info)
		}
		if err == nil {
			return nil
		}
		if errors.Is(err, filepath.SkipDir) {
			return err
		}
		errs = append(errs, err)
		if info != nil && info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	})
	return errors.Join(errs...)
}

// copyMissingOne is [copyMissing] for one entry the walk found.
func copyMissingOne(from, target, skip, path string, info fs.FileInfo) error {
	rel, _ := filepath.Rel(from, path)
	if rel == skip {
		if info.IsDir() {
			return filepath.SkipDir
		}
		return nil
	}
	dst := filepath.Join(target, rel)
	if _, err := os.Lstat(dst); err == nil {
		return nil // a folder is still walked, for what it lacks
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		link, err := os.Readlink(path)
		if err == nil {
			err = os.Symlink(link, dst)
		}
		return err
	case info.IsDir():
		return os.Mkdir(dst, info.Mode().Perm()|0o700)
	case info.Mode().IsRegular():
		if err := copyFile(path, dst); err != nil {
			return err
		}
		return os.Chmod(dst, info.Mode().Perm())
	}
	return nil
}
