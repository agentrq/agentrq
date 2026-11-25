// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// shIDs is a SessionIDs that remembers what it minted, the way a signed
// token remembers it: a second handler given the same shIDs is a restarted
// process that can read the ids the first one handed out.
type shIDs struct {
	mu         sync.Mutex
	n          int
	infos      map[string]SessionInfo
	issueErr   error
	resolveErr error
}

func newSHIDs() *shIDs { return &shIDs{infos: map[string]SessionInfo{}} }

func (f *shIDs) Issue(_ context.Context, info SessionInfo) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.issueErr != nil {
		return "", f.issueErr
	}
	f.n++
	id := fmt.Sprintf("sid-%d", f.n)
	f.infos[id] = info
	return id, nil
}

func (f *shIDs) Resolve(_ context.Context, id string) (SessionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.resolveErr != nil {
		return SessionInfo{}, f.resolveErr
	}
	info, ok := f.infos[id]
	if !ok {
		return SessionInfo{}, errors.New("not an id this server minted")
	}
	return info, nil
}

type shEchoIn struct {
	Text string `json:"text,omitempty"`
}

// shServer is a server with tools that exercise the transport: echo answers
// at once, block waits for release (or its call to be cancelled), notify
// sends a notification in the call's context, and ping pings the client.
func shServer(release chan struct{}) *Server {
	s := NewServer(&Implementation{Name: "streamtest", Version: "1"}, nil)
	AddTool(s, &Tool{Name: "echo"}, func(_ context.Context, _ *CallToolRequest, in shEchoIn) (*CallToolResult, any, error) {
		return &CallToolResult{Content: []Content{&TextContent{Text: "echo:" + in.Text}}}, nil, nil
	})
	AddTool(s, &Tool{Name: "block"}, func(ctx context.Context, _ *CallToolRequest, _ shEchoIn) (*CallToolResult, any, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return &CallToolResult{Content: []Content{&TextContent{Text: "released"}}}, nil, nil
	})
	AddTool(s, &Tool{Name: "notify"}, func(ctx context.Context, req *CallToolRequest, _ shEchoIn) (*CallToolResult, any, error) {
		_ = req.Session.Notify(ctx, "notifications/message", map[string]any{"level": "info", "data": "in call"})
		return &CallToolResult{Content: []Content{&TextContent{Text: "notified"}}}, nil, nil
	})
	AddTool(s, &Tool{Name: "ping"}, func(ctx context.Context, req *CallToolRequest, _ shEchoIn) (*CallToolResult, any, error) {
		err := req.Session.Ping(ctx, nil)
		return &CallToolResult{Content: []Content{&TextContent{Text: fmt.Sprint(err)}}}, nil, nil
	})
	return s
}

func shHandler(s *Server, opts *StreamableHTTPOptions) *StreamableHTTPHandler {
	return NewStreamableHTTPHandler(func(*http.Request) *Server { return s }, opts)
}

// shReq is a POST (or other method) as a client sends it. hdr holds
// name/value pairs; an empty value removes the header.
func shReq(method, body string, hdr ...string) *http.Request {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, "/mcp", r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			req.Header.Del(hdr[i])
			continue
		}
		req.Header.Set(hdr[i], hdr[i+1])
	}
	return req
}

func shDo(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// shData decodes the messages of an SSE body, or a plain JSON body.
func shData(t *testing.T, body string) []map[string]any {
	t.Helper()
	var out []map[string]any
	if strings.HasPrefix(strings.TrimSpace(body), "{") {
		var m map[string]any
		if err := json.Unmarshal([]byte(body), &m); err != nil {
			t.Fatalf("decode %q: %v", body, err)
		}
		return []map[string]any{m}
	}
	for _, line := range strings.Split(body, "\n") {
		data, ok := strings.CutPrefix(line, "data: ")
		if !ok {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(data), &m); err != nil {
			t.Fatalf("decode %q: %v", data, err)
		}
		out = append(out, m)
	}
	return out
}

func shInit(id int, version, client string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"initialize","params":{"protocolVersion":%q,"capabilities":{},"clientInfo":{"name":%q,"version":"9"}}}`, id, version, client)
}

func shCall(id int, tool, text string) string {
	return fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"tools/call","params":{"name":%q,"arguments":{"text":%q}}}`, id, tool, text)
}

// shMeta26 is the per-request identity of a 2026-07-28 request.
const shMeta26 = `"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientInfo":{"name":"sh","version":"1"},"io.modelcontextprotocol/clientCapabilities":{}}`

// shSession finds the server's session with id.
func shSession(t *testing.T, s *Server, id string) *ServerSession {
	t.Helper()
	for ss := range s.Sessions() {
		if ss.ID() == id {
			return ss
		}
	}
	t.Fatalf("no session %q on the server", id)
	return nil
}

// shInitialize opens a session and returns its id.
func shInitialize(t *testing.T, h http.Handler, version, client string) string {
	t.Helper()
	w := shDo(h, shReq(http.MethodPost, shInit(1, version, client), "Mcp-Protocol-Version", version))
	if w.Code != http.StatusOK {
		t.Fatalf("initialize answered %d: %s", w.Code, w.Body.String())
	}
	id := w.Header().Get("Mcp-Session-Id")
	if id == "" {
		t.Fatalf("initialize gave no session id: %s", w.Body.String())
	}
	return id
}

