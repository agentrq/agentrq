// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// An in-process transport, and a client to drive a server over it: how a
// server lists its own tools, and how tests talk to one without HTTP.

// Connection carries messages one way and the other.
type Connection interface {
	Read(context.Context) (jsonrpc.Message, error)
	Write(context.Context, jsonrpc.Message) error
	Close() error
	SessionID() string
}

// Transport opens a Connection.
type Transport interface {
	Connect(context.Context) (Connection, error)
}

// InMemoryTransport is one end of an in-process pipe.
type InMemoryTransport struct {
	in, out chan jsonrpc.Message
	closed  chan struct{}
	once    *sync.Once
}

// NewInMemoryTransports returns the two ends of an in-process pipe.
func NewInMemoryTransports() (*InMemoryTransport, *InMemoryTransport) {
	a, b := make(chan jsonrpc.Message, 64), make(chan jsonrpc.Message, 64)
	closed := make(chan struct{})
	once := &sync.Once{}
	return &InMemoryTransport{in: a, out: b, closed: closed, once: once},
		&InMemoryTransport{in: b, out: a, closed: closed, once: once}
}

// Connect opens this end.
func (t *InMemoryTransport) Connect(context.Context) (Connection, error) {
	return &memConn{t: t}, nil
}

type memConn struct{ t *InMemoryTransport }

