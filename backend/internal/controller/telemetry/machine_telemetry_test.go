// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"gorm.io/gorm"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// Machines, their agent sessions, and somebody watching one.
//
// The mapping is the whole of this: an action that reaches the controller
// without a case here is dropped in silence, which is the failure this file
// exists to make impossible — nothing else in the system would report it, and
// the metric would simply read zero forever.
func TestMachineActionsPersistWithDistinctIDs(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	if err := db.AutoMigrate(&model.Telemetry{}); err != nil {
		t.Fatalf("failed to migrate: %v", err)
	}

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockPubSub := mock_pubsub.NewMockService(ctrl)
	crudChan := make(chan any, 20)
	mcpChan := make(chan any, 10)
	mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
		Return(&pubsub.SubscribeResponse{Events: crudChan}, nil)
	mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).
		Return(&pubsub.SubscribeResponse{Events: mcpChan}, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := New(Params{
		DB:        &testDBConn{db: db},
		PubSub:    mockPubSub,
		BatchSize: 2,
		Interval:  50 * time.Millisecond,
	})
	if err := c.Start(ctx); err != nil {
		t.Fatalf("failed to start: %v", err)
	}

	// The workspace column is deliberately different per case: a machine
	// belongs to an account and has no workspace, while a session knows the
	// one it is working in. Both have to survive the mapping as sent.
	cases := []struct {
		action    entity.Action
		stored    uint8
		workspace int64
		name      string
	}{
		{entity.ActionMachineEnrolCodeCreate, model.ActionIDMachineEnrolCodeCreate, 0, "enrolment code created"},
		{entity.ActionMachineRestart, model.ActionIDMachineRestart, 0, "machine restarted"},
		{entity.ActionMachineUpdate, model.ActionIDMachineUpdate, 0, "machine updated"},
		{entity.ActionMachineAdd, model.ActionIDMachineAdd, 0, "machine added"},
		{entity.ActionMachineRemove, model.ActionIDMachineRemove, 0, "machine removed"},
		{entity.ActionMachineDisable, model.ActionIDMachineDisable, 0, "machine disabled"},
		{entity.ActionMachineEnable, model.ActionIDMachineEnable, 0, "machine enabled"},
		{entity.ActionMachineSessionCreate, model.ActionIDMachineSessionCreate, 70, "session created"},
		{entity.ActionMachineSessionOpen, model.ActionIDMachineSessionOpen, 70, "session opened"},
		{entity.ActionMachineSessionClose, model.ActionIDMachineSessionClose, 70, "session closed"},
		{entity.ActionMachineSessionKill, model.ActionIDMachineSessionKill, 70, "session killed"},
		{entity.ActionMachineTerminalOpen, model.ActionIDMachineTerminalOpen, 70, "terminal opened"},
		{entity.ActionMachineTerminalClose, model.ActionIDMachineTerminalClose, 70, "terminal closed"},
	}

	for _, tc := range cases {
		crudChan <- entity.CRUDEvent{
			UserID: 7, WorkspaceID: tc.workspace,
			Action: tc.action, Actor: entity.ActorHuman,
		}
	}

	time.Sleep(600 * time.Millisecond)
	c.Close()

	seen := map[uint8]string{}
	for _, tc := range cases {
		var rows []model.Telemetry
		if err := db.Where("action = ?", tc.stored).Find(&rows).Error; err != nil {
			t.Fatalf("%s: query failed: %v", tc.name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: expected 1 row, got %d — an unmapped action is dropped in silence", tc.name, len(rows))
		}
		if rows[0].UserID != 7 {
			t.Errorf("%s: user = %d, want 7", tc.name, rows[0].UserID)
		}
		if rows[0].WorkspaceID != tc.workspace {
			t.Errorf("%s: workspace = %d, want %d", tc.name, rows[0].WorkspaceID, tc.workspace)
		}
		if other, clash := seen[tc.stored]; clash {
			t.Errorf("%s and %s share stored id %d", tc.name, other, tc.stored)
		}
		seen[tc.stored] = tc.name
	}

	// And they must not collide with anything already stored, which would
	// silently merge two metrics that were never the same thing. These values
	// are in the database, so a collision is not fixable after the fact.
	for _, existing := range []uint8{
		model.ActionIDTaskCreate,
		model.ActionIDUIShortcutUse,
		model.ActionIDUITrajectoryView,
		model.ActionIDAgentModelSelect,
		model.ActionIDMCPPermissionExtensionAllow,
		model.ActionIDMCPPermissionExtensionDeny,
		model.ActionIDLocalAITitleGenerate,
	} {
		if who, clash := seen[existing]; clash {
			t.Errorf("%s reuses stored id %d, which already means something else", who, existing)
		}
	}
}
