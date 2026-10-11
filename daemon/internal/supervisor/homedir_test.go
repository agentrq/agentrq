// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/daemon/wire"
)

func TestATildeIsThisMachinesHome(t *testing.T) {
	home := t.TempDir()
	for given, want := range map[string]string{
		"~":                   home,
		"~/.agentrq/0kCWYxrV": filepath.Join(home, ".agentrq", "0kCWYxrV"),
		`~\.agentrq\0kCWYxrV`: filepath.Join(home, `.agentrq\0kCWYxrV`),
		" ~/app ":             filepath.Join(home, "app"),
		"/srv/app":            "/srv/app",
		"~other/app":          "~other/app",
	} {
		if got := expandTilde(given, home); got != want {
			t.Errorf("expandTilde(%q) = %q, want %q", given, got, want)
		}
	}
}

// A workspace created without a folder is given ~/.agentrq/<id>, which is
// made on its first launch.
func TestAWorkspacesDefaultFolderIsMadeInHome(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	req := claudeRequest(t, 1)
	req.Dir = "~/.agentrq/0kCWYxrVpuj"

	sess, err := s.Start(t.Context(), "work", req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	want := filepath.Join(s.Home, ".agentrq", "0kCWYxrVpuj")
	if sess.Dir != want {
		t.Errorf("session dir = %q, want %q", sess.Dir, want)
	}
	if spec, _ := st.last(); spec.Dir != want {
		t.Errorf("process dir = %q, want %q", spec.Dir, want)
	}
	if !strings.Contains(readFile(t, filepath.Join(want, MCPConfigName)), testURL) {
		t.Error("the MCP config was not written into the new folder")
	}
	entries, _ := os.ReadDir(want)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".agentrq-write-") {
			t.Errorf("the write check left %s behind", e.Name())
		}
	}
}

func TestAMissingWorkingDirectoryIsMade(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	req := claudeRequest(t, 1)
	req.Dir = filepath.Join(t.TempDir(), "projects", "app")

	sess, err := s.Start(t.Context(), "work", req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if info, err := os.Stat(req.Dir); err != nil || !info.IsDir() {
		t.Fatalf("the folder was not made: %v", err)
	}
	if sess.Dir != req.Dir {
		t.Errorf("session dir = %q, want %q", sess.Dir, req.Dir)
	}
}

func TestAnExistingFolderIsKeptAsItIs(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes.md"), "kept")
	got, err := s.prepareDir(dir)
	if err != nil || got != dir {
		t.Fatalf("prepareDir = %q, %v", got, err)
	}
	if readFile(t, filepath.Join(dir, "notes.md")) != "kept" {
		t.Error("what was in the folder was not kept")
	}
}

func TestAHomeFolderWithNoHomeIsRefused(t *testing.T) {
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "")
	s := New((&recordingStarter{}).start, 0, 0)
	req := claudeRequest(t, 1)
	req.Dir = "~/.agentrq/0kCWYxrVpuj"
	if _, err := s.Start(t.Context(), "work", req); err == nil {
		t.Fatal("started with no home to put the folder in")
	}
}

// A fork of a workspace whose folder was never made is made from it, empty.
func TestAForkOfAFolderThatWasNeverMadeMakesItFirst(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	req := claudeRequest(t, 1)
	req.Fork = &wire.ForkSpec{ID: forkID, From: "~/.agentrq/0kCWYxrVpuj"}

	sess, err := s.Start(t.Context(), "work", req)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	if want := filepath.Join(s.Home, ".agentrq", "forks", forkID); sess.Dir != want {
		t.Errorf("session dir = %q, want %q", sess.Dir, want)
	}
	if _, err := os.Stat(filepath.Join(s.Home, ".agentrq", "0kCWYxrVpuj")); err != nil {
		t.Errorf("the parent's folder was not made: %v", err)
	}
}

func TestAForkFromAFolderThatCannotBeMadeIsRefused(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	s.Home = t.TempDir()
	writeFile(t, filepath.Join(s.Home, ".agentrq"), "")
	req := claudeRequest(t, 1)
	req.Fork = &wire.ForkSpec{ID: forkID, From: "~/.agentrq/0kCWYxrVpuj"}
	if _, err := s.Start(t.Context(), "work", req); err == nil || !strings.Contains(err.Error(), ErrNoFolder.Error()) {
		t.Fatalf("err = %v, want ErrNoFolder", err)
	}
}

func TestAFolderHoldingDotDotIsRefused(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)
	if _, err := s.prepareDir(dotDotDir(t)); !errors.Is(err, ErrBadParameter) {
		t.Fatalf("err = %v, want ErrBadParameter", err)
	}
}
