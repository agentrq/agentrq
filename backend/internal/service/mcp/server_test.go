// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
	"github.com/google/jsonschema-go/jsonschema"
)

const waitFor = 5 * time.Second

// fakeOut is an outbound that records what a session sends.
type fakeOut struct {
	mu     sync.Mutex
	sent   []jsonrpc.Message
	err    error
	calls  bool
	closed int
}

func (o *fakeOut) send(_ context.Context, msg jsonrpc.Message) error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.sent = append(o.sent, msg)
	return o.err
}

func (o *fakeOut) canCall() bool { return o.calls }

func (o *fakeOut) close() {
	o.mu.Lock()
	o.closed++
	o.mu.Unlock()
}

func (o *fakeOut) messages() []jsonrpc.Message {
	o.mu.Lock()
	defer o.mu.Unlock()
	return slices.Clone(o.sent)
}

// testSession is a session of s over a fakeOut, speaking versions.
func testSession(s *Server, versions []string) (*ServerSession, *fakeOut) {
	out := &fakeOut{calls: true}
	ss := newServerSession(s, "sess-1", out, versions)
	s.addSession(ss)
	return ss, out
}

func newTestServer(opts *ServerOptions) *Server {
	return NewServer(&Implementation{Name: "test-server", Version: "1"}, opts)
}

// peer is a raw client of a server over the in-memory transport: it sends
// what a test writes and sorts what comes back.
type peer struct {
	t     *testing.T
	conn  Connection
	ss    *ServerSession
	resps chan *jsonrpc.Response
	notes chan *jsonrpc.Request
	reqs  chan *jsonrpc.Request
	early map[jsonrpc.ID]*jsonrpc.Response
}

