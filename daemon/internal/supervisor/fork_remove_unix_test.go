// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

//go:build unix

package supervisor

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRemovingAFolderThatCannotBeDeletedSaysSo(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root deletes read-only folders")
	}
	home := t.TempDir()
	locked := filepath.Join(home, ".agentrq", "forks", forkID, "locked")
	writeFile(t, filepath.Join(locked, "f"), "x")
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })

	if err := RemoveForkDir(home, forkID); err == nil {
		t.Error("a folder that could not be deleted was reported as deleted")
	}
}
