// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const parentMCPConfig = `{
  "mcpServers": {
    "zeta": {"type": "http", "url": "https://zeta.example/mcp", "headers": {"X": "1"}},
    "agentrq-workspace": {"type": "http", "url": "https://agentrq.example/mcp?token=parent"},
    "agentrq-0mgCzDI46EN": {"type": "http", "url": "https://agentrq.example/mcp?token=old"},
    "agentrq-docs": {"command": "docs-mcp", "args": ["--stdio"]}
  },
  "other": true
}`

const strippedMCPConfig = `{
  "mcpServers": {
    "zeta": {
      "type": "http",
      "url": "https://zeta.example/mcp",
      "headers": {
        "X": "1"
      }
    },
    "agentrq-docs": {
      "command": "docs-mcp",
      "args": [
        "--stdio"
      ]
    }
  },
  "other": true
}
`

// A worktree gets the parent's untracked .mcp.json, without its workspace
// entries, and what .claude holds that the checkout did not bring.
func TestAForkGetsTheParentsUntrackedAgentConfig(t *testing.T) {
	root := gitRepo(t, map[string]string{"app/main.go": "package main\n", "app/.claude/settings.json": "tracked\n"})
	from := filepath.Join(root, "app")
	writeFile(t, filepath.Join(from, MCPConfigName), parentMCPConfig)
	writeFile(t, filepath.Join(from, ".claude", "settings.json"), "the parent's own change\n")
	writeFile(t, filepath.Join(from, ".claude", "settings.local.json"), `{"permissions":{}}`)
	writeFile(t, filepath.Join(from, ".claude", "commands", "ship.md"), "ship it\n")
	writeFile(t, filepath.Join(from, ".claude", "worktrees", "agent-1", "big"), "a checkout\n")
	home := t.TempDir()

	var shown recordingProgress
	dir, err := PrepareForkDir(t.Context(), forksIn(home), from, forkID, &shown)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, MCPConfigName)); got != strippedMCPConfig {
		t.Errorf(".mcp.json =\n%s\nwant\n%s", got, strippedMCPConfig)
	}
	if info, err := os.Stat(filepath.Join(dir, MCPConfigName)); err == nil && runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Errorf(".mcp.json mode = %v, want 0600", info.Mode())
	}
	for name, want := range map[string]string{
		"settings.json":       "tracked\n",
		"settings.local.json": `{"permissions":{}}`,
		"commands/ship.md":    "ship it\n",
	} {
		if got := readFile(t, filepath.Join(dir, ".claude", filepath.FromSlash(name))); got != want {
			t.Errorf(".claude/%s = %q, want %q", name, got, want)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "worktrees")); !errors.Is(err, os.ErrNotExist) {
		t.Error(".claude/worktrees was copied into the fork")
	}
	for _, n := range shown.noticed() {
		if strings.Contains(n, "could not copy") {
			t.Errorf("notice %q, want none", n)
		}
	}
}

// A copy leaves .mcp.json out of the tree, and then writes it stripped.
func TestAForkOfAPlainFolderGetsTheStrippedMCPConfig(t *testing.T) {
	from := t.TempDir()
	writeFile(t, filepath.Join(from, MCPConfigName), parentMCPConfig)
	writeFile(t, filepath.Join(from, ".claude", "worktrees"), "a file, not a folder\n")

	dir, err := PrepareForkDir(t.Context(), forksIn(t.TempDir()), from, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, MCPConfigName)); got != strippedMCPConfig {
		t.Errorf(".mcp.json =\n%s", got)
	}
}

// A tracked .mcp.json is the fork's branch's, and is not overwritten.
func TestAForkKeepsATrackedMCPConfig(t *testing.T) {
	root := gitRepo(t, map[string]string{MCPConfigName: `{"mcpServers":{}}`})
	writeFile(t, filepath.Join(root, MCPConfigName), parentMCPConfig)

	dir, err := PrepareForkDir(t.Context(), forksIn(t.TempDir()), root, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, MCPConfigName)); got != `{"mcpServers":{}}` {
		t.Errorf(".mcp.json = %q, want the tracked one", got)
	}
}

// A parent .mcp.json that cannot be read is said, and the rest still copied.
func TestAParentMCPConfigThatCannotBeReadIsANotice(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	writeFile(t, filepath.Join(root, MCPConfigName), "{not json")
	writeFile(t, filepath.Join(root, ".claude", "settings.local.json"), "{}")

	var shown recordingProgress
	dir, err := PrepareForkDir(t.Context(), forksIn(t.TempDir()), root, forkID, &shown)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, MCPConfigName)); !errors.Is(err, os.ErrNotExist) {
		t.Error("an unreadable .mcp.json was written into the fork")
	}
	if readFile(t, filepath.Join(dir, ".claude", "settings.local.json")) != "{}" {
		t.Error(".claude was not copied")
	}
	if n := noticeContaining(shown.noticed(), "could not copy all of the parent's agent configuration"); !strings.Contains(n, "not valid JSON") {
		t.Errorf("notice = %q, want the reason", n)
	}
}

// ~/.claude is the user's, never a project's: a repository at home does not
// hand it to a fork.
func TestAForkOfAHomeRepositoryLeavesTheUsersClaudeOut(t *testing.T) {
	root := gitRepo(t, map[string]string{"a": "1"})
	writeFile(t, filepath.Join(root, ".claude", ".credentials.json"), "secret")
	t.Setenv("HOME", root)
	t.Setenv("USERPROFILE", root)

	dir, err := PrepareForkDir(t.Context(), forksIn(t.TempDir()), root, forkID, nil)
	if err != nil {
		t.Fatalf("PrepareForkDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the user's ~/.claude was copied into the fork")
	}
}

func noticeContaining(notices []string, text string) string {
	for _, n := range notices {
		if strings.Contains(n, text) {
			return n
		}
	}
	return ""
}
