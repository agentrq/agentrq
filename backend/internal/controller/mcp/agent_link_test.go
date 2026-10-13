// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_auth "github.com/agentrq/agentrq/backend/internal/service/mocks/auth"
	"github.com/golang/mock/gomock"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// fakeLink records every call a WorkspaceServer makes on a link.
type fakeLink struct {
	id      string
	owns    map[int64]bool
	free    int
	hasTask map[int64]bool

	mu       sync.Mutex
	tasks    []string // fmtTask(taskID, content)
	stops    []int64
	verdicts []string // requestID=behavior
	models   []string // session/config/model
	limits   []int
}

func (l *fakeLink) ID() string { return l.id }

func (l *fakeLink) DeliverTask(_ context.Context, taskID int64, content string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tasks = append(l.tasks, fmtTask(taskID, content))
}

func (l *fakeLink) CancelTask(_ context.Context, taskID int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.stops = append(l.stops, taskID)
	return l.hasTask[taskID]
}

func (l *fakeLink) DeliverVerdict(_ context.Context, requestID, behavior string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.verdicts = append(l.verdicts, requestID+"="+behavior)
	return true
}

func (l *fakeLink) SetModel(_ context.Context, sessionID, configID, modelID string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.models = append(l.models, sessionID+"/"+configID+"/"+modelID)
	return true
}

func (l *fakeLink) SetConcurrency(_ context.Context, limit int) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.limits = append(l.limits, limit)
	return true
}

func (l *fakeLink) Dispatch(taskID int64) (bool, int) { return l.owns[taskID], l.free }

// recorded returns a copy of what the link was handed, safe to read while a
// goroutine may still be calling it.
func (l *fakeLink) recorded() (tasks []string, stops []int64, verdicts []string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string(nil), l.tasks...), append([]int64(nil), l.stops...), append([]string(nil), l.verdicts...)
}

func fmtTask(taskID int64, content string) string {
	b, _ := json.Marshal([]any{taskID, content})
	return string(b)
}

// newLinkServer builds a WorkspaceServer with only what the link paths touch,
// and a subscription to its events.
func newLinkServer(t *testing.T) (*WorkspaceServer, chan []byte) {
	t.Helper()
	bus := eventbus.New()
	events := bus.Subscribe(100, "user-1")
	t.Cleanup(func() { bus.Unsubscribe(100, "user-1", events) })
	return &WorkspaceServer{
		workspaceID:            100,
		userID:                 "user-1",
		bus:                    bus,
		agentTelemetryMessages: make(map[string]int64),
		agentModels:            make(map[string]AgentModelsSnapshot),
		agentCommands:          make(map[string]AgentCommandsSnapshot),
		agentIdentities:        make(map[string]AgentClientInfo),
		agentConcurrency:       make(map[string]AgentConcurrencySnapshot),
		permissionRequests:     make(map[string]string),
		requestTaskIDs:         make(map[string]int64),
	}, events
}

// connectedEvents drains the agent.connected values published so far.
func connectedEvents(t *testing.T, events chan []byte) []bool {
	t.Helper()
	var got []bool
	for {
		select {
		case raw := <-events:
			var e struct {
				Type    string `json:"type"`
				Payload struct {
					Connected bool `json:"connected"`
				} `json:"payload"`
			}
			line := strings.TrimSpace(strings.TrimPrefix(string(raw), "data: "))
			if err := json.Unmarshal([]byte(line), &e); err == nil && e.Type == "agent.connected" {
				got = append(got, e.Payload.Connected)
			}
		case <-time.After(50 * time.Millisecond):
			return got
		}
	}
}

