// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package pub

import (
	"context"
	"encoding/json"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	pubsubmocks "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	repomocks "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stream is one Start wired to topic channels the test writes to.
type stream struct {
	c    *statsController
	crud chan any
	mcp  chan any
}

func newController(t *testing.T, totals map[uint8]int64, totalsErr error) (*stream, *pubsubmocks.MockService) {
	t.Helper()
	ctrl := gomock.NewController(t)
	repo := repomocks.NewMockRepository(ctrl)
	ps := pubsubmocks.NewMockService(ctrl)
	repo.EXPECT().GetTelemetryActionCounts(gomock.Any()).Return(totals, totalsErr)
	c := NewStatsController(Params{Repository: repo, PubSub: ps}).(*statsController)
	return &stream{c: c, crud: make(chan any), mcp: make(chan any)}, ps
}

func (s *stream) start(t *testing.T, ctx context.Context, ps *pubsubmocks.MockService) {
	t.Helper()
	ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
		Return(&pubsub.SubscribeResponse{ID: 1, Events: s.crud}, nil)
	ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).
		Return(&pubsub.SubscribeResponse{ID: 2, Events: s.mcp}, nil)
	require.NoError(t, s.c.Start(ctx))
}

func send(t *testing.T, ch chan any, msg any) {
	t.Helper()
	select {
	case ch <- msg:
	case <-time.After(5 * time.Second):
		t.Fatalf("nobody consumed %v", msg)
	}
}

// waitStatsGoroutinesDone waits until every goroutine Start launched has
// returned, so a test proves its consumers stop rather than leak.
func waitStatsGoroutinesDone(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	buf := make([]byte, 1<<20)
	for {
		n := runtime.Stack(buf, true)
		if !strings.Contains(string(buf[:n]), "pub.(*statsController)") {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("stats goroutines still running after 5s:\n%s", buf[:n])
		}
		runtime.Gosched()
	}
}

// nextPayload waits for the next broadcast; the ticker fires every 3s.
func nextPayload(t *testing.T, ch chan []byte) map[string]StatEntry {
	t.Helper()
	select {
	case data := <-ch:
		var entries []StatEntry
		require.NoError(t, json.Unmarshal(data, &entries))
		out := map[string]StatEntry{}
		for _, e := range entries {
			out[e.Action] = e
		}
		return out
	case <-time.After(10 * time.Second):
		t.Fatal("no stats payload within 10s")
		return nil
	}
}

func TestStart_StreamsTotalsAndIncrements(t *testing.T) {
	s, ps := newController(t, map[uint8]int64{
		model.ActionIDTaskCreate: 5,
		200:                      9, // an id with no name is never reported
	}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.start(t, ctx, ps)

	client := s.c.Subscribe()

	send(t, s.crud, entity.CRUDEvent{Action: entity.ActionTaskCreate})
	send(t, s.crud, "not a crud event")
	send(t, s.mcp, mcp.MCPEvent{Action: mcp.ActionMCPToolCall})
	send(t, s.mcp, "not an mcp event")

	// The consumers hand each event over before handling it, so the first tick
	// may still miss it; the totals only grow, so wait for them.
	var got map[string]StatEntry
	for i := 0; i < 3; i++ {
		got = nextPayload(t, client)
		if got["task_create"].Total == 6 && got["mcp_tool_call"].Total == 1 {
			break
		}
	}
	assert.Equal(t, int64(6), got["task_create"].Total, "baseline 5 plus one created")
	assert.Equal(t, int64(1), got["mcp_tool_call"].Total)
	assert.NotContains(t, got, "unknown")

	s.c.Unsubscribe(client)
	s.c.mu.Lock()
	assert.Empty(t, s.c.clients)
	s.c.mu.Unlock()

	cancel()
	s.c.wg.Wait()
	waitStatsGoroutinesDone(t)
}

func TestStart_IncrementsResetAfterEachBroadcast(t *testing.T) {
	s, ps := newController(t, map[uint8]int64{}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.start(t, ctx, ps)

	s.c.recordAction(model.ActionIDTaskCreate)
	client := s.c.Subscribe()

	first := nextPayload(t, client)
	assert.Equal(t, StatEntry{Action: "task_create", Total: 1, Inc: 1}, first["task_create"])
	second := nextPayload(t, client)
	assert.Equal(t, StatEntry{Action: "task_create", Total: 1, Inc: 0}, second["task_create"])

	cancel()
	s.c.wg.Wait()
	waitStatsGoroutinesDone(t)
}

func TestStart_LoadFailureStillStreams(t *testing.T) {
	s, ps := newController(t, nil, errors.New("telemetry table missing"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.start(t, ctx, ps)

	s.c.mu.Lock()
	assert.NotNil(t, s.c.totals, "a failed load keeps the empty map, so recording still works")
	s.c.mu.Unlock()

	// Closed topics end the consumers; stop ends the broadcaster.
	close(s.crud)
	close(s.mcp)
	close(s.c.stop)
	s.c.wg.Wait()
	waitStatsGoroutinesDone(t)
}

func TestStart_SubscribeFailures(t *testing.T) {
	t.Run("crud topic", func(t *testing.T) {
		s, ps := newController(t, map[uint8]int64{}, nil)
		ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
			Return(nil, errors.New("no crud topic"))
		assert.EqualError(t, s.c.Start(context.Background()), "no crud topic")
	})

	t.Run("mcp topic", func(t *testing.T) {
		s, ps := newController(t, map[uint8]int64{}, nil)
		ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicCRUD}).
			Return(&pubsub.SubscribeResponse{ID: 1, Events: s.crud}, nil)
		ps.EXPECT().Subscribe(gomock.Any(), pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicMCP}).
			Return(nil, errors.New("no mcp topic"))
		assert.EqualError(t, s.c.Start(context.Background()), "no mcp topic")
	})
}

func TestBroadcastLoop_SkipsFullClients(t *testing.T) {
	t.Run("a full client is skipped, not waited on", func(t *testing.T) {
		c := NewStatsController(Params{}).(*statsController)
		c.totals[model.ActionIDTaskCreate] = 1
		full := c.Subscribe()
		for i := 0; i < cap(full); i++ {
			full <- []byte("stale")
		}
		live := c.Subscribe()

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		c.wg.Add(1)
		go c.broadcastLoop(ctx)

		got := nextPayload(t, live)
		assert.Equal(t, int64(1), got["task_create"].Total)
		// The tick holds the lock until it has tried every client.
		c.mu.Lock()
		assert.Len(t, full, cap(full))
		c.mu.Unlock()

		cancel()
		c.wg.Wait()
	})
}

func TestRecordAction_IgnoresUnknown(t *testing.T) {
	c := NewStatsController(Params{}).(*statsController)
	c.recordAction(model.ActionIDUnknown)
	assert.Empty(t, c.totals)
	assert.Empty(t, c.inc)
}

func TestHandleCRUD(t *testing.T) {
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
		// A scheduled task is also a created task.
		{entity.ActionTaskFromScheduled, []uint8{model.ActionIDTaskFromScheduled, model.ActionIDTaskCreate}},
		{entity.ActionTaskRejectManual, []uint8{model.ActionIDTaskRejectManual}},
		{entity.ActionUserCreate, []uint8{model.ActionIDUserCreate}},
		// Anything else is not part of the public stats.
		{entity.Action(0), nil},
		{entity.ActionMachineAdd, nil},
	}
	for _, tc := range cases {
		t.Run(tc.action.String(), func(t *testing.T) {
			c := NewStatsController(Params{}).(*statsController)
			c.handleCRUD(entity.CRUDEvent{Action: tc.action})
			want := map[uint8]int64{}
			for _, id := range tc.want {
				want[id] = 1
			}
			assert.Equal(t, want, c.totals)
			assert.Equal(t, want, c.inc)
		})
	}
}