func connectPeer(t *testing.T, s *Server) *peer {
	t.Helper()
	st, ct := NewInMemoryTransports()
	ss, err := s.Connect(context.Background(), st, nil)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := ct.Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p := &peer{t: t, conn: conn, ss: ss,
		resps: make(chan *jsonrpc.Response, 64), notes: make(chan *jsonrpc.Request, 64), reqs: make(chan *jsonrpc.Request, 64),
		early: map[jsonrpc.ID]*jsonrpc.Response{}}
	go func() {
		for {
			msg, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			switch m := msg.(type) {
			case *jsonrpc.Response:
				p.resps <- m
			case *jsonrpc.Request:
				if m.IsCall() {
					p.reqs <- m
				} else {
					p.notes <- m
				}
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return p
}

func (p *peer) write(msg jsonrpc.Message) {
	p.t.Helper()
	if err := p.conn.Write(context.Background(), msg); err != nil {
		p.t.Fatal(err)
	}
}

// send writes a call without waiting for its answer.
func (p *peer) send(id int64, method string, params any) jsonrpc.ID {
	p.t.Helper()
	req, err := jsonrpc.NewCall(jsonrpc.Int64ID(id), method, params)
	if err != nil {
		p.t.Fatal(err)
	}
	p.write(req)
	return req.ID
}

// call writes a call and waits for its answer.
func (p *peer) call(id int64, method string, params any) *jsonrpc.Response {
	p.t.Helper()
	return p.response(p.send(id, method, params))
}

func (p *peer) response(id jsonrpc.ID) *jsonrpc.Response {
	p.t.Helper()
	if r, ok := p.early[id]; ok {
		delete(p.early, id)
		return r
	}
	timeout := time.After(waitFor)
	for {
		select {
		case r := <-p.resps:
			if r.ID == id {
				return r
			}
			p.early[r.ID] = r
		case <-timeout:
			p.t.Fatalf("no response to %v", id.Raw())
			return nil
		}
	}
}

func (p *peer) notify(method string, params any) {
	p.t.Helper()
	n, err := jsonrpc.NewNotification(method, params)
	if err != nil {
		p.t.Fatal(err)
	}
	p.write(n)
}

func (p *peer) note() *jsonrpc.Request {
	p.t.Helper()
	select {
	case n := <-p.notes:
		return n
	case <-time.After(waitFor):
		p.t.Fatal("no notification arrived")
		return nil
	}
}

func (p *peer) handshake(version string) *InitializeResult {
	p.t.Helper()
	resp := p.call(1000, methodInitialize, map[string]any{"protocolVersion": version, "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "peer", "version": "9"}})
	if resp.Error != nil {
		p.t.Fatalf("initialize: %v", resp.Error)
	}
	var res InitializeResult
	mustDecode(p.t, resp, &res)
	p.notify(notificationInitialized, nil)
	return &res
}

func mustDecode(t *testing.T, resp *jsonrpc.Response, v any) {
	t.Helper()
	if resp.Error != nil {
		t.Fatalf("expected a result, got error %v", resp.Error)
	}
	if err := json.Unmarshal(resp.Result, v); err != nil {
		t.Fatalf("decode %s: %v", resp.Result, err)
	}
}

func errorOf(t *testing.T, resp *jsonrpc.Response) *jsonrpc.Error {
	t.Helper()
	if resp.Error == nil {
		t.Fatalf("expected an error, got result %s", resp.Result)
	}
	return jsonrpc.ToWire(resp.Error)
}

// modern is params carrying the 2026-07-28 identity in _meta.
func modern(params map[string]any) map[string]any {
	if params == nil {
		params = map[string]any{}
	}
	params["_meta"] = map[string]any{
		MetaKeyProtocolVersion:    ProtocolVersion20260728,
		MetaKeyClientInfo:         map[string]any{"name": "modern", "version": "2"},
		MetaKeyClientCapabilities: map[string]any{},
	}
	return params
}

func noTool(context.Context, *CallToolRequest) (*CallToolResult, error) {
	return &CallToolResult{Content: []Content{&TextContent{Text: "ok"}}}, nil
}

var objectSchema = &jsonschema.Schema{Type: "object"}

// NewServer refuses what cannot work, and narrows its revisions on request.
func TestNewServer(t *testing.T) {
	mustPanic(t, "nil Implementation", func() { NewServer(nil, nil) })
	mustPanic(t, "go together", func() {
		newTestServer(&ServerOptions{SubscribeHandler: func(context.Context, *SubscribeRequest) error { return nil }})
	})
	mustPanic(t, "unsupported protocol version", func() {
		newTestServer(&ServerOptions{SupportedProtocolVersions: []string{"2024-11-05"}})
	})
	s := newTestServer(&ServerOptions{SupportedProtocolVersions: []string{ProtocolVersion20250618}})
	if !slices.Equal(s.versions, []string{ProtocolVersion20250618}) {
		t.Errorf("versions = %v", s.versions)
	}
	p := connectPeer(t, s)
	if res := p.handshake(ProtocolVersion20251125); res.ProtocolVersion != ProtocolVersion20250618 {
		t.Errorf("a narrowed server settled on %q", res.ProtocolVersion)
	}
}

// The handshake answers with the negotiated revision, what the server can do
// and who it is, and happens once.
func TestHandshake(t *testing.T) {
	s := newTestServer(&ServerOptions{Instructions: "be nice"})
	s.AddTool(&Tool{Name: "t", InputSchema: objectSchema}, noTool)
	p := connectPeer(t, s)

	res := p.handshake(ProtocolVersion20250618)
	if res.ProtocolVersion != ProtocolVersion20250618 || res.Instructions != "be nice" || res.ServerInfo.Name != "test-server" || res.Capabilities.Tools == nil {
		t.Errorf("handshake answered %+v", res)
	}
	if got := p.ss.InitializeParams(); got == nil || got.ClientInfo.Name != "peer" {
		t.Errorf("the session kept %+v", got)
	}
	again := p.call(2, methodInitialize, map[string]any{"protocolVersion": ProtocolVersion20250618})
	if e := errorOf(t, again); e.Code != 0 || e.Message != `duplicate "initialize" received` {
		t.Errorf("second handshake = %+v", e)
	}
	// A second initialized is refused quietly: notifications have no answer.
	p.notify(notificationInitialized, nil)
	if r := p.call(3, methodPing, nil); r.Error != nil {
		t.Errorf("ping after a repeated initialized = %v", r.Error)
	}
	ss := newServerSession(s, "", &fakeOut{}, nil)
	if _, err := ss.initialized(context.Background(), nil); err == nil {
		t.Error("initialized before initialize was accepted")
	}
	if _, err := ss.initialize(context.Background(), nil); !errors.Is(err, jsonrpc.ErrInvalidParams) {
		t.Errorf("initialize without params = %v", err)
	}
}

// A handshake that cannot be read fails as its own error, code 0.
func TestHandshakeParamsErrors(t *testing.T) {
	s := newTestServer(nil)
	ss, _ := testSession(s, s.versions)
	for params, want := range map[string]string{
		"null": `missing required "params"`,
		"[1]":  "unmarshaling",
	} {
		_, err := ss.handle(context.Background(), &jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: methodInitialize, Params: json.RawMessage(params)})
		if err == nil || jsonrpc.ToWire(err).Code != 0 || !strings.Contains(err.Error(), want) {
			t.Errorf("initialize params %s = %v, want a code-0 error about %q", params, err, want)
		}
	}
}

// Before its handshake a session answers ping and nothing else.
func TestCallsBeforeHandshakeAreRefused(t *testing.T) {
	s := newTestServer(nil)
	p := connectPeer(t, s)
	if e := errorOf(t, p.call(1, methodListTools, nil)); e.Message != `method "tools/list" is invalid during session initialization` {
		t.Errorf("tools/list before the handshake = %+v", e)
	}
	if r := p.call(2, methodPing, nil); r.Error != nil || string(r.Result) != "{}" {
		t.Errorf("ping before the handshake = %s, %v", r.Result, r.Error)
	}
}

// A 2026-07-28 request needs no handshake: it names its client in _meta,
// and its result says it is complete and names the server.
func TestModernRequests(t *testing.T) {
	s := newTestServer(nil)
	s.AddTool(&Tool{Name: "t", InputSchema: objectSchema}, noTool)
	p := connectPeer(t, s)

	var list ListToolsResult
	mustDecode(t, p.call(1, methodListTools, modern(nil)), &list)
	if list.ResultType != resultTypeComplete || list.Meta[MetaKeyServerInfo] == nil || len(list.Tools) != 1 {
		t.Errorf("modern tools/list = %+v", list)
	}
	if got := p.ss.InitializeParams(); got == nil || got.ClientInfo.Name != "modern" || got.ProtocolVersion != ProtocolVersion20260728 {
		t.Errorf("the session learned %+v", got)
	}
	var call CallToolResult
	mustDecode(t, p.call(2, methodCallTool, modern(map[string]any{"name": "t"})), &call)
	if call.resultType != resultTypeComplete {
		t.Error("a modern tool result is not marked complete")
	}
	for _, method := range []string{methodInitialize, methodPing, methodSetLevel, methodSubscribe, methodUnsubscribe} {
		e := errorOf(t, p.call(3, method, modern(map[string]any{"level": "info", "uri": "x://a"})))
		if e.Code != jsonrpc.CodeMethodNotFound || e.Message != `method not found: "`+method+`"` {
			t.Errorf("%s on the new protocol = %+v", method, e)
		}
	}
}

// A 2026-07-28 request must name its capabilities, may name its client, and
// must speak a revision the server does.
func TestModernRequestMeta(t *testing.T) {
	s := newTestServer(nil)
	ss, _ := testSession(s, s.versions)
	req := func(meta map[string]any) error {
		raw, _ := json.Marshal(map[string]any{"_meta": meta})
		_, err := ss.handle(context.Background(), &jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: methodListTools, Params: raw})
		return err
	}
	caps := map[string]any{}
	for name, c := range map[string]struct {
		meta map[string]any
		code int64
		msg  string
	}{
		"no caps":      {map[string]any{MetaKeyProtocolVersion: "2026-07-28"}, jsonrpc.CodeInvalidParams, "missing or invalid _meta field"},
		"null caps":    {map[string]any{MetaKeyProtocolVersion: "2026-07-28", MetaKeyClientCapabilities: nil}, jsonrpc.CodeInvalidParams, "missing or invalid"},
		"caps not obj": {map[string]any{MetaKeyProtocolVersion: "2026-07-28", MetaKeyClientCapabilities: 5}, jsonrpc.CodeInvalidParams, "missing or invalid"},
		"bad client":   {map[string]any{MetaKeyProtocolVersion: "2026-07-28", MetaKeyClientCapabilities: caps, MetaKeyClientInfo: 5}, jsonrpc.CodeInvalidParams, "invalid _meta field"},
		"null client":  {map[string]any{MetaKeyProtocolVersion: "2026-07-28", MetaKeyClientCapabilities: caps, MetaKeyClientInfo: nil}, jsonrpc.CodeInvalidParams, "invalid _meta field"},
		"future":       {map[string]any{MetaKeyProtocolVersion: "2027-01-01", MetaKeyClientCapabilities: caps}, CodeUnsupportedProtocolVersion, "unsupported protocol version"},
	} {
		err := req(c.meta)
		if e := jsonrpc.ToWire(err); err == nil || e.Code != c.code || !strings.Contains(e.Message, c.msg) {
			t.Errorf("%s: %v, want code %d about %q", name, err, c.code, c.msg)
		}
	}
	err := req(map[string]any{MetaKeyProtocolVersion: "2027-01-01", MetaKeyClientCapabilities: caps})
	var data UnsupportedProtocolVersionData
	if json.Unmarshal(jsonrpc.ToWire(err).Data, &data) != nil || data.Requested != "2027-01-01" || !slices.Equal(data.Supported, s.versions) {
		t.Errorf("unsupported revision data = %+v", data)
	}
	// An older revision in _meta is no new-protocol request at all.
	if err := req(map[string]any{MetaKeyProtocolVersion: "2025-06-18"}); err == nil || !strings.Contains(err.Error(), "invalid during session initialization") {
		t.Errorf("an old revision in _meta = %v", err)
	}
	if paramsMeta(json.RawMessage(`[1]`)) != nil {
		t.Error("params that are not an object have _meta")
	}
	if m, err := readRequestMeta(&jsonrpc.Request{Method: "notifications/x", Params: json.RawMessage(`{"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28"}}`)}); err != nil || m.modern {
		t.Error("a notification was read as a new-protocol request")
	}
	if remarshal(make(chan int), new(any)) {
		t.Error("a channel was remarshaled")
	}
}

// server/discover is only for the new protocol, and on a transport that
// serves it, it makes the client known; on one that does not, it keeps
// nothing.
func TestDiscover(t *testing.T) {
	s := newTestServer(&ServerOptions{Instructions: "hi"})
	s.AddTool(&Tool{Name: "t", InputSchema: objectSchema}, noTool)
	p := connectPeer(t, s)
	if e := errorOf(t, p.call(1, methodDiscover, nil)); e.Message != `method not found: "server/discover"` {
		t.Errorf("discover without the new protocol = %+v", e)
	}
	var res DiscoverResult
	mustDecode(t, p.call(2, methodDiscover, modern(nil)), &res)
	if !slices.Equal(res.SupportedVersions, s.versions) || res.Instructions != "hi" || res.CacheScope != "public" || res.ResultType != resultTypeComplete {
		t.Errorf("discover = %+v", res)
	}
	if got := p.ss.InitializeParams(); got == nil || got.ClientInfo == nil || got.ClientInfo.Name != "modern" {
		t.Errorf("discover left the session knowing %+v", got)
	}

	stateful, _ := testSession(s, transportVersions(s.versions, false))
	if _, err := stateful.handle(context.Background(), &jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: methodDiscover, Params: mustJSON(modern(nil))}); err != nil {
		t.Fatal(err)
	}
	if stateful.InitializeParams() != nil {
		t.Error("a stateful transport's discover kept the client")
	}
	// No params at all leave nothing to learn but still mark the session.
	direct, _ := testSession(s, s.versions)
	if _, err := s.discover(context.Background(), &ServerRequest[*DiscoverParams]{Session: direct}); err != nil || direct.InitializeParams() == nil {
		t.Errorf("discover without params = %v, %+v", err, direct.InitializeParams())
	}
}

func mustJSON(v any) json.RawMessage {
	data, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return data
}

// Tools are listed in name order; a cursor is refused, since none is ever
// handed out; an unknown tool is an invalid-params error.
func TestToolsListAndCall(t *testing.T) {
	s := newTestServer(nil)
	for _, name := range []string{"b", "a", "c"} {
		s.AddTool(&Tool{Name: name, InputSchema: objectSchema}, noTool)
	}
	s.AddTool(&Tool{Name: "empty", InputSchema: objectSchema}, func(context.Context, *CallToolRequest) (*CallToolResult, error) {
		return &CallToolResult{}, nil
	})
	s.AddTool(&Tool{Name: "broken", InputSchema: objectSchema}, func(context.Context, *CallToolRequest) (*CallToolResult, error) {
		return nil, errors.New("broken")
	})
	s.AddTool(&Tool{Name: "nil", InputSchema: objectSchema}, func(context.Context, *CallToolRequest) (*CallToolResult, error) {
		return nil, nil
	})
	if names := toolNames(s.Tools()); !slices.Equal(names, []string{"a", "b", "broken", "c", "empty", "nil"}) {
		t.Errorf("Tools() = %v", names)
	}
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)

	var list ListToolsResult
	mustDecode(t, p.call(1, methodListTools, nil), &list)
	if names := toolNames(list.Tools); !slices.Equal(names, []string{"a", "b", "broken", "c", "empty", "nil"}) || list.CacheScope != "public" || list.ResultType != "" {
		t.Errorf("tools/list = %v %+v", names, list.Cacheable)
	}
	if e := errorOf(t, p.call(2, methodListTools, map[string]any{"cursor": "x"})); e.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("a cursor = %+v", e)
	}
	if e := errorOf(t, p.call(3, methodCallTool, map[string]any{"name": "nope"})); e.Code != jsonrpc.CodeInvalidParams || e.Message != `unknown tool "nope"` {
		t.Errorf("unknown tool = %+v", e)
	}
	r := p.call(4, methodCallTool, map[string]any{"name": "empty"})
	if string(r.Result) != `{"content":[]}` {
		t.Errorf("a result with no content = %s, want content []", r.Result)
	}
	if e := errorOf(t, p.call(5, methodCallTool, map[string]any{"name": "broken"})); e.Message != "broken" {
		t.Errorf("a failing raw tool = %+v", e)
	}
	if r := p.call(6, methodCallTool, map[string]any{"name": "nil"}); r.Error != nil || string(r.Result) != "null" {
		t.Errorf("a raw tool answering nothing = %s, %v", r.Result, r.Error)
	}
	if e := errorOf(t, p.call(7, methodCallTool, map[string]any{"name": 5})); e.Code != jsonrpc.CodeInvalidParams || !strings.Contains(e.Message, "handling 'tools/call': invalid params") {
		t.Errorf("a malformed call = %+v", e)
	}
	if e := errorOf(t, p.call(8, methodCallTool, nil)); e.Code != jsonrpc.CodeInvalidRequest {
		t.Errorf("a call without params = %+v", e)
	}
	if e := errorOf(t, p.call(9, methodCallTool, json.RawMessage("null"))); e.Code != jsonrpc.CodeInvalidRequest {
		t.Errorf("a call with null params = %+v", e)
	}
}

