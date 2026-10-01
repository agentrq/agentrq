// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

const testURL = "https://abc123.mcp.agentrq.com?token=eyJhbGciOiJIUzI1NiJ9.test.signature"

// ws is the ordinary single entry every non-supervisor launch writes.
func ws(url string) MCPEntry { return MCPEntry{Name: "agentrq-workspace", URL: url} }

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
	path, kept, err := WriteMCPConfig(dir, ws(testURL))
	if err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}
	if len(kept) != 0 {
		t.Errorf("kept = %v, want nothing in an empty folder", kept)
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

// The supervisor workspace talks to the account-wide server as well, and both
// entries have to land in the one file the agent reads.
func TestTheSupervisorGetsBothServersInOneFile(t *testing.T) {
	dir := t.TempDir()
	const coreURL = "https://mcp.agentrq.com/mcp"

	path, _, err := WriteMCPConfig(dir, ws(testURL), MCPEntry{Name: "agentrq", URL: coreURL})
	if err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}

	cfg := readConfig(t, path)
	if len(cfg.Servers) != 2 {
		t.Fatalf("servers = %+v, want two", cfg.Servers)
	}
	if got := cfg.Servers["agentrq"]; got.URL != coreURL || got.Type != "http" {
		t.Errorf("core entry = %+v", got)
	}
	if got := cfg.Servers["agentrq-workspace"]; got.URL != testURL {
		t.Errorf("workspace entry = %+v", got)
	}
}

