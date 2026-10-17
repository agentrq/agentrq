// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestRecordACPV1(t *testing.T) {
	for _, tc := range []struct {
		turn, logout bool
		want         entity.Action
	}{{false, false, entity.ActionACPV1Connect}, {true, false, entity.ActionACPV1Turn}, {false, true, entity.ActionACPV1Logout}} {
		env := newMachineTelemetryEnv(t)
		env.controller.RecordACPV1(context.Background(), entity.RecordACPV1Request{UserID: 7, WorkspaceID: 70, Turn: tc.turn, Logout: tc.logout})
		e := env.only(t)
		if e.Action != tc.want || e.UserID != 7 || e.WorkspaceID != 70 || e.ResourceType != entity.ResourceWorkspace || e.ResourceID != 70 || e.Actor != entity.ActorAgent {
			t.Errorf("turn=%v logout=%v: the event was %+v, want %v for user 7 in workspace 70, by the agent", tc.turn, tc.logout, e, tc.want)
		}
	}
}

// No user is no one to count for, as with RecordSiteShare.
func TestRecordACPV1WithoutUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.controller.RecordACPV1(context.Background(), entity.RecordACPV1Request{WorkspaceID: 70})
	if len(*env.events) != 0 {
		t.Fatalf("published %+v, want nothing for no user", *env.events)
	}
}
