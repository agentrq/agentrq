// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package telemetry

import (
	"context"
	"errors"
	"runtime"
	"strings"
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

func openMemDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if len(models) > 0 {
		if err := db.AutoMigrate(models...); err != nil {
			t.Fatalf("migrate: %v", err)
		}
	}
	return db
}

// queued drains what a record call put on the queue, without a worker.
func queued(c *controller) []model.Telemetry {
	var out []model.Telemetry
	for {
		select {
		case r := <-c.queue:
			out = append(out, r)
		default:
			return out
		}
	}
}

func TestNew_Defaults(t *testing.T) {
	c := New(Params{}).(*controller)
	if c.batchSize != 1000 {
		t.Errorf("batchSize = %d, want 1000", c.batchSize)
	}
	if c.interval != 5*time.Second {
		t.Errorf("interval = %v, want 5s", c.interval)
	}

	c = New(Params{BatchSize: 3, Interval: time.Minute}).(*controller)
	if c.batchSize != 3 || c.interval != time.Minute {
		t.Errorf("explicit batchSize/interval = %d/%v, want 3/1m", c.batchSize, c.interval)
	}
}

func TestStart_SubscribeFailures(t *testing.T) {
	t.Run("crud topic", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ps := mock_pubsub.NewMockService(ctrl)
		ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
			Return(nil, errors.New("no crud topic"))

		err := New(Params{PubSub: ps}).Start(context.Background())
		if err == nil || err.Error() != "no crud topic" {
			t.Fatalf("Start error = %v, want the crud subscribe failure", err)
		}
	})

	t.Run("mcp topic", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		ps := mock_pubsub.NewMockService(ctrl)
		ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
			Return(&pubsub.SubscribeResponse{Events: make(chan any)}, nil)
		ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).
			Return(nil, errors.New("no mcp topic"))

		err := New(Params{PubSub: ps}).Start(context.Background())
		if err == nil || err.Error() != "no mcp topic" {
			t.Fatalf("Start error = %v, want the mcp subscribe failure", err)
		}
	})
}

// Closed topics end the consumers, and Close ends the worker.
func TestStart_StopsWhenTopicsClose(t *testing.T) {
	ctrl := gomock.NewController(t)
	ps := mock_pubsub.NewMockService(ctrl)
	crud, mcpCh := make(chan any), make(chan any)
	ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
		Return(&pubsub.SubscribeResponse{Events: crud}, nil)
	ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).
		Return(&pubsub.SubscribeResponse{Events: mcpCh}, nil)

	c := New(Params{DB: &memDBConn{db: openMemDB(t, &model.Telemetry{})}, PubSub: ps, Interval: time.Hour})
	if err := c.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	close(crud)
	close(mcpCh)
	c.Close() // returns once the worker has

	// Other tests leave workers running, so look for the consumers only.
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "telemetry.(*controller).Start.func") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("telemetry goroutines still running after 5s:\n%s", buf[:n])
		}
		runtime.Gosched()
	}
}

func TestRecordCRUD_MapsActions(t *testing.T) {
	cases := []struct {
		action entity.Action
		want   []uint8
	}{
		{entity.ActionWorkspaceCreate, []uint8{model.ActionIDWorkspaceCreate}},
		{entity.ActionWorkspaceUpdate, []uint8{model.ActionIDWorkspaceUpdate}},
		{entity.ActionWorkspaceDelete, []uint8{model.ActionIDWorkspaceDelete}},
		{entity.ActionTaskCreate, []uint8{model.ActionIDTaskCreate}},
		{entity.ActionTaskUpdate, []uint8{model.ActionIDTaskUpdate}},
		{entity.ActionTaskDelete, []uint8{model.ActionIDTaskDelete}},
		{entity.ActionMessageCreate, []uint8{model.ActionIDMessageCreate}},
		{entity.ActionMessageUpdate, []uint8{model.ActionIDMessageUpdate}},
		{entity.ActionMessageDelete, []uint8{model.ActionIDMessageDelete}},
		{entity.ActionTaskComplete, []uint8{model.ActionIDTaskComplete}},
		{entity.ActionTaskApproveManual, []uint8{model.ActionIDTaskApproveManual}},
		// A scheduled task is recorded as both itself and a created task.
		{entity.ActionTaskFromScheduled, []uint8{model.ActionIDTaskFromScheduled, model.ActionIDTaskCreate}},
		{entity.ActionTaskRejectManual, []uint8{model.ActionIDTaskRejectManual}},
		{entity.ActionUserCreate, []uint8{model.ActionIDUserCreate}},
		// An action with no telemetry id records nothing.
		{entity.Action(0), nil},
	}
	for _, tc := range cases {
		t.Run(tc.action.String(), func(t *testing.T) {
			c := New(Params{}).(*controller)
			c.recordCRUD(entity.CRUDEvent{Action: tc.action, UserID: 1, WorkspaceID: 10, Actor: entity.ActorAgent})

			got := queued(c)
			if len(got) != len(tc.want) {
				t.Fatalf("queued %d rows, want %d: %+v", len(got), len(tc.want), got)
			}
			for i, row := range got {
				if row.Action != tc.want[i] {
					t.Errorf("row %d action = %d, want %d", i, row.Action, tc.want[i])
				}
				if row.UserID != 1 || row.WorkspaceID != 10 || row.Actor != uint8(entity.ActorAgent) {
					t.Errorf("row %d user/workspace/actor = %d/%d/%d, want 1/10/%d", i, row.UserID, row.WorkspaceID, row.Actor, entity.ActorAgent)
				}
			}
		})
	}
}

