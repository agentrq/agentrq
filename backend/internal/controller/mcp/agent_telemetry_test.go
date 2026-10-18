// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

// postedMessage is one chat message the workspace server delivered.
type postedMessage struct {
	chatID   string
	text     string
	metadata map[string]any
}

// metadataRevision is one in-place update to an already-posted message.
type metadataRevision struct {
	messageID int64
	metadata  map[string]any
}

// telemetryRecorder is a workspace server wired up just enough to take agent
// telemetry, plus a record of everything it wrote to the task.
type telemetryRecorder struct {
	ps        *WorkspaceServer
	posted    []postedMessage
	revisions []metadataRevision
	// replyErr, when set, makes every delivery attempt fail.
	replyErr error
	// updateErr, when set, makes every in-place revision fail.
	updateErr error
}

// asMap re-reads metadata the way a client would: as JSON, not as whatever
// concrete map the handler happened to build.
func asMap(t *testing.T, metadata any) map[string]any {
	t.Helper()
	b, err := json.Marshal(metadata)
	if err != nil {
		t.Fatalf("metadata is not serialisable: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("metadata did not round-trip: %v", err)
	}
	return out
}

func newTelemetryRecorder(t *testing.T) *telemetryRecorder {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()

	rec := &telemetryRecorder{}
	rec.ps = &WorkspaceServer{
		workspaceID:            100,
		pubsub:                 pubsubMock,
		sessionTasks:           map[string]int64{"sess-1": 42},
		agentTelemetryMessages: make(map[string]int64),
		getTask: func(ctx context.Context, taskID int64) (model.Task, error) {
			return model.Task{ID: taskID}, nil
		},
		listTasks: func(ctx context.Context, filter ListTasksFilter) ([]model.Task, error) {
			return nil, errors.New("no ongoing task")
		},
		reply: func(ctx context.Context, chatID, text string, a []entity.Attachment, metadata any) (int64, error) {
			if rec.replyErr != nil {
				return 0, rec.replyErr
			}
			rec.posted = append(rec.posted, postedMessage{chatID: chatID, text: text, metadata: asMap(t, metadata)})
			return int64(700 + len(rec.posted)), nil
		},
		updateMessageMetadata: func(ctx context.Context, taskID, messageID int64, metadata any) error {
			if rec.updateErr != nil {
				return rec.updateErr
			}
			rec.revisions = append(rec.revisions, metadataRevision{messageID: messageID, metadata: asMap(t, metadata)})
			return nil
		},
	}
	return rec
}

func thoughtParams(text string) agentlink.TelemetryReport {
	return agentlink.TelemetryReport{SessionID: "sess-1", Kind: "thought", Text: text, Data: map[string]any{}}
}

func planParams(text string, data map[string]any) agentlink.TelemetryReport {
	return agentlink.TelemetryReport{SessionID: "sess-1", Kind: "plan", Text: text, Data: data}
}

func usageText(text string) agentlink.TelemetryReport {
	return agentlink.TelemetryReport{SessionID: "sess-1", Kind: "usage", Text: text, Data: map[string]any{}}
}

// fakePlans is a TaskPlanStore that records what it was asked to keep.
type fakePlans struct {
	saved   []string // taskID:content
	deleted []int64
	err     error
}

func (f *fakePlans) Save(_ context.Context, taskID int64, content string) error {
	if f.err != nil {
		return f.err
	}
	f.saved = append(f.saved, fmt.Sprintf("%d:%s", taskID, content))
	return nil
}

func (f *fakePlans) Delete(_ context.Context, taskID int64) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, taskID)
	return nil
}

