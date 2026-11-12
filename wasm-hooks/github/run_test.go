// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"errors"
	"os"
	"os/exec"
	"testing"
)

// main runs in a child process, whose first argument is a test flag: an
// unknown command, so the hook exits 2.
func TestMainRunsTheHook(t *testing.T) {
	if os.Getenv("HOOK_TEST_MAIN") == "1" {
		main()
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestMainRunsTheHook$")
	cmd.Env = append(os.Environ(), "HOOK_TEST_MAIN=1")
	var exit *exec.ExitError
	if err := cmd.Run(); !errors.As(err, &exit) || exit.ExitCode() != 2 {
		t.Errorf("err = %v, want exit 2", err)
	}
}