func toolNames(tools []*Tool) []string {
	var names []string
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// Resources and templates are listed in order; a read fills in what the
// handler left out, and an unlisted URI is not found.
func TestResources(t *testing.T) {
	s := newTestServer(nil)
	read := func(text string) ResourceHandler {
		return func(_ context.Context, req *ReadResourceRequest) (*ReadResourceResult, error) {
			return &ReadResourceResult{Contents: []*ResourceContents{{Text: text + " " + req.Params.URI}}}, nil
		}
	}
	s.AddResource(&Resource{URI: "x://b", Name: "b", MIMEType: "text/plain"}, read("b"))
	s.AddResource(&Resource{URI: "x://a", Name: "a"}, read("a"))
	s.AddResource(&Resource{URI: "x://err", Name: "err"}, func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error) {
		return nil, errors.New("unreadable")
	})
	s.AddResource(&Resource{URI: "x://nil", Name: "nil"}, func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error) {
		return nil, nil
	})
	s.AddResource(&Resource{URI: "x://none", Name: "none"}, func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error) {
		return &ReadResourceResult{}, nil
	})
	s.AddResource(&Resource{URI: "x://own", Name: "own"}, func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error) {
		return &ReadResourceResult{Contents: []*ResourceContents{{URI: "x://other", MIMEType: "a/b"}}, Cacheable: Cacheable{TTLMs: 5, CacheScope: "private"}}, nil
	})
	s.AddResourceTemplate(&ResourceTemplate{URITemplate: "x://items/{id}", Name: "item", MIMEType: "application/json"}, read("item"))
	s.AddResourceTemplate(&ResourceTemplate{URITemplate: "x://items/{id}/{sub}", Name: "sub"}, read("sub"))
	mustPanic(t, "invalid URL escape", func() { s.AddResource(&Resource{URI: "%zz"}, read("")) })
	mustPanic(t, "is invalid", func() { s.AddResourceTemplate(&ResourceTemplate{URITemplate: "x://{"}, read("")) })

	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	var list ListResourcesResult
	mustDecode(t, p.call(1, methodListResources, nil), &list)
	var uris []string
	for _, r := range list.Resources {
		uris = append(uris, r.URI)
	}
	if !slices.Equal(uris, []string{"x://a", "x://b", "x://err", "x://nil", "x://none", "x://own"}) {
		t.Errorf("resources/list = %v", uris)
	}
	var tl ListResourceTemplatesResult
	mustDecode(t, p.call(2, methodListResourceTemplates, nil), &tl)
	if len(tl.ResourceTemplates) != 2 || tl.ResourceTemplates[0].URITemplate != "x://items/{id}" {
		t.Errorf("resources/templates/list = %+v", tl.ResourceTemplates)
	}
	if e := errorOf(t, p.call(3, methodListResources, map[string]any{"cursor": "c"})); e.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("resources/list with a cursor = %+v", e)
	}
	if e := errorOf(t, p.call(4, methodListResourceTemplates, map[string]any{"cursor": "c"})); e.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("templates/list with a cursor = %+v", e)
	}

	var rr ReadResourceResult
	mustDecode(t, p.call(5, methodReadResource, map[string]any{"uri": "x://b"}), &rr)
	if c := rr.Contents[0]; c.URI != "x://b" || c.MIMEType != "text/plain" || c.Text != "b x://b" || rr.CacheScope != "public" {
		t.Errorf("read x://b = %+v %+v", c, rr.Cacheable)
	}
	mustDecode(t, p.call(6, methodReadResource, map[string]any{"uri": "x://items/7"}), &rr)
	if c := rr.Contents[0]; c.Text != "item x://items/7" || c.MIMEType != "application/json" {
		t.Errorf("read through a template = %+v", c)
	}
	mustDecode(t, p.call(12, methodReadResource, map[string]any{"uri": "x://items/7/8"}), &rr)
	if c := rr.Contents[0]; c.Text != "sub x://items/7/8" || c.MIMEType != "" {
		t.Errorf("read through the second template = %+v", c)
	}
	mustDecode(t, p.call(7, methodReadResource, map[string]any{"uri": "x://own"}), &rr)
	if c := rr.Contents[0]; c.URI != "x://other" || c.MIMEType != "a/b" || rr.TTLMs != 5 || rr.CacheScope != "private" {
		t.Errorf("a read that set its own fields = %+v %+v", c, rr.Cacheable)
	}
	e := errorOf(t, p.call(8, methodReadResource, map[string]any{"uri": "x://nope"}))
	if e.Code != jsonrpc.CodeInvalidParams || e.Message != "Resource not found" || string(e.Data) != `{"uri":"x://nope"}` {
		t.Errorf("an unknown resource = %+v %s", e, e.Data)
	}
	if e := errorOf(t, p.call(9, methodReadResource, map[string]any{"uri": "x://err"})); e.Message != "unreadable" {
		t.Errorf("a failing read = %+v", e)
	}
	for i, uri := range []string{"x://nil", "x://none"} {
		if e := errorOf(t, p.call(int64(10+i), methodReadResource, map[string]any{"uri": uri})); !strings.Contains(e.Message, "read handler returned nil information") {
			t.Errorf("read %s = %+v", uri, e)
		}
	}
}