func (c *memConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	select {
	case msg := <-c.t.in:
		return msg, nil
	case <-c.t.closed:
		return nil, io.EOF
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *memConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	// A message crosses as its wire form, as it would over a network.
	data, err := jsonrpc.Encode(msg)
	if err != nil {
		return err
	}
	decoded, err := jsonrpc.Decode(data)
	if err != nil {
		return err
	}
	// A closed pipe takes nothing, even with room in its buffer.
	select {
	case <-c.t.closed:
		return errors.New("connection closed")
	default:
	}
	select {
	case c.t.out <- decoded:
		return nil
	case <-c.t.closed:
		return errors.New("connection closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *memConn) Close() error {
	c.t.once.Do(func() { close(c.t.closed) })
	return nil
}

func (c *memConn) SessionID() string { return "" }

// LoggingTransport logs every message read and written to Writer.
type LoggingTransport struct {
	Transport Transport
	Writer    io.Writer
}

// Connect opens the transport it wraps.
func (t *LoggingTransport) Connect(ctx context.Context) (Connection, error) {
	conn, err := t.Transport.Connect(ctx)
	if err != nil {
		return nil, err
	}
	return &loggingConn{Connection: conn, w: t.Writer}, nil
}

type loggingConn struct {
	Connection
	mu sync.Mutex
	w  io.Writer
}

func (c *loggingConn) Read(ctx context.Context) (jsonrpc.Message, error) {
	msg, err := c.Connection.Read(ctx)
	c.log("read", msg, err)
	return msg, err
}

func (c *loggingConn) Write(ctx context.Context, msg jsonrpc.Message) error {
	err := c.Connection.Write(ctx, msg)
	c.log("write", msg, err)
	return err
}

func (c *loggingConn) log(dir string, msg jsonrpc.Message, err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err != nil {
		fmt.Fprintf(c.w, "%s error: %v\n", dir, err)
		return
	}
	data, _ := jsonrpc.Encode(msg)
	fmt.Fprintf(c.w, "%s: %s\n", dir, data)
}

// ServerSessionOptions configures a session Connect opens. It has no
// options yet.
type ServerSessionOptions struct{}

// Connect serves one client over t until it closes.
func (s *Server) Connect(ctx context.Context, t Transport, _ *ServerSessionOptions) (*ServerSession, error) {
	conn, err := t.Connect(ctx)
	if err != nil {
		return nil, err
	}
	out := &connOutbound{conn: conn}
	ss := newServerSession(s, conn.SessionID(), out, s.versions)
	out.session = ss
	s.addSession(ss)
	go out.serve()
	return ss, nil
}

// connOutbound runs a server session over a Connection.
type connOutbound struct {
	conn    Connection
	session *ServerSession
}

func (o *connOutbound) send(ctx context.Context, msg jsonrpc.Message) error {
	return o.conn.Write(context.WithoutCancel(ctx), msg)
}

func (o *connOutbound) canCall() bool { return true }

func (o *connOutbound) close() { _ = o.conn.Close() }

// serve reads the client's messages: calls run concurrently, the handshake
// and notifications in order.
func (o *connOutbound) serve() {
	defer o.session.Close()
	ctx := context.Background()
	for {
		msg, err := o.conn.Read(ctx)
		if err != nil {
			return
		}
		switch m := msg.(type) {
		case *jsonrpc.Response:
			o.session.deliverResponse(m)
		case *jsonrpc.Request:
			if !m.IsCall() {
				o.session.handleNotification(ctx, m)
				continue
			}
			if m.Method == methodInitialize {
				_ = o.send(ctx, o.session.handleCall(ctx, m))
				continue
			}
			go func() { _ = o.send(ctx, o.session.handleCall(ctx, m)) }()
		}
	}
}

// ClientOptions configures a Client. It has no options it acts on.
type ClientOptions struct {
	KeepAlive time.Duration
}

// ClientSessionOptions configures a ClientSession. It has no options yet.
type ClientSessionOptions struct{}

// Client is an MCP client named impl.
type Client struct {
	impl *Implementation
}

// NewClient is a client that introduces itself as impl.
func NewClient(impl *Implementation, _ *ClientOptions) *Client {
	return &Client{impl: impl}
}

// ClientSession is a client's session with one server.
type ClientSession struct {
	client  *Client
	conn    Connection
	seq     atomic.Int64
	mu      sync.Mutex
	pending map[jsonrpc.ID]chan *jsonrpc.Response
	done    chan struct{}
	result  *InitializeResult
	// modern is set when the server speaks 2026-07-28: every request then
	// carries the client's identity, and there is no handshake.
	modern bool
}

// Connect opens a session over t. Like any client of the newest revision,
// it asks server/discover first and speaks 2026-07-28 when the server does,
// and otherwise falls back to the 2025-11-25 handshake.
func (c *Client) Connect(ctx context.Context, t Transport, _ *ClientSessionOptions) (*ClientSession, error) {
	conn, err := t.Connect(ctx)
	if err != nil {
		return nil, err
	}
	cs := &ClientSession{client: c, conn: conn, pending: map[jsonrpc.ID]chan *jsonrpc.Response{}, done: make(chan struct{})}
	go cs.serve()

	var disc DiscoverResult
	discover := &DiscoverParams{Meta: cs.identity(ProtocolVersion20260728)}
	if err := cs.call(ctx, methodDiscover, discover, &disc); err == nil && slices.Contains(disc.SupportedVersions, ProtocolVersion20260728) {
		cs.modern = true
		cs.result = &InitializeResult{Capabilities: disc.Capabilities, Instructions: disc.Instructions, ProtocolVersion: ProtocolVersion20260728}
		return cs, nil
	}

	var res InitializeResult
	if err := cs.call(ctx, methodInitialize, &InitializeParams{
		ProtocolVersion: ProtocolVersion20251125,
		Capabilities:    &ClientCapabilities{},
		ClientInfo:      c.impl,
	}, &res); err != nil {
		_ = cs.Close()
		return nil, err
	}
	cs.result = &res
	note, _ := jsonrpc.NewNotification(notificationInitialized, &InitializedParams{})
	if err := conn.Write(ctx, note); err != nil {
		_ = cs.Close()
		return nil, err
	}
	return cs, nil
}

// identity is the _meta a 2026-07-28 request names its client in.
func (cs *ClientSession) identity(version string) Meta {
	return Meta{
		MetaKeyProtocolVersion:    version,
		MetaKeyClientInfo:         cs.client.impl,
		MetaKeyClientCapabilities: map[string]any{},
	}
}

// withIdentity adds the client's identity to a 2026-07-28 request's params.
func withIdentity[T any, P interface {
	*T
	Params
}](cs *ClientSession, p P) any {
	if !cs.modern {
		if p == nil {
			return struct{}{}
		}
		return p
	}
	if p == nil {
		p = new(T)
	}
	meta := p.GetMeta()
	if meta == nil {
		meta = map[string]any{}
	}
	for k, v := range cs.identity(ProtocolVersion20260728) {
		if _, ok := meta[k]; !ok {
			meta[k] = v
		}
	}
	p.SetMeta(meta)
	return p
}

// serve reads the server's messages, answering its pings.
func (cs *ClientSession) serve() {
	defer close(cs.done)
	for {
		msg, err := cs.conn.Read(context.Background())
		if err != nil {
			return
		}
		switch m := msg.(type) {
		case *jsonrpc.Response:
			cs.mu.Lock()
			ch := cs.pending[m.ID]
			cs.mu.Unlock()
			if ch != nil {
				ch <- m
			}
		case *jsonrpc.Request:
			if !m.IsCall() {
				continue
			}
			var resp *jsonrpc.Response
			if m.Method == methodPing {
				resp, _ = jsonrpc.NewResponse(m.ID, struct{}{}, nil)
			} else {
				resp = &jsonrpc.Response{ID: m.ID, Error: fmt.Errorf("%w: %q", jsonrpc.ErrMethodNotFound, m.Method)}
			}
			_ = cs.conn.Write(context.Background(), resp)
		}
	}
}

func (cs *ClientSession) call(ctx context.Context, method string, params, result any) error {
	id := jsonrpc.Int64ID(cs.seq.Add(1))
	msg, err := jsonrpc.NewCall(id, method, params)
	if err != nil {
		return err
	}
	ch := make(chan *jsonrpc.Response, 1)
	cs.mu.Lock()
	cs.pending[id] = ch
	cs.mu.Unlock()
	defer func() {
		cs.mu.Lock()
		delete(cs.pending, id)
		cs.mu.Unlock()
	}()
	if err := cs.conn.Write(ctx, msg); err != nil {
		return err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return resp.Error
		}
		return json.Unmarshal(resp.Result, result)
	case <-cs.done:
		return errors.New("connection closed")
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ID is "": an in-process session has none.
func (cs *ClientSession) ID() string { return cs.conn.SessionID() }

// InitializeResult is what the server answered the handshake with.
func (cs *ClientSession) InitializeResult() *InitializeResult { return cs.result }

// Close ends the session.
func (cs *ClientSession) Close() error { return cs.conn.Close() }

// CallTool calls a tool.
func (cs *ClientSession) CallTool(ctx context.Context, p *CallToolParams) (*CallToolResult, error) {
	return callFor[CallToolResult](ctx, cs, methodCallTool, withIdentity(cs, p))
}

// ListTools lists the server's tools.
func (cs *ClientSession) ListTools(ctx context.Context, p *ListToolsParams) (*ListToolsResult, error) {
	return callFor[ListToolsResult](ctx, cs, methodListTools, withIdentity(cs, p))
}

// Tools yields the server's tools.
func (cs *ClientSession) Tools(ctx context.Context, p *ListToolsParams) iter.Seq2[*Tool, error] {
	return func(yield func(*Tool, error) bool) {
		res, err := cs.ListTools(ctx, p)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, t := range res.Tools {
			if !yield(t, nil) {
				return
			}
		}
	}
}

// ListResources lists the server's resources.
func (cs *ClientSession) ListResources(ctx context.Context, p *ListResourcesParams) (*ListResourcesResult, error) {
	return callFor[ListResourcesResult](ctx, cs, methodListResources, withIdentity(cs, p))
}

// ListResourceTemplates lists the server's resource templates.
func (cs *ClientSession) ListResourceTemplates(ctx context.Context, p *ListResourceTemplatesParams) (*ListResourceTemplatesResult, error) {
	return callFor[ListResourceTemplatesResult](ctx, cs, methodListResourceTemplates, withIdentity(cs, p))
}

// ReadResource reads a resource.
func (cs *ClientSession) ReadResource(ctx context.Context, p *ReadResourceParams) (*ReadResourceResult, error) {
	return callFor[ReadResourceResult](ctx, cs, methodReadResource, withIdentity(cs, p))
}

// ListPrompts lists the server's prompts.
func (cs *ClientSession) ListPrompts(ctx context.Context, p *ListPromptsParams) (*ListPromptsResult, error) {
	return callFor[ListPromptsResult](ctx, cs, methodListPrompts, withIdentity(cs, p))
}

// GetPrompt renders a prompt.
func (cs *ClientSession) GetPrompt(ctx context.Context, p *GetPromptParams) (*GetPromptResult, error) {
	return callFor[GetPromptResult](ctx, cs, methodGetPrompt, withIdentity(cs, p))
}

// callFor calls method and decodes its result into an R.
func callFor[R any](ctx context.Context, cs *ClientSession, method string, params any) (*R, error) {
	var res R
	if err := cs.call(ctx, method, params, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
