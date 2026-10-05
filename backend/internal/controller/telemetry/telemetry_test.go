// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package telemetry

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/glebarez/sqlite"
	"github.com/golang/mock/gomock"
	"gorm.io/gorm"
)

type testDBConn struct {
	db *gorm.DB
}

func (t *testDBConn) Conn(ctx context.Context) *gorm.DB { return t.db }
func (t *testDBConn) Close(ctx context.Context)         {}

func TestTelemetryController(t *testing.T) {
	// Setup in-memory SQLite
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("failed to open database: %v", err)
	}
	db.AutoMigrate(&model.Telemetry{})

	dbConn := &testDBConn{db: db}

	t.Run("StartAndProcessEvents", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockPubSub := mock_pubsub.NewMockService(ctrl)

		crudChan := make(chan any, 10)
		mcpChan := make(chan any, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).Return(&pubsub.SubscribeResponse{Events: crudChan}, nil)
		mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).Return(&pubsub.SubscribeResponse{Events: mcpChan}, nil)

		c := New(Params{
			DB:        dbConn,
			PubSub:    mockPubSub,
			BatchSize: 2,
			Interval:  100 * time.Millisecond,
		})

		if err := c.Start(ctx); err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		// Send CRUD Event
		crudChan <- entity.CRUDEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      entity.ActionTaskCreate,
			Actor:       entity.ActorHuman,
		}

		// Send MCP Notification (Manual Approval)
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPNotification,
			Method:      "permission_manual_allow",
			Actor:       uint8(entity.ActorAgent),
		}

		// Send MCP Tool Call
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPToolCall,
			Actor:       uint8(entity.ActorAgent),
		}

		// Send another MCP Tool Call
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPToolCall,
			Actor:       uint8(entity.ActorAgent),
		}

		// Send MCP Connect
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPConnect,
			Actor:       uint8(entity.ActorAgent),
		}

		// Send Rejection (Manual Task)
		crudChan <- entity.CRUDEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      entity.ActionTaskRejectManual,
			Actor:       entity.ActorHuman,
		}

		// Send Permission Deny (MCP)
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPNotification,
			Method:      "permission_manual_deny",
			Actor:       uint8(entity.ActorAgent),
		}

		// Send Clear Context (MCP)
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPClearContext,
			Actor:       uint8(entity.ActorAgent),
		}

		// Send MCP Method Call (a resource or prompt read, e.g. from coremcp)
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPMethodCall,
			Actor:       uint8(entity.ActorAgent),
		}

		time.Sleep(300 * time.Millisecond)

		var count int64
		db.Model(&model.Telemetry{}).Count(&count)
		if count != 9 {
			t.Errorf("expected 9 telemetry records, got %d", count)
		}

		var records []model.Telemetry
		db.Find(&records)
		manualFound := false
		rejectFound := false
		denyFound := false
		connectFound := false
		clearFound := false
		methodCallFound := false
		for _, r := range records {
			if r.Action == model.ActionIDMCPPermissionManual {
				manualFound = true
			}
			if r.Action == model.ActionIDTaskRejectManual {
				rejectFound = true
			}
			if r.Action == model.ActionIDMCPPermissionDeny {
				denyFound = true
			}
			if r.Action == model.ActionIDMCPConnect {
				connectFound = true
			}
			if r.Action == model.ActionIDMCPClearContext {
				clearFound = true
			}
			if r.Action == model.ActionIDMCPMethodCall {
				methodCallFound = true
			}
		}
		if !manualFound {
			t.Errorf("expected model.ActionIDMCPPermissionManual record, but not found")
		}
		if !rejectFound {
			t.Errorf("expected model.ActionIDTaskRejectManual record, but not found")
		}
		if !denyFound {
			t.Errorf("expected model.ActionIDMCPPermissionDeny record, but not found")
		}
		if !connectFound {
			t.Errorf("expected model.ActionIDMCPConnect record, but not found")
		}
		if !clearFound {
			t.Errorf("expected model.ActionIDMCPClearContext record, but not found")
		}
		if !methodCallFound {
			t.Errorf("expected model.ActionIDMCPMethodCall record, but not found")
		}
	})

	t.Run("IntervalFlush", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		defer ctrl.Finish()

		mockPubSub := mock_pubsub.NewMockService(ctrl)
		crudChan := make(chan any, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).Return(&pubsub.SubscribeResponse{Events: crudChan}, nil)
		mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).Return(&pubsub.SubscribeResponse{Events: make(chan any)}, nil)

		c := New(Params{
			DB:        dbConn,
			PubSub:    mockPubSub,
			BatchSize: 100, // Large batch size
			Interval:  50 * time.Millisecond,
		})

		db.Exec("DELETE FROM telemetries") // Clear table

		if err := c.Start(ctx); err != nil {
			t.Fatalf("failed to start: %v", err)
		}

		crudChan <- entity.CRUDEvent{
			UserID:      2,
			WorkspaceID: 20,
			Action:      entity.ActionWorkspaceCreate,
			Actor:       entity.ActorHuman,
		}

		// Wait for interval flush
		time.Sleep(150 * time.Millisecond)

		var count int64
		db.Model(&model.Telemetry{}).Count(&count)
		if count != 1 {
			t.Errorf("expected 1 record from interval flush, got %d", count)
		}
	})
}

