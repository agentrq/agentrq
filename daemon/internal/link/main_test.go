// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package link

import (
	"io"
	"log/slog"
	"os"
	"testing"
)

// TestMain silences the default logger, for the same reason the supervisor's
// tests do: a supervisor with no logger of its own falls back to it, and the
// notices it writes would otherwise interleave with the output of whichever
// test actually failed.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}