// The helpers that read a session's handshake and the transport's headers.
func TestStreamableHelpers(t *testing.T) {
	t.Run("sessionInfoOf records the handshake, capped", func(t *testing.T) {
		if got := sessionInfoOf(nil); got != (SessionInfo{}) {
			t.Errorf("nil params recorded %+v", got)
		}
		long := strings.Repeat("v", maxSessionField+1)
		got := sessionInfoOf(&InitializeParams{ProtocolVersion: long})
		if got.ProtocolVersion != "" || got.ClientName != "" {
			t.Errorf("an oversized version was recorded: %+v", got)
		}
		got = sessionInfoOf(&InitializeParams{ProtocolVersion: "2025-06-18", ClientInfo: &Implementation{Name: "claude-code", Version: long}})
		want := SessionInfo{ProtocolVersion: "2025-06-18", ClientName: "claude-code", ClientVersion: strings.Repeat("v", maxSessionField)}
		if got != want {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})

	t.Run("capField keeps whole characters", func(t *testing.T) {
		if capField("short") != "short" {
			t.Error("a short field was changed")
		}
		// 63 ASCII bytes and then a 3-byte rune straddling the cap.
		s := strings.Repeat("a", maxSessionField-1) + "€tail"
		got := capField(s)
		if got != strings.Repeat("a", maxSessionField-1) {
			t.Errorf("capField cut a character in half: %q", got)
		}
		if got := capField(strings.Repeat("€", 30)); len(got) != 63 {
			t.Errorf("capField kept %d bytes of runes, want 63", len(got))
		}
	})

	t.Run("accepts reads every Accept form", func(t *testing.T) {
		cases := []struct {
			values      []string
			jsonOK, sse bool
		}{
			{[]string{"application/json, text/event-stream"}, true, true},
			{[]string{"application/*;q=0.9", "text/*"}, true, true},
			{[]string{"*/*"}, true, true},
			{[]string{"text/html"}, false, false},
			{nil, false, false},
		}
		for _, c := range cases {
			if j, s := accepts(c.values); j != c.jsonOK || s != c.sse {
				t.Errorf("accepts(%q) = %v, %v; want %v, %v", c.values, j, s, c.jsonOK, c.sse)
			}
		}
	})

	t.Run("validEventID takes <stream>_<index>", func(t *testing.T) {
		for id, want := range map[string]bool{"abc_1": true, "abc_0": true, "abc": false, "a_b_1": false, "abc_x": false, "abc_-1": false} {
			if validEventID(id) != want {
				t.Errorf("validEventID(%q) = %v", id, !want)
			}
		}
	})

	t.Run("isLoopback", func(t *testing.T) {
		for addr, want := range map[string]bool{
			"127.0.0.1:80": true, "localhost:3000": true, "localhost": true, "[::1]:8080": true, "[::1]": true,
			"127.0.0.1": true, "example.com": false, "10.0.0.1:80": false, "not an ip": false,
		} {
			if isLoopback(addr) != want {
				t.Errorf("isLoopback(%q) = %v", addr, !want)
			}
		}
	})

	t.Run("errorStatus maps 2026-07-28 protocol errors", func(t *testing.T) {
		cases := map[error]int{
			nil:                       0,
			jsonrpc.ErrMethodNotFound: http.StatusNotFound,
			jsonrpc.ErrInvalidParams:  http.StatusBadRequest,
			&jsonrpc.Error{Code: CodeUnsupportedProtocolVersion}:        http.StatusBadRequest,
			&jsonrpc.Error{Code: CodeMissingRequiredClientCapabilities}: http.StatusBadRequest,
			errors.New("a tool's own failure"):                          0,
		}
		for err, want := range cases {
			if got := errorStatus(err); got != want {
				t.Errorf("errorStatus(%v) = %d, want %d", err, got, want)
			}
		}
	})

	t.Run("checkStandardHeaders", func(t *testing.T) {
		call := func(method, params string) *jsonrpc.Request {
			return &jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: method, Params: json.RawMessage(params)}
		}
		hdr := func(kv ...string) http.Header {
			h := http.Header{}
			for i := 0; i+1 < len(kv); i += 2 {
				h.Set(kv[i], kv[i+1])
			}
			return h
		}
		cases := []struct {
			name string
			h    http.Header
			msg  jsonrpc.Message
			want string
		}{
			{"no version", hdr(), call("tools/list", `{}`), ""},
			{"older version", hdr("Mcp-Protocol-Version", "2025-11-25"), call("tools/list", `{}`), ""},
			{"a response", hdr("Mcp-Protocol-Version", "2026-07-28"), &jsonrpc.Response{ID: jsonrpc.Int64ID(1)}, ""},
			{"no method", hdr("Mcp-Protocol-Version", "2026-07-28"), call("tools/list", `{}`), "missing required Mcp-Method header"},
			{"method mismatch", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "ping"), call("tools/list", `{}`), "header mismatch: Mcp-Method header value 'ping' does not match body value 'tools/list'"},
			{"method without a name", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"), call("tools/list", `{}`), ""},
			{"no name", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call"), call("tools/call", `{"name":"x"}`), `missing required Mcp-Name header for method "tools/call"`},
			{"unreadable params", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "prompts/get", "Mcp-Name", "x"), call("prompts/get", `{"name":5}`), `failed to extract name from parameters for method "prompts/get"`},
			{"name mismatch", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "y"), call("tools/call", `{"name":"x"}`), "header mismatch: Mcp-Name header value 'y' does not match body value 'x'"},
			{"resource by uri", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "resources/read", "Mcp-Name", "a://b"), call("resources/read", `{"uri":"a://b"}`), ""},
			{"tool by name", hdr("Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/call", "Mcp-Name", "x"), call("tools/call", `{"name":"x"}`), ""},
		}
		for _, c := range cases {
			err := checkStandardHeaders(c.h, c.msg)
			got := ""
			if err != nil {
				got = err.Error()
			}
			if got != c.want {
				t.Errorf("%s: got %q, want %q", c.name, got, c.want)
			}
		}
	})

	t.Run("writeJSONError writes nothing it cannot encode", func(t *testing.T) {
		w := httptest.NewRecorder()
		writeJSONError(w, http.StatusBadRequest, jsonrpc.Int64ID(1), &jsonrpc.Error{Code: 1, Message: "x", Data: json.RawMessage("{")})
		if w.Code != http.StatusBadRequest || w.Body.Len() != 0 {
			t.Errorf("answered %d %q", w.Code, w.Body.String())
		}
	})
}

// A stateful handshake is answered with a session id SessionIDs minted from
// the handshake, and the session serves calls under it.
func TestStreamableHandshakeIssuesAnID(t *testing.T) {
	srv := shServer(nil)
	ids := newSHIDs()
	h := shHandler(srv, &StreamableHTTPOptions{SessionIDs: ids})

	id := shInitialize(t, h, "2025-06-18", "claude-code")
	if id != "sid-1" {
		t.Fatalf("session id %q, want the minted sid-1", id)
	}
	if got := ids.infos[id]; got.ProtocolVersion != "2025-06-18" || got.ClientName != "claude-code" || got.ClientVersion != "9" {
		t.Errorf("minted from %+v, want the handshake's version and client", got)
	}
	w := shDo(h, shReq(http.MethodPost, shCall(2, "echo", "hi"), "Mcp-Session-Id", id, "Mcp-Protocol-Version", "2025-06-18"))
	msgs := shData(t, w.Body.String())
	if w.Code != http.StatusOK || len(msgs) != 1 || !strings.Contains(w.Body.String(), "echo:hi") {
		t.Fatalf("tools/call answered %d %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type %q, want text/event-stream", ct)
	}
}

// Without SessionIDs a session is named by a random id and lives in this
// process only: an id it never handed out is not found.
func TestStreamableRandomIDsWithoutSessionIDs(t *testing.T) {
	h := shHandler(shServer(nil), nil)
	a, b := shInitialize(t, h, "2025-11-25", "a"), shInitialize(t, h, "2025-11-25", "b")
	if a == b || len(a) < 16 {
		t.Fatalf("ids %q and %q are not random", a, b)
	}
	w := shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "Mcp-Session-Id", "never-issued"))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "session not found") {
		t.Fatalf("an unknown id answered %d %q", w.Code, w.Body.String())
	}
}

