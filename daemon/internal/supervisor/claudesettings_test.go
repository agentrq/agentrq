// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
)

func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("parse: %v\n%s", err, b)
	}
	return out
}

func allowList(t *testing.T, settings map[string]any) []string {
	t.Helper()
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatalf("no permissions object: %+v", settings)
	}
	raw, ok := perms["allow"].([]any)
	if !ok {
		t.Fatalf("no allow list: %+v", perms)
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		out = append(out, v.(string))
	}
	return out
}

// Without this file an agent stops and asks a human before every MCP call, and
// on a machine launched from the panel there is nobody at that terminal.
func TestWriteClaudeSettingsPreApprovesTheServer(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteClaudeSettings(dir, "agentrq-workspace")
	if err != nil {
		t.Fatalf("WriteClaudeSettings: %v", err)
	}
	if want := filepath.Join(dir, ClaudeSettingsDir, ClaudeSettingsName); path != want {
		t.Errorf("wrote %q, want %q", path, want)
	}

	settings := readSettings(t, path)
	if got := allowList(t, settings); !slices.Equal(got, []string{"mcp__agentrq-workspace__*"}) {
		t.Errorf("allow = %v", got)
	}
	if settings["enableAllProjectMcpServers"] != true {
		t.Errorf("enableAllProjectMcpServers = %v, want true", settings["enableAllProjectMcpServers"])
	}
	if got := settings["enabledMcpjsonServers"]; !slices.Equal(toStrings(got), []string{"agentrq-workspace"}) {
		t.Errorf("enabledMcpjsonServers = %v", got)
	}
}

// One wildcard per server rather than a list of tool names: the servers gain
// tools, and a list that has gone stale stalls an agent on the first call to
// the one name nobody copied over.
func TestTheSupervisorGetsBothServersApproved(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteClaudeSettings(dir, "agentrq-workspace", "agentrq")
	if err != nil {
		t.Fatal(err)
	}
	settings := readSettings(t, path)
	want := []string{"mcp__agentrq-workspace__*", "mcp__agentrq__*"}
	if got := allowList(t, settings); !slices.Equal(got, want) {
		t.Errorf("allow = %v, want %v", got, want)
	}
	if got := toStrings(settings["enabledMcpjsonServers"]); !slices.Equal(got, []string{"agentrq-workspace", "agentrq"}) {
		t.Errorf("enabledMcpjsonServers = %v", got)
	}
}

// The file is the person's. Their rules, and the keys this does not know
// about, have to come back out of it unchanged — a launch that deleted
// somebody's hooks would be a launch that lost work.
func TestAPersonsOwnSettingsSurvive(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ClaudeSettingsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	existing := `{
	  "permissions": { "allow": ["Bash(git status)"], "deny": ["Bash(rm:*)"] },
	  "model": "opus",
	  "hooks": { "Stop": [] }
	}`
	path := filepath.Join(dir, ClaudeSettingsDir, ClaudeSettingsName)
	if err := os.WriteFile(path, []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); err != nil {
		t.Fatal(err)
	}

	settings := readSettings(t, path)
	if got := allowList(t, settings); !slices.Equal(got, []string{"Bash(git status)", "mcp__agentrq-workspace__*"}) {
		t.Errorf("allow = %v, want theirs kept and ours appended", got)
	}
	perms := settings["permissions"].(map[string]any)
	if got := toStrings(perms["deny"]); !slices.Equal(got, []string{"Bash(rm:*)"}) {
		t.Errorf("deny = %v, want it untouched", got)
	}
	if settings["model"] != "opus" {
		t.Errorf("model = %v, want it untouched", settings["model"])
	}
	if _, ok := settings["hooks"]; !ok {
		t.Error("their hooks were dropped")
	}
}

