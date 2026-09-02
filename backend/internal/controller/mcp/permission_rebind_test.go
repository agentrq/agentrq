package mcp

import (
	"context"
	"encoding/json"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// permissionServer builds a workspace server with just enough wired up to take
// a permission request and answer it, and reports what it posted to the task.
func permissionServer(t *testing.T, replies *int) *WorkspaceServer {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pubsubMock := mock_pubsub.NewMockService(ctrl)
	pubsubMock.EXPECT().Publish(gomock.Any(), gomock.Any()).AnyTimes()

	return &WorkspaceServer{
		workspaceID:         100,
		pubsub:              pubsubMock,
		permissionRequests:  make(map[string]string),
		requestTools:        make(map[string]string),
		requestParams:       make(map[string]*PermissionRequestParams),
		sessionTasks:        map[string]int64{"sess-old": 42},
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
			*replies++
			return int64(700 + *replies), nil
		},
	}
}

func permissionRequestJSON(requestID string) []byte {
	body, _ := json.Marshal(map[string]any{
		"method": "notifications/claude/channel/permission_request",
		"params": map[string]any{
			"request_id":    requestID,
			"tool_name":     "Bash",
			"description":   "run tests",
			"input_preview": `{"command":"go test ./..."}`,
		},
	})
	return body
}

// A permission request the workspace has never seen must be relayed to the
// human as usual.
func TestRebindPermissionRequest_UnknownRequestIsAskedNormally(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)

	if ps.rebindPermissionRequest(context.Background(), "sess-new", PermissionRequestParams{RequestID: "req-1"}) {
		t.Fatal("an unasked request must not be treated as a re-send")
	}
}

// Without a session there is nowhere to re-point the request to.
func TestRebindPermissionRequest_NoSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.permissionResponses["req-1"] = 700

	if ps.rebindPermissionRequest(context.Background(), "", PermissionRequestParams{RequestID: "req-1"}) {
		t.Fatal("a request with no session must not be treated as a re-send")
	}
}

// The agent reconnected while waiting: the request is re-pointed at its new
// session, and the human is not asked a second time.
func TestHandleCustomNotification_ResendIsReboundNotReasked(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)

	ps.HandleCustomNotification(context.Background(), "sess-old", permissionRequestJSON("req-1"))
	if replies != 1 {
		t.Fatalf("expected the request to be relayed once, got %d", replies)
	}
	if got := ps.permissionRequests["req-1"]; got != "sess-old" {
		t.Fatalf("expected the request bound to sess-old, got %q", got)
	}

	// Same request id, new connection.
	ps.HandleCustomNotification(context.Background(), "sess-new", permissionRequestJSON("req-1"))

	if replies != 1 {
		t.Fatalf("expected no second approval card, got %d replies", replies)
	}
	if got := ps.permissionRequests["req-1"]; got != "sess-new" {
		t.Fatalf("expected the request re-bound to sess-new, got %q", got)
	}
}

// A decision the agent never received is kept, not discarded.
func TestSendPermissionVerdict_UndeliverableVerdictIsKept(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.permissionRequests["req-1"] = "sess-gone"
	ps.permissionResponses["req-1"] = 700
	ps.requestTaskIDs["req-1"] = 42

	// No MCP server, so there is no session to deliver to — the same situation
	// as an agent that reconnected since asking.
	if err := ps.SendPermissionVerdict(context.Background(), 42, "req-1", "allow"); err == nil {
		t.Fatal("expected an error when the session is gone")
	}

	if got := ps.undeliveredVerdicts["req-1"]; got != "allow" {
		t.Fatalf("expected the verdict to be kept, got %q", got)
	}
	// Forgetting the request here is what used to lose the decision: the agent
	// would re-send and the human would be asked all over again.
	if _, ok := ps.permissionResponses["req-1"]; !ok {
		t.Fatal("expected the request to still be known")
	}
}

// Reconnecting after the human has answered gets the answer, not the question.
func TestRebindPermissionRequest_DeliversTheMissedVerdict(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.permissionRequests["req-1"] = "sess-gone"
	ps.permissionResponses["req-1"] = 700
	ps.requestTaskIDs["req-1"] = 42
	_ = ps.SendPermissionVerdict(context.Background(), 42, "req-1", "allow")

	// Still no session to deliver on, but the attempt is what is being checked:
	// the held verdict must be sent again rather than the question re-asked.
	rebound := ps.rebindPermissionRequest(context.Background(), "sess-new", PermissionRequestParams{RequestID: "req-1"})

	if !rebound {
		t.Fatal("expected the re-sent request to be recognised")
	}
	if replies != 0 {
		t.Fatalf("expected no new approval card, got %d", replies)
	}
	if got := ps.permissionRequests["req-1"]; got != "sess-new" {
		t.Fatalf("expected the request re-bound to sess-new, got %q", got)
	}
	// The verdict is still held, ready for whenever the agent can be reached.
	if got := ps.undeliveredVerdicts["req-1"]; got != "allow" {
		t.Fatalf("expected the verdict still held, got %q", got)
	}
}

// A request answered successfully is forgotten, so a later request reusing the
// id is a genuinely new question.
func TestCleanupRequest_ForgetsTheHeldVerdict(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	ps.undeliveredVerdicts["req-1"] = "allow"
	ps.permissionResponses["req-1"] = 700

	ps.cleanupRequest("req-1")

	if _, ok := ps.undeliveredVerdicts["req-1"]; ok {
		t.Fatal("expected the held verdict to be forgotten with the request")
	}
	if ps.rebindPermissionRequest(context.Background(), "sess-new", PermissionRequestParams{RequestID: "req-1"}) {
		t.Fatal("a forgotten request must be asked afresh")
	}
}

// notifySession must not panic on a server with no MCP server attached.
func TestNotifySession_NoServer(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)

	if ps.notifySession(context.Background(), "sess-1", "some/method", map[string]any{}) {
		t.Fatal("expected no delivery without an MCP server")
	}
}

// connectedServer attaches a real MCP server with one live client session, so
// that delivering a verdict actually goes somewhere. Returns the session id and
// a channel of the notification methods the client receives.
func connectedServer(t *testing.T, ps *WorkspaceServer) string {
	t.Helper()

	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	ps.mcpServer = server

	client := mcp.NewClient(&mcp.Implementation{Name: "agent", Version: "0"}, &mcp.ClientOptions{
		KeepAlive: 0,
	})

	serverTransport, clientTransport := mcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	return serverSession.ID()
}

// The verdict reaches an agent that is still connected, and the request is then
// forgotten.
func TestSendPermissionVerdict_DeliversToALiveSession(t *testing.T) {
	replies := 0
	ps := permissionServer(t, &replies)
	sessID := connectedServer(t, ps)

	ps.permissionRequests["req-1"] = sessID
	ps.permissionResponses["req-1"] = 700
	ps.requestTaskIDs["req-1"] = 42
	ps.updateMessageMetadata = func(ctx context.Context, taskID int64, messageID int64, metadata any) error {
		return nil
	}

	if err := ps.SendPermissionVerdict(context.Background(), 42, "req-1", "allow"); err != nil {
		t.Fatalf("expected the verdict delivered, got %v", err)
	}

	if _, ok := ps.permissionResponses["req-1"]; ok {
		t.Fatal("expected the answered request to be forgotten")
	}
	if _, ok := ps.undeliveredVerdicts["req-1"]; ok {
		t.Fatal("a delivered verdict must not be held")
	}
}