// Prompts are listed in name order, and an unknown one is invalid params.
func TestPrompts(t *testing.T) {
	s := newTestServer(nil)
	s.AddPrompt(&Prompt{Name: "z"}, func(_ context.Context, req *GetPromptRequest) (*GetPromptResult, error) {
		return &GetPromptResult{Messages: []*PromptMessage{{Role: "user", Content: &TextContent{Text: req.Params.Arguments["x"]}}}}, nil
	})
	s.AddPrompt(&Prompt{Name: "a"}, func(context.Context, *GetPromptRequest) (*GetPromptResult, error) {
		return nil, errors.New("no prompt today")
	})
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	var list ListPromptsResult
	mustDecode(t, p.call(1, methodListPrompts, nil), &list)
	if len(list.Prompts) != 2 || list.Prompts[0].Name != "a" {
		t.Errorf("prompts/list = %+v", list.Prompts)
	}
	if e := errorOf(t, p.call(2, methodListPrompts, map[string]any{"cursor": "c"})); e.Code != jsonrpc.CodeInvalidParams {
		t.Errorf("a cursor = %+v", e)
	}
	var got GetPromptResult
	mustDecode(t, p.call(3, methodGetPrompt, map[string]any{"name": "z", "arguments": map[string]string{"x": "hello"}}), &got)
	if got.Messages[0].Content.(*TextContent).Text != "hello" || got.resultType != "" {
		t.Errorf("prompts/get = %+v", got)
	}
	if e := errorOf(t, p.call(4, methodGetPrompt, map[string]any{"name": "nope"})); e.Message != `unknown prompt "nope"` {
		t.Errorf("unknown prompt = %+v", e)
	}
	if e := errorOf(t, p.call(5, methodGetPrompt, map[string]any{"name": "a"})); e.Message != "no prompt today" {
		t.Errorf("failing prompt = %+v", e)
	}
	mustDecode(t, p.call(6, methodGetPrompt, modern(map[string]any{"name": "z"})), &got)
	if got.resultType != resultTypeComplete {
		t.Error("a modern prompt result is not marked complete")
	}
}

// completion/complete checks its params, and is otherwise not offered.
func TestComplete(t *testing.T) {
	s := newTestServer(nil)
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	for i, c := range []struct {
		params map[string]any
		want   string
	}{
		{map[string]any{"argument": map[string]any{"name": "a"}}, "missing required 'ref' field"},
		{map[string]any{"ref": nil, "argument": map[string]any{"name": "a"}}, "missing required 'ref' field"},
		{map[string]any{"ref": map[string]any{"type": "ref/prompt"}, "argument": map[string]any{}}, "missing required 'argument.name' field"},
		{map[string]any{"ref": map[string]any{"type": "ref/prompt"}, "argument": map[string]any{"name": "a"}}, `method not found: "completion/complete"`},
	} {
		if e := errorOf(t, p.call(int64(i+1), methodComplete, c.params)); !strings.Contains(e.Message, c.want) {
			t.Errorf("complete %v = %+v, want %q", c.params, e, c.want)
		}
	}
}