// A session survives its process: when its client reopens its push stream,
// another handler with the same SessionIDs rebuilds it from the id alone,
// initialized, as the client it was, speaking the version its handshake
// negotiates. Until then a POST naming it is 404, as it was before ids were
// signed, so a client that has given up on its push stream starts over.
func TestStreamableSessionSurvivesARestart(t *testing.T) {
	ids := newSHIDs()
	before := shHandler(shServer(nil), &StreamableHTTPOptions{SessionIDs: ids})
	id := shInitialize(t, before, "2025-06-18", "claude-code")

	srv := shServer(nil)
	resumed := make(chan string, 4)
	after := shHandler(srv, &StreamableHTTPOptions{SessionIDs: ids, OnResume: func(r *http.Request, ss *ServerSession) {
		if r.Method != http.MethodGet {
			t.Errorf("OnResume ran for a %s", r.Method)
		}
		resumed <- ss.ID()
	}})
	hs := httptest.NewServer(after)
	t.Cleanup(hs.Close)
	call := shCall(2, "echo", "again")
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		if w := shDo(after, shReq(method, call, "Mcp-Session-Id", id, "Mcp-Protocol-Version", "2025-06-18")); w.Code != http.StatusNotFound {
			t.Fatalf("%s before the push stream reopened answered %d %s", method, w.Code, w.Body.String())
		}
	}

	get := shOpen(t, hs.URL, http.MethodGet, "", "Mcp-Session-Id", id, "Accept", "text/event-stream")
	if line := get.next(t); get.resp.StatusCode != http.StatusOK || line != ": ok" {
		t.Fatalf("reopening the push stream answered %d %q", get.resp.StatusCode, line)
	}
	resp, body := shPost(t, hs.URL, call, "Mcp-Session-Id", id, "Mcp-Protocol-Version", "2025-06-18")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "echo:again") {
		t.Fatalf("the restarted server answered %d %s", resp.StatusCode, body)
	}
	if got := <-resumed; got != id || len(resumed) != 0 {
		t.Fatalf("OnResume was told %q, and %d more times; want %q once", got, len(resumed), id)
	}

	ss := shSession(t, srv, id)
	p := ss.InitializeParams()
	if p == nil || p.ProtocolVersion != "2025-06-18" || p.ClientInfo == nil || p.ClientInfo.Name != "claude-code" || p.ClientInfo.Version != "9" {
		t.Fatalf("rebuilt session knows %+v, want the original handshake", p)
	}
	ss.mu.Lock()
	state := ss.state
	ss.mu.Unlock()
	if !state.initialized || state.negotiated != "2025-06-18" {
		t.Errorf("rebuilt state %+v, want initialized on 2025-06-18", state)
	}

	t.Run("a version it no longer speaks negotiates the newest", func(t *testing.T) {
		ids.infos["old"] = SessionInfo{ProtocolVersion: "2024-11-05"}
		sess, rebuilt := after.resume(srv, "old", ids.infos["old"])
		if !rebuilt {
			t.Error("a new session was not reported rebuilt")
		}
		if sess.session.state.negotiated != ProtocolVersion20251125 {
			t.Errorf("negotiated %q, want %q", sess.session.state.negotiated, ProtocolVersion20251125)
		}
		if sess.session.InitializeParams().ClientInfo != nil {
			t.Error("a session that named no client was given one")
		}
	})

	t.Run("two requests rebuilding one session share it", func(t *testing.T) {
		first, _ := after.resume(srv, "twice", SessionInfo{ProtocolVersion: "2025-11-25"})
		second, rebuilt := after.resume(srv, "twice", SessionInfo{ProtocolVersion: "2025-06-18"})
		if first != second || rebuilt {
			t.Fatalf("the same id was rebuilt twice: %v", rebuilt)
		}
	})

	t.Run("an id deleted while it was read stays deleted", func(t *testing.T) {
		after.ended.add("deleted-meanwhile")
		if sess, rebuilt := after.resume(srv, "deleted-meanwhile", SessionInfo{}); sess != nil || rebuilt {
			t.Fatal("a deleted id was rebuilt")
		}
	})
}

// shDeletingIDs reads an id back as if a DELETE of it landed meanwhile.
type shDeletingIDs struct {
	*shIDs
	h *StreamableHTTPHandler
}

func (d *shDeletingIDs) Resolve(ctx context.Context, id string) (SessionInfo, error) {
	d.h.mu.Lock()
	d.h.ended.add(id)
	d.h.mu.Unlock()
	return d.shIDs.Resolve(ctx, id)
}

// An id that cannot be rebuilt is not found, and a request with no server
// to rebuild it on is refused.
func TestStreamableLookupFailures(t *testing.T) {
	ids := newSHIDs()
	ids.resolveErr = errors.New("bad signature")
	h := shHandler(shServer(nil), &StreamableHTTPOptions{SessionIDs: ids})
	w := shDo(h, shReq(http.MethodGet, "", "Mcp-Session-Id", "forged", "Accept", "text/event-stream"))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "session not found") {
		t.Fatalf("a forged id answered %d %q", w.Code, w.Body.String())
	}

	deleting := &shDeletingIDs{shIDs: newSHIDs()}
	deleting.infos["sid-d"] = SessionInfo{ProtocolVersion: "2025-11-25"}
	h = shHandler(shServer(nil), &StreamableHTTPOptions{SessionIDs: deleting})
	deleting.h = h
	w = shDo(h, shReq(http.MethodGet, "", "Mcp-Session-Id", "sid-d", "Accept", "text/event-stream"))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "session not found") {
		t.Fatalf("an id deleted while it was read answered %d %q", w.Code, w.Body.String())
	}

	ids = newSHIDs()
	ids.infos["sid-x"] = SessionInfo{ProtocolVersion: "2025-11-25"}
	srv := shServer(nil)
	noServer := NewStreamableHTTPHandler(func(r *http.Request) *Server {
		if r.Header.Get("X-No-Server") != "" {
			return nil
		}
		return srv
	}, &StreamableHTTPOptions{SessionIDs: ids})
	w = shDo(noServer, shReq(http.MethodGet, "", "Mcp-Session-Id", "sid-x", "X-No-Server", "1", "Accept", "text/event-stream"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no server available") {
		t.Fatalf("rebuilding with no server answered %d %q", w.Code, w.Body.String())
	}
	w = shDo(noServer, shReq(http.MethodPost, shInit(1, "2025-11-25", "c"), "X-No-Server", "1"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no server available") {
		t.Fatalf("a handshake with no server answered %d %q", w.Code, w.Body.String())
	}
}

// DELETE ends a session, and its id is not rebuilt afterwards while the
// process runs.
func TestStreamableDeleteEndsTheSession(t *testing.T) {
	ids := newSHIDs()
	srv := shServer(nil)
	h := shHandler(srv, &StreamableHTTPOptions{SessionIDs: ids})
	id := shInitialize(t, h, "2025-11-25", "c")

	if w := shDo(h, shReq(http.MethodDelete, "", "Mcp-Session-Id", id)); w.Code != http.StatusNoContent {
		t.Fatalf("DELETE answered %d %q", w.Code, w.Body.String())
	}
	if !h.ended.has(id) {
		t.Error("the deleted id was not remembered")
	}
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodDelete} {
		req := shReq(method, `{"jsonrpc":"2.0","id":1,"method":"ping"}`, "Mcp-Session-Id", id, "Accept", "application/json, text/event-stream")
		if w := shDo(h, req); w.Code != http.StatusNotFound {
			t.Errorf("%s after DELETE answered %d, want 404", method, w.Code)
		}
	}
	for range srv.Sessions() {
		t.Error("the deleted session is still on the server")
	}

	t.Run("without SessionIDs nothing is remembered", func(t *testing.T) {
		h := shHandler(shServer(nil), nil)
		id := shInitialize(t, h, "2025-11-25", "c")
		if w := shDo(h, shReq(http.MethodDelete, "", "Mcp-Session-Id", id)); w.Code != http.StatusNoContent {
			t.Fatalf("DELETE answered %d", w.Code)
		}
		if len(h.ended.ids) != 0 {
			t.Errorf("remembered %v", h.ended.ids)
		}
	})

	t.Run("only the most recent deletions are remembered", func(t *testing.T) {
		defer func(n int) { maxEnded = n }(maxEnded)
		maxEnded = 2
		var e endedIDs
		for _, id := range []string{"a", "b", "b", "c", "d"} {
			e.add(id)
		}
		if e.has("a") || e.has("b") || !e.has("c") || !e.has("d") || len(e.ids) != 2 || len(e.ring) != 2 {
			t.Fatalf("remembered %d ids, a %v b %v c %v d %v", len(e.ids), e.has("a"), e.has("b"), e.has("c"), e.has("d"))
		}
	})

	t.Run("DELETE needs an id", func(t *testing.T) {
		w := shDo(h, shReq(http.MethodDelete, ""))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "DELETE requires an Mcp-Session-Id header") {
			t.Fatalf("answered %d %q", w.Code, w.Body.String())
		}
	})
}

