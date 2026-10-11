// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/mock/gomock"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
)

func usageParams(used float64) AgentTelemetryParams {
	return AgentTelemetryParams{SessionID: "sess-1", Kind: "usage", Text: "Context", Data: map[string]any{"used": used, "size": 200000.0}}
}

// nextEventOfType reads events off a subscription until one of the given type
// arrives, skipping the others the same action publishes.
func nextEventOfType(t *testing.T, sub chan []byte, eventType string) busEvent {
	t.Helper()
	for {
		if evt := readEvent(t, sub); evt.Type == eventType {
			return evt
		}
	}
}

// expectNoEvent fails if anything is published within a short window.
func expectNoEvent(t *testing.T, sub chan []byte) {
	t.Helper()
	select {
	case line := <-sub:
		t.Fatalf("unexpected event %q", line)
	case <-time.After(50 * time.Millisecond):
	}
}

// Switching from the gateway to Claude Code ends the gateway's session; its
// usage message has to say so, or the composer keeps drawing its context ring.
func TestAgentSessionEnded_MarksItsUsageMessageEnded(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	rec.ps.agentSessionEnded(ctx, "sess-1")

	if len(rec.revisions) != 1 {
		t.Fatalf("expected the usage message to be revised once, got %d", len(rec.revisions))
	}
	if got := rec.revisions[0]; got.messageID != 701 || got.metadata[MessageMetadataSessionEnded] != true {
		t.Errorf("revision = %+v, want message 701 marked sessionEnded", got)
	}
}

// The next agent on the task starts its own usage message: revising the ended
// one would merge into metadata that still says it has ended.
func TestAgentSessionEnded_NextAgentStartsAFreshUsageMessage(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	rec.ps.agentSessionEnded(ctx, "sess-1")
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(5))

	if len(rec.posted) != 2 {
		t.Fatalf("expected a second usage message after the session ended, got %d", len(rec.posted))
	}
}

// Ending a session a second time finds nothing left to mark.
func TestAgentSessionEnded_MarksEachMessageOnce(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	rec.ps.agentSessionEnded(ctx, "sess-1")
	rec.ps.agentSessionEnded(ctx, "sess-1")

	if len(rec.revisions) != 1 {
		t.Errorf("expected one revision, got %d", len(rec.revisions))
	}
}

// Two sessions revising the same task's usage share one message, and the one
// still attached keeps it live when the other leaves.
func TestAgentSessionEnded_LeavesAMessageAnotherSessionStillWrites(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.sessionTasks["sess-2"] = 42

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	rec.ps.HandleAgentTelemetry(ctx, "sess-2", AgentTelemetryParams{Kind: "usage", Text: "Context", Data: map[string]any{"used": 200.0}})
	revisionsBefore := len(rec.revisions)

	rec.ps.agentSessionEnded(ctx, "sess-1")

	if len(rec.revisions) != revisionsBefore {
		t.Fatalf("a message sess-2 still writes was marked ended: %+v", rec.revisions[revisionsBefore:])
	}
	rec.ps.HandleAgentTelemetry(ctx, "sess-2", AgentTelemetryParams{Kind: "usage", Text: "Context", Data: map[string]any{"used": 300.0}})
	if len(rec.posted) != 1 {
		t.Errorf("sess-2 should keep revising the same message, got %d posted", len(rec.posted))
	}
}

// A message the session wrote that someone else has since taken over is still
// marked, but its key is left to the new owner.
func TestAgentSessionEnded_KeepsAKeyNowPointingElsewhere(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	key := agentTelemetryMessageKey(42, agentTelemetryKindUsage, "")
	rec.ps.agentTelemetryMessages[key] = 999

	rec.ps.agentSessionEnded(ctx, "sess-1")

	if got := rec.ps.agentTelemetryMessages[key]; got != 999 {
		t.Errorf("key = %d, want it left pointing at 999", got)
	}
}

// A store that cannot rewrite messages still releases the key.
func TestAgentSessionEnded_WithoutInPlaceUpdates(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()
	rec.ps.updateMessageMetadata = nil

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	rec.ps.agentSessionEnded(ctx, "sess-1")

	if len(rec.ps.agentTelemetryMessages) != 0 {
		t.Errorf("expected the usage key released, got %v", rec.ps.agentTelemetryMessages)
	}
}

// A failed write is logged rather than retried: the next agent starts a fresh
// message either way.
func TestAgentSessionEnded_UpdateFailureIsNotFatal(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageParams(100))
	rec.updateErr = errors.New("database unavailable")
	rec.ps.agentSessionEnded(ctx, "sess-1")

	if len(rec.ps.agentTelemetryMessages) != 0 {
		t.Errorf("expected the usage key released, got %v", rec.ps.agentTelemetryMessages)
	}
}