// logging/setLevel is recorded; the server sends no logs.
func TestSetLevel(t *testing.T) {
	s := newTestServer(nil)
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	if r := p.call(1, methodSetLevel, map[string]any{"level": "debug"}); r.Error != nil || string(r.Result) != "{}" {
		t.Fatalf("setLevel = %s, %v", r.Result, r.Error)
	}
	p.ss.mu.Lock()
	level := p.ss.state.logLevel
	p.ss.mu.Unlock()
	if level != "debug" {
		t.Errorf("level = %q", level)
	}
}

// A message that names no method, or uses one the wrong way, is refused
// before anything runs; a method nobody serves is method not found.
func TestCheckRequest(t *testing.T) {
	s := newTestServer(nil)
	ss, _ := testSession(s, s.versions)
	ss.setInitialize(&InitializeParams{ProtocolVersion: ProtocolVersion20251125})
	for _, c := range []struct {
		req  *jsonrpc.Request
		want string
	}{
		{&jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: "foo/bar"}, `JSON RPC not handled: "foo/bar" unsupported`},
		{&jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: notificationInitialized}, `unexpected id for "notifications/initialized"`},
		{&jsonrpc.Request{Method: methodListTools}, `missing id for "tools/list"`},
		{&jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: methodCallTool}, `missing required "params"`},
	} {
		if _, err := ss.handle(context.Background(), c.req); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s = %v, want %q", c.req.Method, err, c.want)
		}
	}
	resp := ss.handleCall(context.Background(), &jsonrpc.Request{ID: jsonrpc.Int64ID(9), Method: "foo/bar"})
	if e := jsonrpc.ToWire(resp.Error); e.Code != jsonrpc.CodeMethodNotFound || e.Message != `method not found: "foo/bar"` {
		t.Errorf("an unserved method answered %+v", e)
	}
	if _, err := s.defaultHandler(context.Background(), "nope", nil); !errors.Is(err, jsonrpc.ErrNotHandled) {
		t.Errorf("the default handler served an unknown method: %v", err)
	}
}

// Middleware runs in the order it was added, around the method.
func TestMiddlewareOrder(t *testing.T) {
	s := newTestServer(nil)
	var order []string
	mw := func(name string) Middleware {
		return func(next MethodHandler) MethodHandler {
			return func(ctx context.Context, method string, req Request) (Result, error) {
				order = append(order, name)
				return next(ctx, method, req)
			}
		}
	}
	s.AddReceivingMiddleware(mw("a"), mw("b"))
	s.AddReceivingMiddleware(mw("outer"))
	ss, _ := testSession(s, s.versions)
	if _, err := ss.handle(context.Background(), &jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: methodPing}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(order, []string{"outer", "a", "b"}) {
		t.Errorf("middleware ran %v", order)
	}
}

type echoParams struct {
	ParamsBase
	Say string `json:"say"`
}

type echoResult struct {
	ResultBase
	Said string `json:"said"`
	Bad  any    `json:"bad,omitempty"`
}

// A custom method is served like a standard one, but cannot shadow one.
func TestCustomMethods(t *testing.T) {
	s := newTestServer(nil)
	if err := AddReceivingCustomMethod(s, methodListTools, func(context.Context, *ServerSession, *echoParams) (*echoResult, error) { return nil, nil }); err == nil {
		t.Error("a custom method shadowed tools/list")
	}
	if err := AddReceivingCustomMethod(s, "x/echo", func(_ context.Context, ss *ServerSession, p *echoParams) (*echoResult, error) {
		if p == nil {
			return &echoResult{Said: "nothing"}, nil
		}
		if p.Say == "chan" {
			return &echoResult{Bad: make(chan int)}, nil
		}
		return &echoResult{Said: p.Say}, nil
	}); err != nil {
		t.Fatal(err)
	}
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	var r echoResult
	mustDecode(t, p.call(1, "x/echo", map[string]any{"say": "hi"}), &r)
	if r.Said != "hi" {
		t.Errorf("echo = %+v", r)
	}
	mustDecode(t, p.call(2, "x/echo", nil), &r)
	if r.Said != "nothing" {
		t.Errorf("echo without params = %+v", r)
	}
	if e := errorOf(t, p.call(3, "x/echo", map[string]any{"say": "chan"})); e.Code != jsonrpc.CodeInternalError || !strings.Contains(e.Message, "malformed result") {
		t.Errorf("a result JSON cannot hold = %+v", e)
	}
	var modernRes echoResult
	mustDecode(t, p.call(4, "x/echo", modern(map[string]any{"say": "m"})), &modernRes)
	if modernRes.Meta[MetaKeyServerInfo] == nil {
		t.Error("a modern custom result does not name the server")
	}
}

// A middleware that answers a call with nothing is the server's error.
func TestNoResultIsAnInternalError(t *testing.T) {
	s := newTestServer(nil)
	s.AddReceivingMiddleware(func(MethodHandler) MethodHandler {
		return func(context.Context, string, Request) (Result, error) { return nil, nil }
	})
	ss, _ := testSession(s, s.versions)
	resp := ss.handleCall(context.Background(), &jsonrpc.Request{ID: jsonrpc.Int64ID(1), Method: methodPing})
	if e := jsonrpc.ToWire(resp.Error); e.Code != jsonrpc.CodeInternalError || !strings.Contains(e.Message, "returned no result") {
		t.Errorf("no result = %+v", e)
	}
}

// serverInfo goes into a new-protocol result's _meta, unless it is already
// there, there is nowhere to put it, or no server to name.
func TestAnnotateServerInfo(t *testing.T) {
	impl := &Implementation{Name: "x"}
	r := &ListToolsResult{}
	annotateServerInfo(r, impl)
	if r.Meta[MetaKeyServerInfo] != impl {
		t.Errorf("meta = %v", r.Meta)
	}
	other := &Implementation{Name: "other"}
	annotateServerInfo(r, other)
	if r.Meta[MetaKeyServerInfo] != impl {
		t.Error("an existing serverInfo was replaced")
	}
	e := &emptyResult{}
	annotateServerInfo(e, impl)
	plain := &ListToolsResult{}
	annotateServerInfo(plain, nil)
	if plain.Meta != nil {
		t.Error("no server was named, yet meta was set")
	}
}

