// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

// A folder the config cannot be written into says so, and the file that was
// there is left as it was.
func TestWriteMCPConfigIntoAFolderItCannotWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root writes into a read-only folder anyway")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, MCPConfigName)
	writeFile(t, path, `{"mcpServers":{}}`)
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	if _, _, err := WriteMCPConfig(dir, ws(testURL)); err == nil {
		t.Fatal("a config was reported written into a read-only folder")
	}
	if readFile(t, path) != `{"mcpServers":{}}` {
		t.Error("the existing file was changed")
	}
}

// A config that cannot be read is an error, not an empty file to write over.
func TestWriteMCPConfigOverAFileItCannotRead(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, MCPConfigName), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, _, err := WriteMCPConfig(dir, ws(testURL)); err == nil {
		t.Fatal("a folder named .mcp.json was taken for an empty config")
	}
}
