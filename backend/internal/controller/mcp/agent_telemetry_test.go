package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
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

func thoughtParams(text string) AgentTelemetryParams {
	return AgentTelemetryParams{SessionID: "sess-1", Kind: "thought", Text: text, Data: map[string]any{}}
}

func planParams(planID, text string, data map[string]any) AgentTelemetryParams {
	full := map[string]any{"planId": planID, "planType": "items"}
	for k, v := range data {
		full[k] = v
	}
	return AgentTelemetryParams{SessionID: "sess-1", Kind: "plan", Text: text, Data: full}
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

// An agent that emits an empty reasoning block has said nothing worth showing.
func TestHandleAgentTelemetry_BlankThoughtIsDropped(t *testing.T) {
	rec := newTelemetryRecorder(t)

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", thoughtParams("   \n\t "))

	if len(rec.posted) != 0 {
		t.Fatalf("expected nothing to be posted, got %d messages", len(rec.posted))
	}
}

// A plan card is written once and revised thereafter: a new message per ticked
// checkbox would bury the conversation.
func TestHandleAgentTelemetry_PlanIsRevisedInPlace(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("default", "- ⬜ Add tests", map[string]any{
		"entries": []any{map[string]any{"content": "Add tests", "status": "pending"}},
	}))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("default", "- ✅ Add tests", map[string]any{
		"entries": []any{map[string]any{"content": "Add tests", "status": "completed"}},
	}))

	if len(rec.posted) != 1 {
		t.Fatalf("expected one plan message, got %d", len(rec.posted))
	}
	if len(rec.revisions) != 1 {
		t.Fatalf("expected the second update to revise the first, got %d revisions", len(rec.revisions))
	}
	if rec.revisions[0].messageID != 701 {
		t.Errorf("revised message %d, want the one that was posted", rec.revisions[0].messageID)
	}
	// The body was written once, so the metadata is what has to stay current.
	if got := rec.revisions[0].metadata["text"]; got != "- ✅ Add tests" {
		t.Errorf("revised text = %v, want the latest rendering", got)
	}
}

// Plans are told apart by their ID, so an agent running two at once gets two
// cards rather than one that flickers between them.
func TestHandleAgentTelemetry_DistinctPlansGetDistinctMessages(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ⬜ A", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-b", "- ⬜ B", nil))

	if len(rec.posted) != 2 {
		t.Fatalf("expected a message per plan, got %d", len(rec.posted))
	}
	if len(rec.revisions) != 0 {
		t.Errorf("one plan must not overwrite another, got %d revisions", len(rec.revisions))
	}
}