func TestRecordMCP_PermissionNotificationsAndIgnoredEvents(t *testing.T) {
	cases := []struct {
		name  string
		event mcp.MCPEvent
		want  uint8 // ActionIDUnknown: nothing recorded
	}{
		{"manual allow", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_manual_allow"}, model.ActionIDMCPPermissionManual},
		{"auto allow", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_auto_allow"}, model.ActionIDMCPPermissionAuto},
		{"manual deny", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_manual_deny"}, model.ActionIDMCPPermissionDeny},
		{"extension allow", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_extension_allow"}, model.ActionIDMCPPermissionExtensionAllow},
		{"extension deny", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_extension_deny"}, model.ActionIDMCPPermissionExtensionDeny},
		{"other notification", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "progress"}, model.ActionIDUnknown},
		{"unrecorded action", mcp.MCPEvent{Action: mcp.Action(250)}, model.ActionIDUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := New(Params{}).(*controller)
			c.recordMCP(tc.event)

			got := queued(c)
			if tc.want == model.ActionIDUnknown {
				if len(got) != 0 {
					t.Fatalf("want nothing recorded, got %+v", got)
				}
				return
			}
			if len(got) != 1 || got[0].Action != tc.want {
				t.Fatalf("queued %+v, want one row with action %d", got, tc.want)
			}
		})
	}
}

// A client is written once, then served from the cache; a failed write is not
// cached, so the next event tries again.
func TestRecordMCPClient(t *testing.T) {
	db := openMemDB(t, &model.MCPClient{})
	c := New(Params{DB: &memDBConn{db: db}}).(*controller)

	c.recordMCP(mcp.MCPEvent{Action: mcp.ActionMCPToolCall, ToolName: "task", ClientID: 77, ClientName: "cli", ClientVersion: "1.0"})
	var stored model.MCPClient
	if err := db.First(&stored, 77).Error; err != nil {
		t.Fatalf("client 77 was not stored: %v", err)
	}
	if stored.Name != "cli" || stored.Version != "1.0" {
		t.Errorf("stored client = %+v, want cli 1.0", stored)
	}
	if got := queued(c); len(got) != 1 || got[0].ClientID != 77 {
		t.Fatalf("queued %+v, want one row for client 77", got)
	}

	// From here every write fails: a known client must not reach the database.
	if err := db.Callback().Create().Before("gorm:create").Register("fail_create", func(tx *gorm.DB) {
		tx.AddError(errors.New("database unavailable"))
	}); err != nil {
		t.Fatalf("register callback: %v", err)
	}
	c.recordMCPClient(77, "cli", "1.0")

	c.recordMCPClient(88, "other", "2.0")
	c.seenClientsMu.Lock()
	_, cached := c.seenClients[88]
	c.seenClientsMu.Unlock()
	if cached {
		t.Error("a client whose write failed must not be cached")
	}
}

// A batch the database refuses is dropped, not retried forever or kept growing.
func TestWorker_FlushErrorDropsBatch(t *testing.T) {
	db := openMemDB(t) // no telemetries table
	c := &controller{
		db:        &memDBConn{db: db},
		queue:     make(chan model.Telemetry, 10),
		stop:      make(chan struct{}),
		batchSize: 1,
		interval:  time.Hour,
	}
	c.wg.Add(1)
	go c.worker()

	c.queue <- model.Telemetry{UserID: 1, Action: model.ActionIDTaskCreate}
	c.Close()

	if len(c.queue) != 0 {
		t.Errorf("queue still holds %d records after Close", len(c.queue))
	}
}
