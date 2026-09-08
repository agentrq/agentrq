package mcp

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/golang/mock/gomock"
)

// approvalTelemetry collects the method names of every MCP telemetry event the
// server publishes, which is what the approval counters are computed from.
type approvalTelemetry struct {
	mu      sync.Mutex
	methods []string
}

func (r *approvalTelemetry) record(req pubsub.PublishRequest) {
	ev, ok := req.Event.(MCPEvent)
	if !ok {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.methods = append(r.methods, ev.Method)
}

func (r *approvalTelemetry) count(method string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, m := range r.methods {
		if m == method {
			n++
		}
	}
	return n
}

func (r *approvalTelemetry) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.methods...)
}

// recordedToolCalls collects the tool_calls rows a server would have written.
type recordedToolCalls struct {
	mu   sync.Mutex
	rows []model.ToolCall
}

func (r *recordedToolCalls) add(tc model.ToolCall) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, tc)
}

func (r *recordedToolCalls) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.rows)
}

// withToolCallRecording makes the server persist tool calls into `rows` instead
// of a database, so a test can tell how many rows one call produced.
func withToolCallRecording(ps *WorkspaceServer, rows *recordedToolCalls) {
	var next int64
	ps.idgen = stubIDGen{next: &next}
	ps.recordToolCall = func(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
		rows.add(tc)
		return tc, nil
	}
}

// stubIDGen hands out increasing ids without a real generator.
type stubIDGen struct{ next *int64 }

func (s stubIDGen) NextID() int64 {
	*s.next++
	return *s.next
}

// approvalServer builds a workspace server that answers permission requests and
// reports what it published, with `autoAllowed` pre-approved.
func approvalServer(t *testing.T, autoAllowed []string) (*WorkspaceServer, *approvalTelemetry) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)

	rec := &approvalTelemetry{}
	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ context.Context, req pubsub.PublishRequest) (*pubsub.PublishResponse, error) {
			rec.record(req)
			return &pubsub.PublishResponse{}, nil
		}).AnyTimes()

	ps := &WorkspaceServer{
		workspaceID:         100,
		userID:              "12345",
		pubsub:              pubsubMock,
		autoAllowedTools:    autoAllowed,
		permissionRequests:  make(map[string]string),
		requestTools:        make(map[string]string),
		requestParams:       make(map[string]*PermissionRequestParams),
		sessionTasks:        map[string]int64{"sess-1": 42},
		requestTaskIDs:      make(map[string]int64),
		permissionResponses: make(map[string]int64),
		toolCallIDs:         make(map[string]int64),
		undeliveredVerdicts: make(map[string]string),
		getTask: func(ctx context.Context, taskID int64) (model.Task, error) {
			return model.Task{ID: taskID}, nil
		},
		listTasks: func(ctx context.Context, filter ListTasksFilter) ([]model.Task, error) {
			return []model.Task{{ID: 42}}, nil
		},
		reply: func(ctx context.Context, chatID, text string, a []entity.Attachment, metadata any) (int64, error) {
			return 700, nil
		},
	}
	return ps, rec
}

func permissionNotification(requestID, toolName string) []byte {
	body, _ := json.Marshal(map[string]any{
		"method": "notifications/claude/channel/permission_request",
		"params": map[string]any{
			"request_id":    requestID,
			"tool_name":     toolName,
			"description":   "read a file",
			"input_preview": `{"file_path":"/tmp/x"}`,
		},
	})
	return body
}

// An auto-allowed tool call is one approval, and it is an automatic one.
//
// It used to be counted twice: the auto-allow path emits `permission_auto_allow`
// itself and *also* calls SendPermissionVerdict to answer the agent, which
// emitted `permission_manual_allow` unconditionally because it was written for
// the human-driven path. So every auto-allowed call also incremented the manual
// approval counter — the one number on the analytics screen that is supposed to
// mean "a human stopped and decided this".
func TestAutoAllowedRequestIsNotCountedAsAManualApproval(t *testing.T) {
	ps, rec := approvalServer(t, []string{"Read"})

	ps.HandleCustomNotification(context.Background(), "sess-1", permissionNotification("req-1", "Read"))

	// The verdict is sent from a goroutine after a short settle delay; wait for
	// it to have actually passed the point where it used to over-report.
	waitForVerdictDelivered(t, ps, "req-1")

	if got := rec.count("permission_auto_allow"); got != 1 {
		t.Errorf("expected 1 auto approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("an auto-allowed call must not count as a manual approval, got %d (all: %v)",
			got, rec.snapshot())
	}
}

