// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mustafaturan/monoflake"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
)

// recorder is a Workspace that writes down everything the engine asked of it.
// log keeps the order of the replies and telemetry, as the human reads them.
type recorder struct {
	mu        sync.Mutex
	links     []agentlink.Link
	replies   []string // "taskID|text"
	telemetry []agentlink.TelemetryReport
	models    []agentlink.ModelsReport
	effort    []agentlink.EffortReport
	commands  []agentlink.CommandsReport
	conc      []agentlink.ConcurrencyReport
	ongoing   []int64
	offers    int
	identity  []agentlink.IdentityReport
	detached  []string
	log       []string
	connects  int
	turns     int
	logouts   int
	failReply error
	failMark  error
	onAttach  func(agentlink.Link) // runs as the link attaches, as a push may

	permissions []agentlink.PermissionRequest
	closed      []string // permission requests closed
	toolCalls   []recordedCall
	callStatus  []string // "row|status"
	questions   []askedQuestion
	humanTasks  []string // "title|body"
	ask         func(ctx context.Context, q agentlink.Question) (agentlink.Answer, error)
	failCreate  error
}

// recordedCall is one RecordToolCall, with the row it was given.
type recordedCall struct {
	taskID int64
	row    int64
	call   agentlink.ToolCall
}

// askedQuestion is one AskHuman call.
type askedQuestion struct {
	taskID int64
	q      agentlink.Question
}

func (r *recorder) AttachAgentLink(l agentlink.Link) {
	r.mu.Lock()
	r.links = append(r.links, l)
	onAttach := r.onAttach
	r.mu.Unlock()
	if onAttach != nil {
		onAttach(l)
	}
}

func (r *recorder) DetachAgentLink(_ context.Context, id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.detached = append(r.detached, id)
}

func (r *recorder) HandleAgentIdentity(_ context.Context, _ string, p agentlink.IdentityReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.identity = append(r.identity, p)
}

func (r *recorder) HandleAgentModels(_ context.Context, _ string, p agentlink.ModelsReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.models = append(r.models, p)
}

func (r *recorder) HandleAgentEffort(_ context.Context, _ string, p agentlink.EffortReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.effort = append(r.effort, p)
}

func (r *recorder) HandleAgentCommands(_ context.Context, _ string, p agentlink.CommandsReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.commands = append(r.commands, p)
}

func (r *recorder) HandleAgentConcurrency(_ context.Context, _ string, p agentlink.ConcurrencyReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.conc = append(r.conc, p)
}

func (r *recorder) HandleAgentTelemetry(_ context.Context, _ string, p agentlink.TelemetryReport) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.telemetry = append(r.telemetry, p)
	r.log = append(r.log, p.Kind+":"+p.Text)
}

func (r *recorder) HandlePermissionRequest(_ context.Context, _ string, p agentlink.PermissionRequest) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.permissions = append(r.permissions, p)
}

func (r *recorder) ClosePermissionRequest(_ context.Context, requestID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = append(r.closed, requestID)
}

func (r *recorder) RecordToolCall(_ context.Context, taskID int64, c agentlink.ToolCall) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	row := int64(100 + len(r.toolCalls))
	r.toolCalls = append(r.toolCalls, recordedCall{taskID, row, c})
	return row
}

func (r *recorder) SetToolCallStatus(_ context.Context, id int64, status string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.callStatus = append(r.callStatus, fmt.Sprintf("%d|%s", id, status))
}

func (r *recorder) PostAgentReply(_ context.Context, _ string, taskID int64, text string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failReply != nil {
		return 0, r.failReply
	}
	r.replies = append(r.replies, fmt.Sprintf("%d|%s", taskID, text))
	r.log = append(r.log, "reply:"+text)
	return int64(len(r.replies)), nil
}

func (r *recorder) AskHuman(ctx context.Context, taskID int64, q agentlink.Question) (agentlink.Answer, error) {
	r.mu.Lock()
	r.questions = append(r.questions, askedQuestion{taskID, q})
	ask := r.ask
	r.mu.Unlock()
	if ask == nil {
		return agentlink.Answer{Action: "cancel"}, nil
	}
	return ask(ctx, q)
}

func (r *recorder) CreateHumanTask(_ context.Context, title, body string) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failCreate != nil {
		return 0, r.failCreate
	}
	r.humanTasks = append(r.humanTasks, title+"|"+body)
	return 77, nil
}

func (r *recorder) MarkTaskOngoing(_ context.Context, _ string, taskID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ongoing = append(r.ongoing, taskID)
	return r.failMark
}

func (r *recorder) OfferNextTask(context.Context) int64 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.offers++
	return 0
}

func (r *recorder) ToolNames() map[string]struct{} {
	return map[string]struct{}{"reply": {}, "task": {}, "updateTaskStatus": {}}
}

// read runs fn under the recorder's lock.
func (r *recorder) read(fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	fn()
}

// wait waits until cond, run under the recorder's lock, holds.
func (r *recorder) wait(t *testing.T, cond func() bool) {
	t.Helper()
	waitFor(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return cond() })
}

func (r *recorder) link(t *testing.T) agentlink.Link {
	t.Helper()
	r.wait(t, func() bool { return len(r.links) == 1 })
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.links[0]
}

// agent is the test's side of the socket: the bridge and the ACP agent in one.
// It counts the ACP frames it read and keeps those it sent, so it can resume
// the way the bridge does.
type agent struct {
	t        *testing.T
	srv      *httptest.Server
	hdr      http.Header
	ws       *websocket.Conn
	connID   string
	received int64
	sent     [][]byte
	backlog  []rpcMessage
	controls []control
	syncs    int
}

type agentOpts struct {
	header map[string]string
	deps   func(*EngineDeps)
}

func startAgent(t *testing.T, rec *recorder) *agent {
	t.Helper()
	return startAgentWith(t, rec, agentOpts{})
}

func startAgentWith(t *testing.T, rec *recorder, o agentOpts) *agent {
	t.Helper()
	deps := EngineDeps{
		Workspace:  func(int64, string) Workspace { return rec },
		MCPURL:     func(int64) string { return "https://agentrq.test/mcp/ws1" },
		AppVersion: "0.9.20",
	}
	if o.deps != nil {
		o.deps(&deps)
	}
	h := &Handler{Hub: NewHub(), Auth: fakeAuth{}, Enabled: func() bool { return true }, NewEngine: NewEngineFactory(deps)}
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	hdr := bridgeHeader("")
	for k, v := range o.header {
		hdr.Set(k, v)
	}
	ws, resp, err := dialWith(srv, hdr)
	if err != nil {
		t.Fatalf("dial failed: %v (status %v)", err, resp)
	}
	a := &agent{t: t, srv: srv, hdr: hdr, ws: ws}
	t.Cleanup(func() { _ = a.ws.Close() })
	a.connID = welcomeOf(t, readFrame(t, ws)).ConnectionID
	return a
}

// next returns the next ACP frame, keeping control frames aside and skipping
// acks.
func (a *agent) next() rpcMessage {
	a.t.Helper()
	for {
		f := readFrame(a.t, a.ws)
		if isControl([]byte(f)) {
			if ctl, _ := decodeControl([]byte(f)); ctl.Kind != "ack" {
				a.controls = append(a.controls, ctl)
			}
			continue
		}
		a.received++
		var m rpcMessage
		if err := json.Unmarshal([]byte(f), &m); err != nil {
			a.t.Fatalf("frame %s is not JSON-RPC: %v", f, err)
		}
		return m
	}
}

// expect returns the next frame calling method, keeping any other frame for
// a later expect: calls made on separate goroutines arrive in any order.
func (a *agent) expect(method string) rpcMessage {
	a.t.Helper()
	for i, m := range a.backlog {
		if m.Method == method {
			a.backlog = slices.Delete(a.backlog, i, i+1)
			return m
		}
	}
	for {
		m := a.next()
		if m.Method == method {
			return m
		}
		a.backlog = append(a.backlog, m)
	}
}