func TestHandleMCP(t *testing.T) {
	cases := []struct {
		name  string
		event mcp.MCPEvent
		want  uint8
	}{
		{"tool call", mcp.MCPEvent{Action: mcp.ActionMCPToolCall}, model.ActionIDMCPToolCall},
		{"manual allow", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_manual_allow"}, model.ActionIDMCPPermissionManual},
		{"auto allow", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_auto_allow"}, model.ActionIDMCPPermissionAuto},
		{"manual deny", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_manual_deny"}, model.ActionIDMCPPermissionDeny},
		{"extension allow", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_extension_allow"}, model.ActionIDMCPPermissionExtensionAllow},
		{"extension deny", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_extension_deny"}, model.ActionIDMCPPermissionExtensionDeny},
		{"other notification", mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "progress"}, model.ActionIDUnknown},
		{"method call", mcp.MCPEvent{Action: mcp.ActionMCPMethodCall}, model.ActionIDUnknown},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewStatsController(Params{}).(*statsController)
			c.handleMCP(tc.event)
			want := map[uint8]int64{}
			if tc.want != model.ActionIDUnknown {
				want[tc.want] = 1
			}
			assert.Equal(t, want, c.totals)
		})
	}
}

func TestActionName(t *testing.T) {
	names := map[uint8]string{
		model.ActionIDWorkspaceCreate:             "workspace_create",
		model.ActionIDWorkspaceUpdate:             "workspace_update",
		model.ActionIDWorkspaceDelete:             "workspace_delete",
		model.ActionIDTaskCreate:                  "task_create",
		model.ActionIDTaskUpdate:                  "task_update",
		model.ActionIDTaskDelete:                  "task_delete",
		model.ActionIDMessageCreate:               "message_create",
		model.ActionIDMessageUpdate:               "message_update",
		model.ActionIDMessageDelete:               "message_delete",
		model.ActionIDMCPToolCall:                 "mcp_tool_call",
		model.ActionIDTaskApproveManual:           "task_approve_manual",
		model.ActionIDMCPPermissionManual:         "mcp_permission_manual",
		model.ActionIDMCPPermissionAuto:           "mcp_permission_auto",
		model.ActionIDMCPPermissionDeny:           "mcp_permission_deny",
		model.ActionIDMCPPermissionExtensionAllow: "mcp_permission_extension_allow",
		model.ActionIDMCPPermissionExtensionDeny:  "mcp_permission_extension_deny",
		model.ActionIDTaskRejectManual:            "task_reject_manual",
		model.ActionIDTaskComplete:                "task_complete",
		model.ActionIDTaskFromScheduled:           "task_from_scheduled",
		model.ActionIDUserCreate:                  "user_create",
	}
	for id, want := range names {
		assert.Equal(t, want, actionName(id), "action id %d", id)
	}
	assert.Equal(t, "unknown", actionName(model.ActionIDUnknown))
	assert.Equal(t, "unknown", actionName(model.ActionIDMCPMethodCall))
}