// Reasoning is appended: each block explains a different moment of the turn.
func TestHandleAgentTelemetry_ThoughtIsPostedAsItsOwnMessage(t *testing.T) {
	rec := newTelemetryRecorder(t)

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", thoughtParams("Checking the config first."))
	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", thoughtParams("Now running the tests."))

	if len(rec.posted) != 2 {
		t.Fatalf("expected each reasoning block to be its own message, got %d", len(rec.posted))
	}
	if rec.posted[0].text != "Checking the config first." {
		t.Errorf("reasoning text was not delivered: %q", rec.posted[0].text)
	}
	if got := rec.posted[0].metadata["type"]; got != MessageTypeAgentThought {
		t.Errorf("metadata type = %v, want %v", got, MessageTypeAgentThought)
	}
	if got := rec.posted[0].metadata["text"]; got != "Checking the config first." {
		t.Errorf("metadata text = %v, want the reasoning itself", got)
	}
	if len(rec.revisions) != 0 {
		t.Errorf("reasoning must never overwrite an earlier block, got %d revisions", len(rec.revisions))
	}
}

// A notice is appended as its own message, never revising an earlier one.
func TestHandleAgentTelemetry_NoticeIsPostedAsItsOwnMessage(t *testing.T) {
	rec := newTelemetryRecorder(t)
	notice := agentlink.TelemetryReport{SessionID: "sess-1", Kind: "notice", Text: "MCP server unavailable",
		Data: map[string]any{"severity": "warning", "title": "MCP server unavailable", "description": ""}}

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", notice)
	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", notice)

	if len(rec.posted) != 2 || len(rec.revisions) != 0 {
		t.Fatalf("got %d messages and %d revisions, want each notice as its own message", len(rec.posted), len(rec.revisions))
	}
	if got := rec.posted[0].metadata; got["type"] != MessageTypeAgentNotice || got["severity"] != "warning" || got["text"] != "MCP server unavailable" {
		t.Errorf("metadata = %v, want an agent_notice carrying its severity and text", got)
	}
}

// An agent that emits an empty reasoning block has said nothing worth showing.
func TestHandleAgentTelemetry_BlankThoughtIsDropped(t *testing.T) {
	rec := newTelemetryRecorder(t)

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", thoughtParams("   \n\t "))

	if len(rec.posted) != 0 {
		t.Fatalf("expected nothing to be posted, got %d messages", len(rec.posted))
	}
}

func TestHandleAgentTelemetry_UsageIsRevisedInPlace(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()
	usage := func(text string, used float64) agentlink.TelemetryReport {
		return agentlink.TelemetryReport{
			SessionID: "sess-1",
			Kind:      "usage",
			Text:      text,
			Data:      map[string]any{"used": used, "size": 200000.0},
		}
	}

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usage("Context 100 / 200,000 tokens (0%)", 100))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usage("Context 50,000 / 200,000 tokens (25%)", 50000))

	if len(rec.posted) != 1 {
		t.Fatalf("expected one usage message, got %d", len(rec.posted))
	}
	if len(rec.revisions) != 1 {
		t.Fatalf("expected the later snapshot to revise the first, got %d", len(rec.revisions))
	}
	if got := rec.revisions[0].metadata["used"]; got != 50000.0 {
		t.Errorf("revised used = %v, want the later snapshot", got)
	}
	if got := rec.posted[0].metadata["type"]; got != MessageTypeAgentUsage {
		t.Errorf("metadata type = %v, want %v", got, MessageTypeAgentUsage)
	}
}

// A plan is the task's: each one replaces it in the store, and none is a
// message in the conversation.
func TestHandleAgentTelemetry_PlanIsTheTasks(t *testing.T) {
	rec := newTelemetryRecorder(t)
	plans := &fakePlans{}
	rec.ps.plans = plans
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("- [ ] Add tests", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("- [x] Add tests", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("Plan withdrawn.", map[string]any{"removed": true}))

	if want := []string{"42:- [ ] Add tests", "42:- [x] Add tests"}; fmt.Sprint(plans.saved) != fmt.Sprint(want) {
		t.Errorf("saved %q, want %q", plans.saved, want)
	}
	if len(plans.deleted) != 1 || plans.deleted[0] != 42 {
		t.Errorf("deleted %v, want task 42's plan once", plans.deleted)
	}
	if len(rec.posted) != 0 || len(rec.revisions) != 0 {
		t.Errorf("a plan wrote %d messages and %d revisions, want none", len(rec.posted), len(rec.revisions))
	}
}

// A stored plan is counted; a plan the store refused, or a withdrawal, is not.
func TestHandleAgentTelemetry_PlanUpdateIsCounted(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctrl := gomock.NewController(t)
	pub := mock_pubsub.NewMockService(ctrl)
	rec.ps.pubsub = pub
	rec.ps.userID = "1"
	plans := &fakePlans{}
	rec.ps.plans = plans
	ctx := context.Background()

	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).Do(func(_ context.Context, req pubsub.PublishRequest) {
		ev, ok := req.Event.(MCPEvent)
		if !ok || ev.Action != ActionMCPPlanUpdate || ev.WorkspaceID != 100 || ev.Actor != 2 {
			t.Errorf("published %+v, want one agent plan_update for workspace 100", req.Event)
		}
	}).Times(1)
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("- [ ] A", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("Plan withdrawn.", map[string]any{"removed": true}))
	plans.err = errors.New("database unavailable")
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("- [x] A", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("Plan withdrawn.", map[string]any{"removed": true}))
	if len(plans.saved) != 1 || len(plans.deleted) != 1 {
		t.Errorf("saved %v and deleted %v, want only what the store took", plans.saved, plans.deleted)
	}
}