// A call can be cancelled by its id, and an id still in flight cannot be
// reused.
func TestCancelAndDuplicateIDs(t *testing.T) {
	s := newTestServer(nil)
	started := make(chan struct{}, 1)
	if err := AddReceivingCustomMethod(s, "x/wait", func(ctx context.Context, _ *ServerSession, _ *ParamsBase) (*ResultBase, error) {
		started <- struct{}{}
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}); err != nil {
		t.Fatal(err)
	}
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	id := p.send(5, "x/wait", nil)
	<-started
	dup := p.call(5, methodPing, nil)
	if e := errorOf(t, dup); e.Code != jsonrpc.CodeInvalidRequest || !strings.Contains(e.Message, "request ID 5 already in use") {
		t.Errorf("a duplicate id = %+v", e)
	}
	p.notify(notificationCancelled, map[string]any{"requestId": 5, "reason": "user"})
	if e := errorOf(t, p.response(id)); e.Message != errCancelledByClient.Error() {
		t.Errorf("cancelled call = %+v", e)
	}
	// Cancellations naming no call, or nothing at all, do nothing.
	p.ss.cancelRequest(map[string]any{})
	p.ss.cancelRequest(nil)
	p.ss.cancelRequest(float64(404))
	p.notify(notificationCancelled, json.RawMessage(`"not an object"`))
	p.notify(notificationProgress, map[string]any{"progressToken": "t", "progress": 1})
	p.notify(notificationRootsListChanged, nil)
	if r := p.call(6, methodPing, nil); r.Error != nil {
		t.Errorf("ping after stray notifications = %v", r.Error)
	}
}

// A session sends notifications and calls through its transport, and once
// closed, neither.
func TestSessionNotifyCallClose(t *testing.T) {
	s := newTestServer(nil)
	ss, out := testSession(s, s.versions)
	var closed int
	ss.onClose = func() { closed++ }

	if err := ss.Notify(context.Background(), "notifications/x", map[string]int{"a": 1}); err != nil {
		t.Fatal(err)
	}
	if err := ss.Notify(context.Background(), "x", make(chan int)); err == nil {
		t.Error("unmarshalable params were sent")
	}
	if _, err := ss.call(context.Background(), "x", make(chan int)); err == nil {
		t.Error("a call with unmarshalable params was sent")
	}
	out.err = errors.New("no stream")
	if err := ss.Ping(context.Background(), nil); err == nil || err.Error() != "no stream" {
		t.Errorf("ping on a broken transport = %v", err)
	}
	out.err = nil
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := ss.Ping(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("ping on a cancelled context = %v", err)
	}
	out.calls = false
	if err := ss.Ping(context.Background(), nil); !errors.Is(err, errRejected) {
		t.Errorf("ping where no call can be answered = %v", err)
	}
	out.calls = true

	if got := len(slices.Collect(s.Sessions())); got != 1 {
		t.Fatalf("%d sessions, want 1", got)
	}
	if err := ss.Close(); err != nil || ss.Close() != nil {
		t.Fatal("close failed")
	}
	if closed != 1 || out.closed != 1 || len(slices.Collect(s.Sessions())) != 0 {
		t.Errorf("close ran onClose %d times, closed the transport %d times, left %d sessions", closed, out.closed, len(slices.Collect(s.Sessions())))
	}
	if err := ss.Notify(context.Background(), "x", nil); err == nil || err.Error() != "session is closed" {
		t.Errorf("notify after close = %v", err)
	}
	if err := ss.Ping(context.Background(), nil); err == nil || err.Error() != "session is closed" {
		t.Errorf("ping after close = %v", err)
	}
	if (*ServerSession)(nil).ID() != "" || ss.ID() != "sess-1" {
		t.Error("ID is wrong")
	}
	ss.setID("renamed")
	if ss.ID() != "renamed" {
		t.Error("setID did not rename")
	}
	if len(out.messages()) < 2 {
		t.Errorf("sent %d messages", len(out.messages()))
	}
}

// A response is handed to the call awaiting it; a stray one, or a second
// one, is dropped.
func TestDeliverResponse(t *testing.T) {
	s := newTestServer(nil)
	ss, _ := testSession(s, s.versions)
	ch := make(chan *jsonrpc.Response, 1)
	ss.calls[jsonrpc.Int64ID(1)] = ch
	ss.deliverResponse(&jsonrpc.Response{ID: jsonrpc.Int64ID(1), Result: json.RawMessage(`{"n":1}`)})
	ss.deliverResponse(&jsonrpc.Response{ID: jsonrpc.Int64ID(1), Result: json.RawMessage(`{"n":2}`)})
	ss.deliverResponse(&jsonrpc.Response{ID: jsonrpc.Int64ID(99)})
	if len(ch) != 1 || string((<-ch).Result) != `{"n":1}` {
		t.Error("the awaited response was not the first one delivered")
	}
}

// A client that answers ping is alive; one that answers with an error is not.
func TestPingOverConnection(t *testing.T) {
	s := newTestServer(nil)
	p := connectPeer(t, s)
	go func() {
		req := <-p.reqs
		resp, _ := jsonrpc.NewResponse(req.ID, struct{}{}, nil)
		p.write(resp)
		req = <-p.reqs
		p.write(&jsonrpc.Response{ID: req.ID, Error: &jsonrpc.Error{Code: -1, Message: "no"}})
	}()
	if err := p.ss.Ping(context.Background(), nil); err != nil {
		t.Errorf("answered ping = %v", err)
	}
	if err := p.ss.Ping(context.Background(), nil); err == nil || err.Error() != "no" {
		t.Errorf("refused ping = %v", err)
	}
}

// What a session's client speaks decides how it is told about changes and
// whether results say they are complete.
func TestSessionProtocolChecks(t *testing.T) {
	s := newTestServer(nil)
	ss, _ := testSession(s, s.versions)
	r := &CallToolResult{}
	ss.markResultType(r)
	if r.resultType != resultTypeComplete || ss.speaksModern() {
		t.Error("a session with no handshake: want results marked, and not modern")
	}
	ss.setInitialize(&InitializeParams{ProtocolVersion: ProtocolVersion20250618})
	r = &CallToolResult{}
	ss.markResultType(r)
	if r.resultType != "" || ss.speaksModern() {
		t.Error("an old client got a modern result")
	}
	ss.setInitialize(&InitializeParams{ProtocolVersion: ProtocolVersion20260728})
	if !ss.speaksModern() {
		t.Error("a 2026-07-28 client is not modern")
	}
	got := ss.supportedVersions()
	got[0] = "x"
	if ss.versions[0] == "x" {
		t.Error("supportedVersions handed out its own slice")
	}
}