// syncBuffer is a bytes.Buffer an SDK goroutine can write to while the test
// reads it.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// withMCPSession gives ps a real MCP server with one streaming in-memory
// session, and returns what that session's client has read so far.
func withMCPSession(t *testing.T, ps *WorkspaceServer) (string, *syncBuffer) {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	// SendChannelNotification logs each session's auth state.
	tokenMock := mock_auth.NewMockTokenService(ctrl)
	tokenMock.EXPECT().ValidateToken(gomock.Any()).Return(nil, errNotTheRealThing).AnyTimes()
	ps.tokenSvc = tokenMock
	ps.mcpServer = mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	ctx := context.Background()
	serverT, clientT := mcp.NewInMemoryTransports()
	ss, err := ps.mcpServer.Connect(ctx, serverT, nil)
	if err != nil {
		t.Fatalf("connect server: %v", err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	log := &syncBuffer{}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "acp-gateway", Version: "0"}, nil).
		Connect(ctx, &mcp.LoggingTransport{Transport: clientT, Writer: log}, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	t.Cleanup(streamFor(ps, ss.ID()))
	return ss.ID(), log
}

// channelPushesRead counts the channel pushes a client has read.
func channelPushesRead(log *syncBuffer) int {
	n := 0
	for _, line := range strings.Split(log.String(), "\n") {
		if strings.HasPrefix(line, "read: ") && strings.Contains(line, `"method":"notifications/claude/channel"`) {
			n++
		}
	}
	return n
}

func TestAgentLinks(t *testing.T) {
	t.Run("a workspace with only a link attached counts as connected and says so once", func(t *testing.T) {
		ps, events := newLinkServer(t)
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)
		ps.AttachAgentLink(link) // twice: still one link, still one event

		if !ps.IsAgentConnected() {
			t.Fatal("IsAgentConnected returned false with a link attached, want true")
		}
		if got := connectedEvents(t, events); len(got) != 1 || !got[0] {
			t.Fatalf("agent.connected events were %v, want exactly [true]", got)
		}
		if ids := ps.streamingSessionIDs(); len(ids) != 1 || ids[0] != "acp1:a" {
			t.Fatalf("streaming sessions were %v, want [acp1:a]", ids)
		}
	})

	t.Run("detaching the last link says disconnected and ends what the link reported", func(t *testing.T) {
		ps, events := newLinkServer(t)
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)
		ps.HandleAgentModels(context.Background(), "acp1:a", AgentModelsParams{
			SessionID: "s1", ConfigID: "model", CurrentModel: "m1",
			Models: []AgentModel{{ID: "m1", Name: "Model one"}},
		})
		if m := ps.AgentModels(); m == nil {
			t.Fatal("AgentModels returned nil while the link was attached, want its snapshot")
		}
		ps.DetachAgentLink(context.Background(), "acp1:a")
		ps.DetachAgentLink(context.Background(), "acp1:a") // a second detach changes nothing

		if ps.IsAgentConnected() {
			t.Fatal("IsAgentConnected returned true after the only link left, want false")
		}
		got := connectedEvents(t, events)
		if len(got) != 2 || !got[0] || got[1] {
			t.Fatalf("agent.connected events were %v, want [true false]", got)
		}
		if m := ps.AgentModels(); m != nil {
			t.Fatalf("AgentModels returned %+v after the link left, want nil", m)
		}
	})

	t.Run("a push goes to the link already holding the task, not the freer one", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		holder := &fakeLink{id: "acp1:a", owns: map[int64]bool{7: true}, free: 0}
		freer := &fakeLink{id: "acp1:b", free: 3}
		ps.AttachAgentLink(holder)
		ps.AttachAgentLink(freer)

		ps.SendChannelNotification(context.Background(), 7, "hello")

		if len(holder.tasks) != 1 || holder.tasks[0] != fmtTask(7, "hello") || len(freer.tasks) != 0 {
			t.Fatalf("holder got %v and freer got %v, want only the holder to get the task", holder.tasks, freer.tasks)
		}
	})

	t.Run("a push goes to the link with the most free capacity when none holds the task", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		busy := &fakeLink{id: "acp1:a", free: 0}
		idle := &fakeLink{id: "acp1:b", free: 2}
		ps.AttachAgentLink(busy)
		ps.AttachAgentLink(idle)

		ps.SendChannelNotification(context.Background(), 9, "work")

		if len(idle.tasks) != 1 || len(busy.tasks) != 0 {
			t.Fatalf("idle got %v and busy got %v, want only the idle link to get the task", idle.tasks, busy.tasks)
		}
	})

	t.Run("a verdict for a request a link raised reaches that link", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)
		ps.permissionRequests["acp1:a:s1:t1"] = "acp1:a"

		if !ps.notifySession(context.Background(), "acp1:a", "notifications/claude/channel/permission",
			map[string]any{"request_id": "acp1:a:s1:t1", "behavior": "allow"}) {
			t.Fatal("notifySession returned false for a live link, want true")
		}
		if len(link.verdicts) != 1 || link.verdicts[0] != "acp1:a:s1:t1=allow" {
			t.Fatalf("link verdicts were %v, want [acp1:a:s1:t1=allow]", link.verdicts)
		}
	})

	t.Run("an MCP stream opening while a link is attached publishes no second connected event", func(t *testing.T) {
		ps, events := newLinkServer(t)
		ps.AttachAgentLink(&fakeLink{id: "acp1:a"})
		connectedEvents(t, events)

		ps.agentConnections.Add(1)
		ps.refreshConnected()

		if got := connectedEvents(t, events); len(got) != 0 {
			t.Fatalf("agent.connected events were %v, want none", got)
		}
	})

	t.Run("a push reaches the MCP sessions and exactly one link", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		_, log := withMCPSession(t, ps)
		first := &fakeLink{id: "acp1:a", free: 1}
		second := &fakeLink{id: "acp1:b", free: 1}
		ps.AttachAgentLink(first)
		ps.AttachAgentLink(second)

		ps.SendChannelNotification(context.Background(), 7, "next task")

		deadline := time.Now().Add(2 * time.Second)
		for channelPushesRead(log) == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if n := channelPushesRead(log); n != 1 {
			t.Fatalf("the MCP client read %d channel pushes, want 1", n)
		}
		if len(first.tasks) != 1 || len(second.tasks) != 0 {
			t.Fatalf("first got %v and second got %v, want only the first (a tie goes to the first attached)", first.tasks, second.tasks)
		}
	})

	t.Run("a push with no link attached leaves the MCP path as it was", func(t *testing.T) {
		ps, events := newLinkServer(t)

		ps.SendChannelNotification(context.Background(), 7, "next task")

		if got := connectedEvents(t, events); len(got) != 0 {
			t.Fatalf("agent.connected events were %v, want none", got)
		}
	})

	t.Run("Stop reaches every link and counts the one that had the task", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		a := &fakeLink{id: "acp1:a", hasTask: map[int64]bool{5: true}}
		b := &fakeLink{id: "acp1:b"}
		ps.AttachAgentLink(a)
		ps.AttachAgentLink(b)

		if !ps.SendCancelNotification(context.Background(), 5).Stopped {
			t.Fatal("SendCancelNotification did not report the task stopped, want Stopped")
		}
		if len(a.stops) != 1 || a.stops[0] != 5 || len(b.stops) != 1 || b.stops[0] != 5 {
			t.Fatalf("a saw stops %v and b saw %v, want both [5]", a.stops, b.stops)
		}
	})

	t.Run("Stop where no link has the task denies the approvals the task waits on", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		ps.getTask = func(ctx context.Context, taskID int64) (model.Task, error) { return model.Task{ID: taskID}, nil }
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)
		ps.requestTaskIDs["r1"] = 5
		ps.permissionRequests["r1"] = "acp1:a"

		out := ps.SendCancelNotification(context.Background(), 5)

		if out.Stopped || out.ApprovalsDenied != 1 {
			t.Fatalf("SendCancelNotification returned %+v, want one approval denied and nothing stopped", out)
		}
		if _, _, verdicts := link.recorded(); len(verdicts) != 1 || verdicts[0] != "r1=deny" {
			t.Fatalf("link verdicts were %v, want [r1=deny]", verdicts)
		}
	})

	t.Run("set_model for a link's snapshot reaches the link as a typed call", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)
		ps.HandleAgentModels(context.Background(), "acp1:a", AgentModelsParams{
			SessionID: "s1", ConfigID: "model", CurrentModel: "m1", CanSet: true,
			Models: []AgentModel{{ID: "m1", Name: "Model one"}, {ID: "m2", Name: "Model two"}},
		})

		if err := ps.SendSetModelNotification(context.Background(), "m2"); err != nil {
			t.Fatalf("SendSetModelNotification returned %v, want nil", err)
		}
		if len(link.models) != 1 || link.models[0] != "s1/model/m2" {
			t.Fatalf("link models were %v, want [s1/model/m2]", link.models)
		}
	})

	t.Run("set_concurrency for a link's snapshot reaches the link as a typed call", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)
		limit, canSet := 1, true
		ps.HandleAgentConcurrency(context.Background(), "acp1:a", AgentConcurrencyParams{MaxConcurrency: &limit, CanSet: &canSet})

		if err := ps.SendSetConcurrencyNotification(context.Background(), 3); err != nil {
			t.Fatalf("SendSetConcurrencyNotification returned %v, want nil", err)
		}
		if len(link.limits) != 1 || link.limits[0] != 3 {
			t.Fatalf("link limits were %v, want [3]", link.limits)
		}
	})

	t.Run("live and streaming lists put MCP sessions first, then links in attach order", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		m, _ := withMCPSession(t, ps)
		ps.AttachAgentLink(&fakeLink{id: "acp1:b"})
		ps.AttachAgentLink(&fakeLink{id: "acp1:a"})

		want := []string{m, "acp1:b", "acp1:a"}
		for name, got := range map[string][]string{"live": ps.liveSessionIDs(), "streaming": ps.streamingSessionIDs()} {
			if strings.Join(got, ",") != strings.Join(want, ",") {
				t.Errorf("%s sessions were %v, want %v", name, got, want)
			}
		}
	})

	t.Run("AgentClient names the agent a link reported", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		ps.AttachAgentLink(&fakeLink{id: "acp1:a"})
		ps.HandleAgentIdentity(context.Background(), "acp1:a", AgentIdentityParams{Name: "codex-acp", Version: "0.9"})

		got := ps.AgentClient()
		if got == nil || *got != (AgentClientInfo{Name: "codex-acp", Version: "0.9"}) {
			t.Fatalf("AgentClient returned %+v, want codex-acp 0.9", got)
		}
	})

	t.Run("AgentClient names nobody when no link has reported an agent", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		ps.AttachAgentLink(&fakeLink{id: "acp1:a"})

		if got := ps.AgentClient(); got != nil {
			t.Fatalf("AgentClient returned %+v, want nil", got)
		}
	})

	t.Run("SupportsStop is true with only a link attached", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		if ps.SupportsStop() {
			t.Fatal("SupportsStop returned true with nothing attached, want false")
		}
		ps.AttachAgentLink(&fakeLink{id: "acp1:a"})
		if !ps.SupportsStop() {
			t.Fatal("SupportsStop returned false with a link attached, want true")
		}
	})

	t.Run("nothing attached means not connected and nothing published", func(t *testing.T) {
		ps, events := newLinkServer(t)
		ps.refreshConnected()

		if ps.IsAgentConnected() {
			t.Fatal("IsAgentConnected returned true with nothing attached, want false")
		}
		if got := connectedEvents(t, events); len(got) != 0 {
			t.Fatalf("agent.connected events were %v, want none", got)
		}
	})

	t.Run("notifySession refuses a channel method a link is never sent", func(t *testing.T) {
		ps, _ := newLinkServer(t)
		link := &fakeLink{id: "acp1:a"}
		ps.AttachAgentLink(link)

		if ps.notifySession(context.Background(), "acp1:a", "notifications/claude/channel", map[string]any{"content": "x"}) {
			t.Fatal("notifySession returned true for a push addressed to a link, want false")
		}
		if tasks, stops, verdicts := link.recorded(); len(tasks)+len(stops)+len(verdicts) != 0 {
			t.Fatalf("the link was called: tasks %v, stops %v, verdicts %v", tasks, stops, verdicts)
		}
	})
}