// A tool call and a method call for two different tools must not just persist
// as generic ActionIDMCPToolCall/ActionIDMCPMethodCall rows — the whole point
// of adding SubActionID was to tell them apart afterwards.
func TestRecordMCP_PersistsSubActionID(t *testing.T) {
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
	mcpChan := make(chan any, 10)
	mockPubSub.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
		Return(&pubsub.SubscribeResponse{Events: make(chan any)}, nil)
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

	mcpChan <- mcp.MCPEvent{
		UserID: 1, WorkspaceID: 10,
		Action: mcp.ActionMCPToolCall, ToolName: "getTask",
		Actor: uint8(entity.ActorAgent),
	}
	mcpChan <- mcp.MCPEvent{
		UserID: 1, WorkspaceID: 10,
		Action: mcp.ActionMCPMethodCall, ToolName: "resource:new-workspace-guide",
		Actor: uint8(entity.ActorAgent),
	}

	time.Sleep(150 * time.Millisecond)

	var records []model.Telemetry
	db.Find(&records)
	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	var gotToolCall, gotMethodCall bool
	for _, r := range records {
		switch r.Action {
		case model.ActionIDMCPToolCall:
			gotToolCall = true
			if r.SubActionID != model.SubActionIDMCPGetTask {
				t.Errorf("tool call SubActionID = %d, want SubActionIDMCPGetTask (%d)", r.SubActionID, model.SubActionIDMCPGetTask)
			}
		case model.ActionIDMCPMethodCall:
			gotMethodCall = true
			if r.SubActionID != model.SubActionIDMCPResourceNewWorkspaceGuide {
				t.Errorf("method call SubActionID = %d, want SubActionIDMCPResourceNewWorkspaceGuide (%d)", r.SubActionID, model.SubActionIDMCPResourceNewWorkspaceGuide)
			}
		}
	}
	if !gotToolCall || !gotMethodCall {
		t.Fatalf("missing expected rows: toolCall=%v methodCall=%v", gotToolCall, gotMethodCall)
	}
}

