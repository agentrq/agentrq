// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import "testing"

func TestTeamErrorIsItsMessage(t *testing.T) {
	if got := (&TeamError{Kind: TeamGone, Message: "no longer valid"}).Error(); got != "no longer valid" {
		t.Errorf("TeamError.Error returned %q, want its message", got)
	}
}
