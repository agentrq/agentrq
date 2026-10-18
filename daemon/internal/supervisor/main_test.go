// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// TestMain silences the default logger for the whole package.
//
// A supervisor with no logger of its own falls back to it, and several tests
// launch into a folder that already has a config — which is a thing the
// daemon says out loud. Left alone, those lines go to stderr and interleave
// with the test output, so a real failure arrives buried in notices about
// temp directories.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}
