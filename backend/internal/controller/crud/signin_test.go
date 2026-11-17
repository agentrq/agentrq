// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// A session belongs to the account, so it is counted on the person with
// workspace 0.
func TestRecordSignIn(t *testing.T) {
	for _, tc := range []struct {
		signOut bool
		want    entity.Action
	}{{false, entity.ActionUserSignIn}, {true, entity.ActionUserSignOut}} {
		env := newMachineTelemetryEnv(t)
		env.controller.RecordSignIn(context.Background(), entity.RecordSignInRequest{UserID: 7, SignOut: tc.signOut})
		e := env.only(t)
		if e.Action != tc.want || e.UserID != 7 || e.WorkspaceID != 0 || e.ResourceType != entity.ResourceUser || e.ResourceID != 7 || e.Actor != entity.ActorHuman {
			t.Errorf("signOut=%v published %+v, want %s by user 7 with workspace 0", tc.signOut, e, tc.want)
		}
	}
}

// No user is no one to count for, as with RecordOAuthConsent.
func TestRecordSignInWithoutUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.controller.RecordSignIn(context.Background(), entity.RecordSignInRequest{})
	if len(*env.events) != 0 {
		t.Fatalf("published %+v for no user", *env.events)
	}
}
