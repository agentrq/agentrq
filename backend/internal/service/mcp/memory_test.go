// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// failingTransport cannot be connected.
type failingTransport struct{}

func (failingTransport) Connect(context.Context) (Connection, error) {
	return nil, errors.New("unreachable")
}

// writeLimitTransport lets only so many writes through.
type writeLimitTransport struct {
	Transport
	left int
}

func (t *writeLimitTransport) Connect(ctx context.Context) (Connection, error) {
	c, err := t.Transport.Connect(ctx)
	return &writeLimitConn{Connection: c, left: t.left}, err
}

type writeLimitConn struct {
	Connection
	mu   sync.Mutex
	left int
}

func (c *writeLimitConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	c.mu.Lock()
	if c.left == 0 {
		c.mu.Unlock()
		return errors.New("write refused")
	}
	c.left--
	c.mu.Unlock()
	return c.Connection.Write(ctx, msg)
}

// scripted is the far end of a pipe that answers each call with what answer
// returns, in order, and nothing when it returns nothing.
func scripted(t *testing.T, answer func(*jsonrpc.Request) []jsonrpc.Message) (*InMemoryTransport, Connection) {
	t.Helper()
	st, ct := NewInMemoryTransports()
	conn, _ := st.Connect(context.Background())
	go func() {
		for {
			msg, err := conn.Read(context.Background())
			if err != nil {
				return
			}
			if req, ok := msg.(*jsonrpc.Request); ok && req.IsCall() {
				for _, m := range answer(req) {
					_ = conn.Write(context.Background(), m)
				}
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close() })
	return ct, conn
}

func refuseAll(req *jsonrpc.Request) []jsonrpc.Message {
	return []jsonrpc.Message{&jsonrpc.Response{ID: req.ID, Error: &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: "no"}}}
}

// handshakeOnly refuses discover, accepts the handshake after an answer to
// a call nobody made, and answers nothing else.
func handshakeOnly(req *jsonrpc.Request) []jsonrpc.Message {
	switch req.Method {
	case methodDiscover:
		return refuseAll(req)
	case methodInitialize:
		stray := &jsonrpc.Response{ID: jsonrpc.Int64ID(9999), Result: json.RawMessage(`{}`)}
		resp, _ := jsonrpc.NewResponse(req.ID, &InitializeResult{ProtocolVersion: ProtocolVersion20251125}, nil)
		return []jsonrpc.Message{stray, resp}
	}
	return nil
}