// GET's refusals, before any stream is opened.
func TestStreamableGETRefusals(t *testing.T) {
	h := shHandler(shServer(nil), nil)
	id := shInitialize(t, h, "2025-11-25", "c")
	cases := []struct {
		name   string
		hdr    []string
		status int
		body   string
	}{
		{"wrong accept", []string{"Accept", "application/json", "Mcp-Session-Id", id}, 400, "Accept must contain 'text/event-stream' for GET requests"},
		{"no id", []string{"Accept", "text/event-stream"}, 400, "GET requires an Mcp-Session-Id header"},
		{"unknown id", []string{"Accept", "text/event-stream", "Mcp-Session-Id", "nope"}, 404, "session not found"},
		{"malformed Last-Event-ID", []string{"Accept", "text/event-stream", "Mcp-Session-Id", id, "Last-Event-ID", "abc"}, 400, `malformed Last-Event-ID "abc"`},
		{"replay", []string{"Accept", "text/event-stream", "Mcp-Session-Id", id, "Last-Event-ID", "abc_1"}, 400, "stream replay unsupported"},
	}
	for _, c := range cases {
		w := shDo(h, shReq(http.MethodGet, "", c.hdr...))
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.body) {
			t.Errorf("%s: answered %d %q, want %d %q", c.name, w.Code, w.Body.String(), c.status, c.body)
		}
	}
}

// shStream is an open response read event by event.
type shStream struct {
	resp   *http.Response
	lines  chan string
	cancel context.CancelFunc
}

func shOpen(t *testing.T, url, method, body string, hdr ...string) *shStream {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req, _ := http.NewRequestWithContext(ctx, method, url, r)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	s := &shStream{resp: resp, lines: make(chan string, 64), cancel: cancel}
	go func() {
		defer close(s.lines)
		sc := bufio.NewScanner(resp.Body)
		for sc.Scan() {
			s.lines <- sc.Text()
		}
	}()
	t.Cleanup(func() { cancel(); resp.Body.Close() })
	return s
}

// next is the next non-empty line, "" at the end of the stream.
func (s *shStream) next(t *testing.T) string {
	t.Helper()
	for {
		select {
		case line, ok := <-s.lines:
			if !ok {
				return ""
			}
			if line != "" && line != "event: message" {
				return line
			}
		case <-time.After(5 * time.Second):
			t.Fatal("nothing arrived on the stream within 5s")
			return ""
		}
	}
}

// shPost posts body to a running server.
func shPost(t *testing.T, url, body string, hdr ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return resp, string(data)
}

// The GET stream: it opens with a comment, carries what the server pushes
// and the pings it sends, answered by POSTed responses; a second GET is
// refused while it is open; and it ends when the session does.
func TestStreamableGETStream(t *testing.T) {
	srv := shServer(nil)
	opened := make(chan string, 1)
	h := shHandler(srv, &StreamableHTTPOptions{SessionIDs: newSHIDs(), OnStreamOpen: func(_ context.Context, id string) { opened <- id }})
	hs := httptest.NewServer(h)
	t.Cleanup(hs.Close)

	resp, _ := shPost(t, hs.URL, shInit(1, "2025-11-25", "c"), "Mcp-Protocol-Version", "2025-11-25")
	id := resp.Header.Get("Mcp-Session-Id")
	ss := shSession(t, srv, id)

	// With no stream open, a push and a ping have nowhere to go.
	if err := ss.Notify(context.Background(), "notifications/x", nil); err == nil || !strings.Contains(err.Error(), "stream not connected") {
		t.Fatalf("a push with no stream returned %v", err)
	}
	if err := ss.Ping(context.Background(), nil); err == nil || !strings.Contains(err.Error(), "stream not connected") {
		t.Fatalf("a ping with no stream returned %v", err)
	}

	get := shOpen(t, hs.URL, http.MethodGet, "", "Mcp-Session-Id", id, "Accept", "text/event-stream")
	if get.resp.StatusCode != http.StatusOK || get.resp.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("GET answered %d %q", get.resp.StatusCode, get.resp.Header.Get("Content-Type"))
	}
	if line := get.next(t); line != ": ok" {
		t.Fatalf("the stream opened with %q, want the comment", line)
	}
	if got := <-opened; got != id {
		t.Fatalf("OnStreamOpen was told %q, want %q", got, id)
	}

	if err := ss.Notify(context.Background(), "notifications/claude/channel", map[string]any{"content": "hello"}); err != nil {
		t.Fatal(err)
	}
	if line := get.next(t); !strings.Contains(line, `"method":"notifications/claude/channel"`) || !strings.Contains(line, "hello") {
		t.Fatalf("pushed %q", line)
	}

	pinged := make(chan error, 1)
	go func() { pinged <- ss.Ping(context.Background(), nil) }()
	line := get.next(t)
	var ping struct {
		ID     int64  `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ping); err != nil || ping.Method != "ping" {
		t.Fatalf("the ping arrived as %q", line)
	}
	answer, _ := shPost(t, hs.URL, fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{}}`, ping.ID), "Mcp-Session-Id", id)
	if answer.StatusCode != http.StatusAccepted {
		t.Fatalf("the ping's answer was taken with %d", answer.StatusCode)
	}
	if err := <-pinged; err != nil {
		t.Fatalf("the answered ping failed: %v", err)
	}

	second, body := func() (*http.Response, string) {
		req, _ := http.NewRequest(http.MethodGet, hs.URL, nil)
		req.Header.Set("Accept", "text/event-stream")
		req.Header.Set("Mcp-Session-Id", id)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		return resp, string(data)
	}()
	if second.StatusCode != http.StatusConflict || !strings.Contains(body, "stream ID conflicts with ongoing stream") {
		t.Fatalf("a second GET answered %d %q", second.StatusCode, body)
	}

	_ = ss.Close()
	if line := get.next(t); line != "" {
		t.Fatalf("the stream carried %q after the session closed", line)
	}
	if err := ss.Ping(context.Background(), nil); err == nil {
		t.Fatal("a closed session pinged")
	}
}

// A ping the client never answers gives up with its context.
func TestStreamablePingTimesOut(t *testing.T) {
	srv := shServer(nil)
	h := shHandler(srv, nil)
	hs := httptest.NewServer(h)
	t.Cleanup(hs.Close)
	resp, _ := shPost(t, hs.URL, shInit(1, "2025-11-25", "c"))
	id := resp.Header.Get("Mcp-Session-Id")
	get := shOpen(t, hs.URL, http.MethodGet, "", "Mcp-Session-Id", id, "Accept", "text/event-stream")
	get.next(t)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := shSession(t, srv, id).Ping(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("an unanswered ping returned %v", err)
	}
}