// The whole credential is a query parameter in that URL, so the file is the
// only place it may live — and only the owner may read it.
func TestMCPConfigIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply")
	}
	dir := t.TempDir()
	path, _, err := WriteMCPConfig(dir, ws(testURL))
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

	_, _, err := WriteMCPConfig(t.TempDir(), ws(bad))
	if err == nil {
		t.Fatal("plaintext http to a remote host should be refused")
	}
	if strings.Contains(err.Error(), secret) {
		t.Errorf("the error leaked the token: %v", err)
	}

	_, _, err = WriteMCPConfig(t.TempDir(), ws("://broken?token="+secret))
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
	if _, _, err := WriteMCPConfig(t.TempDir(), MCPEntry{Name: "s", URL: "http://mcp.example.com?token=x"}); !errors.Is(err, ErrInsecureMCP) {
		t.Errorf("error = %v, want ErrInsecureMCP", err)
	}
	for _, u := range []string{
		"http://localhost:3000?token=x",
		"http://127.0.0.1:3000?token=x",
	} {
		if _, _, err := WriteMCPConfig(t.TempDir(), MCPEntry{Name: "s", URL: u}); err != nil {
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

	path, _, err := WriteMCPConfig(dir, ws(testURL))
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

// A folder that already names one of our servers has been set up by somebody
// who meant it, so the entry stands and the launch says which ones it left.
func TestAnEntryAlreadyThereIsKeptAndReported(t *testing.T) {
	dir := t.TempDir()
	const theirs = "https://someone-elses.example"
	body := `{"mcpServers":{"agentrq-workspace":{"type":"http","url":"` + theirs + `"}}}`
	if err := os.WriteFile(filepath.Join(dir, MCPConfigName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	path, kept, err := WriteMCPConfig(dir, ws(testURL))
	if err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}
	if len(kept) != 1 || kept[0] != "agentrq-workspace" {
		t.Errorf("kept = %v, want the entry that was already there", kept)
	}
	if got := readConfig(t, path).Servers["agentrq-workspace"].URL; got != theirs {
		t.Errorf("url = %q, want the existing %q left alone", got, theirs)
	}
}

// One entry already configured must not stop the other being written.
func TestOnlyTheMissingEntryIsAdded(t *testing.T) {
	dir := t.TempDir()
	body := `{"mcpServers":{"agentrq":{"type":"http","url":"https://their-own-core.example/mcp"}}}`
	if err := os.WriteFile(filepath.Join(dir, MCPConfigName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	path, kept, err := WriteMCPConfig(dir, ws(testURL), MCPEntry{Name: "agentrq", URL: "https://mcp.agentrq.com/mcp"})
	if err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}
	if len(kept) != 1 || kept[0] != "agentrq" {
		t.Errorf("kept = %v, want only the core entry", kept)
	}
	cfg := readConfig(t, path)
	if cfg.Servers["agentrq"].URL != "https://their-own-core.example/mcp" {
		t.Errorf("their core entry changed: %+v", cfg.Servers["agentrq"])
	}
	if cfg.Servers["agentrq-workspace"].URL != testURL {
		t.Errorf("the workspace entry was not written: %+v", cfg.Servers["agentrq-workspace"])
	}
}

// A second launch into the same folder keeps the credential already there.
//
// The opposite of what this file used to assert, and a deliberate decision
// (2026-09-23): a workspace token is good for a year, so the entry from the
// first launch goes on working, and not touching it is what makes a folder
// somebody configured by hand stay configured. The way to move an entry is to
// delete it.
func TestRelaunchingKeepsTheCredentialAlreadyThere(t *testing.T) {
	dir := t.TempDir()
	const endpoint = "https://agentrq.example/mcp/0inioPIhhbt"

	if _, _, err := WriteMCPConfig(dir, ws(endpoint+"?token=first")); err != nil {
		t.Fatalf("first launch: %v", err)
	}
	path, kept, err := WriteMCPConfig(dir, ws(endpoint+"?token=second"))
	if err != nil {
		t.Fatalf("second launch: %v", err)
	}
	if len(kept) != 1 {
		t.Errorf("kept = %v, want the first launch's entry", kept)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "token=first") {
		t.Errorf("the first launch's credential was replaced:\n%s", b)
	}
}

// Rewriting a file to the bytes it already holds still changes its timestamp,
// and on a folder somebody is watching that is a change they have to explain.
func TestAFileWithNothingToAddIsNotRewritten(t *testing.T) {
	dir := t.TempDir()
	path, _, err := WriteMCPConfig(dir, ws(testURL))
	if err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// A timestamp is too coarse to compare after a write that takes
	// microseconds, so the file is aged instead: if it is rewritten, the
	// change is unmistakable.
	old := before.ModTime().Add(-time.Hour)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if _, _, err := WriteMCPConfig(dir, ws(testURL)); err != nil {
		t.Fatal(err)
	}
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !after.ModTime().Equal(old) {
		t.Error("a config with nothing to add was rewritten")
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
	if _, _, err := WriteMCPConfig(dir, ws(testURL)); !errors.Is(err, ErrForeignConfig) {
		t.Fatalf("error = %v, want ErrForeignConfig", err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{not json at all" {
		t.Error("an unparseable config was overwritten")
	}
}

func TestWriteMCPConfigValidatesItsInputs(t *testing.T) {
	if _, _, err := WriteMCPConfig(t.TempDir()); !errors.Is(err, ErrNoMCPURL) {
		t.Errorf("error = %v, want ErrNoMCPURL", err)
	}
	if _, _, err := WriteMCPConfig(t.TempDir(), MCPEntry{Name: "s", URL: "  "}); !errors.Is(err, ErrNoMCPURL) {
		t.Errorf("error = %v, want ErrNoMCPURL", err)
	}
	// The server name reaches an argv as `server:<name>`, so it gets the same
	// check every other parameter does.
	if _, _, err := WriteMCPConfig(t.TempDir(), MCPEntry{Name: "bad name", URL: testURL}); !errors.Is(err, ErrBadParameter) {
		t.Errorf("error = %v, want ErrBadParameter", err)
	}
}

// An interrupted write must leave the previous config intact rather than a
// truncated one the agent cannot parse.
func TestNoTemporaryFilesAreLeftBehind(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := WriteMCPConfig(dir, ws(testURL)); err != nil {
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

// HasMCPConfig answers what a restored session needs to know.
func TestHasMCPConfig(t *testing.T) {
	dir := t.TempDir()
	if HasMCPConfig(dir, "agentrq-workspace") {
		t.Error("an empty folder claimed to have a config")
	}
	if _, _, err := WriteMCPConfig(dir, ws("https://agentrq.example/mcp/ws?token=a")); err != nil {
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

// Adding our entry keeps everything else in the file: a stdio server's
// command, args and env, a server's headers, and keys besides mcpServers.
// Rewriting it through a struct with only type and url used to drop them all.
func TestWriteMCPConfigKeepsWhatItHasNoFieldFor(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, MCPConfigName)
	writeFile(t, path, `{"inputs":[{"id":"key"}],"mcpServers":{`+
		`"tools":{"command":"run-tools","args":["--fast"],"env":{"A":"1"}},`+
		`"api":{"type":"http","url":"https://api.example.com/mcp","headers":{"X-Key":"k"}}}}`)

	if _, _, err := WriteMCPConfig(dir, ws(testURL)); err != nil {
		t.Fatalf("WriteMCPConfig: %v", err)
	}
	var got struct {
		Inputs  []map[string]string        `json:"inputs"`
		Servers map[string]json.RawMessage `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Inputs) != 1 || got.Inputs[0]["id"] != "key" {
		t.Errorf("inputs = %v, want them kept", got.Inputs)
	}
	for name, want := range map[string]string{
		"tools": `{"args":["--fast"],"command":"run-tools","env":{"A":"1"}}`,
		"api":   `{"headers":{"X-Key":"k"},"type":"http","url":"https://api.example.com/mcp"}`,
	} {
		if compact(t, got.Servers[name]) != want {
			t.Errorf("%s = %s, want %s", name, got.Servers[name], want)
		}
	}
	if readConfig(t, path).Servers["agentrq-workspace"].URL != testURL {
		t.Error("our entry was not added")
	}
}

// compact is b re-encoded with its keys sorted, for comparing JSON values.
func compact(t *testing.T, b []byte) string {
	t.Helper()
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// A file that says null, or has null servers, is an empty config.
func TestWriteMCPConfigOverNulls(t *testing.T) {
	for _, body := range []string{`null`, `{"mcpServers":null}`} {
		dir := t.TempDir()
		path := filepath.Join(dir, MCPConfigName)
		writeFile(t, path, body)
		if _, _, err := WriteMCPConfig(dir, ws(testURL)); err != nil {
			t.Fatalf("%s: %v", body, err)
		}
		if readConfig(t, path).Servers["agentrq-workspace"].URL != testURL {
			t.Errorf("%s: our entry was not written", body)
		}
	}
}

// A fork's folder replaces an entry at another endpoint, keeps one at its own
// (an earlier launch's, older token), and names only the first.
func TestWriteForkMCPConfigReplacesOnlyWhatPointsElsewhere(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, MCPConfigName)
	writeFile(t, path, `{"mcpServers":{`+
		`"agentrq-workspace":{"type":"http","url":"https://parent.example.com/mcp"},`+
		`"agentrq":{"type":"http","url":"https://mcp.agentrq.com/mcp?token=old"}}}`)

	_, kept, replaced, err := WriteForkMCPConfig(dir, ws(testURL),
		MCPEntry{Name: "agentrq", URL: "https://mcp.agentrq.com/mcp?token=new"})
	if err != nil {
		t.Fatalf("WriteForkMCPConfig: %v", err)
	}
	if !slices.Equal(replaced, []string{"agentrq-workspace"}) || !slices.Equal(kept, []string{"agentrq"}) {
		t.Errorf("replaced %v kept %v, want the parent's entry replaced and our own kept", replaced, kept)
	}
	cfg := readConfig(t, path)
	if cfg.Servers["agentrq-workspace"].URL != testURL || cfg.Servers["agentrq"].URL != "https://mcp.agentrq.com/mcp?token=old" {
		t.Errorf("servers = %v", cfg.Servers)
	}
}