// A server built without a plan store drops plans rather than failing.
func TestHandleAgentTelemetry_NoPlanStoreDropsPlans(t *testing.T) {
	rec := newTelemetryRecorder(t)
	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", planParams("- [ ] A", nil))
	if len(rec.posted) != 0 {
		t.Errorf("posted %d messages, want none", len(rec.posted))
	}
}

// If the message a usage counter was written to is gone, the update must not be lost.
func TestHandleAgentTelemetry_UnrevisableMessageIsRewritten(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 1"))
	rec.updateErr = errors.New("message not found")
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 2"))

	if len(rec.posted) != 2 {
		t.Fatalf("expected the update to be posted afresh, got %d messages", len(rec.posted))
	}
	if rec.posted[1].text != "Context 2" {
		t.Errorf("rewritten message = %q, want the latest rendering", rec.posted[1].text)
	}

	// And the counter now tracks the message it was actually written to.
	rec.updateErr = nil
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 3"))
	if len(rec.revisions) != 1 || rec.revisions[0].messageID != 702 {
		t.Fatalf("expected the next update to revise message 702, got %+v", rec.revisions)
	}
}

// A counter that could not be delivered must not be recorded as delivered, or the
// next update would try to revise a message that does not exist.
func TestHandleAgentTelemetry_UndeliverableUsageIsNotRemembered(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()
	rec.replyErr = errors.New("workspace is archived")

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 1"))

	if len(rec.ps.agentTelemetryMessages) != 0 {
		t.Fatalf("a failed delivery must not be tracked, got %v", rec.ps.agentTelemetryMessages)
	}

	rec.replyErr = nil
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 2"))
	if len(rec.posted) != 1 || len(rec.revisions) != 0 {
		t.Fatalf("expected a fresh post rather than a revision, got %d posts / %d revisions",
			len(rec.posted), len(rec.revisions))
	}
}

// The payload describes the telemetry; it does not get to relabel the message.
func TestAgentTelemetryMetadata_PayloadCannotOverrideTypeOrText(t *testing.T) {
	metadata := agentTelemetryMetadata(MessageTypeAgentNotice, agentlink.TelemetryReport{
		Text: "the real rendering",
		Data: map[string]any{
			"type":    "permission_request",
			"text":    "something else",
			"planId":  "plan-a",
			"entries": []any{},
		},
	})

	if metadata["type"] != MessageTypeAgentNotice {
		t.Errorf("type = %v, want %v", metadata["type"], MessageTypeAgentNotice)
	}
	if metadata["text"] != "the real rendering" {
		t.Errorf("text = %v, want the rendering the gateway produced", metadata["text"])
	}
	if metadata["planId"] != "plan-a" {
		t.Errorf("planId = %v, want it carried through", metadata["planId"])
	}
}

// Telemetry that belongs to no task has nowhere to go.
func TestHandleAgentTelemetry_NoTaskPostsNothing(t *testing.T) {
	rec := newTelemetryRecorder(t)

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-unknown", thoughtParams("Nowhere to put this."))

	if len(rec.posted) != 0 {
		t.Fatalf("expected nothing to be posted, got %d messages", len(rec.posted))
	}
}