// The POST checks that come before any message is handled.
func TestStreamablePOSTRefusals(t *testing.T) {
	srv := shServer(nil)
	h := shHandler(srv, &StreamableHTTPOptions{MaxRequestBodyBytes: 512})
	id := shInitialize(t, h, "2025-11-25", "c")
	S := []string{"Mcp-Session-Id", id}
	with := func(extra ...string) []string { return append(append([]string{}, S...), extra...) }
	cases := []struct {
		name   string
		body   string
		hdr    []string
		status int
		want   string
	}{
		{"Last-Event-ID", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, with("Last-Event-ID", "x_1"), 400, "can't send Last-Event-ID for POST request"},
		{"too large", `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"_meta":{"x":"` + strings.Repeat("y", 600) + `"}}}`, S, 413, "request body exceeds 512 bytes"},
		{"empty", ``, S, 400, "POST requires a non-empty body"},
		{"malformed", `{"jsonrpc":`, S, 400, "malformed payload"},
		{"batch on 2025-06-18", `[{"jsonrpc":"2.0","id":1,"method":"ping"}]`, with("Mcp-Protocol-Version", "2025-06-18"), 400, "JSON-RPC batching is not supported in 2025-06-18 and later (request version: 2025-06-18)"},
		{"unknown method", `{"jsonrpc":"2.0","id":1,"method":"foo/bar"}`, S, 400, `JSON RPC not handled: "foo/bar" unsupported`},
		{"notification with an id", `{"jsonrpc":"2.0","id":1,"method":"notifications/initialized"}`, S, 400, `unexpected id for "notifications/initialized"`},
		{"wrong content type", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, with("Content-Type", "text/plain"), 415, "Content-Type must be 'application/json'"},
		{"no content type", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, with("Content-Type", ""), 415, "Content-Type must be 'application/json'"},
		{"accepts json only", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, with("Accept", "application/json"), 400, "Accept must contain both 'application/json' and 'text/event-stream'"},
		{"unsupported version header", `{"jsonrpc":"2.0","id":1,"method":"ping"}`, with("Mcp-Protocol-Version", "2025-03-26"), 400, "Bad Request: Unsupported protocol version (supported versions: 2026-07-28,2025-11-25,2025-06-18)"},
	}
	for _, c := range cases {
		w := shDo(h, shReq(http.MethodPost, c.body, c.hdr...))
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: answered %d %q, want %d %q", c.name, w.Code, w.Body.String(), c.status, c.want)
		}
	}

	t.Run("a body that cannot be read", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/mcp", shFailingReader{})
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Session-Id", id)
		if w := shDo(h, req); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "failed to read body") {
			t.Fatalf("answered %d %q", w.Code, w.Body.String())
		}
	})

	t.Run("PUT is not a method of the transport", func(t *testing.T) {
		w := shDo(h, shReq(http.MethodPut, `{}`))
		if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "GET, POST, DELETE" {
			t.Fatalf("answered %d Allow %q", w.Code, w.Header().Get("Allow"))
		}
	})

	t.Run("no limit at all", func(t *testing.T) {
		h := shHandler(srv, &StreamableHTTPOptions{MaxRequestBodyBytes: -1})
		w := shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"ping","params":{"_meta":{"x":"`+strings.Repeat("y", 5<<20)+`"}}}`))
		if w.Code != http.StatusOK {
			t.Fatalf("an unlimited handler refused a large body: %d", w.Code)
		}
	})
}

type shFailingReader struct{}

func (shFailingReader) Read([]byte) (int, error) { return 0, errors.New("connection reset") }

// Messages that ask for no answer are taken with 202: notifications, and the
// client's answers to calls nobody is waiting for. A batch with no version
// header is read as 2025-03-26, which allowed batches.
func TestStreamableNotificationsAndBatches(t *testing.T) {
	srv := shServer(nil)
	h := shHandler(srv, nil)
	id := shInitialize(t, h, "2025-11-25", "c")
	for _, body := range []string{
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":42,"result":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":7}}`,
	} {
		if w := shDo(h, shReq(http.MethodPost, body, "Mcp-Session-Id", id)); w.Code != http.StatusAccepted || w.Body.Len() != 0 {
			t.Errorf("%s answered %d %q", body, w.Code, w.Body.String())
		}
	}

	w := shDo(h, shReq(http.MethodPost, `[{"jsonrpc":"2.0","method":"notifications/cancelled","params":{"requestId":1}},{"jsonrpc":"2.0","id":5,"method":"ping"},{"jsonrpc":"2.0","id":6,"method":"tools/list"}]`, "Mcp-Session-Id", id))
	msgs := shData(t, w.Body.String())
	if w.Code != http.StatusOK || len(msgs) != 2 {
		t.Fatalf("the batch answered %d %s", w.Code, w.Body.String())
	}

	t.Run("a session closing under a notification", func(t *testing.T) {
		sess, _ := h.resume(srv, "closing", SessionInfo{ProtocolVersion: "2025-11-25"})
		_ = sess.session.Close()
		w := httptest.NewRecorder()
		h.servePOST(w, shReq(http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`), srv, sess)
		if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), "session is closing") {
			t.Fatalf("answered %d %q", w.Code, w.Body.String())
		}
	})
}

// A request with no session runs on one of its own that does not outlive
// it: a ping is answered, anything else is refused before the handshake,
// and no session id goes out.
func TestStreamableRequestsWithoutASession(t *testing.T) {
	srv := shServer(nil)
	h := shHandler(srv, nil)
	w := shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"ping"}`))
	if w.Code != http.StatusOK || w.Header().Get("Mcp-Session-Id") != "" || !strings.Contains(w.Body.String(), `"result":{}`) {
		t.Fatalf("ping answered %d %q %s", w.Code, w.Header().Get("Mcp-Session-Id"), w.Body.String())
	}
	w = shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`))
	if !strings.Contains(w.Body.String(), `method \"tools/list\" is invalid during session initialization`) {
		t.Fatalf("tools/list before a handshake answered %s", w.Body.String())
	}
	if w := shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)); w.Code != http.StatusAccepted {
		t.Fatalf("a notification answered %d", w.Code)
	}
	for range srv.Sessions() {
		t.Fatal("a request without a session left one behind")
	}
}

// The handshake's own failures: a handshake that fails gets no session id,
// one SessionIDs cannot mint for is an internal error, and a second
// handshake on a session is refused under that session's id.
func TestStreamableHandshakeFailures(t *testing.T) {
	srv := shServer(nil)

	t.Run("a failed handshake gets no id", func(t *testing.T) {
		h := shHandler(srv, &StreamableHTTPOptions{SessionIDs: newSHIDs()})
		w := shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":[1]}`))
		if w.Header().Get("Mcp-Session-Id") != "" || !strings.Contains(w.Body.String(), `"error"`) {
			t.Fatalf("answered %q %s", w.Header().Get("Mcp-Session-Id"), w.Body.String())
		}
		if len(h.sessions) != 0 {
			t.Fatalf("kept %v", h.sessions)
		}
	})

	t.Run("an id that cannot be minted", func(t *testing.T) {
		ids := newSHIDs()
		ids.issueErr = errors.New("no key")
		h := shHandler(srv, &StreamableHTTPOptions{SessionIDs: ids})
		w := shDo(h, shReq(http.MethodPost, shInit(1, "2025-11-25", "c")))
		msgs := shData(t, w.Body.String())
		errObj, _ := msgs[0]["error"].(map[string]any)
		if w.Header().Get("Mcp-Session-Id") != "" || errObj == nil || errObj["code"] != float64(jsonrpc.CodeInternalError) || errObj["message"] != "internal error: could not start a session" {
			t.Fatalf("answered %q %s", w.Header().Get("Mcp-Session-Id"), w.Body.String())
		}
	})

	t.Run("a second handshake is refused under the session's id", func(t *testing.T) {
		h := shHandler(srv, nil)
		id := shInitialize(t, h, "2025-11-25", "c")
		w := shDo(h, shReq(http.MethodPost, shInit(2, "2025-11-25", "c"), "Mcp-Session-Id", id))
		if w.Header().Get("Mcp-Session-Id") != id || !strings.Contains(w.Body.String(), `duplicate \"initialize\" received`) {
			t.Fatalf("answered %q %s", w.Header().Get("Mcp-Session-Id"), w.Body.String())
		}
	})

	t.Run("two handshakes in one batch keep the first", func(t *testing.T) {
		h := shHandler(srv, nil)
		w := shDo(h, shReq(http.MethodPost, "["+shInit(1, "2025-11-25", "c")+","+shInit(2, "2025-11-25", "c")+"]"))
		id := w.Header().Get("Mcp-Session-Id")
		msgs := shData(t, w.Body.String())
		if id == "" || len(msgs) != 2 || !strings.Contains(w.Body.String(), "duplicate") {
			t.Fatalf("answered %q %s", id, w.Body.String())
		}
	})
}