// The client-reported actions have to survive the whole path — pubsub event to
// persisted row — with their scoping intact, since a row that loses its user or
// workspace cannot be shown back to the user it belongs to.
func TestLocalAIActionsPersistScopedToUserAndWorkspace(t *testing.T) {
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
	crudChan := make(chan any, 10)
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

	crudChan <- entity.CRUDEvent{
		UserID: 7, WorkspaceID: 70,
		Action: entity.ActionLocalAITitleGenerate, Actor: entity.ActorHuman,
	}
	crudChan <- entity.CRUDEvent{
		UserID: 7, WorkspaceID: 70,
		Action: entity.ActionLocalAIRecordingEnd, Actor: entity.ActorHuman,
	}

	time.Sleep(300 * time.Millisecond)
	c.Close()

	for _, tc := range []struct {
		action uint8
		name   string
	}{
		{model.ActionIDLocalAITitleGenerate, "title generate"},
		{model.ActionIDLocalAIRecordingEnd, "recording end"},
	} {
		var rows []model.Telemetry
		if err := db.Where("action = ?", tc.action).Find(&rows).Error; err != nil {
			t.Fatalf("%s: query failed: %v", tc.name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: expected 1 row, got %d", tc.name, len(rows))
		}
		if rows[0].UserID != 7 || rows[0].WorkspaceID != 70 {
			t.Errorf("%s: expected user 7 / workspace 70, got user %d / workspace %d",
				tc.name, rows[0].UserID, rows[0].WorkspaceID)
		}
		if rows[0].Actor != uint8(entity.ActorHuman) {
			t.Errorf("%s: expected a human actor, got %d", tc.name, rows[0].Actor)
		}
		if rows[0].OccurredAt == 0 {
			t.Errorf("%s: OccurredAt should be stamped by the server", tc.name)
		}
	}

	// The two must not collide: distinct action ids are what makes them
	// separate metrics rather than one.
	if model.ActionIDLocalAITitleGenerate == model.ActionIDLocalAIRecordingEnd {
		t.Error("the two local-AI actions share an id")
	}
}

// Every interface-usage action must reach a stored row with its own id.
//
// The mapping is a switch with a silent `default: return`, so an action added
// to the allowlist but not to that switch is accepted by the API, reported by
// the browser, and then dropped — a metric that reads as zero rather than as
// broken. This is the test that fails instead.
func TestUIActionsPersistWithDistinctIDs(t *testing.T) {
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
	crudChan := make(chan any, 10)
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

	cases := []struct {
		action entity.Action
		stored uint8
		name   string
	}{
		{entity.ActionUIShortcutUse, model.ActionIDUIShortcutUse, "shortcut use"},
		{entity.ActionUISearch, model.ActionIDUISearch, "search"},
		{entity.ActionUISearchOpen, model.ActionIDUISearchOpen, "search open"},
		{entity.ActionUICopyLink, model.ActionIDUICopyLink, "copy link"},
		{entity.ActionUICopyMarkdown, model.ActionIDUICopyMarkdown, "copy markdown"},
		{entity.ActionUITrajectoryView, model.ActionIDUITrajectoryView, "trajectory view"},
		{entity.ActionUICopyCode, model.ActionIDUICopyCode, "copy code"},
		{entity.ActionUIDictationEnd, model.ActionIDUIDictationEnd, "dictation end"},
		// Backend-emitted rather than browser-reported, unlike everything above
		// it, but it travels the same bus and needs the same mapping — an
		// action that reaches here unmapped is dropped in silence.
		{entity.ActionAgentModelSelect, model.ActionIDAgentModelSelect, "agent model select"},
		{entity.ActionAgentConcurrencySelect, model.ActionIDAgentConcurrencySelect, "agent concurrency select"},
		{entity.ActionAgentLaunchClaudeCode, model.ActionIDAgentLaunchClaudeCode, "agent launch claude-code"},
		{entity.ActionAgentLaunchACPGateway, model.ActionIDAgentLaunchACPGateway, "agent launch acp-gateway"},
		{entity.ActionSkillImport, model.ActionIDSkillImport, "skill import"},
		{entity.ActionSkillView, model.ActionIDSkillView, "skill view"},
		{entity.ActionSkillSearch, model.ActionIDSkillSearch, "skill search"},
		{entity.ActionTaskFork, model.ActionIDTaskFork, "task fork"},
		{entity.ActionSiteShare, model.ActionIDSiteShare, "site share"},
		{entity.ActionSiteUnshare, model.ActionIDSiteUnshare, "site unshare"},
		{entity.ActionWorkspaceForkCreate, model.ActionIDWorkspaceForkCreate, "workspace fork create"},
		{entity.ActionWorkspaceForkMerge, model.ActionIDWorkspaceForkMerge, "workspace fork merge"},
		{entity.ActionUISpinUp, model.ActionIDUISpinUp, "spin up"},
		{entity.ActionUISidePanelOpen, model.ActionIDUISidePanelOpen, "side panel open"},
		{entity.ActionUISidePanelLink, model.ActionIDUISidePanelLink, "side panel link"},
		{entity.ActionOAuthConsentAllow, model.ActionIDOAuthConsentAllow, "oauth consent allow"},
		{entity.ActionOAuthConsentDeny, model.ActionIDOAuthConsentDeny, "oauth consent deny"},
		{entity.ActionTaskTitleUpdate, model.ActionIDTaskTitleUpdate, "task title update"},
		{entity.ActionSkillEnable, model.ActionIDSkillEnable, "skill enable"},
		{entity.ActionSkillDisable, model.ActionIDSkillDisable, "skill disable"},
	}

	for _, tc := range cases {
		crudChan <- entity.CRUDEvent{
			UserID: 7, WorkspaceID: 70,
			Action: tc.action, Actor: entity.ActorHuman,
		}
	}

	time.Sleep(400 * time.Millisecond)
	c.Close()

	seen := map[uint8]string{}
	for _, tc := range cases {
		var rows []model.Telemetry
		if err := db.Where("action = ?", tc.stored).Find(&rows).Error; err != nil {
			t.Fatalf("%s: query failed: %v", tc.name, err)
		}
		if len(rows) != 1 {
			t.Fatalf("%s: expected 1 row, got %d", tc.name, len(rows))
		}
		if rows[0].UserID != 7 || rows[0].WorkspaceID != 70 {
			t.Errorf("%s: expected user 7 / workspace 70, got user %d / workspace %d",
				tc.name, rows[0].UserID, rows[0].WorkspaceID)
		}
		if other, clash := seen[tc.stored]; clash {
			t.Errorf("%s and %s share stored id %d", tc.name, other, tc.stored)
		}
		seen[tc.stored] = tc.name
	}

	// And they must not collide with the actions already stored, which would
	// silently merge two metrics that were never the same thing.
	for _, existing := range []uint8{model.ActionIDLocalAITitleGenerate, model.ActionIDLocalAIRecordingEnd, model.ActionIDTaskCreate} {
		if _, clash := seen[existing]; clash {
			t.Errorf("a UI action reuses stored id %d", existing)
		}
	}
}