// A newer gateway may send kinds this build does not know; they are ignored
// rather than shown as an empty message.
func TestHandleAgentTelemetry_UnknownKindIsIgnored(t *testing.T) {
	rec := newTelemetryRecorder(t)

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", agentlink.TelemetryReport{
		SessionID: "sess-1", Kind: "something_new", Text: "…",
	})

	if len(rec.posted) != 0 {
		t.Fatalf("expected nothing to be posted, got %d messages", len(rec.posted))
	}
}

// The payload's own task ID wins over the session mapping, so telemetry from a
// gateway juggling several tasks lands on the right one.
func TestHandleAgentTelemetry_PayloadTaskIDIsHonoured(t *testing.T) {
	rec := newTelemetryRecorder(t)
	p := thoughtParams("For another task.")
	p.TaskID = "0an2BXTfpGj"

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", p)

	if len(rec.posted) != 1 {
		t.Fatalf("expected one message, got %d", len(rec.posted))
	}
	if rec.posted[0].chatID != "0an2BXTfpGj" {
		t.Errorf("posted to %q, want the task the payload named", rec.posted[0].chatID)
	}
}

// Usage is keyed per task.
func TestAgentTelemetryMessageKey(t *testing.T) {
	if got, want := agentTelemetryMessageKey(42, agentTelemetryKindUsage), "42:usage"; got != want {
		t.Errorf("usage key = %q, want %q", got, want)
	}
}

// A build without the in-place update wired up still shows the telemetry; it
// just appends instead of revising.
func TestHandleAgentTelemetry_WithoutInPlaceUpdatesUsageIsAppended(t *testing.T) {
	rec := newTelemetryRecorder(t)
	rec.ps.updateMessageMetadata = nil
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 1"))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usageText("Context 2"))

	if len(rec.posted) != 2 {
		t.Fatalf("expected both snapshots to be posted, got %d", len(rec.posted))
	}
}

// The footer is revised in place, so the time of the revision is what tells
// the composer a turn ended after the person's last message.
func TestHandleAgentTelemetry_UsageCarriesWhenItWasReported(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()
	defer func(f func() time.Time) { nowFunc = f }(nowFunc)
	nowFunc = func() time.Time { return time.Date(2026, 10, 12, 10, 1, 9, 500, time.UTC) }

	usage := agentlink.TelemetryReport{
		SessionID: "sess-1", Kind: "usage", Text: "Context 1 / 2 tokens (50%)",
		Data: map[string]any{"used": 1.0, "size": 2.0, "reportedAt": "1999-01-01T00:00:00Z"},
	}
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usage)

	if len(rec.posted) != 1 {
		t.Fatalf("expected one usage message, got %d", len(rec.posted))
	}
	if got, want := rec.posted[0].metadata["reportedAt"], "2026-10-12T10:01:09.0000005Z"; got != want {
		t.Errorf("reportedAt was %v, want the server's own clock %v (a payload must not choose it)", got, want)
	}

	nowFunc = func() time.Time { return time.Date(2026, 10, 12, 10, 2, 0, 0, time.FixedZone("CEST", 2*60*60)) }
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", usage)

	if len(rec.revisions) != 1 {
		t.Fatalf("expected the footer to be revised once, got %d revisions", len(rec.revisions))
	}
	if got, want := rec.revisions[0].metadata["reportedAt"], "2026-10-12T08:02:00Z"; got != want {
		t.Errorf("revised reportedAt was %v, want the revision's time in UTC %v", got, want)
	}
}

// Only the footer is revised in a way its position cannot show, so only the
// footer carries reportedAt.
func TestHandleAgentTelemetry_ThoughtCarriesNoReportedAt(t *testing.T) {
	rec := newTelemetryRecorder(t)
	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", thoughtParams("thinking"))

	if len(rec.posted) != 1 {
		t.Fatalf("expected one thought message, got %d", len(rec.posted))
	}
	if got, ok := rec.posted[0].metadata["reportedAt"]; ok {
		t.Errorf("a thought carried reportedAt %v", got)
	}
}
