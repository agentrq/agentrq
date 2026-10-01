// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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

// MCPEntry is a server to write, named.
//
// A slice of these rather than one name and one URL because the "supervisor"
// workspace gets a second entry — the account-wide server — alongside its own,
// and the two have to land in one file: writing them in two passes would mean
// a folder that briefly has only half of what the agent is about to read.
type MCPEntry struct {
	Name string
	URL  string
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

// WriteMCPConfig puts the MCP endpoints the agent is to have where it will
// find them. It returns the path and the names it left alone.
//
// Usually one entry, the workspace's own. A "supervisor" workspace also gets
// the account-wide server, and the backend decides that by sending its URL.
//
// The whole credential lives inside the workspace URL as a query parameter,
// which shapes everything here:
//
//   - The file is 0600. On a machine with other users, a default-permission
//     file hands the workspace token to all of them.
//   - The URL is never logged, never put in an argv, and never returned in an
//     error. A token in a command line is visible in `ps` to every user on the
//     box, which would undo the file permission entirely.
//   - **An entry that is already there is never rewritten**, whatever it
//     points at, and its name comes back in `kept` so the daemon can say so.
//     A person's own .mcp.json is theirs, and a folder that already names
//     these servers has been set up by somebody who meant it.
//     A fork's folder is the exception: see [WriteForkMCPConfig].
//
// The cost of that rule, and it is worth knowing before changing it back: a
// relaunch into a folder that already has an entry reuses the token that entry
// carries rather than the one this launch minted. A workspace token is good
// for a year, so in practice the old one keeps working — but a workspace whose
// entry was written against a different deployment stays pointed there, and
// the way to move it is to delete the entry.
//
// Everything else in the file is written back as it was read: the other
// servers with all their fields, and any other key at the top.
func WriteMCPConfig(dir string, entries ...MCPEntry) (path string, kept []string, err error) {
	path, kept, _, err = writeMCPConfig(dir, false, entries)
	return path, kept, err
}

// WriteForkMCPConfig is [WriteMCPConfig] for a fork's own folder, where an
// entry that points at another endpoint is replaced rather than kept, and its
// name comes back in replaced. The folder is the daemon's and not somebody's
// setup, and an entry from a repository that commits its .mcp.json would
// connect the fork as the workspace it was forked from. An entry at this
// launch's own endpoint is an earlier launch's, and is kept like any other.
func WriteForkMCPConfig(dir string, entries ...MCPEntry) (path string, kept, replaced []string, err error) {
	return writeMCPConfig(dir, true, entries)
}

func writeMCPConfig(dir string, replaceElsewhere bool, entries []MCPEntry) (path string, kept, replaced []string, err error) {
	if len(entries) == 0 {
		return "", nil, nil, ErrNoMCPURL
	}

	path = filepath.Join(dir, MCPConfigName)
	cfg, raw, _, err := loadMCPConfig(path)
	if err != nil {
		return "", nil, nil, err
	}

	var added int
	for _, e := range entries {
		if strings.TrimSpace(e.URL) == "" {
			return "", nil, nil, ErrNoMCPURL
		}
		u, parseErr := url.Parse(e.URL)
		if parseErr != nil {
			// Deliberately not including the URL: it holds the token.
			return "", nil, nil, fmt.Errorf("supervisor: MCP URL is not usable")
		}
		if u.Scheme != "https" && !isLoopbackHost(u.Hostname()) {
			// The token is in the query string, so plain HTTP puts it on the
			// wire in clear. Loopback has no wire to be on.
			return "", nil, nil, fmt.Errorf("%w: %s is not https", ErrInsecureMCP, u.Hostname())
		}
		if checkErr := checkParam("serverName", e.Name); checkErr != nil {
			return "", nil, nil, checkErr
		}
		if existing, ok := cfg.Servers[e.Name]; ok && existing.URL != "" {
			if !replaceElsewhere || !endpointDiffers(existing.URL, e.URL) {
				kept = append(kept, e.Name)
				continue
			}
			replaced = append(replaced, e.Name)
		}
		raw.servers[e.Name], _ = json.Marshal(MCPServer{Type: "http", URL: e.URL}) // two strings: cannot fail
		added++
	}

	// Nothing to add means nothing to write: rewriting a file to the bytes it
	// already holds still changes its timestamp, and on a folder somebody is
	// watching with a file watcher that is a change they have to explain.
	if added == 0 {
		return path, kept, replaced, nil
	}

	// Neither can fail: every value is JSON that was just read or encoded.
	raw.top["mcpServers"], _ = json.Marshal(raw.servers)
	body, _ := json.MarshalIndent(raw.top, "", "  ")
	if err := writePrivate(dir, path, ".mcp-*.json", append(body, '\n')); err != nil {
		return "", nil, nil, err
	}
	return path, kept, replaced, nil
}

// rawMCPConfig is the file as it was read, every field of it, so writing our
// entries back does not drop what this daemon has no struct for: a stdio
// server's command and args, a server's headers, a key besides mcpServers.
type rawMCPConfig struct {
	top     map[string]json.RawMessage
	servers map[string]json.RawMessage
}

// endpointDiffers reports whether two MCP URLs name different endpoints. The
// query, which holds the token, is not compared: an entry an earlier launch
// wrote with an older token is still this workspace's.
func endpointDiffers(got, want string) bool {
	g, gotErr := url.Parse(got)
	w, wantErr := url.Parse(want)
	return gotErr != nil || wantErr != nil ||
		g.Scheme != w.Scheme || g.Host != w.Host || g.Path != w.Path
}

// writePrivate replaces a file with content only its owner can read.
//
// Written through a temp file in the same directory and renamed, so an
// interrupted write leaves the previous file intact rather than a truncated
// one the agent cannot parse. Created 0600 before anything is written to it,
// because both files this is used for are read by an agent on a machine that
// may have other people on it.
func writePrivate(dir, path, pattern string, body []byte) error {
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return fmt.Errorf("supervisor: create temp config: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("supervisor: chmod temp config: %w", err)
	}
	if _, err := tmp.Write(body); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("supervisor: write temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("supervisor: close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("supervisor: replace %s: %w", path, err)
	}
	// Rename preserves the temp file's mode, but an existing file replaced by
	// rename keeps the new inode — so this is belt and braces for the case
	// where the umask or the platform surprises us.
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("supervisor: chmod %s: %w", path, err)
	}
	return nil
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
	cfg, _, existed, err := loadMCPConfig(path)
	return cfg, existed, err
}

// loadMCPConfig is [readMCPConfig], with the file as it was read besides.
func loadMCPConfig(path string) (MCPConfig, rawMCPConfig, bool, error) {
	cfg := MCPConfig{Servers: map[string]MCPServer{}}
	raw := rawMCPConfig{top: map[string]json.RawMessage{}, servers: map[string]json.RawMessage{}}

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, raw, false, nil
	}
	if err != nil {
		return cfg, raw, false, fmt.Errorf("supervisor: read %s: %w", path, err)
	}
	if json.Unmarshal(b, &cfg) != nil || json.Unmarshal(b, &raw.top) != nil {
		return cfg, raw, true, fmt.Errorf("%w: %s is not valid JSON", ErrForeignConfig, path)
	}
	// Accepted already, as the typed read's servers.
	_ = json.Unmarshal(raw.top["mcpServers"], &raw.servers)
	if cfg.Servers == nil {
		cfg.Servers = map[string]MCPServer{}
	}
	if raw.top == nil { // the file said null
		raw.top = map[string]json.RawMessage{}
	}
	if raw.servers == nil {
		raw.servers = map[string]json.RawMessage{}
	}
	return cfg, raw, true, nil
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return strings.HasPrefix(host, "127.")
}