// The same, through the middleware rather than the out-of-band notification:
// both paths auto-allow, and both answered by way of SendPermissionVerdict.
func TestAutoAllowedViaTaskAllowAllIsNotCountedAsManual(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	// Nothing is pre-approved by rule; the task itself allows everything.
	ps.getTask = func(ctx context.Context, taskID int64) (model.Task, error) {
		return model.Task{ID: taskID, AllowAllCommands: true}, nil
	}

	ps.HandleCustomNotification(context.Background(), "sess-1", permissionNotification("req-2", "Read"))

	waitForVerdictDelivered(t, ps, "req-2")

	if got := rec.count("permission_auto_allow"); got != 1 {
		t.Errorf("expected 1 auto approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("a task-level auto-allow must not count as a manual approval, got %d (all: %v)",
			got, rec.snapshot())
	}
}

// The counter still has to work: a verdict a human actually gave is manual.
func TestHumanVerdictIsCountedAsAManualApproval(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-3"] = "sess-1"

	_ = ps.SendPermissionVerdict(context.Background(), 42, "req-3", "allow")

	if got := rec.count("permission_manual_allow"); got != 1 {
		t.Errorf("expected 1 manual approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_auto_allow"); got != 0 {
		t.Errorf("a human verdict is not an automatic one, got %d auto (all: %v)",
			got, rec.snapshot())
	}
}

// A human denial is a denial, and still not an approval of either kind.
func TestHumanDenialIsCountedAsAManualDenial(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-4"] = "sess-1"

	_ = ps.SendPermissionVerdict(context.Background(), 42, "req-4", "deny")

	if got := rec.count("permission_manual_deny"); got != 1 {
		t.Errorf("expected 1 manual denial, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("a denial must not count as an approval, got %d (all: %v)", got, rec.snapshot())
	}
}

// "Allow always" is a human decision made once, so it counts as one manual
// approval — the automatic ones it licenses are counted as they happen.
func TestAllowAlwaysIsCountedAsAManualApproval(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-5"] = "sess-1"
	ps.requestTools["req-5"] = "Read"
	ps.requestParams["req-5"] = &PermissionRequestParams{RequestID: "req-5", ToolName: "Read"}

	_ = ps.SendPermissionVerdict(context.Background(), 42, "req-5", "allow_always")

	if got := rec.count("permission_manual_allow"); got != 1 {
		t.Errorf("expected 1 manual approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_auto_allow"); got != 0 {
		t.Errorf("the decision itself is manual, got %d auto (all: %v)", got, rec.snapshot())
	}
}

// Re-delivering a verdict the agent missed is not a second decision.
//
// When an agent reconnects it re-sends the request it was waiting on, and the
// server answers with the verdict it already has rather than asking the human
// again. That replay went through the same delivery path, so it reported a
// second manual approval for one decision — an agent that reconnected twice
// turned one click into three.
func TestReplayedVerdictIsNotCountedAgain(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-6"] = "sess-1"
	ps.requestTaskIDs["req-6"] = 42
	// rebind only treats a request as a re-send if the human was asked about it,
	// which is recorded by the message posted to the task.
	ps.permissionResponses["req-6"] = 900

	// The human decides, and the agent is gone, so the verdict is kept.
	ps.rememberUndeliveredVerdict("req-6", "allow")
	_ = ps.SendPermissionVerdict(context.Background(), 42, "req-6", "allow")

	if got := rec.count("permission_manual_allow"); got != 1 {
		t.Fatalf("the decision itself should count once, got %d (all: %v)", got, rec.snapshot())
	}

	// The agent comes back on a new session and asks again.
	if !ps.rebindPermissionRequest(context.Background(), "sess-2",
		PermissionRequestParams{RequestID: "req-6", ToolName: "Read"}) {
		t.Fatal("a re-sent request should be recognised as a re-send")
	}

	if got := rec.count("permission_manual_allow"); got != 1 {
		t.Errorf("re-delivering one decision must not count it twice, got %d (all: %v)",
			got, rec.snapshot())
	}
	if got := rec.count("permission_auto_allow"); got != 0 {
		t.Errorf("a replay is not an automatic approval either, got %d (all: %v)",
			got, rec.snapshot())
	}
}

// Stopping a task refuses what it was waiting on, on the human's behalf — so
// that is a manual denial, not an automatic one.
func TestStopRefusingOutstandingRequestsCountsAsManual(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-7"] = "sess-1"
	ps.requestTaskIDs["req-7"] = 42

	// The return value counts refusals actually delivered to an agent, and this
	// harness has no live session; the verdict is still recorded, which is what
	// this test is about.
	ps.denyOutstandingRequests(context.Background(), 42)

	if got := rec.count("permission_manual_deny"); got != 1 {
		t.Errorf("expected 1 manual denial, got %d (all: %v)", got, rec.snapshot())
	}
}

// An auto-allowed tool call is one approval and one tool call, however many
// times the agent has to ask for it.
//
// An agent that reconnects re-sends the request it was waiting on. For a
// request a human was asked about, rebindPermissionRequest recognises that and
// re-delivers the verdict already given. An auto-allowed request never posts a
// message to the task, so it had no such record and was not recognised: the
// auto-allow branch simply ran again, reporting a second automatic approval and
// writing a second tool_calls row for the one call.
func TestResentAutoAllowedRequestIsCountedOnce(t *testing.T) {
	ps, rec := approvalServer(t, []string{"Read"})
	rows := &recordedToolCalls{}
	withToolCallRecording(ps, rows)

	ps.HandleCustomNotification(context.Background(), "sess-1", permissionNotification("req-8", "Read"))
	waitForVerdictDelivered(t, ps, "req-8")

	if got := rec.count("permission_auto_allow"); got != 1 {
		t.Fatalf("expected 1 automatic approval to begin with, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rows.len(); got != 1 {
		t.Fatalf("expected 1 tool call row to begin with, got %d", got)
	}

	// The agent reconnects and asks for the very same request again.
	ps.HandleCustomNotification(context.Background(), "sess-2", permissionNotification("req-8", "Read"))
	waitForVerdictDelivered(t, ps, "req-8")

	if got := rec.count("permission_auto_allow"); got != 1 {
		t.Errorf("a re-sent request is the same approval, got %d automatic (all: %v)",
			got, rec.snapshot())
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("and it is certainly not a manual one, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rows.len(); got != 1 {
		t.Errorf("a re-sent request is the same tool call, got %d rows", got)
	}
}

// Recognising a re-send is only half the job: the agent has to be answered.
//
// It re-sent the request because it never saw the verdict, so a fix that merely
// stops re-deciding would leave it holding a question forever — trading a
// double count for a stalled agent, which is the worse bug.
func TestResentAutoAllowedRequestIsAnsweredAgain(t *testing.T) {
	ps, rec := approvalServer(t, []string{"Read"})

	ps.HandleCustomNotification(context.Background(), "sess-1", permissionNotification("req-9", "Read"))
	waitForVerdictDelivered(t, ps, "req-9")

	// Delivery failed (this harness has no live agent), so the verdict is being
	// held. Clear that record so a second delivery attempt is observable rather
	// than indistinguishable from the first.
	ps.undeliveredVerdictsMu.Lock()
	delete(ps.undeliveredVerdicts, "req-9")
	ps.undeliveredVerdictsMu.Unlock()

	// The agent comes back and asks again.
	ps.HandleCustomNotification(context.Background(), "sess-2", permissionNotification("req-9", "Read"))

	// It is answered: the verdict reaches delivery a second time.
	waitForVerdictDelivered(t, ps, "req-9")

	ps.undeliveredVerdictsMu.RLock()
	behavior := ps.undeliveredVerdicts["req-9"]
	ps.undeliveredVerdictsMu.RUnlock()
	if behavior != "allow" {
		t.Errorf("the re-sent request should have been allowed again, got %q", behavior)
	}

	// And answering it again is still not another approval.
	if got := rec.count("permission_auto_allow"); got != 1 {
		t.Errorf("expected 1 automatic approval, got %d (all: %v)", got, rec.snapshot())
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("expected no manual approval, got %d (all: %v)", got, rec.snapshot())
	}
}

// A request the human was asked about keeps its old behaviour: the auto-decided
// signal must widen what rebind recognises, not replace it.
func TestManualRequestStillRebindsWithoutTheAutoSignal(t *testing.T) {
	ps, rec := approvalServer(t, nil)
	ps.permissionRequests["req-10"] = "sess-1"
	ps.requestTaskIDs["req-10"] = 42
	ps.permissionResponses["req-10"] = 900

	if ps.wasAutoDecided("req-10") {
		t.Fatal("a request nobody auto-allowed must not be marked auto-decided")
	}

	ps.rememberUndeliveredVerdict("req-10", "allow")
	if !ps.rebindPermissionRequest(context.Background(), "sess-2",
		PermissionRequestParams{RequestID: "req-10", ToolName: "Read"}) {
		t.Fatal("a request the human answered should still be recognised as a re-send")
	}
	if got := rec.count("permission_manual_allow"); got != 0 {
		t.Errorf("a replay reports nothing, got %d (all: %v)", got, rec.snapshot())
	}
}

// waitForVerdictDelivered blocks until the auto-allow goroutine has actually
// run its verdict through sendVerdict.
//
// Waiting on the auto-allow telemetry would not do: that is published
// synchronously, before the goroutine has even started its settle sleep, so the
// wait would return immediately and only a fixed sleep would stand between the
// assertions and the emit they are checking for — a test that passes on a
// loaded machine whether or not the bug is present.
//
// This harness has no live agent session, so delivery always fails and the
// goroutine always records the verdict as undelivered. That write happens after
// the point where the manual event would have been emitted, which makes it an
// exact signal that the goroutine is past it.
func waitForVerdictDelivered(t *testing.T, ps *WorkspaceServer, requestID string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		ps.undeliveredVerdictsMu.RLock()
		_, arrived := ps.undeliveredVerdicts[requestID]
		ps.undeliveredVerdictsMu.RUnlock()
		if arrived {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the verdict for %s never reached delivery", requestID)
}