// A call whose id is still in flight on the session is refused, and a call
// the client stops waiting for runs on to completion. A POST's headers go
// out with its first event, so a blocked call is posted from a goroutine.
func TestStreamableCallsInFlight(t *testing.T) {
	// shPosted posts in the background, with a context the test can cancel.
	type posted struct {
		body string
		err  error
	}
	shPostAsync := func(ctx context.Context, url, body string, hdr ...string) chan posted {
		done := make(chan posted, 1)
		go func() {
			req, _ := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Accept", "application/json, text/event-stream")
			for i := 0; i+1 < len(hdr); i += 2 {
				req.Header.Set(hdr[i], hdr[i+1])
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				done <- posted{err: err}
				return
			}
			defer resp.Body.Close()
			data, err := io.ReadAll(resp.Body)
			done <- posted{body: string(data), err: err}
		}()
		return done
	}
	// waitFor polls until cond holds.
	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for !cond() {
			if time.Now().After(deadline) {
				t.Fatalf("timed out waiting until %s", what)
			}
			time.Sleep(5 * time.Millisecond)
		}
	}
	sessionOf := func(h *StreamableHTTPHandler, id string) *httpSession {
		h.mu.Lock()
		defer h.mu.Unlock()
		return h.sessions[id]
	}

	release := make(chan struct{})
	srv := shServer(release)
	h := shHandler(srv, nil)
	hs := httptest.NewServer(h)
	t.Cleanup(hs.Close)
	resp, _ := shPost(t, hs.URL, shInit(1, "2025-11-25", "c"))
	id := resp.Header.Get("Mcp-Session-Id")

	first := shPostAsync(context.Background(), hs.URL, shCall(7, "block", ""), "Mcp-Session-Id", id)
	waitFor("the first call is in flight", func() bool {
		sess := sessionOf(h, id)
		sess.mu.Lock()
		defer sess.mu.Unlock()
		_, busy := sess.byRequest[jsonrpc.Int64ID(7)]
		return busy
	})
	dup, body := shPost(t, hs.URL, shCall(7, "echo", "x"), "Mcp-Session-Id", id)
	if dup.StatusCode != http.StatusBadRequest || !strings.Contains(body, "duplicate in-flight request ID 7") {
		t.Fatalf("a duplicate id answered %d %q", dup.StatusCode, body)
	}
	close(release)
	if got := <-first; got.err != nil || !strings.Contains(got.body, "released") {
		t.Fatalf("the blocked call answered %q %v", got.body, got.err)
	}

	t.Run("a client that goes away", func(t *testing.T) {
		release := make(chan struct{})
		srv := shServer(release)
		h := shHandler(srv, nil)
		hs := httptest.NewServer(h)
		t.Cleanup(hs.Close)
		resp, _ := shPost(t, hs.URL, shInit(1, "2025-11-25", "c"))
		id := resp.Header.Get("Mcp-Session-Id")
		ctx, cancel := context.WithCancel(context.Background())
		call := shPostAsync(ctx, hs.URL, shCall(9, "block", ""), "Mcp-Session-Id", id)
		waitFor("the call is in flight", func() bool {
			sess := sessionOf(h, id)
			sess.mu.Lock()
			defer sess.mu.Unlock()
			return len(sess.streams) == 1
		})
		cancel()
		if got := <-call; got.err == nil {
			t.Fatal("the cancelled POST completed")
		}
		waitFor("the dropped POST's stream is released", func() bool {
			sess := sessionOf(h, id)
			sess.mu.Lock()
			defer sess.mu.Unlock()
			return len(sess.streams) == 0
		})
		close(release)
	})

	t.Run("a long call keeps its POST alive", func(t *testing.T) {
		release := make(chan struct{})
		srv := shServer(release)
		h := shHandler(srv, nil)
		h.keepalive = 10 * time.Millisecond
		hs := httptest.NewServer(h)
		t.Cleanup(hs.Close)
		resp, _ := shPost(t, hs.URL, shInit(1, "2025-11-25", "c"))
		id := resp.Header.Get("Mcp-Session-Id")

		req, _ := http.NewRequest(http.MethodPost, hs.URL, strings.NewReader(shCall(3, "block", "")))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Mcp-Session-Id", id)
		// The headers arrive while the call still waits: before, they came
		// only with its answer.
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		if res.StatusCode != http.StatusOK || res.Header.Get("Content-Type") != "text/event-stream" {
			t.Fatalf("the waiting call answered %d %q", res.StatusCode, res.Header.Get("Content-Type"))
		}
		buf := make([]byte, len(": ok\n\n"))
		if _, err := io.ReadFull(res.Body, buf); err != nil || string(buf) != ": ok\n\n" {
			t.Fatalf("the waiting call sent %q %v", buf, err)
		}
		close(release)
		rest, err := io.ReadAll(res.Body)
		if err != nil || !strings.Contains(string(rest), "released") {
			t.Fatalf("the call then answered %q %v", rest, err)
		}
		if msgs := shData(t, string(rest)); len(msgs) != 1 {
			t.Fatalf("the comments read as messages: %v", msgs)
		}
	})
}

// A call's own notifications go out on its POST, before its answer.
func TestStreamableNotificationInACall(t *testing.T) {
	h := shHandler(shServer(nil), nil)
	id := shInitialize(t, h, "2025-11-25", "c")
	w := shDo(h, shReq(http.MethodPost, shCall(3, "notify", ""), "Mcp-Session-Id", id))
	msgs := shData(t, w.Body.String())
	if len(msgs) != 2 || msgs[0]["method"] != "notifications/message" || msgs[1]["id"] != float64(3) {
		t.Fatalf("the POST carried %s", w.Body.String())
	}
}

// On a stateful server a 2026-07-28 request is refused with the revisions a
// handshake can still reach; server/discover is the exception, and a method
// nobody serves is 404 under that revision.
func TestStreamableStatefulRefusesTheModernRevision(t *testing.T) {
	h := shHandler(shServer(nil), nil)
	w := shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{`+shMeta26+`}}`, "Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "tools/list"))
	var env struct {
		Error struct {
			Code int64 `json:"code"`
			Data UnsupportedProtocolVersionData
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &env); err != nil || w.Code != http.StatusBadRequest || env.Error.Code != CodeUnsupportedProtocolVersion {
		t.Fatalf("answered %d %s", w.Code, w.Body.String())
	}
	if strings.Join(env.Error.Data.Supported, ",") != "2025-11-25,2025-06-18" || env.Error.Data.Requested != "2026-07-28" {
		t.Errorf("offered %+v", env.Error.Data)
	}

	w = shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{`+shMeta26+`}}`, "Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "server/discover"))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"supportedVersions":["2025-11-25","2025-06-18"]`) {
		t.Fatalf("discover answered %d %s", w.Code, w.Body.String())
	}

	w = shDo(h, shReq(http.MethodPost, `{"jsonrpc":"2.0","id":1,"method":"foo/bar","params":{`+shMeta26+`}}`, "Mcp-Protocol-Version", "2026-07-28"))
	if w.Code != http.StatusNotFound || !strings.Contains(w.Body.String(), `"code":-32601`) || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("an unknown method answered %d %s", w.Code, w.Body.String())
	}
}