// The capabilities are what was configured, plus what the server has.
func TestCapabilities(t *testing.T) {
	s := newTestServer(nil)
	if c := s.capabilities(); c.Logging == nil || c.Tools != nil || c.Prompts != nil || c.Resources != nil {
		t.Errorf("an empty server = %+v", c)
	}
	s.AddTool(&Tool{Name: "t", InputSchema: objectSchema}, noTool)
	s.AddPrompt(&Prompt{Name: "p"}, nil)
	s.AddResourceTemplate(&ResourceTemplate{URITemplate: "x://{id}", Name: "x"}, nil)
	if c := s.capabilities(); !c.Tools.ListChanged || !c.Prompts.ListChanged || !c.Resources.ListChanged || c.Resources.Subscribe {
		t.Errorf("a full server = %+v", c)
	}

	given := &ServerCapabilities{Tools: &ToolCapabilities{}, Experimental: map[string]any{"x": 1}}
	given.AddExtension("io.ext/a", nil)
	given.AddExtension("io.ext/b", map[string]any{"on": true})
	sub := func(context.Context, *SubscribeRequest) error { return nil }
	unsub := func(context.Context, *UnsubscribeRequest) error { return nil }
	s2 := newTestServer(&ServerOptions{Capabilities: given, SubscribeHandler: sub, UnsubscribeHandler: unsub})
	s2.AddTool(&Tool{Name: "t", InputSchema: objectSchema}, noTool)
	s2.AddResource(&Resource{URI: "x://a", Name: "a"}, nil)
	c := s2.capabilities()
	if c.Tools.ListChanged || c.Logging != nil || !c.Resources.Subscribe || c.Experimental["x"] != 1 || len(c.Extensions) != 2 {
		t.Errorf("configured capabilities = %+v", c)
	}
	c.Experimental["x"] = 2
	if given.Experimental["x"] != 1 {
		t.Error("the capabilities handed out were the configured ones")
	}
}

// A list that changes after a client connected is announced: on the shared
// channel to clients of earlier revisions, and on its listen to a modern
// client that asked.
func TestListChangedNotifications(t *testing.T) {
	s := newTestServer(nil)
	s.AddTool(&Tool{Name: "first", InputSchema: objectSchema}, noTool)
	s.AddPrompt(&Prompt{Name: "p"}, nil)
	s.AddResource(&Resource{URI: "x://a", Name: "a"}, nil)
	legacy := connectPeer(t, s)
	legacy.handshake(ProtocolVersion20251125)
	listener := connectPeer(t, s)
	listenID := listener.send(7, methodSubscriptionsListen, modern(map[string]any{"notifications": map[string]any{
		"toolsListChanged": true, "promptsListChanged": true, "resourcesListChanged": true,
	}}))
	ack := listener.note()
	if ack.Method != notificationSubscriptionsAck || !strings.Contains(string(ack.Params), `"toolsListChanged":true`) {
		t.Fatalf("ack = %s %s", ack.Method, ack.Params)
	}

	s.AddTool(&Tool{Name: "second", InputSchema: objectSchema}, noTool)
	if n := legacy.note(); n.Method != notificationToolListChanged || string(n.Params) != "{}" {
		t.Errorf("legacy client heard %s %s", n.Method, n.Params)
	}
	if n := listener.note(); n.Method != notificationToolListChanged || !strings.Contains(string(n.Params), `"io.modelcontextprotocol/subscriptionId":7`) {
		t.Errorf("listener heard %s %s", n.Method, n.Params)
	}
	s.AddPrompt(&Prompt{Name: "q"}, nil)
	if n := legacy.note(); n.Method != notificationPromptListChanged {
		t.Errorf("legacy client heard %s", n.Method)
	}
	if n := listener.note(); n.Method != notificationPromptListChanged {
		t.Errorf("listener heard %s", n.Method)
	}
	s.AddResource(&Resource{URI: "x://b", Name: "b"}, nil)
	if n := legacy.note(); n.Method != notificationResourceListChanged {
		t.Errorf("legacy client heard %s", n.Method)
	}
	if n := listener.note(); n.Method != notificationResourceListChanged {
		t.Errorf("listener heard %s", n.Method)
	}

	listener.notify(notificationCancelled, map[string]any{"requestId": 7})
	var done SubscriptionsListenResult
	mustDecode(t, listener.response(listenID), &done)
	if done.Meta[MetaKeySubscriptionID] != float64(7) || done.ResultType != resultTypeComplete || done.Meta[MetaKeyServerInfo] == nil {
		t.Errorf("listen ended with %+v", done)
	}
	s.mu.Lock()
	left := len(s.listenSubs[notificationToolListChanged])
	s.mu.Unlock()
	if left != 0 {
		t.Errorf("%d listens still follow tools", left)
	}
}

// A resource update reaches the sessions that subscribed: an older client's
// as it was sent, a listen's with the listen's id; and stops with them.
func TestResourceUpdated(t *testing.T) {
	var subscribed, unsubscribed []string
	s := newTestServer(&ServerOptions{
		SubscribeHandler: func(_ context.Context, req *SubscribeRequest) error {
			if req.Params.URI == "x://forbidden" {
				return errors.New("not yours")
			}
			subscribed = append(subscribed, req.Params.URI)
			return nil
		},
		UnsubscribeHandler: func(_ context.Context, req *UnsubscribeRequest) error {
			if req.Params.URI == "x://stuck" {
				return errors.New("cannot leave")
			}
			unsubscribed = append(unsubscribed, req.Params.URI)
			return nil
		},
	})
	s.AddResource(&Resource{URI: "x://a", Name: "a"}, nil)

	legacy := connectPeer(t, s)
	legacy.handshake(ProtocolVersion20251125)
	if r := legacy.call(1, methodSubscribe, map[string]any{"uri": "x://a"}); r.Error != nil {
		t.Fatalf("subscribe = %v", r.Error)
	}
	if e := errorOf(t, legacy.call(2, methodSubscribe, map[string]any{"uri": "x://forbidden"})); e.Message != "not yours" {
		t.Errorf("a refused subscribe = %+v", e)
	}
	listener := connectPeer(t, s)
	listenID := listener.send(9, methodSubscriptionsListen, modern(map[string]any{"notifications": map[string]any{"resourceSubscriptions": []string{"x://a"}}}))
	if ack := listener.note(); !strings.Contains(string(ack.Params), `"resourceSubscriptions":["x://a"]`) {
		t.Fatalf("ack = %s", ack.Params)
	}

	params := &ResourceUpdatedNotificationParams{URI: "x://a", Meta: Meta{"k": "v"}}
	if err := s.ResourceUpdated(context.Background(), params); err != nil {
		t.Fatal(err)
	}
	if n := legacy.note(); string(n.Params) != `{"_meta":{"k":"v"},"uri":"x://a"}` {
		t.Errorf("legacy client heard %s", n.Params)
	}
	if n := listener.note(); string(n.Params) != `{"_meta":{"io.modelcontextprotocol/subscriptionId":9,"k":"v"},"uri":"x://a"}` {
		t.Errorf("listener heard %s", n.Params)
	}
	if len(params.Meta) != 1 {
		t.Error("the caller's params were changed")
	}
	_ = s.ResourceUpdated(context.Background(), &ResourceUpdatedNotificationParams{URI: "x://a"})
	if n := listener.note(); string(n.Params) != `{"_meta":{"io.modelcontextprotocol/subscriptionId":9},"uri":"x://a"}` {
		t.Errorf("listener heard %s", n.Params)
	}
	_ = legacy.note()

	if r := legacy.call(3, methodUnsubscribe, map[string]any{"uri": "x://a"}); r.Error != nil {
		t.Fatalf("unsubscribe = %v", r.Error)
	}
	if r := legacy.call(4, methodUnsubscribe, map[string]any{"uri": "x://never"}); r.Error != nil {
		t.Fatalf("unsubscribe from nothing = %v", r.Error)
	}
	if e := errorOf(t, legacy.call(5, methodUnsubscribe, map[string]any{"uri": "x://stuck"})); e.Message != "cannot leave" {
		t.Errorf("a refused unsubscribe = %+v", e)
	}
	listener.notify(notificationCancelled, map[string]any{"requestId": 9})
	if r := listener.response(listenID); r.Error != nil {
		t.Fatalf("listen ended with %v", r.Error)
	}
	s.mu.Lock()
	left := len(s.resSubs)
	s.mu.Unlock()
	if left != 0 || !slices.Equal(subscribed, []string{"x://a", "x://a"}) || !slices.Contains(unsubscribed, "x://a") {
		t.Errorf("subscriptions left %d, subscribed %v, unsubscribed %v", left, subscribed, unsubscribed)
	}
	// A closed session's subscriptions go with it.
	again := connectPeer(t, s)
	again.handshake(ProtocolVersion20251125)
	again.call(1, methodSubscribe, map[string]any{"uri": "x://a"})
	_ = again.ss.Close()
	s.mu.Lock()
	left = len(s.resSubs["x://a"])
	s.mu.Unlock()
	if left != 0 {
		t.Errorf("a closed session still follows x://a")
	}
}

