// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package link

import (
	"io"
	"log/slog"
	"os"
	"testing"
	"time"
)

// TestMain silences the default logger, for the same reason the supervisor's
// tests do: a supervisor with no logger of its own falls back to it, and the
// notices it writes would otherwise interleave with the output of whichever
// test actually failed.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	// Set once, before any session's watcher can read them, and short, so no
	// watcher outlives its test by long.
	devChannelWait, devChannelPoll, devChannelRetry = 5*time.Second, time.Millisecond, 100*time.Millisecond
	os.Exit(m.Run())
}
