package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// The agent.connected payload names the workspace the way the REST API does.
//
// It used to carry the raw int64 while view.Workspace exposes the base62 ID, so
// no lookup on the client ever matched and the live connection indicator never
// moved off whatever the last page load had fetched.
func TestPublishAgentConnected_NamesTheWorkspaceInBase62(t *testing.T) {
	const workspaceID int64 = 1234567890123
	const userID = "0iAx25vra8v"

	bus := eventbus.New()
	sub := bus.Subscribe(workspaceID, "")
	defer bus.Unsubscribe(workspaceID, "", sub)

	ps := &WorkspaceServer{bus: bus, workspaceID: workspaceID, userID: userID}

	for _, connected := range []bool{true, false} {
		ps.publishAgentConnected(connected)

		evt := readEvent(t, sub)
		if evt.Type != "agent.connected" {
			t.Fatalf("event type = %q, want agent.connected", evt.Type)
		}
		if got, want := evt.Payload["workspaceId"], monoflake.ID(workspaceID).String(); got != want {
			t.Errorf("workspaceId = %#v, want %q", got, want)
		}
		if got := evt.Payload["connected"]; got != connected {
			t.Errorf("connected = %#v, want %v", got, connected)
		}
	}
}

// The workspace owner's own stream carries it too: the sidebar subscribes
// user-wide, not per workspace, and that is the indicator this bug was about.
func TestPublishAgentConnected_ReachesTheUserWideStream(t *testing.T) {
	const workspaceID int64 = 987654321
	const userID = "0ZzhYQG2qtl"

	bus := eventbus.New()
	sub := bus.Subscribe(0, userID)
	defer bus.Unsubscribe(0, userID, sub)

	ps := &WorkspaceServer{bus: bus, workspaceID: workspaceID, userID: userID}
	ps.publishAgentConnected(true)

	evt := readEvent(t, sub)
	if got, want := evt.Payload["workspaceId"], monoflake.ID(workspaceID).String(); got != want {
		t.Errorf("workspaceId = %#v, want %q", got, want)
	}
}

type busEvent struct {
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

// readEvent takes the next frame off an eventbus subscription and strips the
// SSE framing the bus adds.
func readEvent(t *testing.T, sub chan []byte) busEvent {
	t.Helper()

	select {
	case line := <-sub:
		const prefix = "data: "
		if len(line) < len(prefix) || string(line[:len(prefix)]) != prefix {
			t.Fatalf("frame is not SSE-framed: %q", line)
		}
		var evt busEvent
		if err := json.Unmarshal(line[len(prefix):], &evt); err != nil {
			t.Fatalf("unmarshal event: %v (frame %q)", err, line)
		}
		return evt
	case <-time.After(time.Second):
		t.Fatal("no event published")
		return busEvent{}
	}
}
