// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// stub writes a tiny executable that prints what it is told to.
//
// A shell script rather than a compiled binary: the thing under test is
// "does running this file produce the expected version", and compiling a Go
// program per case would make the test slow for no extra truth.
func stub(t *testing.T, dir, name, prints string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		path := filepath.Join(dir, name+".bat")
		body := "@echo off\r\necho " + prints + "\r\nexit /b " + itoa(exitCode) + "\r\n"
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\necho '" + prints + "'\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	return string(rune('0' + n))
}

func TestSelfTestAcceptsABinaryThatRunsAndIsTheRightVersion(t *testing.T) {
	path := stub(t, t.TempDir(), "agentrqd", "agentrqd 0.7.1 (linux/arm64)", 0)
	if err := SelfTest(context.Background(), path, "0.7.1"); err != nil {
		t.Errorf("SelfTest: %v", err)
	}
}

// This is what makes "never auto-update on a failed start" enforceable rather
// than aspirational: once the swap has happened and this process has gone,
// nothing can observe the new binary failing.
func TestSelfTestRefusesABinaryThatDoesNotRun(t *testing.T) {
	dir := t.TempDir()

	t.Run("it exits non-zero", func(t *testing.T) {
		path := stub(t, dir, "broken", "boom", 1)
		if err := SelfTest(context.Background(), path, "0.7.1"); !errors.Is(err, ErrSelfTestFailed) {
			t.Errorf("error = %v, want ErrSelfTestFailed", err)
		}
	})

	t.Run("it is not there at all", func(t *testing.T) {
		if err := SelfTest(context.Background(), filepath.Join(dir, "nothing"), "0.7.1"); !errors.Is(err, ErrSelfTestFailed) {
			t.Errorf("error = %v, want ErrSelfTestFailed", err)
		}
	})
}

// A binary that runs but is not the release being installed means the manifest
// and the artefact disagree, which is exactly the case where carrying on is
// worst.
func TestSelfTestRefusesTheWrongVersion(t *testing.T) {
	path := stub(t, t.TempDir(), "agentrqd", "agentrqd 0.6.4 (linux/arm64)", 0)
	err := SelfTest(context.Background(), path, "0.7.1")
	if !errors.Is(err, ErrSelfTestFailed) {
		t.Fatalf("error = %v, want ErrSelfTestFailed", err)
	}
	// The reason names both versions, or the log line is useless.
	if got := err.Error(); !contains(got, "0.6.4") || !contains(got, "0.7.1") {
		t.Errorf("error = %q, want both versions named", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