// A stateless server's 2026-07-28 rules: the header and the _meta must both
// name the revision and agree, protocol errors carry an HTTP status, and
// requests on earlier revisions are served as an initialized client of the
// revision they name.
func TestStreamableStateless(t *testing.T) {
	srv := shServer(nil)
	h := shHandler(srv, &StreamableHTTPOptions{Stateless: true})
	v26 := []string{"Mcp-Protocol-Version", "2026-07-28"}
	cases := []struct {
		name   string
		body   string
		hdr    []string
		status int
		want   string
	}{
		{"no header", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + shMeta26 + `}}`, nil, 400, `"code":-32020,"message":"Mcp-Protocol-Version header is required for requests carrying \"io.modelcontextprotocol/protocolVersion\""`},
		{"no _meta", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, append(v26, "Mcp-Method", "tools/list"), 400, `"code":-32602,"message":"missing or invalid _meta field \"io.modelcontextprotocol/protocolVersion\""`},
		{"disagree", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + shMeta26 + `}}`, []string{"Mcp-Protocol-Version", "2025-11-25", "Mcp-Method", "tools/list"}, 400, `"code":-32020,"message":"Mcp-Protocol-Version header \"2025-11-25\" does not match request io.modelcontextprotocol/protocolVersion \"2026-07-28\""`},
		{"no Mcp-Method", `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{` + shMeta26 + `}}`, v26, 400, "missing required Mcp-Method header"},
		{"unknown tool is 400", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope",` + shMeta26 + `}}`, append(v26, "Mcp-Method", "tools/call", "Mcp-Name", "nope"), 400, `unknown tool \"nope\"`},
		{"a removed method is 404", `{"jsonrpc":"2.0","id":1,"method":"ping","params":{` + shMeta26 + `}}`, append(v26, "Mcp-Method", "ping"), 404, `method not found: \"ping\"`},
		{"served", `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"text":"x"},` + shMeta26 + `}}`, append(v26, "Mcp-Method", "tools/call", "Mcp-Name", "echo"), 200, `"resultType":"complete"`},
		{"an older revision", shCall(1, "echo", "old"), []string{"Mcp-Protocol-Version", "2025-06-18"}, 200, "echo:old"},
		{"no revision at all", shCall(1, "echo", "none"), nil, 200, "echo:none"},
		{"a handshake", shInit(1, "2025-06-18", "c"), nil, 200, `"protocolVersion":"2025-06-18"`},
		{"handshake and initialized together", "[" + shInit(1, "2025-06-18", "c") + `,{"jsonrpc":"2.0","method":"notifications/initialized"}]`, nil, 200, `"protocolVersion":"2025-06-18"`},
	}
	for _, c := range cases {
		w := shDo(h, shReq(http.MethodPost, c.body, c.hdr...))
		if w.Code != c.status || !strings.Contains(w.Body.String(), c.want) {
			t.Errorf("%s: answered %d %s, want %d %q", c.name, w.Code, w.Body.String(), c.status, c.want)
		}
		if w.Header().Get("Mcp-Session-Id") != "" {
			t.Errorf("%s: a stateless server handed out an id", c.name)
		}
	}
	for range srv.Sessions() {
		t.Error("a stateless request left its session behind")
	}

	t.Run("a stateless session cannot ask the client anything", func(t *testing.T) {
		w := shDo(h, shReq(http.MethodPost, shCall(1, "ping", "")))
		if !strings.Contains(w.Body.String(), "stateless servers cannot make requests") {
			t.Fatalf("a ping from a stateless call: %s", w.Body.String())
		}
	})

	t.Run("GET and DELETE are not served", func(t *testing.T) {
		for _, m := range []string{http.MethodGet, http.MethodDelete} {
			w := shDo(h, shReq(m, ""))
			if w.Code != http.StatusMethodNotAllowed || w.Header().Get("Allow") != "POST" {
				t.Errorf("%s answered %d Allow %q", m, w.Code, w.Header().Get("Allow"))
			}
		}
	})

	t.Run("POST headers are checked", func(t *testing.T) {
		w := shDo(h, shReq(http.MethodPost, shCall(1, "echo", ""), "Content-Type", "text/plain"))
		if w.Code != http.StatusUnsupportedMediaType {
			t.Fatalf("answered %d", w.Code)
		}
	})

	t.Run("no server", func(t *testing.T) {
		h := NewStreamableHTTPHandler(func(*http.Request) *Server { return nil }, &StreamableHTTPOptions{Stateless: true})
		w := shDo(h, shReq(http.MethodPost, shCall(1, "echo", ""), "Mcp-Protocol-Version", "2025-06-18"))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "no server available") {
			t.Fatalf("answered %d %q", w.Code, w.Body.String())
		}
	})
}

// A stateless listen: acknowledged on its own stream, told of the resources
// it follows as they change, and over when its client goes.
func TestStreamableStatelessListen(t *testing.T) {
	srv := NewServer(&Implementation{Name: "listen", Version: "1"}, &ServerOptions{
		SubscribeHandler:   func(context.Context, *SubscribeRequest) error { return nil },
		UnsubscribeHandler: func(context.Context, *UnsubscribeRequest) error { return nil },
	})
	srv.AddResource(&Resource{URI: "test://r", Name: "r"}, func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error) {
		return &ReadResourceResult{Contents: []*ResourceContents{{Text: "x"}}}, nil
	})
	hs := httptest.NewServer(shHandler(srv, &StreamableHTTPOptions{Stateless: true}))
	t.Cleanup(hs.Close)

	listen := shOpen(t, hs.URL, http.MethodPost,
		`{"jsonrpc":"2.0","id":"L","method":"subscriptions/listen","params":{"notifications":{"resourceSubscriptions":["test://r"]},`+shMeta26+`}}`,
		"Mcp-Protocol-Version", "2026-07-28", "Mcp-Method", "subscriptions/listen")
	if line := listen.next(t); !strings.Contains(line, "notifications/subscriptions/acknowledged") || !strings.Contains(line, `"io.modelcontextprotocol/subscriptionId":"L"`) {
		t.Fatalf("the listen opened with %q", line)
	}
	if err := srv.ResourceUpdated(context.Background(), &ResourceUpdatedNotificationParams{URI: "test://r"}); err != nil {
		t.Fatal(err)
	}
	if line := listen.next(t); !strings.Contains(line, "notifications/resources/updated") || !strings.Contains(line, `"io.modelcontextprotocol/subscriptionId":"L"`) {
		t.Fatalf("the update arrived as %q", line)
	}
	listen.cancel()
	deadline := time.Now().Add(5 * time.Second)
	for {
		srv.mu.Lock()
		n := len(srv.resSubs)
		srv.mu.Unlock()
		if n == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the listen's subscription outlived its client")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// Requests refused before the transport looks at them: DNS rebinding, a
// cross-origin request, and a revision the server does not speak.
func TestStreamableProtections(t *testing.T) {
	srv := shServer(nil)
	loopback := &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 3000}
	withLocal := func(req *http.Request, addr net.Addr, host string) *http.Request {
		req = req.WithContext(context.WithValue(req.Context(), http.LocalAddrContextKey, addr))
		req.Host = host
		return req
	}

	h := shHandler(srv, nil)
	w := shDo(h, withLocal(shReq(http.MethodPost, shInit(1, "2025-11-25", "c")), loopback, "evil.example"))
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), `Forbidden: invalid Host header "evil.example"`) {
		t.Fatalf("a rebound request answered %d %q", w.Code, w.Body.String())
	}
	if w := shDo(h, withLocal(shReq(http.MethodPost, shInit(1, "2025-11-25", "c")), loopback, "localhost:3000")); w.Code != http.StatusOK {
		t.Fatalf("a local request answered %d", w.Code)
	}
	public := &net.TCPAddr{IP: net.IPv4(10, 0, 0, 1), Port: 3000}
	if w := shDo(h, withLocal(shReq(http.MethodPost, shInit(1, "2025-11-25", "c")), public, "agentrq.example")); w.Code != http.StatusOK {
		t.Fatalf("a request to a public address answered %d", w.Code)
	}
	open := shHandler(srv, &StreamableHTTPOptions{DisableLocalhostProtection: true})
	if w := shDo(open, withLocal(shReq(http.MethodPost, shInit(1, "2025-11-25", "c")), loopback, "evil.example")); w.Code != http.StatusOK {
		t.Fatalf("with the protection off the request answered %d", w.Code)
	}

	cop := http.NewCrossOriginProtection()
	guarded := shHandler(srv, &StreamableHTTPOptions{CrossOriginProtection: cop})
	if w := shDo(guarded, shReq(http.MethodPost, shInit(1, "2025-11-25", "c"), "Sec-Fetch-Site", "cross-site")); w.Code != http.StatusForbidden {
		t.Fatalf("a cross-origin request answered %d", w.Code)
	}
	if w := shDo(guarded, shReq(http.MethodPost, shInit(1, "2025-11-25", "c"))); w.Code != http.StatusOK {
		t.Fatalf("a same-origin request answered %d", w.Code)
	}

	noServer := NewStreamableHTTPHandler(func(*http.Request) *Server { return nil }, nil)
	w = shDo(noServer, shReq(http.MethodPost, shInit(1, "2025-11-25", "c"), "Mcp-Protocol-Version", "2024-11-05"))
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "supported versions: 2026-07-28,2025-11-25,2025-06-18") {
		t.Fatalf("an unsupported revision answered %d %q", w.Code, w.Body.String())
	}
}

