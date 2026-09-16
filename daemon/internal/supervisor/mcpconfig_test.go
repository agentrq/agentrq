// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const testURL = "https://abc123.mcp.agentrq.com?token=eyJhbGciOiJIUzI1NiJ9.test.signature"

func readConfig(t *testing.T, path string) MCPConfig {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var cfg MCPConfig
	if err := json.Unmarshal(b, &cfg); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return cfg
}

func TestWriteMCPConfigProducesTheShapeClaudeReads(t *testing.T) {
	dir := t.TempDir()
	path, err := WriteMCPConfig(dir, "agentrq-workspace", testURL)
	if err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}
	if filepath.Base(path) != MCPConfigName {
		t.Errorf("wrote %q, want %s", path, MCPConfigName)
	}

	cfg := readConfig(t, path)
	srv, ok := cfg.Servers["agentrq-workspace"]
	if !ok {
		t.Fatalf("no entry under the server name: %+v", cfg)
	}
	if srv.Type != "http" || srv.URL != testURL {
		t.Errorf("entry = %+v", srv)
	}
}

// The whole credential is a query parameter in that URL, so the file is the
// only place it may live — and only the owner may read it.
func TestMCPConfigIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply")
	}
	dir := t.TempDir()
	path, err := WriteMCPConfig(dir, "agentrq-workspace", testURL)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600 — the token is in this file", perm)
	}
}

// A token in a command line is visible in `ps` to every user on the box, which
// would undo the file permission entirely. It must not reach an error message
// either, since those get logged and pasted into bug reports.
func TestTheTokenNeverAppearsInAnError(t *testing.T) {
	const secret = "SUPERSECRETTOKENVALUE"
	bad := "http://not-loopback.example.com?token=" + secret

	_, err := WriteMCPConfig(t.TempDir(), "agentrq-workspace", bad)
	if err == nil {
		t.Fatal("plaintext http to a remote host should be refused")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error leaked the token: %v", err)
	}

	_, err = WriteMCPConfig(t.TempDir(), "agentrq-workspace", "://broken?token="+secret)
	if err == nil {
		t.Fatal("an unparseable URL should be refused")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error leaked the token: %v", err)
	}
}

// The token is in the query string, so plain HTTP puts it on the wire in
// clear. Loopback has no wire to be on.
func TestPlainHTTPIsRefusedExceptOnLoopback(t *testing.T) {
	if _, err := WriteMCPConfig(t.TempDir(), "s", "http://mcp.example.com?token=x"); !errors.Is(err, ErrInsecureMCP) {
		t.Errorf("error = %v, want ErrInsecureMCP", err)
	}
	for _, u := range []string{
		"http://localhost:3000?token=x",
		"http://127.0.0.1:3000?token=x",
	} {
		if _, err := WriteMCPConfig(t.TempDir(), "s", u); err != nil {
			t.Errorf("WriteMCPConfig(%q) = %v, want it allowed", u, err)
		}
	}
}

// A person's own .mcp.json is theirs.
func TestExistingEntriesAreKept(t *testing.T) {
	dir := t.TempDir()
	existing := `{"mcpServers":{"my-own-thing":{"type":"http","url":"https://elsewhere.example"}}}`
	if err := os.WriteFile(filepath.Join(dir, MCPConfigName), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	path, err := WriteMCPConfig(dir, "agentrq-workspace", testURL)
	if err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}
	cfg := readConfig(t, path)
	if _, ok := cfg.Servers["my-own-thing"]; !ok {
		t.Error("the user's own server entry was discarded")
	}
	if _, ok := cfg.Servers["agentrq-workspace"]; !ok {
		t.Error("our entry was not added")
	}
}

// Overwriting somebody else's entry under the same name would break whatever
// they had it pointed at, silently.
func TestAConflictingEntryIsAnErrorRatherThanAnOverwrite(t *testing.T) {
	dir := t.TempDir()
	existing := `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"https://someone-elses.example"}}}`
	if err := os.WriteFile(filepath.Join(dir, MCPConfigName), []byte(existing), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteMCPConfig(dir, "agentrq-workspace", testURL); !errors.Is(err, ErrForeignConfig) {
		t.Fatalf("error = %v, want ErrForeignConfig", err)
	}
	// And it must not have been touched.
	cfg := readConfig(t, filepath.Join(dir, MCPConfigName))
	if cfg.Servers["agentrq-workspace"].URL != "https://someone-elses.example" {
		t.Error("the existing entry was modified despite the refusal")
	}
}

// Rewriting the same entry is how a restart works and must be harmless.
func TestRewritingOurOwnEntryIsFine(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", testURL); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", testURL); err != nil {
		t.Errorf("rewriting the same config failed: %v", err)
	}
}

