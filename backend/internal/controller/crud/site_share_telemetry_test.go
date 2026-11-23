// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestRecordSiteShare(t *testing.T) {
	for _, tc := range []struct {
		shared bool
		want   entity.Action
	}{{true, entity.ActionSiteShare}, {false, entity.ActionSiteUnshare}} {
		env := newMachineTelemetryEnv(t)
		env.controller.RecordSiteShare(context.Background(), entity.RecordSiteShareRequest{UserID: 7, WorkspaceID: 70, Shared: tc.shared})
		e := env.only(t)
		if e.Action != tc.want || e.UserID != 7 || e.WorkspaceID != 70 || e.ResourceType != entity.ResourceWorkspace || e.ResourceID != 70 || e.Actor != entity.ActorHuman {
			t.Errorf("shared=%v: event = %+v", tc.shared, e)
		}
	}
}

// No user is no one to count for, as with RecordTerminalView.
func TestRecordSiteShareWithoutUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.controller.RecordSiteShare(context.Background(), entity.RecordSiteShareRequest{WorkspaceID: 70, Shared: true})
	if len(*env.events) != 0 {
		t.Fatalf("published %+v for no user", *env.events)
	}
}