// Usage counters are cumulative, so one card per task is revised as the turn
// goes on.
func TestHandleAgentTelemetry_UsageIsRevisedInPlace(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()
	usage := func(text string, used float64) AgentTelemetryParams {
		return AgentTelemetryParams{
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

// Withdrawing a plan marks the card the human is already looking at.
func TestHandleAgentTelemetry_WithdrawnPlanMarksTheExistingCard(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ⬜ A", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "Plan withdrawn.", map[string]any{"removed": true}))

	if len(rec.revisions) != 1 {
		t.Fatalf("expected the withdrawal to revise the plan card, got %d", len(rec.revisions))
	}
	if got := rec.revisions[0].metadata["removed"]; got != true {
		t.Errorf("revised metadata removed = %v, want true", got)
	}

	// The card is released, so an agent reusing the ID starts a fresh one
	// rather than reviving a card marked withdrawn.
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ⬜ A again", nil))
	if len(rec.posted) != 2 {
		t.Fatalf("expected a fresh card after a withdrawal, got %d messages", len(rec.posted))
	}
}

// A withdrawal for a plan the human never saw would leave an orphaned card.
func TestHandleAgentTelemetry_WithdrawnUnknownPlanPostsNothing(t *testing.T) {
	rec := newTelemetryRecorder(t)

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1",
		planParams("plan-never-shown", "Plan withdrawn.", map[string]any{"removed": true}))

	if len(rec.posted) != 0 {
		t.Fatalf("expected nothing to be posted, got %d messages", len(rec.posted))
	}
}

// If the message a plan was written to is gone, the update must not be lost.
func TestHandleAgentTelemetry_UnrevisableMessageIsRewritten(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ⬜ A", nil))
	rec.updateErr = errors.New("message not found")
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ✅ A", nil))

	if len(rec.posted) != 2 {
		t.Fatalf("expected the update to be posted afresh, got %d messages", len(rec.posted))
	}
	if rec.posted[1].text != "- ✅ A" {
		t.Errorf("rewritten message = %q, want the latest rendering", rec.posted[1].text)
	}

	// And the plan now tracks the message it was actually written to.
	rec.updateErr = nil
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ✅ A done", nil))
	if len(rec.revisions) != 1 || rec.revisions[0].messageID != 702 {
		t.Fatalf("expected the next update to revise message 702, got %+v", rec.revisions)
	}
}

// A plan that could not be delivered must not be recorded as delivered, or the
// next update would try to revise a message that does not exist.
func TestHandleAgentTelemetry_UndeliverablePlanIsNotRemembered(t *testing.T) {
	rec := newTelemetryRecorder(t)
	ctx := context.Background()
	rec.replyErr = errors.New("workspace is archived")

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ⬜ A", nil))

	if len(rec.ps.agentTelemetryMessages) != 0 {
		t.Fatalf("a failed delivery must not be tracked, got %v", rec.ps.agentTelemetryMessages)
	}

	rec.replyErr = nil
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ✅ A", nil))
	if len(rec.posted) != 1 || len(rec.revisions) != 0 {
		t.Fatalf("expected a fresh post rather than a revision, got %d posts / %d revisions",
			len(rec.posted), len(rec.revisions))
	}
}

// The payload describes the telemetry; it does not get to relabel the message.
func TestAgentTelemetryMetadata_PayloadCannotOverrideTypeOrText(t *testing.T) {
	metadata := agentTelemetryMetadata(MessageTypeAgentPlan, AgentTelemetryParams{
		Text: "the real rendering",
		Data: map[string]any{
			"type":    "permission_request",
			"text":    "something else",
			"planId":  "plan-a",
			"entries": []any{},
		},
	})

	if metadata["type"] != MessageTypeAgentPlan {
		t.Errorf("type = %v, want %v", metadata["type"], MessageTypeAgentPlan)
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

	rec.ps.HandleAgentTelemetry(context.Background(), "sess-1", AgentTelemetryParams{
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

// The SDK rejects this notification method, so it arrives as a raw body the
// handler recognises and forwards here.
func TestHandleCustomNotification_RoutesAgentTelemetry(t *testing.T) {
	rec := newTelemetryRecorder(t)
	body, _ := json.Marshal(map[string]any{
		"method": AgentTelemetryNotificationMethod,
		"params": map[string]any{
			"task_id":    "",
			"session_id": "sess-1",
			"kind":       "thought",
			"text":       "Reasoning from the wire.",
			"data":       map[string]any{},
		},
	})

	rec.ps.HandleCustomNotification(context.Background(), "sess-1", body)

	if len(rec.posted) != 1 {
		t.Fatalf("expected the notification to be relayed, got %d messages", len(rec.posted))
	}
	if rec.posted[0].text != "Reasoning from the wire." {
		t.Errorf("relayed text = %q", rec.posted[0].text)
	}
}

// A telemetry body the shared notification envelope accepts but the telemetry
// payload does not is dropped rather than half-read.
func TestHandleCustomNotification_MalformedAgentTelemetryIsDropped(t *testing.T) {
	rec := newTelemetryRecorder(t)
	// `kind` as a number: the permission envelope this shares a decode with
	// ignores the field, so only the telemetry decode rejects it.
	body := []byte(`{"method":"` + AgentTelemetryNotificationMethod +
		`","params":{"session_id":"sess-1","kind":123,"text":"…"}}`)

	rec.ps.HandleCustomNotification(context.Background(), "sess-1", body)

	if len(rec.posted) != 0 {
		t.Fatalf("expected nothing to be posted, got %d messages", len(rec.posted))
	}
}

// Plans are keyed per plan; usage is keyed per task.
func TestAgentTelemetryMessageKey(t *testing.T) {
	if got, want := agentTelemetryMessageKey(42, agentTelemetryKindPlan, "plan-a"), "42:plan:plan-a"; got != want {
		t.Errorf("plan key = %q, want %q", got, want)
	}
	if got, want := agentTelemetryMessageKey(42, agentTelemetryKindUsage, ""), "42:usage"; got != want {
		t.Errorf("usage key = %q, want %q", got, want)
	}
}

// A build without the in-place update wired up still shows the telemetry; it
// just appends instead of revising.
func TestHandleAgentTelemetry_WithoutInPlaceUpdatesPlansAreAppended(t *testing.T) {
	rec := newTelemetryRecorder(t)
	rec.ps.updateMessageMetadata = nil
	ctx := context.Background()

	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ⬜ A", nil))
	rec.ps.HandleAgentTelemetry(ctx, "sess-1", planParams("plan-a", "- ✅ A", nil))

	if len(rec.posted) != 2 {
		t.Fatalf("expected both plans to be posted, got %d", len(rec.posted))
	}
}