// It is somebody's configuration, and it is not ours to discard because we
// could not read it.
func TestAnUnreadableConfigIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, MCPConfigName)
	if err := os.WriteFile(path, []byte("{not json at all"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", testURL); !errors.Is(err, ErrForeignConfig) {
		t.Fatalf("error = %v, want ErrForeignConfig", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{not json at all" {
		t.Error("an unparseable config was overwritten")
	}
}

func TestWriteMCPConfigValidatesItsInputs(t *testing.T) {
	if _, err := WriteMCPConfig(t.TempDir(), "s", "  "); !errors.Is(err, ErrNoMCPURL) {
		t.Errorf("error = %v, want ErrNoMCPURL", err)
	}
	// The server name reaches an argv as `server:<name>`, so it gets the same
	// check every other parameter does.
	if _, err := WriteMCPConfig(t.TempDir(), "bad name", testURL); !errors.Is(err, ErrBadParameter) {
		t.Errorf("error = %v, want ErrBadParameter", err)
	}
}

// An interrupted write must leave the previous config intact rather than a
// truncated one the agent cannot parse.
func TestNoTemporaryFilesAreLeftBehind(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", testURL); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != MCPConfigName {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("directory holds %v, want only %s", names, MCPConfigName)
	}
}

// readMCPConfigExists reports whether a config file is present.
func readMCPConfigExists(dir string) (MCPConfig, error) {
	cfg, existed, err := readMCPConfig(filepath.Join(dir, MCPConfigName))
	if err != nil {
		return cfg, err
	}
	if !existed {
		return cfg, os.ErrNotExist
	}
	return cfg, nil
}

// Every launch mints a fresh, short-lived token, so the URL for the same
// workspace differs every time. Comparing whole URLs made the first launch
// into a folder succeed and every launch after it fail — found by launching an
// agent twice.
func TestRelaunchingTheSameWorkspaceReplacesTheCredential(t *testing.T) {
	dir := t.TempDir()
	const endpoint = "https://agentrq.example/mcp/0inioPIhhbt"

	if _, err := WriteMCPConfig(dir, "agentrq-workspace", endpoint+"?token=first"); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	path, err := WriteMCPConfig(dir, "agentrq-workspace", endpoint+"?token=second")
	if err != nil {
		t.Fatalf("second launch: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "token=second") {
		t.Errorf("the config still has the old credential:\n%s", b)
	}
	if strings.Contains(string(b), "token=first") {
		t.Errorf("the old credential is still in the file:\n%s", b)
	}
}

// The protection it was written for still holds: an entry under our name that
// points somewhere else is somebody's own and is not ours to replace.
func TestAnEntryPointingElsewhereIsStillRefused(t *testing.T) {
	for name, existing := range map[string]string{
		"a different host":      "https://somewhere-else.example/mcp/0inioPIhhbt?token=x",
		"a different workspace": "https://agentrq.example/mcp/somebody-elses?token=x",
		"a different scheme":    "http://agentrq.example/mcp/0inioPIhhbt?token=x",
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			body := `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"` + existing + `"}}}`
			if err := os.WriteFile(filepath.Join(dir, MCPConfigName), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := WriteMCPConfig(dir, "agentrq-workspace", "https://agentrq.example/mcp/0inioPIhhbt?token=ours")
			if !errors.Is(err, ErrForeignConfig) {
				t.Errorf("error = %v, want ErrForeignConfig", err)
			}
			// And their URL is untouched.
			b, _ := os.ReadFile(filepath.Join(dir, MCPConfigName))
			if !strings.Contains(string(b), existing) {
				t.Errorf("their entry was changed:\n%s", b)
			}
		})
	}
}

// A trailing slash is not a different endpoint.
func TestATrailingSlashIsTheSameEndpoint(t *testing.T) {
	dir := t.TempDir()
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", "https://agentrq.example/mcp/ws/?token=a"); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", "https://agentrq.example/mcp/ws?token=b"); err != nil {
		t.Errorf("a trailing slash was treated as a different endpoint: %v", err)
	}
}

// HasMCPConfig answers what a restored session needs to know.
func TestHasMCPConfig(t *testing.T) {
	dir := t.TempDir()
	if HasMCPConfig(dir, "agentrq-workspace") {
		t.Error("an empty folder claimed to have a config")
	}
	if _, err := WriteMCPConfig(dir, "agentrq-workspace", "https://agentrq.example/mcp/ws?token=a"); err != nil {
		t.Fatal(err)
	}
	if !HasMCPConfig(dir, "agentrq-workspace") {
		t.Error("a folder with a config claimed not to have one")
	}
	if HasMCPConfig(dir, "some-other-server") {
		t.Error("a config for one server answered for another")
	}

	// A file that cannot be read is not a config.
	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, MCPConfigName), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if HasMCPConfig(bad, "agentrq-workspace") {
		t.Error("unreadable JSON claimed to be a config")
	}
}