// Subscriptions need a server that takes them, and a call to belong to.
func TestSubscribeRefusals(t *testing.T) {
	s := newTestServer(nil)
	s.AddResource(&Resource{URI: "x://a", Name: "a"}, nil)
	p := connectPeer(t, s)
	p.handshake(ProtocolVersion20251125)
	if e := errorOf(t, p.call(1, methodSubscribe, map[string]any{"uri": "x://a"})); e.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("subscribe without a handler = %+v", e)
	}
	if e := errorOf(t, p.call(2, methodUnsubscribe, map[string]any{"uri": "x://a"})); e.Code != jsonrpc.CodeMethodNotFound {
		t.Errorf("unsubscribe without a handler = %+v", e)
	}
	ss, out := testSession(s, s.versions)
	if _, err := s.subscribe(context.Background(), &SubscribeRequest{Session: ss, Params: &SubscribeParams{URI: "x://a"}}); !errors.Is(err, jsonrpc.ErrInvalidRequest) {
		t.Errorf("subscribe outside a call = %v", err)
	}
	if _, err := s.subscriptionsListen(context.Background(), &SubscriptionsListenRequest{Session: ss, Params: &SubscriptionsListenParams{}}); !errors.Is(err, jsonrpc.ErrInvalidRequest) {
		t.Errorf("listen outside a call = %v", err)
	}
	ctx := withRequestID(context.Background(), ss, jsonrpc.Int64ID(1))
	if _, err := s.subscriptionsListen(ctx, &SubscriptionsListenRequest{Session: ss, Params: &SubscriptionsListenParams{}}); err == nil || !strings.Contains(err.Error(), "missing required 'notifications' field") {
		t.Errorf("listen without notifications = %v", err)
	}
	// A listen for nothing the server can send is acknowledged and over.
	res, err := s.subscriptionsListen(ctx, &SubscriptionsListenRequest{Session: ss, Params: &SubscriptionsListenParams{Notifications: &NotificationSubscriptions{ResourceSubscriptions: []string{"x://a"}}}})
	if err != nil || res.Meta[MetaKeySubscriptionID] != int64(1) {
		t.Errorf("a listen for nothing = %+v, %v", res, err)
	}
	if ack := out.messages()[0].(*jsonrpc.Request); ack.Method != notificationSubscriptionsAck || string(ack.Params) != `{"_meta":{"io.modelcontextprotocol/subscriptionId":1},"notifications":{}}` {
		t.Errorf("ack = %s %s", ack.Method, ack.Params)
	}
	_ = ss.Close()
	if _, err := s.subscriptionsListen(ctx, &SubscriptionsListenRequest{Session: ss, Params: &SubscriptionsListenParams{Notifications: &NotificationSubscriptions{}}}); err == nil || !strings.Contains(err.Error(), "sending subscriptions/acknowledged") {
		t.Errorf("a listen whose ack cannot go = %v", err)
	}

	refusing := newTestServer(&ServerOptions{
		SubscribeHandler:   func(context.Context, *SubscribeRequest) error { return errors.New("no") },
		UnsubscribeHandler: func(context.Context, *UnsubscribeRequest) error { return nil },
	})
	refusing.AddResource(&Resource{URI: "x://a", Name: "a"}, nil)
	rs, _ := testSession(refusing, refusing.versions)
	ctx = withRequestID(context.Background(), rs, jsonrpc.Int64ID(1))
	if _, err := refusing.subscriptionsListen(ctx, &SubscriptionsListenRequest{Session: rs, Params: &SubscriptionsListenParams{Notifications: &NotificationSubscriptions{ResourceSubscriptions: []string{"x://a"}}}}); err == nil || err.Error() != "no" {
		t.Errorf("a listen whose subscription is refused = %v", err)
	}
}

// A change made before anyone connected tells nobody.
func TestChangedWithNoSessions(t *testing.T) {
	s := newTestServer(nil)
	s.changed(notificationToolListChanged)
	if err := s.ResourceUpdated(context.Background(), &ResourceUpdatedNotificationParams{URI: "x://none"}); err != nil {
		t.Fatal(err)
	}
}

// A request's view: its session, params and transport details.
func TestServerRequestAccessors(t *testing.T) {
	ss := &ServerSession{}
	extra := &RequestExtra{}
	r := &CallToolRequest{Session: ss, Params: &CallToolParamsRaw{Name: "x"}, Extra: extra}
	if r.GetSession() != ss || r.GetParams().(*CallToolParamsRaw).Name != "x" || r.GetExtra() != extra {
		t.Error("accessors disagree with the fields")
	}
	if id, ok := requestIDFrom(context.Background(), ss); ok || id.IsValid() {
		t.Error("a context with no call has a request id")
	}
	other := &ServerSession{}
	ctx := withRequestID(context.Background(), other, jsonrpc.Int64ID(7))
	if _, ok := requestIDFrom(ctx, ss); ok {
		t.Error("another session's call id was read as this session's")
	}
	if id, ok := requestIDFrom(ctx, other); !ok || id.Raw() != int64(7) {
		t.Errorf("the call's own session read %v, %v", id, ok)
	}
}