// expectParams is expect, also checking the params byte for byte.
func (a *agent) expectParams(method, params string) rpcMessage {
	a.t.Helper()
	m := a.expect(method)
	if string(m.Params) != params {
		a.t.Fatalf("%s params were\n%s\nwant\n%s", method, m.Params, params)
	}
	return m
}

// quiet checks that no ACP frame arrives for a while.
func (a *agent) quiet() {
	a.t.Helper()
	if len(a.backlog) > 0 {
		a.t.Fatalf("frames were waiting: %+v, want none", a.backlog)
	}
	_ = a.ws.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
	for {
		_, f, err := a.ws.ReadMessage()
		if err != nil {
			// A timed-out gorilla connection cannot be read again: open a
			// fresh socket on the same connection, as the bridge would.
			a.resume()
			return
		}
		if !isControl(f) {
			a.t.Fatalf("got %s, want no frame", f)
		}
	}
}

func (a *agent) write(frame string) {
	a.t.Helper()
	a.sent = append(a.sent, []byte(frame))
	_ = a.ws.WriteMessage(websocket.TextMessage, []byte(frame))
}

func (a *agent) answer(req rpcMessage, result string) {
	a.t.Helper()
	a.write(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"result":` + result + `}`)
}

func (a *agent) fail(req rpcMessage, code int, message string) {
	a.t.Helper()
	a.write(`{"jsonrpc":"2.0","id":` + string(req.ID) + `,"error":{"code":` + strconv.Itoa(code) + `,"message":"` + message + `"}}`)
}

func (a *agent) update(sessionID, update string) {
	a.t.Helper()
	a.write(`{"jsonrpc":"2.0","method":"session/update","params":{"sessionId":"` + sessionID + `","update":` + update + `}}`)
}

func (a *agent) chunk(sessionID, kind, text string) {
	a.t.Helper()
	a.update(sessionID, `{"sessionUpdate":"`+kind+`","content":{"type":"text","text":`+strconv.Quote(text)+`}}`)
}

// control sends the bridge's control frame.
func (a *agent) control(frame string) {
	a.t.Helper()
	_ = a.ws.WriteMessage(websocket.TextMessage, []byte(frame))
}

// drop loses the socket without a word, as a network blip does.
func (a *agent) drop() { _ = a.ws.Close() }

// resume opens a new socket on the same connection and sends again what the
// server says it did not get.
func (a *agent) resume() {
	a.t.Helper()
	_ = a.ws.Close()
	hdr := a.hdr.Clone()
	hdr.Set("X-AgentRQ-Resume", fmt.Sprintf("%s:%d", a.connID, a.received))
	ws, resp, err := dialWith(a.srv, hdr)
	if err != nil {
		a.t.Fatalf("resume failed: %v (status %v)", err, resp)
	}
	a.ws = ws
	w := welcomeOf(a.t, readFrame(a.t, ws))
	if w.Resumed == nil || !*w.Resumed {
		a.t.Fatalf("the resume was not taken: %+v", w)
	}
	for _, f := range a.sent[*w.Received:] {
		_ = a.ws.WriteMessage(websocket.TextMessage, f)
	}
}

// closeCode reads until the socket closes and returns the close code, keeping
// the control frames it passes.
func (a *agent) closeCode() int {
	a.t.Helper()
	_ = a.ws.SetReadDeadline(time.Now().Add(2 * time.Second))
	for {
		_, f, err := a.ws.ReadMessage()
		if err != nil {
			var ce *websocket.CloseError
			if !errors.As(err, &ce) {
				a.t.Fatalf("the socket failed with %v, want a close frame", err)
			}
			return ce.Code
		}
		if isControl(f) {
			ctl, _ := decodeControl(f)
			a.controls = append(a.controls, ctl)
		}
	}
}

func (a *agent) controlOf(kind string) control {
	a.t.Helper()
	for _, c := range a.controls {
		if c.Kind == kind {
			return c
		}
	}
	a.t.Fatalf("no %s control frame among %+v", kind, a.controls)
	return control{}
}

// ready runs the handshake and opens the idle session with the given answer,
// then waits for the link.
func (a *agent) ready(rec *recorder, sessionNew string) agentlink.Link {
	a.t.Helper()
	a.answer(a.expect("initialize"), string(fixture(a.t, "initialize.response.json")))
	a.answer(a.expect("session/new"), sessionNew)
	l := rec.link(a.t)
	rec.wait(a.t, func() bool { return rec.offers >= 1 }) // attach asked for work
	return l
}

// readyAsking is ready with the fixture's session, answering the switch to
// the mode that asks.
func (a *agent) readyAsking(rec *recorder) agentlink.Link {
	a.t.Helper()
	a.answer(a.expect("initialize"), string(fixture(a.t, "initialize.response.json")))
	a.answer(a.expect("session/new"), string(fixture(a.t, "session_new.response.json")))
	a.answer(a.expectParams("session/set_mode", `{"sessionId":"sess-1","modeId":"read-only"}`), `{}`)
	l := rec.link(a.t)
	rec.wait(a.t, func() bool { return rec.offers >= 1 })
	return l
}

// sync waits until the engine has applied every frame sent so far: the
// answer to a request proves the reader got past them, and a barrier that
// they were applied.
func (a *agent) sync(l agentlink.Link) {
	a.t.Helper()
	a.syncs++
	id := strconv.Itoa(1_000_000 + a.syncs)
	a.write(`{"jsonrpc":"2.0","id":` + id + `,"method":"agentrq/sync"}`)
	for {
		m := a.next()
		if m.Method == "" && string(m.ID) == id {
			break
		}
		a.backlog = append(a.backlog, m)
	}
	l.(*link).e.barrier()
}

// turn delivers a message, answers the prompt on sessionID with the frames
// in between, and waits until the turn has finished.
func (a *agent) turn(rec *recorder, l agentlink.Link, taskID int64, sessionID, stopReason string, between ...func()) {
	a.t.Helper()
	var offers int
	rec.read(func() { offers = rec.offers })
	l.DeliverTask(context.Background(), taskID, fmt.Sprintf("[Task %d] go", taskID))
	p := a.expect("session/prompt")
	if !strings.Contains(string(p.Params), `"sessionId":"`+sessionID+`"`) {
		a.t.Fatalf("prompt params were %s, want session %s", p.Params, sessionID)
	}
	for _, fn := range between {
		fn()
	}
	a.answer(p, `{"stopReason":"`+stopReason+`"}`)
	rec.wait(a.t, func() bool { return rec.offers > offers }) // a finished turn asks for work
}

const taskReplyID = "0kFbQ0UCE5J"

func TestEngine(t *testing.T) {
	t.Run("AgentRQ opens with initialize declaring no fs and no terminal", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		req := a.expect("initialize")
		want := `{"protocolVersion":1,"clientCapabilities":{"fs":{"readTextFile":false,"writeTextFile":false},"terminal":false,"elicitation":{"form":{},"url":{}},"plan":{},"auth":{"terminal":false}},"clientInfo":{"name":"agentrq","title":"AgentRQ","version":"0.9.20"}}`
		if string(req.Params) != want {
			t.Fatalf("initialize params were\n%s\nwant\n%s", req.Params, want)
		}
	})

	t.Run("the idle session gets the workspace MCP server and no folder", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.expectParams("session/new", `{"mcpServers":[{"type":"http","name":"agentrq-workspace","url":"https://agentrq.test/mcp/ws1","headers":[{"name":"Authorization","value":"Bearer t"}]}]}`)
	})

	t.Run("an agent that refuses http MCP gets no MCP servers", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		a.answer(a.expect("initialize"), `{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"http":false}}}`)
		a.expectParams("session/new", `{"mcpServers":[]}`)
	})

	t.Run("an agent that does not mention http MCP gets no MCP servers", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		a.answer(a.expect("initialize"), `{"protocolVersion":1,"agentCapabilities":{"mcpCapabilities":{"sse":true}}}`)
		a.expectParams("session/new", `{"mcpServers":[]}`)
	})

	t.Run("chunks that arrive just before the prompt answer are in the reply", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn", func() {
			a.chunk("idle-1", "agent_message_chunk", "Hel")
			a.chunk("idle-1", "agent_message_chunk", "lo")
		})
		rec.read(func() {
			if !slices.Equal(rec.replies, []string{"42|Hello"}) {
				t.Fatalf("replies were %q, want both chunks: 42|Hello", rec.replies)
			}
			if !slices.Equal(rec.ongoing, []int64{42}) {
				t.Fatalf("ongoing marks were %v, want [42] before the prompt", rec.ongoing)
			}
		})
	})

	t.Run("an agent answering another protocol version is told which flag to use", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		a.answer(a.expect("initialize"), `{"protocolVersion":2}`)
		if code := a.closeCode(); code != closeVersion {
			t.Fatalf("socket closed with %d, want 4002", code)
		}
		if ctl := a.controlOf("error"); ctl.Error != "version_mismatch" || !strings.Contains(ctl.Message, "--acp-version 2") {
			t.Fatalf("got %+v, want a version_mismatch naming --acp-version 2", ctl)
		}
	})

	t.Run("an initialize answer with no version is refused as a protocol error", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		a.answer(a.expect("initialize"), `{"agentInfo":{"name":"x"}}`)
		if code := a.closeCode(); code != websocket.ClosePolicyViolation {
			t.Fatalf("socket closed with %d, want 1008", code)
		}
		if ctl := a.controlOf("error"); ctl.Error != "protocol_error" {
			t.Fatalf("got %+v, want a protocol_error", ctl)
		}
	})

	t.Run("an initialize answered with an error ends the connection", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.fail(a.expect("initialize"), codeInternalError, "no")
		if code := a.closeCode(); code != websocket.CloseNormalClosure {
			t.Fatalf("socket closed with %d, want 1000", code)
		}
		rec.read(func() {
			if len(rec.links) != 0 {
				t.Fatal("an agent that failed initialize was attached")
			}
		})
	})

	t.Run("an agent that never answers initialize is restarted", func(t *testing.T) {
		shrink(t, &initializeTimeout, 50*time.Millisecond)
		a := startAgent(t, &recorder{})
		a.expect("initialize")
		if code := a.closeCode(); code != closeRestart {
			t.Fatalf("socket closed with %d, want 4000", code)
		}
		a.controlOf("restart")
	})

	t.Run("the link attaches only after the idle session exists", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		req := a.expect("session/new")
		time.Sleep(50 * time.Millisecond)
		rec.read(func() {
			if len(rec.links) != 0 {
				t.Fatal("the link attached before the idle session was answered")
			}
		})
		a.answer(req, `{"sessionId":"idle-1"}`)
		rec.link(t)
		rec.wait(t, func() bool { return len(rec.conc) == 1 && rec.offers == 1 })
		rec.read(func() {
			if c := rec.conc[0]; *c.MaxConcurrency != 1 || *c.Min != 1 || *c.Max != 64 || !*c.CanSet {
				t.Fatalf("the concurrency report was %+v, want limit 1 of 1–64, settable", c)
			}
		})
	})

	t.Run("a session that will not open still attaches, and the first task opens its own", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), `{}`) // no session id
		l := rec.link(t)
		l.DeliverTask(context.Background(), 42, "go")
		a.answer(a.expect("session/new"), `{"sessionId":"s-42","modes":{"currentModeId":"x","availableModes":[]}}`) // nothing to switch
		a.expectParams("session/prompt", `{"sessionId":"s-42","prompt":[{"type":"text","text":"go"}]}`)
	})

	t.Run("a task's files go with its text, each as a link to read it at", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), `{"sessionId":"s-42"}`)
		l := rec.link(t)
		l.DeliverTask(context.Background(), 42, "go", agentlink.Attachment{Name: "spec.pdf", MimeType: "application/pdf", URL: "https://example.com/storage/artifacts/w-1/2/3"})
		a.expectParams("session/prompt", `{"sessionId":"s-42","prompt":[{"type":"text","text":"go"},`+
			`{"type":"resource_link","uri":"https://example.com/storage/artifacts/w-1/2/3","name":"spec.pdf","mimeType":"application/pdf"}]}`)
	})

	t.Run("a task whose session needs a terminal login is told the command", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{header: map[string]string{"X-AgentRQ-Agent": "example-agent"}})
		a.answer(a.expect("initialize"), `{"protocolVersion":1,"authMethods":[{"id":"api-key","name":"API key","type":"terminal"}]}`)
		a.fail(a.expect("session/new"), codeInternalError, "busy")
		l := rec.link(t)
		l.DeliverTask(context.Background(), 42, "go")
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if !strings.Contains(rec.replies[0], "--login --agent example-agent") || !strings.HasPrefix(rec.replies[0], "42|"+noteErrorPrefix) {
				t.Fatalf("reply was %q, want the error note naming the login command", rec.replies[0])
			}
		})
	})

	t.Run("a task whose session will not open gets the error note", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.fail(a.expect("session/new"), codeInternalError, "busy")
		l := rec.link(t)
		l.DeliverTask(context.Background(), 42, "go")
		a.fail(a.expect("session/new"), codeInternalError, "still busy")
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if want := "42|" + noteAnsweredWithError("still busy"); rec.replies[0] != want {
				t.Fatalf("reply was %q, want %q", rec.replies[0], want)
			}
		})
	})

	t.Run("identity comes from agentInfo, else from the bridge's agent header", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.ready(rec, `{"sessionId":"idle-1"}`)
		rec.wait(t, func() bool { return len(rec.identity) == 1 })
		rec.read(func() {
			if got := rec.identity[0]; got != (agentlink.IdentityReport{Name: "example-agent", Title: "Example Agent", Version: "0.9.2"}) {
				t.Fatalf("identity was %+v, want the agentInfo", got)
			}
		})

		rec2 := &recorder{}
		b := startAgentWith(t, rec2, agentOpts{header: map[string]string{"X-AgentRQ-Agent": "other-agent"}})
		b.answer(b.expect("initialize"), `{"protocolVersion":1}`)
		b.answer(b.expect("session/new"), `{"sessionId":"idle-1"}`)
		rec2.wait(t, func() bool { return len(rec2.identity) == 1 })
		rec2.read(func() {
			if got := rec2.identity[0]; got != (agentlink.IdentityReport{Name: "other-agent"}) {
				t.Fatalf("identity was %+v, want the bridge's agent header", got)
			}
		})
	})

	t.Run("a title that repeats the name is dropped, and no name reports no identity", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), `{"protocolVersion":1,"agentInfo":{"name":"solo","title":"solo","version":"1"}}`)
		a.answer(a.expect("session/new"), `{"sessionId":"idle-1"}`)
		rec.wait(t, func() bool { return len(rec.identity) == 1 })
		rec.read(func() {
			if got := rec.identity[0]; got != (agentlink.IdentityReport{Name: "solo", Version: "1"}) {
				t.Fatalf("identity was %+v, want no title", got)
			}
		})

		rec2 := &recorder{}
		b := startAgent(t, rec2)
		b.answer(b.expect("initialize"), `{"protocolVersion":1}`)
		b.answer(b.expect("session/new"), `{"sessionId":"idle-1"}`)
		rec2.link(t)
		rec2.wait(t, func() bool { return len(rec2.conc) == 1 })
		rec2.read(func() {
			if len(rec2.identity) != 0 {
				t.Fatalf("identity was %+v, want none for an agent with no name", rec2.identity)
			}
		})
	})

	t.Run("auth_required triggers the agent login and one retry", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		a.answer(a.expectParams("authenticate", `{"methodId":"account"}`), `{}`)
		a.answer(a.expect("session/new"), `{"sessionId":"idle-1"}`)
		rec.link(t)
	})

	t.Run("a terminal-only login closes with 4003 and names the command", func(t *testing.T) {
		a := startAgentWith(t, &recorder{}, agentOpts{header: map[string]string{"X-AgentRQ-Agent": "example-agent"}})
		a.answer(a.expect("initialize"), `{"protocolVersion":1,"authMethods":[{"id":"api-key","name":"API key","type":"terminal"}]}`)
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		if code := a.closeCode(); code != closeAuthRequired {
			t.Fatalf("socket closed with %d, want 4003", code)
		}
		ctl := a.controlOf("error")
		if ctl.Error != "auth_required" || !strings.Contains(ctl.Message, "npx -y @agentrq/acp-gateway@latest --login --agent example-agent") {
			t.Fatalf("got %+v, want auth_required naming the login command", ctl)
		}
	})

	t.Run("a login that does not take is refused after one retry", func(t *testing.T) {
		a := startAgent(t, &recorder{})
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		a.answer(a.expect("authenticate"), `{}`)
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		if code := a.closeCode(); code != closeAuthRequired {
			t.Fatalf("socket closed with %d, want 4003", code)
		}
		if msg := a.controlOf("error").Message; !strings.Contains(msg, "--login --agent <agent>") {
			t.Fatalf("message was %q, want the command with a placeholder for an unnamed agent", msg)
		}
	})

	t.Run("a login the agent fails leaves the agent attached without an idle session", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		a.fail(a.expect("authenticate"), codeInternalError, "browser closed")
		rec.link(t)
	})

	t.Run("the preferred method from the bridge is used, and an unknown one is refused", func(t *testing.T) {
		for _, refused := range []string{"api-key", "nope"} {
			a := startAgentWith(t, &recorder{}, agentOpts{header: map[string]string{"X-AgentRQ-Auth-Method": refused}})
			a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
			a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
			if code := a.closeCode(); code != closeAuthRequired {
				t.Fatalf("preferring %s closed with %d, want 4003", refused, code)
			}
		}
		a := startAgentWith(t, &recorder{}, agentOpts{header: map[string]string{"X-AgentRQ-Auth-Method": "account"}})
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.fail(a.expect("session/new"), codeAuthRequired, "auth required")
		a.expectParams("authenticate", `{"methodId":"account"}`)
	})

	t.Run("models from config options are reported with can_set", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		rec.read(func() {
			m := rec.models[0]
			if m.ConfigID != "model" || m.CurrentModel != "model-large" || !m.CanSet || len(m.Models) != 2 || m.SessionID != "sess-1" || m.TaskID != "" {
				t.Fatalf("models report was %+v, want model-large of 2 on sess-1, settable, no task", m)
			}
		})
	})

	t.Run("the bridge's model is applied to every new session", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{header: map[string]string{"X-AgentRQ-Agent-Model": "Small"}})
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), string(fixture(t, "session_new.response.json")))
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-1","configId":"model","value":"model-small"}`), `{}`)
		a.answer(a.expect("session/set_mode"), `{}`)
		rec.link(t)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		rec.read(func() {
			if m := rec.models[0]; m.CurrentModel != "model-small" || !m.Models[1].Current || m.Models[0].Current {
				t.Fatalf("models report was %+v, want model-small current", m)
			}
		})
	})

	t.Run("a failed model switch re-reports what is true", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{header: map[string]string{"X-AgentRQ-Agent-Model": "model-small"}})
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), string(fixture(t, "session_new.response.json")))
		a.fail(a.expect("session/set_config_option"), codeInternalError, "no")
		a.answer(a.expect("session/set_mode"), `{}`)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		rec.read(func() {
			if m := rec.models[0]; m.CurrentModel != "model-large" {
				t.Fatalf("models report was %+v, want model-large still current", m)
			}
		})
	})

	t.Run("set_model from the workspace switches that session and becomes the default", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		if !l.SetModel(context.Background(), "sess-1", "model", "model-small") {
			t.Fatal("SetModel was refused")
		}
		answered := strings.Replace(string(fixture(t, "session_new.response.json")), `"currentValue":"model-large"`, `"currentValue":"model-small"`, 1)
		var opts struct {
			ConfigOptions json.RawMessage `json:"configOptions"`
		}
		_ = json.Unmarshal([]byte(answered), &opts)
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-1","configId":"model","value":"model-small"}`), `{"configOptions":`+string(opts.ConfigOptions)+`}`)
		rec.wait(t, func() bool { return len(rec.models) == 2 })
		rec.read(func() {
			if m := rec.models[1]; m.CurrentModel != "model-small" {
				t.Fatalf("models report was %+v, want model-small after the switch", m)
			}
		})
		a.turn(rec, l, 42, "sess-1", "end_turn")
		l.DeliverTask(context.Background(), 43, "next")
		a.answer(a.expect("session/new"), strings.Replace(string(fixture(t, "session_new.response.json")), "sess-1", "sess-2", 1))
		a.answer(a.expectParams("session/set_config_option", `{"sessionId":"sess-2","configId":"model","value":"model-small"}`), `{}`)
		a.answer(a.expect("session/set_mode"), `{}`)
		a.expect("session/prompt")
	})

	t.Run("a model nobody offers re-reports the known list, and an unknown session is left alone", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		l.SetModel(context.Background(), "sess-1", "model", "model-huge")
		rec.wait(t, func() bool { return len(rec.models) == 2 })
		l.SetModel(context.Background(), "nope", "model", "model-small")
		a.quiet()
	})

	t.Run("a session in an auto-approving mode is switched to one that asks", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.readyAsking(rec) // expects set_mode read-only on sess-1
	})

	t.Run("an agent that leaves the asking mode is put back at most three times", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.readyAsking(rec)
		for range 4 {
			a.update("sess-1", `{"sessionUpdate":"current_mode_update","currentModeId":"agent-full-access"}`)
		}
		for range 3 {
			a.answer(a.expectParams("session/set_mode", `{"sessionId":"sess-1","modeId":"read-only"}`), `{}`)
		}
		a.quiet()
		a.update("sess-1", `{"sessionUpdate":"current_mode_update","currentModeId":"read-only"}`)
		a.update("sess-1", `{"sessionUpdate":"current_mode_update","currentModeId":"agent"}`)
		a.answer(a.expect("session/set_mode"), `{}`) // a mode that asks reset the count
	})

	t.Run("modes the engine cannot or need not change are left alone", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), `{"sessionId":"idle-1","modes":{"currentModeId":"yolo","availableModes":[{"id":"yolo","name":"YOLO"}]}}`)
		l := rec.link(t)
		a.update("idle-1", `{"sessionUpdate":"current_mode_update","currentModeId":"yolo"}`) // nothing that asks to go back to
		l.DeliverTask(context.Background(), 42, "go")
		a.answer(a.expect("session/prompt"), `{"stopReason":"end_turn"}`)
		l.DeliverTask(context.Background(), 43, "go")
		a.answer(a.expect("session/new"), `{"sessionId":"s-43","modes":{"currentModeId":"ask","availableModes":[{"id":"ask","name":"Ask"}]}}`)
		a.answer(a.expect("session/prompt"), `{"stopReason":"end_turn"}`)
		l.DeliverTask(context.Background(), 44, "go")
		a.answer(a.expect("session/new"), `{"sessionId":"s-44","modes":{"currentModeId":"auto","availableModes":[{"id":"auto","name":"Auto"},{"id":"ask","name":"Ask"}]}}`)
		a.fail(a.expect("session/set_mode"), codeInternalError, "no")
		a.answer(a.expect("session/prompt"), `{"stopReason":"end_turn"}`)
		a.update("s-43", `{"sessionUpdate":"current_mode_update","currentModeId":"ask"}`)
		a.quiet()
	})

	t.Run("the first task adopts the idle session", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		a.turn(rec, l, 42, "sess-1", "end_turn")
		rec.wait(t, func() bool { return len(rec.models) == 2 })
		rec.read(func() {
			if m := rec.models[1]; m.TaskID != b62(42) {
				t.Fatalf("the adopted session's models were reported for %q, want the task %q", m.TaskID, b62(42))
			}
		})
		if len(a.backlog) != 0 {
			t.Fatalf("frames %+v came besides the prompt, want no second session/new", a.backlog)
		}
	})

	t.Run("a task's second turn reuses its session", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn")
		a.turn(rec, l, 42, "idle-1", "end_turn")
	})

	t.Run("a second task gets a session of its own", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{header: map[string]string{"X-AgentRQ-Max-Concurrency": "2"}})
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "a")
		l.DeliverTask(context.Background(), 43, "b")
		a.answer(a.expect("session/new"), `{"sessionId":"s-43"}`)
		first, second := a.expect("session/prompt"), a.expect("session/prompt")
		if got := string(first.Params) + string(second.Params); !strings.Contains(got, `"idle-1"`) || !strings.Contains(got, `"s-43"`) {
			t.Fatalf("prompts were %s and %s, want one on idle-1 and one on s-43", first.Params, second.Params)
		}
	})

	t.Run("a task while the idle session is still opening waits for it", func(t *testing.T) {
		shrink(t, &idleSessionWait, 50*time.Millisecond)
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		req := a.expect("session/new")
		l := rec.link(t) // attached once the wait ran out
		l.DeliverTask(context.Background(), 42, "go")
		time.Sleep(50 * time.Millisecond)
		a.answer(req, `{"sessionId":"idle-1"}`)
		a.expectParams("session/prompt", `{"sessionId":"idle-1","prompt":[{"type":"text","text":"go"}]}`)
	})

	t.Run("Stop sends session/cancel for the task's session", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		p := a.expect("session/prompt")
		if !l.CancelTask(context.Background(), 42) {
			t.Fatal("CancelTask reported no turn for a task mid-turn")
		}
		a.expectParams("session/cancel", `{"sessionId":"idle-1"}`)
		a.answer(p, `{"stopReason":"cancelled"}`)
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if rec.replies[0] != "42|"+noteCancelled {
				t.Fatalf("reply was %q, want the cancelled note", rec.replies[0])
			}
		})
		if l.CancelTask(context.Background(), 99) {
			t.Fatal("CancelTask reported a turn for a task the link never had")
		}
	})

	t.Run("a Stop the agent never answers ends the turn after the settle wait", func(t *testing.T) {
		shrink(t, &cancelSettleWait, 50*time.Millisecond)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		a.expect("session/prompt")
		if !l.CancelTask(context.Background(), 42) {
			t.Fatal("CancelTask reported no turn for a task mid-turn")
		}
		a.expectParams("session/cancel", `{"sessionId":"idle-1"}`)
		a.expect(methodCancelRequest) // the prompt is given up on
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if rec.replies[0] != "42|"+noteCancelled {
				t.Fatalf("reply was %q, want the cancelled note", rec.replies[0])
			}
		})
		if l.CancelTask(context.Background(), 42) {
			t.Fatal("the given-up turn left its task running")
		}
	})

	t.Run("a Stop before the turn starts skips it", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "first")
		p := a.expect("session/prompt")
		l.DeliverTask(context.Background(), 43, "waiting") // waits for the only slot
		if !l.CancelTask(context.Background(), 43) {
			t.Fatal("CancelTask reported nothing for a task waiting for a slot")
		}
		a.answer(p, `{"stopReason":"end_turn"}`)
		rec.wait(t, func() bool { return rec.offers >= 2 })
		a.quiet()
	})

	t.Run("a Stop while the task's session is opening skips the prompt", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), `{}`) // no idle session
		l := rec.link(t)
		l.DeliverTask(context.Background(), 42, "go")
		req := a.expect("session/new")
		l.CancelTask(context.Background(), 42)
		a.answer(req, `{"sessionId":"s-42"}`)
		rec.wait(t, func() bool { return rec.offers >= 2 })
		a.quiet()
		if l.CancelTask(context.Background(), 42) {
			t.Fatal("the skipped turn's session was left busy")
		}
	})

	t.Run("a freed slot asks the workspace for the next task", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		rec.wait(t, func() bool { return rec.offers == 1 }) // attach asked for the free slot
		a.turn(rec, l, 42, "idle-1", "end_turn")
		rec.read(func() {
			if rec.offers != 2 {
				t.Fatalf("offers were %d, want 2: one at attach, one when the turn ended", rec.offers)
			}
		})
	})

	t.Run("raising the limit from the workspace is reported and asks for work", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		rec.wait(t, func() bool { return rec.offers == 1 })
		if !l.SetConcurrency(context.Background(), 3) {
			t.Fatal("SetConcurrency was refused")
		}
		rec.wait(t, func() bool { return rec.offers == 2 && len(rec.conc) == 2 })
		rec.read(func() {
			if got := *rec.conc[1].MaxConcurrency; got != 3 {
				t.Fatalf("the limit reported was %d, want 3", got)
			}
		})
		if owns, free := l.Dispatch(42); owns || free != 3 {
			t.Fatalf("Dispatch(42) was (%v, %d), want (false, 3)", owns, free)
		}
		a.turn(rec, l, 42, "idle-1", "end_turn")
		if owns, _ := l.Dispatch(42); !owns {
			t.Fatal("Dispatch did not see the session the task ran on")
		}
	})

	t.Run("reasoning is posted before the answer and closed by a tool call", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn", func() {
			a.chunk("idle-1", "agent_thought_chunk", "Think A")
			a.update("idle-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"write_file","name":"write","status":"pending","rawInput":null}`)
			a.update("idle-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c1","status":"in_progress","rawInput":{"path":"x"}}`)
			a.chunk("idle-1", "agent_thought_chunk", "Think B")
			a.chunk("idle-1", "agent_message_chunk", "Answer")
			a.chunk("idle-1", "agent_thought_chunk", "Think C")
			a.update("idle-1", `{"sessionUpdate":"agent_message_chunk","content":{"type":"image","data":"x"}}`)
		})
		rec.read(func() {
			want := []string{"thought:Think A", "thought:Think B", "thought:Think C", "reply:Answer"}
			if !slices.Equal(rec.log, want) {
				t.Fatalf("the human read %q, want %q", rec.log, want)
			}
		})
	})

	t.Run("an idle session's reasoning and plans are not posted", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.chunk("idle-1", "agent_thought_chunk", "musing")
		a.update("idle-1", `{"sessionUpdate":"plan_removed","planId":"p"}`)
		a.chunk("idle-1", "agent_message_chunk", "hello")
		a.sync(l)
		a.turn(rec, l, 42, "idle-1", "end_turn")
		rec.read(func() {
			if want := []string{"reply:hello"}; !slices.Equal(rec.log, want) {
				t.Fatalf("the human read %q, want %q", rec.log, want)
			}
		})
	})

	t.Run("a reply identical to the agent's own reply tool call is not posted twice", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"sess-1"}`)
		task := monoflake.IDFromBase62(taskReplyID).Int64()
		a.turn(rec, l, task, "sess-1", "end_turn", func() {
			a.write(`{"jsonrpc":"2.0","method":"session/update","params":` + string(fixture(t, "session_update.tool_call.json")) + `}`)
			a.chunk("sess-1", "agent_message_chunk", "Done.")
		})
		a.turn(rec, l, task, "sess-1", "end_turn", func() {
			a.update("sess-1", `{"sessionUpdate":"tool_call","toolCallId":"c2","title":"Replying to task","name":"mcp__agentrq-workspace__task","status":"in_progress","rawInput":{"action":"reply","payload":{"taskId":"`+taskReplyID+`","text":"Again."}}}`)
			a.update("sess-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c2","status":"completed"}`)
			a.chunk("sess-1", "agent_message_chunk", "Again.")
		})
		a.turn(rec, l, task, "sess-1", "end_turn", func() {
			a.chunk("sess-1", "agent_message_chunk", "Done.") // no tool reply this turn
		})
		rec.read(func() {
			if want := []string{fmt.Sprintf("%d|Done.", task)}; !slices.Equal(rec.replies, want) {
				t.Fatalf("replies were %q, want only the third turn's %q", rec.replies, want)
			}
		})
	})

	t.Run("a reply tool call for another task, or not ours, does not hide the answer", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn", func() {
			a.update("idle-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"mcp__agentrq-workspace__reply","status":"completed","rawInput":{"chatId":"0kOther","text":"Hi"}}`)
			a.update("idle-1", `{"sessionUpdate":"tool_call","toolCallId":"c2","title":"mcp__elsewhere__reply","status":"completed","rawInput":{"chatId":"`+b62(42)+`","text":"Hi"}}`)
			a.update("idle-1", `{"sessionUpdate":"tool_call_update","toolCallId":"c3","status":"failed"}`)
			a.update("idle-1", `{"sessionUpdate":"tool_call_update","status":"completed"}`)
			a.chunk("idle-1", "agent_message_chunk", "Hi")
		})
		rec.read(func() {
			if want := []string{"42|Hi"}; !slices.Equal(rec.replies, want) {
				t.Fatalf("replies were %q, want %q", rec.replies, want)
			}
		})
	})

	t.Run("the usage footer is re-reported at every turn end, even when the turn sent none", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"sess-1"}`)
		a.turn(rec, l, 42, "sess-1", "end_turn", func() {
			a.write(`{"jsonrpc":"2.0","method":"session/update","params":` + string(fixture(t, "session_update.usage_update.json")) + `}`)
		})
		a.turn(rec, l, 42, "sess-1", "end_turn")
		rec.read(func() {
			if len(rec.telemetry) != 2 {
				t.Fatalf("telemetry was %+v, want two usage reports", rec.telemetry)
			}
			for _, u := range rec.telemetry {
				if u.Kind != "usage" || u.Text != "Context 53,000 / 200,000 tokens (27%) · 0.0042 USD" || u.TaskID != b62(42) {
					t.Fatalf("usage report was %+v, want the same footer for task 42", u)
				}
			}
		})
	})

	t.Run("every stop reason posts the gateway's exact note", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		reasons := []string{"refusal", "max_tokens", "max_turn_requests", "cancelled", "tea_break"}
		var want []string
		for _, r := range reasons {
			a.turn(rec, l, 42, "idle-1", r)
			want = append(want, "42|"+stopNote(r))
		}
		rec.read(func() {
			if !slices.Equal(rec.replies, want) {
				t.Fatalf("notes were %q, want %q", rec.replies, want)
			}
		})
	})

	t.Run("a prompt answered with an error posts the error note", func(t *testing.T) {
		rec := &recorder{failMark: errors.New("database unavailable")}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		a.fail(a.expect("session/prompt"), codeInternalError, "model overloaded")
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if want := "42|" + noteAnsweredWithError("model overloaded"); rec.replies[0] != want {
				t.Fatalf("reply was %q, want %q", rec.replies[0], want)
			}
		})
	})

	t.Run("a prompt that cannot be sent posts the error note", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		e := l.(*link).e
		e.peer.mu.Lock()
		for i := range maxPendingCalls {
			e.peer.calls[int64(1000+i)] = make(chan rpcMessage, 1)
		}
		e.peer.mu.Unlock()
		l.DeliverTask(context.Background(), 42, "go")
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if want := "42|" + noteAnsweredWithError(errTooManyCalls.Error()); rec.replies[0] != want {
				t.Fatalf("reply was %q, want %q", rec.replies[0], want)
			}
		})
	})

	t.Run("a reply the workspace refuses is logged, not retried", func(t *testing.T) {
		rec := &recorder{failReply: errors.New("database unavailable")}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "refusal", func() { a.chunk("idle-1", "agent_message_chunk", "x") })
		rec.read(func() {
			if len(rec.replies) != 0 {
				t.Fatalf("replies were %q, want none recorded", rec.replies)
			}
		})
	})

	t.Run("a reply over 1 MiB is cut with a marker", func(t *testing.T) {
		shrink(t, &maxReplyBytes, 16)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn", func() {
			a.chunk("idle-1", "agent_message_chunk", "0123456789X")
			a.chunk("idle-1", "agent_message_chunk", "abcdéfghij") // the cap falls inside é
			a.chunk("idle-1", "agent_message_chunk", "more")
		})
		rec.read(func() {
			if want := "42|0123456789Xabcd" + truncatedMarker; rec.replies[0] != want {
				t.Fatalf("reply was %q, want %q", rec.replies[0], want)
			}
		})
	})

	t.Run("a long thought is cut with a marker", func(t *testing.T) {
		shrink(t, &maxThoughtBytes, 4)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn", func() { a.chunk("idle-1", "agent_thought_chunk", "abcdef") })
		rec.read(func() {
			if want := []string{"thought:abcd" + truncatedMarker}; !slices.Equal(rec.log, want) {
				t.Fatalf("the human read %q, want %q", rec.log, want)
			}
		})
	})

	t.Run("plans post, revise and withdraw", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"sess-1"}`)
		a.turn(rec, l, 42, "sess-1", "end_turn", func() {
			a.write(`{"jsonrpc":"2.0","method":"session/update","params":` + string(fixture(t, "session_update.plan_update.json")) + `}`)
			a.update("sess-1", `{"sessionUpdate":"plan_update","plan":{"type":"markdown","planId":"p1","content":"# Revised"}}`)
			a.update("sess-1", `{"sessionUpdate":"plan_removed","planId":"p1"}`)
			a.update("sess-1", `{"sessionUpdate":"plan","entries":[{"content":"Old style","priority":"low","status":"pending"}]}`)
		})
		rec.read(func() {
			want := []string{"plan:- ✅ Read the code\n- 🔄 Write the fix", "plan:# Revised", "plan:Plan withdrawn.", "plan:- ⬜ Old style"}
			if !slices.Equal(rec.log, want) {
				t.Fatalf("the human read %q, want %q", rec.log, want)
			}
			if d := rec.telemetry[2].Data; d["planId"] != "p1" || d["removed"] != true {
				t.Fatalf("the withdrawal's data was %v, want planId p1 removed", d)
			}
			if d := rec.telemetry[3].Data; d["planId"] != "default" {
				t.Fatalf("the legacy plan's data was %v, want planId default", d)
			}
		})
	})

	t.Run("slash commands are normalised and an empty list withdraws them", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), `{"protocolVersion":1}`) // no logout, so no /logout of AgentRQ's own
		req := a.expect("session/new")
		// Sent ahead of the answer, before AgentRQ knows the session: held,
		// and applied once it is registered. The other session's stays held.
		a.update("idle-1", `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":" init ","description":"x"},{"name":"","description":"y"},{"name":"review","description":"r"},{"name":"init","description":" z ","input":{"hint":" path "}}]}`)
		a.update("other", `{"sessionUpdate":"usage_update","used":1,"size":2}`)
		a.answer(req, `{"sessionId":"idle-1"}`)
		rec.link(t)
		rec.wait(t, func() bool { return len(rec.commands) == 1 })
		rec.read(func() {
			want := []agentlink.Command{{Name: "init", Description: "z", Hint: "path"}, {Name: "review", Description: "r"}}
			if got := rec.commands[0].Commands; !slices.Equal(got, want) {
				t.Fatalf("commands were %+v, want %+v", got, want)
			}
		})
		a.update("idle-1", `{"sessionUpdate":"available_commands_update","availableCommands":[]}`)
		rec.wait(t, func() bool { return len(rec.commands) == 2 })
		rec.read(func() {
			if got := rec.commands[1].Commands; got == nil || len(got) != 0 {
				t.Fatalf("commands were %+v, want an empty list that withdraws the menu", got)
			}
		})
	})

	t.Run("a model change the agent reports is passed on", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.readyAsking(rec)
		rec.wait(t, func() bool { return len(rec.models) == 1 })
		a.update("sess-1", `{"sessionUpdate":"config_option_update","configOptions":[{"id":"model","name":"Model","type":"select","currentValue":"model-small","options":[{"value":"model-small","name":"Small"}]}]}`)
		a.update("sess-1", `{"sessionUpdate":"config_option_update","configOptions":[{"id":"effort","name":"Effort","type":"select","options":[{"value":"low"}]}]}`)
		rec.wait(t, func() bool { return len(rec.models) == 2 })
		rec.read(func() {
			if m := rec.models[1]; m.CurrentModel != "model-small" || len(m.Models) != 1 {
				t.Fatalf("models report was %+v, want model-small alone", m)
			}
		})
	})

	t.Run("an update for an unknown session is ignored", func(t *testing.T) {
		shrink(t, &maxEarlyUpdates, 2)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		for range 3 {
			a.chunk("nope", "agent_message_chunk", "lost")
		}
		a.update("idle-1", `{"sessionUpdate":"session_info_update","title":"x"}`)
		a.update("idle-1", `{"sessionUpdate":"current_mode_update","currentModeId":"yolo"}`) // a session with no modes
		a.update("idle-1", `{"sessionUpdate":"current_mode_update","currentModeId":5}`)
		a.write(`{"jsonrpc":"2.0","method":"session/update","params":"not an object"}`)
		a.write(`{"jsonrpc":"2.0","method":"something/else","params":{}}`)
		a.turn(rec, l, 42, "idle-1", "end_turn")
		rec.read(func() {
			if len(rec.replies)+len(rec.telemetry) != 0 {
				t.Fatalf("recorded replies %q and telemetry %+v, want nothing", rec.replies, rec.telemetry)
			}
		})
		a.quiet()
		e := l.(*link).e
		e.barrier()
		if len(e.early) != 2 {
			t.Fatalf("%d early updates were held, want the latest 2", len(e.early))
		}
	})

	t.Run("the agent's requests are refused until AgentRQ serves them", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.ready(rec, `{"sessionId":"idle-1"}`)
		a.write(`{"jsonrpc":"2.0","id":7,"method":"fs/read_text_file","params":{"sessionId":"idle-1","path":"/etc/hosts"}}`)
		m := a.next()
		if m.Error == nil || m.Error.Code != codeMethodNotFound || m.Error.Message != "fs/read_text_file is not supported by AgentRQ" {
			t.Fatalf("answer was %+v, want -32601 naming the method", m)
		}
	})

	t.Run("an agent exit mid-turn posts the process-exited note and detaches the link", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		a.expect("session/prompt")
		lines, _ := json.Marshal(slices.Repeat([]string{"noise"}, maxStderrLines))
		a.control(`{"agentrqBridge":"stderr","lines":["panic: oops"]}`)
		a.control(`{"agentrqBridge":"stderr","lines":` + string(lines) + `}`)
		a.control(`{"agentrqBridge":"restart"}`)
		a.control(`{"agentrqBridge":"exit","code":1,"signal":"SIGSEGV"}`)
		rec.wait(t, func() bool { return len(rec.detached) == 1 && len(rec.replies) == 1 })
		rec.read(func() {
			if rec.replies[0] != "42|"+noteProcessExited || rec.detached[0] != l.ID() {
				t.Fatalf("replies %q and detached %q, want the process-exited note and %s", rec.replies, rec.detached, l.ID())
			}
		})
	})

	t.Run("the bridge saying goodbye mid-turn posts the cancelled note", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		a.expect("session/prompt")
		a.control(`{"agentrqBridge":"bye"}`)
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		rec.read(func() {
			if rec.replies[0] != "42|"+noteCancelled {
				t.Fatalf("reply was %q, want the cancelled note", rec.replies[0])
			}
		})
	})

	t.Run("turns that never started are dropped without a note when the connection ends", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), `{}`) // no idle session
		l := rec.link(t)
		l.DeliverTask(context.Background(), 42, "opening")
		a.expect("session/new")                            // 42's session is opening
		l.DeliverTask(context.Background(), 43, "waiting") // and 43 waits for the slot
		a.control(`{"agentrqBridge":"bye"}`)
		rec.wait(t, func() bool { return len(rec.detached) == 1 && !l.(*link).e.q.owns(43) })
		rec.read(func() {
			if len(rec.replies)+len(rec.ongoing) != 0 {
				t.Fatalf("replies %q and ongoing %v, want nothing for turns that never started", rec.replies, rec.ongoing)
			}
		})
	})

	t.Run("a connection lost past grace posts the connection-lost note", func(t *testing.T) {
		shrink(t, &graceWindow, 50*time.Millisecond)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		a.expect("session/prompt")
		a.drop()
		rec.wait(t, func() bool { return len(rec.replies) == 1 && len(rec.detached) == 1 })
		rec.read(func() {
			if rec.replies[0] != "42|"+noteConnectionLost {
				t.Fatalf("reply was %q, want the connection-lost note", rec.replies[0])
			}
		})
	})

	t.Run("a connection that ends before the agent is ready attaches nothing", func(t *testing.T) {
		shrink(t, &graceWindow, 50*time.Millisecond)
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.expect("session/new")
		a.drop()
		time.Sleep(200 * time.Millisecond)
		rec.read(func() {
			if len(rec.links)+len(rec.detached) != 0 {
				t.Fatalf("links %d and detaches %q, want none for an agent that never got ready", len(rec.links), rec.detached)
			}
		})
	})

	t.Run("a turn survives a resume between the last chunk and the prompt answer", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "go")
		p := a.expect("session/prompt")
		a.chunk("idle-1", "agent_message_chunk", "Hello")
		a.drop()
		a.resume()
		a.answer(p, `{"stopReason":"end_turn"}`)
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		time.Sleep(50 * time.Millisecond)
		rec.read(func() {
			if !slices.Equal(rec.replies, []string{"42|Hello"}) {
				t.Fatalf("replies were %q, want the reply once and no note", rec.replies)
			}
		})
	})

	t.Run("the LRU evicts the least recently used idle session and closes it when the agent can", func(t *testing.T) {
		shrink(t, &maxSessions, 2)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"s-1"}`)
		a.turn(rec, l, 1, "s-1", "end_turn")
		l.DeliverTask(context.Background(), 2, "go")
		a.answer(a.expect("session/new"), `{"sessionId":"s-2"}`)
		a.answer(a.expect("session/prompt"), `{"stopReason":"end_turn"}`)
		l.DeliverTask(context.Background(), 3, "go")
		a.answer(a.expect("session/new"), `{"sessionId":"s-3"}`)
		a.fail(a.expectParams("session/close", `{"sessionId":"s-1"}`), codeInternalError, "gone already")
		a.expect("session/prompt")
		if owns, _ := l.Dispatch(1); owns {
			t.Fatal("the evicted session's task still counted as held")
		}
	})

	t.Run("an evicted session is just forgotten by an agent that cannot close one", func(t *testing.T) {
		shrink(t, &maxSessions, 1)
		rec := &recorder{}
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), `{"protocolVersion":1}`)
		a.answer(a.expect("session/new"), `{"sessionId":"s-1"}`)
		l := rec.link(t)
		rec.wait(t, func() bool { return rec.offers == 1 })
		a.turn(rec, l, 1, "s-1", "end_turn")
		l.DeliverTask(context.Background(), 2, "go")
		a.answer(a.expect("session/new"), `{"sessionId":"s-2"}`) // evicts s-1
		a.expectParams("session/prompt", `{"sessionId":"s-2","prompt":[{"type":"text","text":"go"}]}`)
		if len(a.backlog) != 0 {
			t.Fatalf("frames %+v arrived, want no session/close", a.backlog)
		}
	})

	t.Run("/logout logs an agent that can log out out, instead of prompting it", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{deps: func(d *EngineDeps) {
			d.OnLogout = func(context.Context, int64, string) { rec.mu.Lock(); rec.logouts++; rec.mu.Unlock() }
		}})
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, " /logout \n- report.pdf")
		a.answer(a.expectParams("logout", `{}`), `{}`)
		rec.wait(t, func() bool { return len(rec.replies) == 1 && rec.logouts == 1 })
		if want := "42|" + noteLoggedOut; rec.replies[0] != want {
			t.Fatalf("replies were %q, want only %q", rec.replies, want)
		}
		a.turn(rec, l, 42, "idle-1", "end_turn") // the task's next message is a prompt again
	})

	t.Run("a /logout the agent refuses ends with the error note", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "/logout")
		a.fail(a.expect("logout"), codeInternalError, "not logged in")
		rec.wait(t, func() bool { return len(rec.replies) == 1 })
		if want := "42|" + noteAnsweredWithError("not logged in"); rec.replies[0] != want {
			t.Fatalf("replies were %q, want %q", rec.replies, want)
		}
	})

	t.Run("a /logout cut off by the connection ending posts no note", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		l.DeliverTask(context.Background(), 42, "/logout")
		a.expect("logout")
		a.control(`{"agentrqBridge":"bye"}`)
		rec.wait(t, func() bool { return len(rec.detached) == 1 })
		rec.read(func() {
			if len(rec.replies) != 0 {
				t.Fatalf("replies were %q, want none", rec.replies)
			}
		})
	})

	t.Run("/logout takes the place of the agent's own logout command in its menu", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.update("idle-1", `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"logout","description":"the agent's own"},{"name":"web","description":"Search"}]}`)
		a.sync(l)
		rec.read(func() {
			got := rec.commands[len(rec.commands)-1].Commands
			if want := []agentlink.Command{{Name: "web", Description: "Search"}, logoutCommand}; !slices.Equal(got, want) {
				t.Fatalf("commands were %+v, want %+v", got, want)
			}
		})
	})

	t.Run("an agent that can log out but lists no commands still offers /logout", func(t *testing.T) {
		rec := &recorder{}
		a := startAgent(t, rec)
		a.ready(rec, `{"sessionId":"idle-1"}`)
		rec.read(func() {
			want := agentlink.CommandsReport{SessionID: "idle-1", Commands: []agentlink.Command{logoutCommand}}
			if len(rec.commands) != 1 || rec.commands[0].SessionID != want.SessionID || !slices.Equal(rec.commands[0].Commands, want.Commands) {
				t.Fatalf("commands reports were %+v, want only %+v", rec.commands, want)
			}
		})
	})

	for name, caps := range map[string]string{"omits auth.logout": `{}`, "sends a null auth.logout": `{"auth":{"logout":null}}`} {
		t.Run("an agent that "+name+" gets /logout as a prompt and no menu entry", func(t *testing.T) {
			rec := &recorder{}
			a := startAgent(t, rec)
			a.answer(a.expect("initialize"), `{"protocolVersion":1,"agentCapabilities":`+caps+`}`)
			a.answer(a.expect("session/new"), `{"sessionId":"s-1"}`)
			l := rec.link(t)
			rec.wait(t, func() bool { return rec.offers == 1 })
			l.DeliverTask(context.Background(), 42, "/logout")
			a.answer(a.expectParams("session/prompt", `{"sessionId":"s-1","prompt":[{"type":"text","text":"/logout"}]}`), `{"stopReason":"end_turn"}`)
			a.update("s-1", `{"sessionUpdate":"available_commands_update","availableCommands":[{"name":"web"}]}`)
			a.sync(l)
			rec.read(func() {
				if got := rec.commands[len(rec.commands)-1].Commands; !slices.Equal(got, []agentlink.Command{{Name: "web"}}) {
					t.Fatalf("commands were %+v, want only the agent's own", got)
				}
			})
		})
	}

	t.Run("telemetry hooks hear of the connection and each turn", func(t *testing.T) {
		rec := &recorder{}
		a := startAgentWith(t, rec, agentOpts{deps: func(d *EngineDeps) {
			d.OnConnect = func(context.Context, int64, string) { rec.mu.Lock(); rec.connects++; rec.mu.Unlock() }
			d.OnTurn = func(context.Context, int64, string) { rec.mu.Lock(); rec.turns++; rec.mu.Unlock() }
		}})
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.turn(rec, l, 42, "idle-1", "end_turn")
		rec.wait(t, func() bool { return rec.connects == 1 && rec.turns == 1 })
	})
}

func TestEngineEdges(t *testing.T) {
	t.Run("a task pushed while the link attaches starts, and its numbers wait for the link", func(t *testing.T) {
		rec := &recorder{}
		rec.onAttach = func(l agentlink.Link) { l.DeliverTask(context.Background(), 42, "early") }
		a := startAgent(t, rec)
		a.answer(a.expect("initialize"), string(fixture(t, "initialize.response.json")))
		a.answer(a.expect("session/new"), `{"sessionId":"idle-1"}`)
		a.expectParams("session/prompt", `{"sessionId":"idle-1","prompt":[{"type":"text","text":"early"}]}`)
		rec.wait(t, func() bool { return len(rec.conc) >= 1 })
	})

	t.Run("past the cap the oldest unfinished call is forgotten", func(t *testing.T) {
		shrink(t, &maxToolCallsKept, 1)
		rec := &recorder{}
		a := startAgent(t, rec)
		l := a.ready(rec, `{"sessionId":"idle-1"}`)
		a.update("idle-1", `{"sessionUpdate":"tool_call","toolCallId":"c1","title":"one","status":"pending"}`)
		a.update("idle-1", `{"sessionUpdate":"tool_call","toolCallId":"c2","title":"two","status":"pending"}`)
		a.sync(l)
		e := l.(*link).e
		s := e.sessionByID("idle-1")
		s.proj.mu.Lock()
		defer s.proj.mu.Unlock()
		if _, kept := s.proj.calls["c2"]; len(s.proj.calls) != 1 || !kept {
			t.Fatalf("calls kept were %v, want only c2", s.proj.calls)
		}
	})

	// bare is an engine with no socket and no update loop, whose peer answers
	// every call at once with result.
	bare := func(t *testing.T, queue int, result string) *engineV1 {
		t.Helper()
		ctx, stop := context.WithCancel(context.Background())
		t.Cleanup(stop)
		e := &engineV1{c: newConnection("A", 100, "user-1", bridgeMeta{}), ctx: ctx, stop: stop, lim: currentLimits(),
			updates: make(chan update, queue), sessions: newSessionSet(), ws: &recorder{},
			deps: EngineDeps{MCPURL: func(int64) string { return "https://agentrq.test/mcp" }}}
		e.peer = newPeer(func(frame []byte) error {
			var m rpcMessage
			_ = json.Unmarshal(frame, &m)
			go e.peer.receive([]byte(`{"jsonrpc":"2.0","id":` + string(m.ID) + `,"result":` + result + `}`))
			return nil
		}, e)
		return e
	}

	t.Run("a session answered after the engine stopped is not registered", func(t *testing.T) {
		e := bare(t, 0, `{"sessionId":"late"}`)
		e.stop()
		if _, err := e.newSession(context.Background(), 1); !errors.Is(err, errPeerClosed) {
			t.Fatalf("newSession returned %v, want %v", err, errPeerClosed)
		}
	})

	t.Run("a session whose registration the stopping engine never ran is not used", func(t *testing.T) {
		e := bare(t, 1, `{"sessionId":"late"}`)
		go func() {
			time.Sleep(20 * time.Millisecond)
			e.stop()
		}()
		if _, err := e.newSession(context.Background(), 1); !errors.Is(err, errPeerClosed) {
			t.Fatalf("newSession returned %v, want %v", err, errPeerClosed)
		}
	})

	t.Run("a barrier gives up when the engine stops before applying it", func(t *testing.T) {
		e := bare(t, 1, `{}`)
		go func() {
			time.Sleep(20 * time.Millisecond)
			e.stop()
		}()
		e.barrier() // queued, never applied: returns only because the engine stopped
	})

	t.Run("a turn waiting for the idle session gives up when the engine stops", func(t *testing.T) {
		e := bare(t, 1, `{}`)
		e.idlePending = make(chan struct{})
		e.stop()
		if _, err := e.sessionFor(e.ctx, 1); !errors.Is(err, context.Canceled) {
			t.Fatalf("sessionFor returned %v, want %v", err, context.Canceled)
		}
	})
}

func TestSessionSet(t *testing.T) {
	t.Run("running sessions are never evicted, so the set may stay over its limit", func(t *testing.T) {
		ss := newSessionSet()
		for i := range 3 {
			ss.add(&session{id: strconv.Itoa(i), taskID: int64(i + 1), busy: true})
		}
		if out := ss.evict(1); len(out) != 0 || ss.lru.Len() != 3 {
			t.Fatalf("evicted %d of three running sessions, want none", len(out))
		}
	})
	t.Run("removing a session whose task moved on keeps the task's new session", func(t *testing.T) {
		ss := newSessionSet()
		old, now := &session{id: "a", taskID: 1}, &session{id: "b", taskID: 1}
		ss.add(old)
		ss.add(now)
		ss.remove(old)
		if ss.byTask[1] != now {
			t.Fatal("removing the old session dropped the task's current one")
		}
	})
}
