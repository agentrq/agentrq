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
func (t *testDBConn) Close(ctx context.Context)          {}

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

		// Send MCP Event
		mcpChan <- mcp.MCPEvent{
			UserID:      1,
			WorkspaceID: 10,
			Action:      mcp.ActionMCPToolCall,
			Actor:       uint8(entity.ActorAgent),
		}

		// Wait for batch flush (BatchSize is 2)
		time.Sleep(200 * time.Millisecond)

		var count int64
		db.Model(&model.Telemetry{}).Count(&count)
		if count != 2 {
			t.Errorf("expected 2 telemetry records, got %d", count)
		}

		var records []model.Telemetry
		db.Find(&records)
		for _, r := range records {
			if r.WorkspaceID != 10 || r.UserID != 1 {
				t.Errorf("invalid record data: %+v", r)
			}
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