// A message written at one end is read at the other, as its wire form; once
// either end closes, both are closed.
func TestInMemoryPipe(t *testing.T) {
	a, b := NewInMemoryTransports()
	ca, _ := a.Connect(context.Background())
	cb, _ := b.Connect(context.Background())
	if ca.SessionID() != "" {
		t.Error("an in-memory connection has a session id")
	}
	note, _ := jsonrpc.NewNotification("x", map[string]int{"a": 1})
	note.Extra = "dropped"
	if err := ca.Write(context.Background(), note); err != nil {
		t.Fatal(err)
	}
	got, err := cb.Read(context.Background())
	if err != nil || got.(*jsonrpc.Request).Method != "x" || got.(*jsonrpc.Request).Extra != nil {
		t.Fatalf("read %+v, %v", got, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := cb.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("read on a cancelled context = %v", err)
	}
	if err := ca.Write(context.Background(), &jsonrpc.Request{Method: "x", Params: json.RawMessage("{")}); err == nil {
		t.Error("a message with broken params was written")
	}
	if err := ca.Write(context.Background(), &jsonrpc.Response{}); err == nil {
		t.Error("a response with no id was written")
	}
	for range 64 {
		_ = ca.Write(context.Background(), note)
	}
	if err := ca.Write(ctx, note); !errors.Is(err, context.Canceled) {
		t.Errorf("write to a full pipe on a cancelled context = %v", err)
	}
	_ = cb.Close()
	_ = ca.Close()
	if err := ca.Write(context.Background(), note); err == nil || err.Error() != "connection closed" {
		t.Errorf("write after close = %v", err)
	}
	for {
		if _, err := ca.Read(context.Background()); err != nil {
			if !errors.Is(err, io.EOF) {
				t.Errorf("read after close = %v", err)
			}
			break
		}
	}
}

type syncBuf struct {
	mu sync.Mutex
	sb strings.Builder
}

func (b *syncBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.Write(p)
}

func (b *syncBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.sb.String()
}

// A logging transport writes each message read and written, and each
// failure.
func TestLoggingTransport(t *testing.T) {
	if _, err := (&LoggingTransport{Transport: failingTransport{}}).Connect(context.Background()); err == nil {
		t.Error("a logging transport over an unreachable one connected")
	}
	a, b := NewInMemoryTransports()
	log := &syncBuf{}
	la, err := (&LoggingTransport{Transport: &writeLimitTransport{Transport: a, left: 1}, Writer: log}).Connect(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cb, _ := b.Connect(context.Background())
	note, _ := jsonrpc.NewNotification("hello", nil)
	_ = la.Write(context.Background(), note)
	_ = la.Write(context.Background(), note)
	_ = cb.Write(context.Background(), note)
	_, _ = la.Read(context.Background())
	_ = la.Close()
	_, _ = la.Read(context.Background())
	want := "write: {\"jsonrpc\":\"2.0\",\"method\":\"hello\"}\nwrite error: write refused\nread: {\"jsonrpc\":\"2.0\",\"method\":\"hello\"}\nread error: EOF\n"
	if log.String() != want {
		t.Errorf("log =\n%s\nwant\n%s", log.String(), want)
	}
}

// A server cannot be connected over a transport that will not connect.
func TestServerConnectFails(t *testing.T) {
	if _, err := newTestServer(nil).Connect(context.Background(), failingTransport{}, nil); err == nil {
		t.Error("connected over an unreachable transport")
	}
}

func connectClient(t *testing.T, s *Server) *ClientSession {
	t.Helper()
	st, ct := NewInMemoryTransports()
	if _, err := s.Connect(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := NewClient(&Implementation{Name: "client", Version: "3"}, &ClientOptions{}).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func fullServer(opts *ServerOptions) (*Server, *CallToolParamsRaw) {
	s := newTestServer(opts)
	var last CallToolParamsRaw
	var mu sync.Mutex
	s.AddTool(&Tool{Name: "echo", InputSchema: objectSchema}, func(_ context.Context, req *CallToolRequest) (*CallToolResult, error) {
		mu.Lock()
		last = *req.Params
		mu.Unlock()
		return &CallToolResult{Content: []Content{&TextContent{Text: string(req.Params.Arguments)}}}, nil
	})
	s.AddTool(&Tool{Name: "a", InputSchema: objectSchema}, noTool)
	s.AddResource(&Resource{URI: "x://r", Name: "r"}, func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error) {
		return &ReadResourceResult{Contents: []*ResourceContents{{Text: "body"}}}, nil
	})
	s.AddResourceTemplate(&ResourceTemplate{URITemplate: "x://t/{id}", Name: "t"}, nil)
	s.AddPrompt(&Prompt{Name: "p"}, func(context.Context, *GetPromptRequest) (*GetPromptResult, error) {
		return &GetPromptResult{Messages: []*PromptMessage{{Role: "user", Content: &TextContent{Text: "prompted"}}}}, nil
	})
	return s, &last
}

// Against a server that speaks 2026-07-28 the client needs no handshake: it
// discovers, and every request names it.
func TestClientModern(t *testing.T) {
	s, last := fullServer(nil)
	cs := connectClient(t, s)
	if !cs.modern || cs.InitializeResult().ProtocolVersion != ProtocolVersion20260728 || cs.ID() != "" {
		t.Fatalf("client = modern %v, %+v", cs.modern, cs.InitializeResult())
	}
	var ss *ServerSession
	for x := range s.Sessions() {
		ss = x
	}
	if p := ss.InitializeParams(); p == nil || p.ClientInfo.Name != "client" {
		t.Errorf("the server knows the client as %+v", p)
	}

	res, err := cs.CallTool(context.Background(), &CallToolParams{Name: "echo", Arguments: map[string]any{"x": 1}, Meta: Meta{MetaKeyClientInfo: map[string]any{"name": "custom", "version": "0"}}})
	if err != nil || res.Content[0].(*TextContent).Text != `{"x":1}` || res.resultType != resultTypeComplete {
		t.Fatalf("CallTool = %+v, %v", res, err)
	}
	if info, _ := last.Meta[MetaKeyClientInfo].(map[string]any); info["name"] != "custom" || last.Meta[MetaKeyProtocolVersion] != ProtocolVersion20260728 {
		t.Errorf("the call carried meta %v", last.Meta)
	}
	if res, err := cs.CallTool(context.Background(), &CallToolParams{Name: "nope"}); res != nil || err == nil || !strings.Contains(err.Error(), "unknown tool") {
		t.Errorf("unknown tool = %+v, %v", res, err)
	}
	tools, err := cs.ListTools(context.Background(), nil)
	if err != nil || len(tools.Tools) != 2 {
		t.Fatalf("ListTools = %+v, %v", tools, err)
	}
	var names []string
	for tool, err := range cs.Tools(context.Background(), nil) {
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, tool.Name)
	}
	if strings.Join(names, ",") != "a,echo" {
		t.Errorf("Tools = %v", names)
	}
	for range cs.Tools(context.Background(), nil) {
		break
	}
	if r, err := cs.ListResources(context.Background(), nil); err != nil || len(r.Resources) != 1 {
		t.Errorf("ListResources = %+v, %v", r, err)
	}
	if r, err := cs.ListResourceTemplates(context.Background(), nil); err != nil || len(r.ResourceTemplates) != 1 {
		t.Errorf("ListResourceTemplates = %+v, %v", r, err)
	}
	if r, err := cs.ReadResource(context.Background(), &ReadResourceParams{URI: "x://r"}); err != nil || r.Contents[0].Text != "body" {
		t.Errorf("ReadResource = %+v, %v", r, err)
	}
	if r, err := cs.ListPrompts(context.Background(), nil); err != nil || len(r.Prompts) != 1 {
		t.Errorf("ListPrompts = %+v, %v", r, err)
	}
	if r, err := cs.GetPrompt(context.Background(), &GetPromptParams{Name: "p"}); err != nil || r.Messages[0].Content.(*TextContent).Text != "prompted" {
		t.Errorf("GetPrompt = %+v, %v", r, err)
	}
}

// Against a server that does not speak 2026-07-28 the client falls back to
// the 2025-11-25 handshake.
func TestClientHandshakeFallback(t *testing.T) {
	s, _ := fullServer(&ServerOptions{SupportedProtocolVersions: []string{ProtocolVersion20251125, ProtocolVersion20250618}})
	cs := connectClient(t, s)
	if cs.modern || cs.InitializeResult().ProtocolVersion != ProtocolVersion20251125 {
		t.Fatalf("client = modern %v, %+v", cs.modern, cs.InitializeResult())
	}
	var ss *ServerSession
	for x := range s.Sessions() {
		ss = x
	}
	if p := ss.InitializeParams(); p == nil || p.ClientInfo.Name != "client" || p.ProtocolVersion != ProtocolVersion20251125 {
		t.Errorf("the server knows the client as %+v", p)
	}
	if tools, err := cs.ListTools(context.Background(), nil); err != nil || len(tools.Tools) != 2 {
		t.Errorf("ListTools = %+v, %v", tools, err)
	}
	if res, err := cs.CallTool(context.Background(), &CallToolParams{Name: "a"}); err != nil || res.resultType != "" {
		t.Errorf("CallTool = %+v, %v", res, err)
	}
}

// The client answers the server's ping, refuses any other call, and lets
// notifications pass.
func TestClientAnswersTheServer(t *testing.T) {
	s, _ := fullServer(nil)
	cs := connectClient(t, s)
	var ss *ServerSession
	for x := range s.Sessions() {
		ss = x
	}
	if err := ss.Notify(context.Background(), "notifications/message", map[string]string{"level": "info"}); err != nil {
		t.Fatal(err)
	}
	if err := ss.Ping(context.Background(), nil); err != nil {
		t.Errorf("ping = %v", err)
	}
	_, err := ss.call(context.Background(), "roots/list", nil)
	var wire *jsonrpc.Error
	if !errors.As(err, &wire) || wire.Code != jsonrpc.CodeMethodNotFound || wire.Message != `method not found: "roots/list"` {
		t.Errorf("roots/list = %v", err)
	}
	_ = cs
}

// A client that cannot connect, or whose handshake fails, reports it.
func TestClientConnectFailures(t *testing.T) {
	c := NewClient(&Implementation{Name: "c", Version: "1"}, nil)
	if _, err := c.Connect(context.Background(), failingTransport{}, nil); err == nil {
		t.Error("connected over an unreachable transport")
	}
	ct, _ := scripted(t, refuseAll)
	if _, err := c.Connect(context.Background(), ct, nil); err == nil || err.Error() != "no" {
		t.Errorf("a refused handshake = %v", err)
	}
	ct, _ = scripted(t, handshakeOnly)
	if _, err := c.Connect(context.Background(), &writeLimitTransport{Transport: ct, left: 2}, nil); err == nil || err.Error() != "write refused" {
		t.Errorf("a handshake whose initialized cannot be sent = %v", err)
	}
}

// A call fails when it cannot be sent, when the connection goes, or when
// the caller stops waiting; answers to calls nobody made are ignored.
func TestClientCallFailures(t *testing.T) {
	ct, conn := scripted(t, handshakeOnly)
	cs, err := NewClient(&Implementation{Name: "c", Version: "1"}, nil).Connect(context.Background(), ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := cs.CallTool(context.Background(), &CallToolParams{Name: "x", Arguments: make(chan int)}); err == nil {
		t.Error("a call with unmarshalable arguments was sent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if _, err := cs.ListPrompts(ctx, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("a call nobody answers = %v", err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := cs.ListTools(context.Background(), nil)
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)
	_ = conn.Close()
	select {
	case err := <-done:
		if err == nil || err.Error() != "connection closed" {
			t.Errorf("a call whose connection went = %v", err)
		}
	case <-time.After(waitFor):
		t.Fatal("a call outlived its connection")
	}
	if _, err := cs.ListTools(context.Background(), nil); err == nil {
		t.Error("a call on a closed connection was sent")
	}
	for tool, err := range cs.Tools(context.Background(), nil) {
		if tool != nil || err == nil {
			t.Errorf("Tools on a closed connection yielded %v, %v", tool, err)
		}
	}
}

// A write waiting on a full pipe gives up when the pipe closes under it.
func TestInMemoryPipe_CloseReleasesABlockedWrite(t *testing.T) {
	a, _ := NewInMemoryTransports()
	ca, _ := a.Connect(context.Background())
	note, _ := jsonrpc.NewNotification("x", nil)
	for range 64 {
		if err := ca.Write(context.Background(), note); err != nil {
			t.Fatal(err)
		}
	}
	done := make(chan error, 1)
	go func() { done <- ca.Write(context.Background(), note) }()
	select {
	case err := <-done:
		t.Fatalf("a write to a full pipe returned %v before the close", err)
	case <-time.After(50 * time.Millisecond):
	}
	_ = ca.Close()
	select {
	case err := <-done:
		if err == nil || err.Error() != "connection closed" {
			t.Errorf("the blocked write returned %v, want connection closed", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the blocked write never returned")
	}
}
