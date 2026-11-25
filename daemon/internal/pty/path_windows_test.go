// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package pty

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// The registry's PATH, expanded: every Windows machine's holds System32, and
// stored as %SystemRoot%\system32 it must come back as a real folder.
func TestSystemPathReadsTheRegistry(t *testing.T) {
	system32 := filepath.Join(os.Getenv("SystemRoot"), "System32")
	found := false
	for _, dir := range filepath.SplitList(systemPath()) {
		if strings.EqualFold(filepath.Clean(dir), system32) {
			found = true
		}
	}
	if !found {
		t.Errorf("systemPath() = %q, want it to hold %s", systemPath(), system32)
	}
	if got := registryPath(registry.CURRENT_USER, `Software\definitely-not-a-key`); got != "" {
		t.Errorf("missing key: got %q, want empty", got)
	}
	if got := registryPath(registry.LOCAL_MACHINE, `SOFTWARE\Microsoft\Windows NT\CurrentVersion`); got != "" {
		t.Errorf("key with no Path value: got %q, want empty", got)
	}
}
