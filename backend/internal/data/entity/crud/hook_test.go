// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import "testing"

func TestHookErrorIsItsMessage(t *testing.T) {
	if got := (&HookError{Kind: HookConflict, Message: "registered"}).Error(); got != "registered" {
		t.Errorf("Error() = %q", got)
	}
}
