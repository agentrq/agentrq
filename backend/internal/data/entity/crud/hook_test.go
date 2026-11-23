// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import "testing"

func TestHookErrorIsItsMessage(t *testing.T) {
	if got := (&HookError{Kind: HookConflict, Message: "registered"}).Error(); got != "registered" {
		t.Errorf("Error() = %q", got)
	}
}

func TestAppConfigurationErrorIsItsMessage(t *testing.T) {
	if got := (&AppConfigurationError{Message: "memory.maxFileSize: -1 is negative"}).Error(); got != "memory.maxFileSize: -1 is negative" {
		t.Errorf("Error() = %q", got)
	}
}
