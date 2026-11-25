// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

//go:build !windows

package pty

import "testing"

func TestSystemPathIsEmptyOffWindows(t *testing.T) {
	if got := systemPath(); got != "" {
		t.Errorf("systemPath() = %q, want empty", got)
	}
}
