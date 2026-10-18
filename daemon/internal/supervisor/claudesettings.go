// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Where claude-code reads per-project settings that are not checked in.
const (
	ClaudeSettingsDir  = ".claude"
	ClaudeSettingsName = "settings.local.json"
)

// ErrForeignSettings means the folder's settings file is not ours to replace.
var ErrForeignSettings = errors.New("supervisor: .claude/settings.local.json is not valid JSON")

// WriteClaudeSettings pre-approves the MCP servers this session was given, and
// returns the path written.
//
// Without it an agent stops and asks a human before every MCP call — and on a
// machine started from the panel there is no human at that terminal, so the
// task simply stalls. That is the whole reason this file exists, and it is why
// it is written at launch rather than left to whoever set the folder up.
//
// A server's tools are allowed with one `mcp__<server>__*` rule rather than by
// listing them. The rule matches every tool that server offers, so a tool
// added to the server next month is allowed the day it ships — an enumerated
// list is a copy of the server's catalogue, and a copy that has gone stale
// fails in the worst way available: the agent works until its first call to
// the one missing tool, then waits for somebody who is not there.
//
// The file is the person's, so it is merged and never replaced: rules they
// added stay, keys this does not know about stay, and one that cannot be
// parsed is an error rather than something to overwrite.
func WriteClaudeSettings(dir string, serverNames ...string) (string, error) {
	if len(serverNames) == 0 {
		return "", ErrMissingParam
	}
	for _, name := range serverNames {
		if err := checkParam("serverName", name); err != nil {
			return "", err
		}
	}

	settingsDir := filepath.Join(dir, ClaudeSettingsDir)
	// 0700: the folder holds one person's local settings, and on a shared
	// machine the rest of it is nobody else's business either.
	if err := os.MkdirAll(settingsDir, 0o700); err != nil {
		return "", fmt.Errorf("supervisor: create %s: %w", settingsDir, err)
	}

	path := filepath.Join(settingsDir, ClaudeSettingsName)
	settings, err := readClaudeSettings(path)
	if err != nil {
		return "", err
	}

	allow := make([]string, 0, len(serverNames))
	for _, name := range serverNames {
		allow = append(allow, "mcp__"+name+"__*")
	}
	settings.Permissions.Allow = union(settings.Permissions.Allow, allow)
	settings.EnabledMcpjsonServers = union(settings.EnabledMcpjsonServers, serverNames)
	settings.EnableAllProjectMcpServers = true

	body, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return "", fmt.Errorf("supervisor: encode %s: %w", ClaudeSettingsName, err)
	}
	body = append(body, '\n')

	if err := writePrivate(settingsDir, path, ".settings-*.json", body); err != nil {
		return "", err
	}
	return path, nil
}

// claudeSettings is the part of the file this daemon has an opinion about.
//
// Everything else in it is somebody's configuration and has to survive being
// rewritten, which is what Rest is for: unknown keys are decoded into it and
// written back out unchanged. Without that, writing this file would silently
// delete a person's hooks, model choice or status line.
type claudeSettings struct {
	Permissions                claudePermissions `json:"permissions"`
	EnableAllProjectMcpServers bool              `json:"enableAllProjectMcpServers"`
	EnabledMcpjsonServers      []string          `json:"enabledMcpjsonServers"`

	Rest map[string]json.RawMessage `json:"-"`
}

// claudePermissions keeps deny and ask whole for the same reason.
type claudePermissions struct {
	Allow []string `json:"allow"`

	Rest map[string]json.RawMessage `json:"-"`
}

func (s claudeSettings) MarshalJSON() ([]byte, error) {
	type known claudeSettings
	return mergeJSON(known(s), s.Rest)
}

func (s *claudeSettings) UnmarshalJSON(b []byte) error {
	type known claudeSettings
	var k known
	if err := json.Unmarshal(b, &k); err != nil {
		return err
	}
	*s = claudeSettings(k)
	return splitJSON(b, &s.Rest, "permissions", "enableAllProjectMcpServers", "enabledMcpjsonServers")
}

func (p claudePermissions) MarshalJSON() ([]byte, error) {
	type known claudePermissions
	return mergeJSON(known(p), p.Rest)
}

func (p *claudePermissions) UnmarshalJSON(b []byte) error {
	type known claudePermissions
	var k known
	if err := json.Unmarshal(b, &k); err != nil {
		return err
	}
	*p = claudePermissions(k)
	return splitJSON(b, &p.Rest, "allow")
}

// mergeJSON encodes the known fields and puts the unknown ones back.
func mergeJSON(known any, rest map[string]json.RawMessage) ([]byte, error) {
	b, err := json.Marshal(known)
	if err != nil {
		return nil, err
	}
	if len(rest) == 0 {
		return b, nil
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	for k, v := range rest {
		out[k] = v
	}
	return json.Marshal(out)
}

// splitJSON collects the keys a struct does not name.
func splitJSON(b []byte, rest *map[string]json.RawMessage, known ...string) error {
	var all map[string]json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		return err
	}
	for _, k := range known {
		delete(all, k)
	}
	if len(all) > 0 {
		*rest = all
	}
	return nil
}

// readClaudeSettings loads an existing file, or an empty one.
//
// A file that exists but cannot be parsed is an error rather than something to
// overwrite. It is local settings a person wrote by hand — the likeliest thing
// in the folder to have a trailing comma in it — and losing them to a launch
// that could have said so is not a trade worth making.
func readClaudeSettings(path string) (claudeSettings, error) {
	var s claudeSettings

	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, fmt.Errorf("supervisor: read %s: %w", path, err)
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return s, fmt.Errorf("%w: %s", ErrForeignSettings, path)
	}
	return s, nil
}

// union appends what is missing, in order, and changes nothing that is there.
//
// Order matters only in that a person's own rules keep the positions they had;
// what must not happen is the same rule arriving twice, which is what a plain
// append does on the second launch into the same folder.
func union(have, want []string) []string {
	seen := make(map[string]struct{}, len(have))
	for _, v := range have {
		seen[v] = struct{}{}
	}
	for _, v := range want {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		have = append(have, v)
	}
	return have
}
