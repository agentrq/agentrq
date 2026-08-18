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
	"github.com/golang/mock/gomock"
	"gorm.io/driver/sqlite"
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

		time.Sleep(300 * time.Millisecond)

		var count int64
		db.Model(&model.Telemetry{}).Count(&count)
		if count != 7 {
			t.Errorf("expected 7 telemetry records, got %d", count)
		}

		var records []model.Telemetry
		db.Find(&records)
		manualFound := false
		rejectFound := false
		denyFound := false
		connectFound := false
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
