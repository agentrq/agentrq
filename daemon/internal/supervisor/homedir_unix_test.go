// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

//go:build unix

package supervisor

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestAFolderThatCannotBeWrittenToSaysTheFolderIsNotSet(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })

	s := New((&recordingStarter{}).start, 0, 0)
	_, err := s.prepareDir(dir)
	if !errors.Is(err, ErrNoFolder) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err = %v, want ErrNoFolder and a permission error", err)
	}
	if want := "the folder is not set for this workspace: " + dir + " cannot be written to"; !strings.Contains(err.Error(), want) {
		t.Errorf("err = %q, want it to contain %q", err, want)
	}
}

func TestAFolderInAHomeThatCannotBeWrittenToSaysTheFolderIsNotSet(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can write anywhere")
	}
	s := New((&recordingStarter{}).start, 0, 0)
	s.Home = t.TempDir()
	if err := os.Chmod(s.Home, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.Home, 0o700) })

	_, err := s.prepareDir("~/.agentrq/0kCWYxrVpuj")
	if !errors.Is(err, ErrNoFolder) || !errors.Is(err, os.ErrPermission) {
		t.Fatalf("err = %v, want ErrNoFolder and a permission error", err)
	}
	if !strings.Contains(err.Error(), "could not be made") {
		t.Errorf("err = %q", err)
	}
}