// Where a message goes when the stream it belongs on is not there.
func TestStreamableSendRouting(t *testing.T) {
	srv := shServer(nil)
	h := shHandler(srv, nil)
	sess := h.newSession(srv, "s")
	ctx := context.Background()

	t.Run("a message that cannot be encoded", func(t *testing.T) {
		bad := &jsonrpc.Request{Method: "x", Params: json.RawMessage("{")}
		if err := sess.send(ctx, bad); err == nil {
			t.Fatal("an unencodable message was sent")
		}
	})

	t.Run("a push for a call that is over", func(t *testing.T) {
		note, _ := jsonrpc.NewNotification("notifications/x", nil)
		err := sess.send(withRequestID(ctx, sess.session, jsonrpc.Int64ID(99)), note)
		if err == nil || !strings.Contains(err.Error(), "write to closed stream") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("a push made in another session's call goes to the push stream", func(t *testing.T) {
		other := h.newSession(srv, "other")
		w := httptest.NewRecorder()
		st := sess.attachStandalone(w)
		st.open()
		defer sess.detachStandalone(st)
		note, _ := jsonrpc.NewNotification("notifications/x", nil)
		if err := sess.send(withRequestID(ctx, other.session, jsonrpc.Int64ID(99)), note); err != nil {
			t.Fatalf("got %v", err)
		}
		if !strings.Contains(w.Body.String(), `"method":"notifications/x"`) {
			t.Fatalf("the push stream carried %q", w.Body.String())
		}
	})

	t.Run("a stream whose writer fails", func(t *testing.T) {
		st := sess.attachStandalone(shBrokenWriter{httptest.NewRecorder()})
		st.open()
		defer sess.detachStandalone(st)
		note, _ := jsonrpc.NewNotification("notifications/x", nil)
		if err := sess.send(ctx, note); err == nil || !strings.Contains(err.Error(), "undelivered message: broken pipe") {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("a closed session takes no push", func(t *testing.T) {
		st := sess.attachStandalone(httptest.NewRecorder())
		sess.close()
		sess.close()
		note, _ := jsonrpc.NewNotification("notifications/x", nil)
		if err := sess.send(ctx, note); err == nil || err.Error() != "session is closed" {
			t.Fatalf("got %v", err)
		}
		sess.detachStandalone(st)
		sess.detachStandalone(st)
	})

	t.Run("forget leaves another session's id alone", func(t *testing.T) {
		other := h.newSession(srv, "s")
		h.sessions["s"] = other
		h.forget(sess)
		if h.sessions["s"] != other {
			t.Fatal("forget dropped a session it does not own")
		}
		h.forget(other)
		if _, ok := h.sessions["s"]; ok {
			t.Fatal("forget kept the session it owns")
		}
		h.forget(h.newSession(srv, ""))
	})
}

type shBrokenWriter struct{ *httptest.ResponseRecorder }

func (shBrokenWriter) Write([]byte) (int, error) { return 0, errors.New("broken pipe") }

// The stream's own bookkeeping: what is held waits for the handshake, a
// protocol error that comes after the stream has started still goes out,
// and a finished request's stream takes nothing more.
func TestStreamableStreamUnits(t *testing.T) {
	t.Run("held events wait, and are dropped if the request is over", func(t *testing.T) {
		w := httptest.NewRecorder()
		st := newStream(w, map[jsonrpc.ID]bool{jsonrpc.Int64ID(1): true}, false, false)
		st.unhold() // not held: nothing to do
		st.hold()
		if err := st.write([]byte(`{"a":1}`), jsonrpc.ID{}, 0); err != nil {
			t.Fatal(err)
		}
		if w.Body.Len() != 0 {
			t.Fatal("a held event went out")
		}
		st.unhold()
		if !strings.Contains(w.Body.String(), `data: {"a":1}`) {
			t.Fatalf("the held event never went out: %q", w.Body.String())
		}

		w2 := httptest.NewRecorder()
		st2 := newStream(w2, map[jsonrpc.ID]bool{jsonrpc.Int64ID(1): true}, false, false)
		st2.hold()
		_ = st2.write([]byte(`{"b":1}`), jsonrpc.ID{}, 0)
		st2.release()
		st2.unhold()
		if w2.Body.Len() != 0 {
			t.Fatal("a released stream was written to")
		}
		if err := st2.write([]byte(`{}`), jsonrpc.Int64ID(1), 0); err == nil {
			t.Fatal("a released stream took a write")
		}
		select {
		case <-st2.done:
		default:
			t.Fatal("the answered stream is not done")
		}
	})

	t.Run("a push stream holds what it is sent until it is open", func(t *testing.T) {
		srv := shServer(nil)
		sess := shHandler(srv, nil).newSession(srv, "s")
		w := httptest.NewRecorder()
		st := sess.attachStandalone(w)
		note, _ := jsonrpc.NewNotification("notifications/x", nil)
		if err := sess.send(context.Background(), note); err != nil {
			t.Fatal(err)
		}
		if w.Body.Len() != 0 {
			t.Fatalf("a push went out before the stream opened: %q", w.Body.String())
		}
		w.Header().Set("Content-Type", "text/event-stream")
		st.open()
		body := w.Body.String()
		if !strings.HasPrefix(body, ": ok\n\n") || !strings.Contains(body, `"method":"notifications/x"`) || w.Header().Get("Content-Type") != "text/event-stream" {
			t.Fatalf("the stream opened %q with %q", w.Header().Get("Content-Type"), body)
		}
	})

	t.Run("a keepalive skips a held, released or finished stream", func(t *testing.T) {
		w := httptest.NewRecorder()
		st := newStream(w, map[jsonrpc.ID]bool{jsonrpc.Int64ID(1): true}, false, false)
		st.hold()
		st.keepalive()
		if w.Body.Len() != 0 {
			t.Fatalf("a held stream was kept alive: %q", w.Body.String())
		}
		st.unhold()
		st.keepalive()
		if w.Body.String() != ": ok\n\n" {
			t.Fatalf("the keepalive wrote %q", w.Body.String())
		}
		_ = st.write([]byte(`{}`), jsonrpc.Int64ID(1), 0)
		n := w.Body.Len()
		st.keepalive()
		st.release()
		st.keepalive()
		if w.Body.Len() != n {
			t.Fatalf("a finished stream was kept alive: %q", w.Body.String())
		}
	})

	t.Run("a status after the stream has started", func(t *testing.T) {
		w := httptest.NewRecorder()
		st := newStream(w, map[jsonrpc.ID]bool{jsonrpc.Int64ID(1): true}, true, false)
		_ = st.write([]byte(`{"first":1}`), jsonrpc.ID{}, 0)
		_ = st.write([]byte(`{"error":1}`), jsonrpc.Int64ID(1), http.StatusBadRequest)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `data: {"error":1}`) {
			t.Fatalf("answered %d %q", w.Code, w.Body.String())
		}
	})
}
