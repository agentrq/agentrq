// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// MCPConfigName is the file claude-code reads from its working directory.
const MCPConfigName = ".mcp.json"

// MCPServer is one entry in that file.
type MCPServer struct {
	Type string `json:"type"`
	URL  string `json:"url"`
}

// MCPConfig is the file's shape.
type MCPConfig struct {
	Servers map[string]MCPServer `json:"mcpServers"`
}

// Errors from writing the agent's MCP configuration.
var (
	ErrNoMCPURL      = errors.New("supervisor: no MCP URL given")
	ErrInsecureMCP   = errors.New("supervisor: refusing to write a plaintext MCP URL")
	ErrForeignConfig = errors.New("supervisor: .mcp.json already exists and is not ours to replace")
)

// WriteMCPConfig puts the workspace's MCP endpoint where the agent will find
// it, and returns the path written.
//
// The whole credential lives inside that URL as a query parameter, which
// shapes everything here:
//
//   - The file is 0600. On a machine with other users, a default-permission
//     file hands the workspace token to all of them.
//   - The URL is never logged, never put in an argv, and never returned in an
//     error. A token in a command line is visible in `ps` to every user on the
//     box, which would undo the file permission entirely.
//   - An existing file is merged rather than replaced, and an entry under our
//     own name that points somewhere else is an error rather than an
//     overwrite. A person's own .mcp.json is theirs.
//
// "Points somewhere else" compares the endpoint and deliberately ignores the
// query string, because the query string is the credential and it is *meant*
// to change: every launch mints a fresh, short-lived token. Comparing whole
// URLs made the first launch into a folder succeed and every launch after it
// fail — found by launching an agent twice.
func WriteMCPConfig(dir, serverName, mcpURL string) (string, error) {
	if strings.TrimSpace(mcpURL) == "" {
		return "", ErrNoMCPURL
	}
	u, err := url.Parse(mcpURL)
	if err != nil {
		// Deliberately not including the URL: it holds the token.
		return "", fmt.Errorf("supervisor: MCP URL is not usable")
	}
	if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
		// The token is in the query string, so plain HTTP puts it on the wire
		// in clear. Loopback has no wire to be on.
		return "", fmt.Errorf("%w: %s is not https", ErrInsecureMCP, u.Hostname())
	}
	if err := checkParam("serverName", serverName); err != nil {
		return "", err
	}

	path := filepath.Join(dir, MCPConfigName)
	cfg, existed, err := readMCPConfig(path)
	if err != nil {
		return "", err
	}
	if existing, ok := cfg.Servers[serverName]; ok && !sameEndpoint(existing.URL, mcpURL) {
		// Somebody else's entry under the same name, pointing at a different
		// place. Overwriting it silently would break whatever they had it
		// pointed at.
		return "", fmt.Errorf("%w: %s already defines %q", ErrForeignConfig, path, serverName)
	}

	cfg.Servers[serverName] = MCPServer{Type: "http", URL: mcpURL}

	body, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return "", fmt.Errorf("supervisor: encode %s: %w", MCPConfigName, err)
	}
	body = append(body, '\n')

	// Written through a temp file and renamed, so an interrupted write leaves
	// the previous config intact rather than a truncated one the agent cannot
	// parse. Created 0600 before anything is written to it.
	tmp, err := os.CreateTemp(dir, ".mcp-*.json")
	if err != nil {
		return "", fmt.Errorf("supervisor: create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("supervisor: chmod temp config: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("supervisor: write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("supervisor: close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return "", fmt.Errorf("supervisor: replace %s: %w", path, err)
	}
	// Rename preserves the temp file's mode, but an existing file replaced by
	// rename keeps the new inode — so this is belt and braces for the case
	// where the umask or the platform surprises us.
	if err := os.Chmod(path, 0o600); err != nil {
		return "", fmt.Errorf("supervisor: chmod %s: %w", path, err)
	}

	_ = existed
	return path, nil
}

// HasMCPConfig reports whether a folder already has a usable entry for a
// server.
//
// Asked when a session is being restored after an update. The note that
// survives a restart deliberately carries no MCP URL — the credential is
// inside it, and writing that to disk to survive a restart would turn a
// short-lived token into a file — so a restored agent reads the config that
// was already in its folder before the update, written when it was first
// launched.
func HasMCPConfig(dir, serverName string) bool {
	cfg, existed, err := readMCPConfig(filepath.Join(dir, MCPConfigName))
	if err != nil || !existed {
		return false
	}
	entry, ok := cfg.Servers[serverName]
	return ok && entry.URL != ""
}

// readMCPConfig loads an existing config, or an empty one.
//
// A file that exists but cannot be parsed is an error rather than something to
// overwrite: it is somebody's configuration and it is not ours to discard
// because we could not read it.
func readMCPConfig(path string) (MCPConfig, bool, error) {
	cfg := MCPConfig{Servers: map[string]MCPServer{}}

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, false, nil
	}
	if err != nil {
		return cfg, false, fmt.Errorf("supervisor: read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return cfg, true, fmt.Errorf("%w: %s is not valid JSON", ErrForeignConfig, path)
	}
	if cfg.Servers == nil {
		cfg.Servers = map[string]MCPServer{}
	}
	return cfg, true, nil
}

// sameEndpoint reports whether two MCP URLs address the same thing.
//
// Scheme, host and path. Not the query, which is where the token lives and
// which changes on every launch by design — a fresh credential is the point,
// not a sign that somebody else wrote this entry.
func sameEndpoint(a, b string) bool {
	ua, err := url.Parse(a)
	if err != nil {
		return false
	}
	ub, err := url.Parse(b)
	if err != nil {
		return false
	}
	return ua.Scheme == ub.Scheme &&
		strings.EqualFold(ua.Host, ub.Host) &&
		strings.TrimSuffix(ua.Path, "/") == strings.TrimSuffix(ub.Path, "/")
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}