// Every launch into the same folder writes this file, and a rule that arrived
// twice is a file that grows without bound.
func TestRelaunchingDoesNotDuplicateRules(t *testing.T) {
	dir := t.TempDir()
	for range 3 {
		if _, err := WriteClaudeSettings(dir, "agentrq-workspace", "agentrq"); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(dir, ClaudeSettingsDir, ClaudeSettingsName)
	want := []string{"mcp__agentrq-workspace__*", "mcp__agentrq__*"}
	if got := allowList(t, readSettings(t, path)); !slices.Equal(got, want) {
		t.Errorf("allow = %v, want %v", got, want)
	}
}

// Local settings are the likeliest file in a folder to have been hand-edited,
// and losing them to a launch that could have said so is not a trade worth
// making.
func TestUnparseableSettingsAreRefusedRatherThanReplaced(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ClaudeSettingsDir), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ClaudeSettingsDir, ClaudeSettingsName)
	const theirs = `{"permissions": {"allow": ["Bash(ls)"],}}`
	if err := os.WriteFile(path, []byte(theirs), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); !errors.Is(err, ErrForeignSettings) {
		t.Fatalf("error = %v, want ErrForeignSettings", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != theirs {
		t.Errorf("their file was rewritten:\n%s", b)
	}
}

func TestWriteClaudeSettingsValidatesItsInputs(t *testing.T) {
	if _, err := WriteClaudeSettings(t.TempDir()); !errors.Is(err, ErrMissingParam) {
		t.Errorf("error = %v, want ErrMissingParam", err)
	}
	// A name goes into a permission rule, so it gets the same check the rest
	// of the parameters get.
	if _, err := WriteClaudeSettings(t.TempDir(), "bad name"); !errors.Is(err, ErrBadParameter) {
		t.Errorf("error = %v, want ErrBadParameter", err)
	}
}

// The folder holds one person's local settings; on a shared machine the rest
// of it is nobody else's business either.
func TestSettingsAreOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply")
	}
	dir := t.TempDir()
	path, err := WriteClaudeSettings(dir, "agentrq-workspace")
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("file mode = %o, want 600", perm)
	}
	dirInfo, err := os.Stat(filepath.Join(dir, ClaudeSettingsDir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Errorf("directory mode = %o, want 700", perm)
	}
}

// An interrupted write must not leave a half-file the agent reads instead.
func TestSettingsLeaveNoTemporaryFiles(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(filepath.Join(dir, ClaudeSettingsDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ClaudeSettingsName {
		t.Errorf("directory holds %d entries, want only %s", len(entries), ClaudeSettingsName)
	}
}

func toStrings(v any) []string {
	raw, ok := v.([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, e := range raw {
		s, ok := e.(string)
		if !ok {
			return nil
		}
		out = append(out, s)
	}
	return out
}

// unwritable makes a directory that cannot be written to, or skips: the
// permission bits mean nothing on Windows, and root ignores them.
func unwritable(t *testing.T, dir string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply")
	}
	if os.Geteuid() == 0 {
		t.Skip("root is not stopped by a permission bit")
	}
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
}

// A folder the daemon cannot write to fails the launch with the reason,
// rather than starting an agent that will stop at its first tool call.
func TestSettingsSayWhyTheyCouldNotBeWritten(t *testing.T) {
	t.Run("the .claude directory cannot be created", func(t *testing.T) {
		dir := t.TempDir()
		unwritable(t, dir)
		if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); err == nil {
			t.Error("a folder that cannot hold the file reported success")
		}
	})

	t.Run("the file cannot be replaced", func(t *testing.T) {
		dir := t.TempDir()
		settingsDir := filepath.Join(dir, ClaudeSettingsDir)
		if err := os.MkdirAll(settingsDir, 0o700); err != nil {
			t.Fatal(err)
		}
		unwritable(t, settingsDir)
		if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); err == nil {
			t.Error("an unwritable settings directory reported success")
		}
	})

	t.Run("the existing file cannot be read", func(t *testing.T) {
		dir := t.TempDir()
		settingsDir := filepath.Join(dir, ClaudeSettingsDir)
		if err := os.MkdirAll(settingsDir, 0o700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(settingsDir, ClaudeSettingsName)
		if err := os.WriteFile(path, []byte(`{}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS == "windows" {
			t.Skip("Unix permission bits do not apply")
		}
		if os.Geteuid() == 0 {
			t.Skip("root is not stopped by a permission bit")
		}
		if err := os.Chmod(path, 0o000); err != nil {
			t.Fatal(err)
		}
		if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); err == nil {
			t.Error("a file that could not be read was overwritten")
		}
	})
}

// A file that parses as JSON but not as settings is still somebody's file.
func TestSettingsOfTheWrongShapeAreRefused(t *testing.T) {
	for name, body := range map[string]string{
		"permissions is not an object": `{"permissions": 5}`,
		"allow is not a list":          `{"permissions": {"allow": "everything"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.MkdirAll(filepath.Join(dir, ClaudeSettingsDir), 0o700); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, ClaudeSettingsDir, ClaudeSettingsName)
			if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := WriteClaudeSettings(dir, "agentrq-workspace"); !errors.Is(err, ErrForeignSettings) {
				t.Errorf("error = %v, want ErrForeignSettings", err)
			}
			b, _ := os.ReadFile(path)
			if string(b) != body {
				t.Errorf("their file was rewritten:\n%s", b)
			}
		})
	}
}