// A usage report that could not be delivered leaves nothing to mark.
func TestHandleAgentTelemetry_UndeliveredUsageIsNotRemembered(t *testing.T) {
	rec := newTelemetryRecorder(t)
	rec.replyErr = errors.New("database unavailable")

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", usageParams(100))

	if len(rec.ps.agentUsageMessages) != 0 {
		t.Errorf("expected nothing remembered, got %v", rec.ps.agentUsageMessages)
	}
}

func sessionEndServer(t *testing.T) (*WorkspaceServer, chan []byte) {
	t.Helper()
	ps := &WorkspaceServer{
		bus:              eventbus.New(),
		workspaceID:      7,
		userID:           "0ZzhYQG2qtl",
		agentModels:      make(map[string]AgentModelsSnapshot),
		agentCommands:    make(map[string]AgentCommandsSnapshot),
		agentConcurrency: make(map[string]AgentConcurrencySnapshot),
	}
	sub := ps.bus.Subscribe(ps.workspaceID, "")
	t.Cleanup(func() { ps.bus.Unsubscribe(ps.workspaceID, "", sub) })
	return ps, sub
}

// The gateway's limit, models and commands leave with it, and the open page
// hears that they did.
func TestAgentSessionEnded_WithdrawsWhatTheSessionReported(t *testing.T) {
	ps, sub := sessionEndServer(t)
	ps.agentModels["gw"] = AgentModelsSnapshot{ConfigID: "model", CurrentModel: "opus", Models: []AgentModel{{ID: "opus"}}}
	ps.agentCommands["gw"] = AgentCommandsSnapshot{Commands: []AgentCommand{{Name: "compact"}}}
	ps.agentConcurrency["gw"] = AgentConcurrencySnapshot{MaxConcurrency: 4, CanSet: true}

	ps.agentSessionEnded(context.Background(), "gw")

	if got := readEvent(t, sub); got.Type != "agent.models" || len(got.Payload["models"].([]any)) != 0 {
		t.Errorf("models event = %+v, want an empty list", got)
	}
	if got := readEvent(t, sub); got.Type != "agent.commands" || len(got.Payload["commands"].([]any)) != 0 {
		t.Errorf("commands event = %+v, want an empty list", got)
	}
	if got := readEvent(t, sub); got.Type != "agent.concurrency" || got.Payload["maxConcurrency"] != 0.0 {
		t.Errorf("concurrency event = %+v, want no limit", got)
	}
}

// A session that never reported any of them, Claude Code among them, ends
// without anything published for it.
func TestAgentSessionEnded_SilentSessionPublishesNothing(t *testing.T) {
	ps, sub := sessionEndServer(t)
	ps.agentConcurrency["gw"] = AgentConcurrencySnapshot{MaxConcurrency: 4}

	ps.agentSessionEnded(context.Background(), "claude")

	expectNoEvent(t, sub)
}

func TestRemoveStreamingSession_ReportsTheLastStream(t *testing.T) {
	ps := &WorkspaceServer{}
	ps.addStreamingSession("s")
	ps.addStreamingSession("s")

	if ps.removeStreamingSession("s") {
		t.Error("first close reported as the last while another stream is open")
	}
	if !ps.removeStreamingSession("s") {
		t.Error("last close not reported as the last")
	}
}

// End to end through the HTTP handler: a stream closing is what withdraws the
// gateway's concurrency.
func TestHandler_ClosedStreamWithdrawsConcurrency(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pub := mock_pubsub.NewMockService(ctrl)
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	listTasks := func(context.Context, ListTasksFilter) ([]model.Task, error) { return nil, nil }

	bus := eventbus.New()
	ps := NewWorkspaceServer(
		1, 0, "user", "http://localhost",
		nil, nil, nil, nil, listTasks, nil, nil, nil, nil,
		nil, nil, nil, nil, nil, nil, nil, nil, nil,
		bus, nil, nil, "icon", "name", "desc", nil, nil, nil, pub,
	)
	ps.agentConcurrency["gw"] = AgentConcurrencySnapshot{MaxConcurrency: 4, CanSet: true}
	sub := bus.Subscribe(1, "")
	t.Cleanup(func() { bus.Unsubscribe(1, "", sub) })

	srv := httptest.NewServer(ps.Handler())
	t.Cleanup(srv.Close)
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("Mcp-Session-Id", "gw")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if got := nextEventOfType(t, sub, "agent.concurrency"); got.Payload["maxConcurrency"] != 0.0 {
		t.Errorf("concurrency event = %+v, want no limit", got)
	}
}
