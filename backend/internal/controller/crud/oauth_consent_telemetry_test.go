// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

func TestRecordOAuthConsentForAWorkspace(t *testing.T) {
	for _, tc := range []struct {
		allowed bool
		want    entity.Action
	}{{true, entity.ActionOAuthConsentAllow}, {false, entity.ActionOAuthConsentDeny}} {
		env := newMachineTelemetryEnv(t)
		env.controller.RecordOAuthConsent(context.Background(), entity.RecordOAuthConsentRequest{UserID: 7, WorkspaceID: 70, Allowed: tc.allowed})
		e := env.only(t)
		if e.Action != tc.want || e.UserID != 7 || e.WorkspaceID != 70 || e.ResourceType != entity.ResourceWorkspace || e.ResourceID != 70 || e.Actor != entity.ActorHuman {
			t.Errorf("allowed=%v published %+v, want %s by user 7 on workspace 70", tc.allowed, e, tc.want)
		}
	}
}

// The supervisor is granted the whole account, so the answer is about the
// person, not about any one workspace.
func TestRecordOAuthConsentForTheSupervisor(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.controller.RecordOAuthConsent(context.Background(), entity.RecordOAuthConsentRequest{UserID: 7, Allowed: true})
	e := env.only(t)
	if e.Action != entity.ActionOAuthConsentAllow || e.WorkspaceID != 0 || e.ResourceType != entity.ResourceUser || e.ResourceID != 7 {
		t.Errorf("a supervisor consent published %+v, want an allow on user 7 with workspace 0", e)
	}
}

// No user is no one to count for, as with RecordSiteShare.
func TestRecordOAuthConsentWithoutUser(t *testing.T) {
	env := newMachineTelemetryEnv(t)
	env.controller.RecordOAuthConsent(context.Background(), entity.RecordOAuthConsentRequest{WorkspaceID: 70, Allowed: true})
	if len(*env.events) != 0 {
		t.Fatalf("published %+v for no user", *env.events)
	}
}
