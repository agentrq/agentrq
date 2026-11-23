// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sync"
	"sync/atomic"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// Methods and notifications the server receives or sends.
const (
	methodInitialize                = "initialize"
	methodPing                      = "ping"
	methodListTools                 = "tools/list"
	methodCallTool                  = "tools/call"
	methodListResources             = "resources/list"
	methodListResourceTemplates     = "resources/templates/list"
	methodReadResource              = "resources/read"
	methodSubscribe                 = "resources/subscribe"
	methodUnsubscribe               = "resources/unsubscribe"
	methodListPrompts               = "prompts/list"
	methodGetPrompt                 = "prompts/get"
	methodSetLevel                  = "logging/setLevel"
	methodComplete                  = "completion/complete"
	methodDiscover                  = "server/discover"
	methodSubscriptionsListen       = "subscriptions/listen"
	notificationInitialized         = "notifications/initialized"
	notificationCancelled           = "notifications/cancelled"
	notificationProgress            = "notifications/progress"
	notificationRootsListChanged    = "notifications/roots/list_changed"
	notificationToolListChanged     = "notifications/tools/list_changed"
	notificationPromptListChanged   = "notifications/prompts/list_changed"
	notificationResourceListChanged = "notifications/resources/list_changed"
	notificationResourceUpdated     = "notifications/resources/updated"
	notificationSubscriptionsAck    = "notifications/subscriptions/acknowledged"
)

// Session is the session a request arrived on.
type Session interface {
	ID() string
}

// Request is a received request or notification, as a middleware sees it.
type Request interface {
	GetSession() Session
	GetParams() Params
	GetExtra() *RequestExtra
}

// ServerRequest is a received request with params of type P.
type ServerRequest[P Params] struct {
	Session *ServerSession
	Params  P
	Extra   *RequestExtra
}

// GetSession returns the session the request arrived on.
func (r *ServerRequest[P]) GetSession() Session { return r.Session }

// GetParams returns the request's params.
func (r *ServerRequest[P]) GetParams() Params { return r.Params }

// GetExtra returns what the transport knew about the request.
func (r *ServerRequest[P]) GetExtra() *RequestExtra { return r.Extra }

// The requests of each method.
type (
	CallToolRequest              = ServerRequest[*CallToolParamsRaw]
	CompleteRequest              = ServerRequest[*CompleteParams]
	GetPromptRequest             = ServerRequest[*GetPromptParams]
	InitializedRequest           = ServerRequest[*InitializedParams]
	ListPromptsRequest           = ServerRequest[*ListPromptsParams]
	ListResourcesRequest         = ServerRequest[*ListResourcesParams]
	ListResourceTemplatesRequest = ServerRequest[*ListResourceTemplatesParams]
	ListToolsRequest             = ServerRequest[*ListToolsParams]
	ReadResourceRequest          = ServerRequest[*ReadResourceParams]
	SubscribeRequest             = ServerRequest[*SubscribeParams]
	SubscriptionsListenRequest   = ServerRequest[*SubscriptionsListenParams]
	UnsubscribeRequest           = ServerRequest[*UnsubscribeParams]
)

// sessionState is what a session knows of its client.
type sessionState struct {
	initialize  *InitializeParams
	initialized bool
	negotiated  string
	logLevel    string
}

// outbound is how a session reaches its client.
type outbound interface {
	// send delivers a message: a response to the stream of the request it
	// answers, anything else to the stream of the request ctx belongs to, or
	// the session's standalone stream.
	send(ctx context.Context, msg jsonrpc.Message) error
	// canCall reports whether the client can answer a request: it needs
	// somewhere to send the response.
	canCall() bool
	// close ends the session's streams.
	close()
}

// ServerSession is one client's session.
type ServerSession struct {
	server *Server
	id     string
	out    outbound
	// versions are the revisions the session's transport can speak.
	versions []string

	mu      sync.Mutex
	state   sessionState
	calls   map[jsonrpc.ID]chan *jsonrpc.Response // server -> client calls awaiting an answer
	running map[jsonrpc.ID]context.CancelCauseFunc
	closed  bool
	onClose func()

	seq atomic.Int64
}

func newServerSession(s *Server, id string, out outbound, versions []string) *ServerSession {
	return &ServerSession{
		server:   s,
		id:       id,
		out:      out,
		versions: versions,
		calls:    map[jsonrpc.ID]chan *jsonrpc.Response{},
		running:  map[jsonrpc.ID]context.CancelCauseFunc{},
	}
}

// ID is the session's id: "" for a session with none, which stateless and
// in-memory sessions are.
func (ss *ServerSession) ID() string {
	if ss == nil {
		return ""
	}
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.id
}

// setID names the session once its handshake has given it an id.
func (ss *ServerSession) setID(id string) {
	ss.mu.Lock()
	ss.id = id
	ss.mu.Unlock()
}

// InitializeParams is what the client sent in its handshake, nil before it.
func (ss *ServerSession) InitializeParams() *InitializeParams {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.state.initialize
}

func (ss *ServerSession) setInitialize(p *InitializeParams) {
	ss.mu.Lock()
	ss.state.initialize = p
	ss.mu.Unlock()
}

// speaksModern reports whether the client speaks 2026-07-28, which hears
// notifications only on the listens it opened.
func (ss *ServerSession) speaksModern() bool {
	p := ss.InitializeParams()
	return p != nil && p.ProtocolVersion >= ProtocolVersion20260728
}

// markResultType marks a tools/call, resources/read or prompts/get result
// complete for a client that knows the field: one on 2026-07-28 or later.
func (ss *ServerSession) markResultType(res resultTyped) {
	if p := ss.InitializeParams(); p == nil || p.ProtocolVersion >= ProtocolVersion20260728 {
		res.setResultType(resultTypeComplete)
	}
}

func (ss *ServerSession) supportedVersions() []string {
	return slices.Clone(ss.versions)
}

// Notify sends the client a notification. It fails when the client cannot
// be reached: a stateful session whose client holds no stream, for one.
func (ss *ServerSession) Notify(ctx context.Context, method string, params any) error {
	msg, err := jsonrpc.NewNotification(method, params)
	if err != nil {
		return err
	}
	if ss.isClosed() {
		return errors.New("session is closed")
	}
	return ss.out.send(ctx, msg)
}

// Ping asks the client to answer, and waits for it to.
func (ss *ServerSession) Ping(ctx context.Context, _ *PingParams) error {
	_, err := ss.call(ctx, methodPing, nil)
	return err
}

// call sends the client a request and waits for its response.
func (ss *ServerSession) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if !ss.out.canCall() {
		return nil, fmt.Errorf("%w: stateless servers cannot make requests", errRejected)
	}
	id := jsonrpc.Int64ID(ss.seq.Add(1))
	msg, err := jsonrpc.NewCall(id, method, params)
	if err != nil {
		return nil, err
	}
	ch := make(chan *jsonrpc.Response, 1)
	ss.mu.Lock()
	if ss.closed {
		ss.mu.Unlock()
		return nil, errors.New("session is closed")
	}
	ss.calls[id] = ch
	ss.mu.Unlock()
	defer func() {
		ss.mu.Lock()
		delete(ss.calls, id)
		ss.mu.Unlock()
	}()
	if err := ss.out.send(ctx, msg); err != nil {
		return nil, err
	}
	select {
	case resp := <-ch:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// deliverResponse hands a client's response to the call awaiting it. A
// response nobody awaits is dropped.
func (ss *ServerSession) deliverResponse(resp *jsonrpc.Response) {
	ss.mu.Lock()
	ch, ok := ss.calls[resp.ID]
	ss.mu.Unlock()
	if ok {
		select {
		case ch <- resp:
		default:
		}
	}
}

// Close ends the session: its streams close and it leaves the server. Calls
// it is still handling run to completion, and their responses still reach
// the requests that asked.
func (ss *ServerSession) Close() error {
	ss.mu.Lock()
	if ss.closed {
		ss.mu.Unlock()
		return nil
	}
	ss.closed = true
	onClose := ss.onClose
	ss.mu.Unlock()
	ss.server.removeSession(ss)
	ss.out.close()
	if onClose != nil {
		onClose()
	}
	return nil
}

func (ss *ServerSession) isClosed() bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	return ss.closed
}

// track records a call being handled, so notifications/cancelled can stop
// it. A second call with an id still in flight is refused.
func (ss *ServerSession) track(id jsonrpc.ID, cancel context.CancelCauseFunc) bool {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if _, ok := ss.running[id]; ok {
		return false
	}
	ss.running[id] = cancel
	return true
}

func (ss *ServerSession) untrack(id jsonrpc.ID) {
	ss.mu.Lock()
	delete(ss.running, id)
	ss.mu.Unlock()
}

// cancelRequest stops the call a notifications/cancelled names.
func (ss *ServerSession) cancelRequest(raw any) {
	id, err := jsonrpc.MakeID(raw)
	if err != nil || !id.IsValid() {
		return
	}
	ss.mu.Lock()
	cancel, ok := ss.running[id]
	ss.mu.Unlock()
	if ok {
		cancel(errCancelledByClient)
	}
}

var errCancelledByClient = errors.New("request cancelled by the client")

// errRejected is a message the transport could not deliver. It does not
// break the session.
var errRejected = &jsonrpc.Error{Code: -32005, Message: "rejected by transport"}

// handleCall runs a call to its response.
func (ss *ServerSession) handleCall(ctx context.Context, req *jsonrpc.Request) *jsonrpc.Response {
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	if !ss.track(req.ID, cancel) {
		return &jsonrpc.Response{ID: req.ID, Error: fmt.Errorf("%w: request ID %v already in use", jsonrpc.ErrInvalidRequest, req.ID.Raw())}
	}
	defer ss.untrack(req.ID)
	res, err := ss.handle(ctx, req)
	return ss.response(req, res, err)
}

// response is the wire answer to req.
func (ss *ServerSession) response(req *jsonrpc.Request, res Result, err error) *jsonrpc.Response {
	if errors.Is(err, jsonrpc.ErrNotHandled) || errors.Is(err, jsonrpc.ErrMethodNotFound) {
		err = fmt.Errorf("%w: %q", jsonrpc.ErrMethodNotFound, req.Method)
	}
	if err != nil {
		return &jsonrpc.Response{ID: req.ID, Error: err}
	}
	if res == nil {
		return &jsonrpc.Response{ID: req.ID, Error: fmt.Errorf("%w: %q returned no result", jsonrpc.ErrInternal, req.Method)}
	}
	resp, merr := jsonrpc.NewResponse(req.ID, res, nil)
	if merr != nil {
		return &jsonrpc.Response{ID: req.ID, Error: fmt.Errorf("%w: %q returned a malformed result: %v", jsonrpc.ErrInternal, req.Method, merr)}
	}
	return resp
}

// handleNotification runs a notification. Its errors have nowhere to go.
func (ss *ServerSession) handleNotification(ctx context.Context, req *jsonrpc.Request) {
	if req.Method == notificationCancelled {
		var p CancelledParams
		if json.Unmarshal(req.Params, &p) == nil {
			ss.cancelRequest(p.RequestID)
		}
	}
	_, _ = ss.handle(ctx, req)
}

// handle checks a message may be received now, and runs it through the
// middleware to its method.
func (ss *ServerSession) handle(ctx context.Context, req *jsonrpc.Request) (Result, error) {
	initialized := ss.InitializeParams() != nil
	meta, err := readRequestMeta(req)
	if err != nil {
		return nil, err
	}
	if meta.modern && !slices.Contains(ss.server.versions, meta.initialize.ProtocolVersion) {
		data, _ := json.Marshal(UnsupportedProtocolVersionData{Supported: ss.server.versions, Requested: meta.initialize.ProtocolVersion})
		return nil, &jsonrpc.Error{Code: CodeUnsupportedProtocolVersion, Message: "unsupported protocol version", Data: data}
	}
	switch req.Method {
	case methodInitialize, methodPing, notificationInitialized, notificationRootsListChanged, methodSetLevel, methodSubscribe, methodUnsubscribe:
		if meta.modern {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: fmt.Sprintf("%q is not supported in the new protocol", req.Method)}
		}
	case methodDiscover:
		if !meta.modern {
			return nil, &jsonrpc.Error{Code: jsonrpc.CodeMethodNotFound, Message: fmt.Sprintf("%q is only supported in protocol version >= %s", req.Method, ProtocolVersion20260728)}
		}
	default:
		if !initialized && !meta.modern && req.IsCall() {
			return nil, fmt.Errorf("method %q is invalid during session initialization", req.Method)
		}
		if !initialized && meta.modern {
			ss.setInitialize(meta.initialize)
		}
	}
	ctx = withRequestID(ctx, req.ID)
	res, err := ss.receive(ctx, req)
	if err != nil {
		return nil, err
	}
	if meta.modern && res != nil {
		if r, ok := res.(resultTyped); ok {
			r.setResultType(resultTypeComplete)
		}
		annotateServerInfo(res, ss.server.impl)
	}
	return res, nil
}

// receive decodes a message's params and hands it to the middleware.
func (ss *ServerSession) receive(ctx context.Context, req *jsonrpc.Request) (Result, error) {
	ss.server.mu.Lock()
	methods := ss.server.methods
	ss.server.mu.Unlock()
	info, err := checkRequest(req, methods)
	if err != nil {
		return nil, err
	}
	params, err := info.unmarshal(req.Params)
	if err != nil {
		return nil, fmt.Errorf("handling '%s': %w", req.Method, err)
	}
	extra, _ := req.Extra.(*RequestExtra)
	return ss.server.receivingHandler()(ctx, req.Method, info.newRequest(ss, params, extra))
}

// annotateServerInfo names the server in a 2026-07-28 result's _meta.
func annotateServerInfo(res Result, impl *Implementation) {
	if _, empty := res.(*emptyResult); empty || impl == nil {
		return
	}
	m := res.GetMeta()
	if m == nil {
		m = map[string]any{}
	}
	if _, ok := m[MetaKeyServerInfo]; ok {
		return
	}
	m[MetaKeyServerInfo] = impl
	res.SetMeta(m)
}

// checkRequest reports what is wrong with a message for the method it
// names, before anything runs.
func checkRequest(req *jsonrpc.Request, methods map[string]methodInfo) (methodInfo, error) {
	info, ok := methods[req.Method]
	if !ok {
		return methodInfo{}, fmt.Errorf("%w: %q unsupported", jsonrpc.ErrNotHandled, req.Method)
	}
	if info.flags&notification != 0 && req.IsCall() {
		return methodInfo{}, fmt.Errorf("%w: unexpected id for %q", jsonrpc.ErrInvalidRequest, req.Method)
	}
	if info.flags&notification == 0 && !req.IsCall() {
		return methodInfo{}, fmt.Errorf("%w: missing id for %q", jsonrpc.ErrInvalidRequest, req.Method)
	}
	if info.flags&missingParamsOK == 0 && len(req.Params) == 0 {
		return methodInfo{}, fmt.Errorf("%w: missing required \"params\"", jsonrpc.ErrInvalidRequest)
	}
	return info, nil
}

// requestMeta is what a request's _meta says about the protocol it speaks.
type requestMeta struct {
	modern     bool
	initialize *InitializeParams
}

// readRequestMeta reads the 2026-07-28 identity of a call: the revision, the
// client's capabilities, which it must send, and optionally its name.
// Notifications and earlier revisions carry none.
func readRequestMeta(req *jsonrpc.Request) (requestMeta, error) {
	if !req.IsCall() {
		return requestMeta{}, nil
	}
	meta := paramsMeta(req.Params)
	version, _ := meta[MetaKeyProtocolVersion].(string)
	if version < ProtocolVersion20260728 {
		return requestMeta{}, nil
	}
	init := &InitializeParams{ProtocolVersion: version}
	if raw, present := meta[MetaKeyClientInfo]; present {
		var info *Implementation
		if raw == nil || !remarshal(raw, &info) {
			return requestMeta{}, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("invalid _meta field %q", MetaKeyClientInfo)}
		}
		init.ClientInfo = info
	}
	var caps *ClientCapabilities
	if raw, ok := meta[MetaKeyClientCapabilities]; !ok || raw == nil || !remarshal(raw, &caps) {
		return requestMeta{}, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("missing or invalid _meta field %q", MetaKeyClientCapabilities)}
	}
	init.Capabilities = caps
	return requestMeta{modern: true, initialize: init}, nil
}

// paramsMeta is the _meta object of raw params, nil when there is none.
func paramsMeta(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var p struct {
		Meta map[string]any `json:"_meta"`
	}
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	return p.Meta
}

// remarshal decodes a value already decoded into map form into out.
func remarshal(v any, out any) bool {
	data, err := json.Marshal(v)
	if err != nil {
		return false
	}
	return json.Unmarshal(data, out) == nil
}

type requestIDKey struct{}

func withRequestID(ctx context.Context, id jsonrpc.ID) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// requestIDFrom is the id of the call ctx belongs to.
func requestIDFrom(ctx context.Context) (jsonrpc.ID, bool) {
	id, ok := ctx.Value(requestIDKey{}).(jsonrpc.ID)
	return id, ok && id.IsValid()
}

// methodInfo is how the server receives one method.
type methodInfo struct {
	flags      methodFlags
	unmarshal  func(json.RawMessage) (Params, error)
	newRequest func(*ServerSession, Params, *RequestExtra) Request
	handle     func(context.Context, Request) (Result, error)
}

type methodFlags int

const (
	notification methodFlags = 1 << iota
	missingParamsOK
)

type paramsPtr[T any] interface {
	*T
	Params
}

func newMethodInfo[P paramsPtr[T], R Result, T any](h func(context.Context, *ServerRequest[P]) (R, error), flags methodFlags) methodInfo {
	return methodInfo{
		flags: flags,
		unmarshal: func(raw json.RawMessage) (Params, error) {
			var p P
			if raw != nil {
				if err := json.Unmarshal(raw, &p); err != nil {
					return nil, fmt.Errorf("%w: unmarshaling %q into a %T: %w", jsonrpc.ErrInvalidParams, raw, p, err)
				}
			}
			if flags&missingParamsOK == 0 && p == nil {
				return nil, fmt.Errorf("%w: missing required \"params\"", jsonrpc.ErrInvalidRequest)
			}
			if p == nil {
				return nil, nil
			}
			return p, nil
		},
		newRequest: func(ss *ServerSession, p Params, extra *RequestExtra) Request {
			r := &ServerRequest[P]{Session: ss, Extra: extra}
			if p != nil {
				r.Params = p.(P)
			}
			return r
		},
		handle: func(ctx context.Context, req Request) (Result, error) {
			res, err := h(ctx, req.(*ServerRequest[P]))
			if err != nil {
				return nil, err
			}
			return res, nil
		},
	}
}

// serverMethods are the standard methods every server receives.
var serverMethods = map[string]methodInfo{
	methodInitialize:            initializeMethod(),
	methodPing:                  newMethodInfo(sessionMethod((*ServerSession).ping), missingParamsOK),
	methodListTools:             newMethodInfo(serverMethod((*Server).listTools), missingParamsOK),
	methodCallTool:              newMethodInfo(serverMethod((*Server).callTool), 0),
	methodListResources:         newMethodInfo(serverMethod((*Server).listResources), missingParamsOK),
	methodListResourceTemplates: newMethodInfo(serverMethod((*Server).listResourceTemplates), missingParamsOK),
	methodReadResource:          newMethodInfo(serverMethod((*Server).readResource), 0),
	methodSubscribe:             newMethodInfo(serverMethod((*Server).subscribe), 0),
	methodUnsubscribe:           newMethodInfo(serverMethod((*Server).unsubscribe), 0),
	methodListPrompts:           newMethodInfo(serverMethod((*Server).listPrompts), missingParamsOK),
	methodGetPrompt:             newMethodInfo(serverMethod((*Server).getPrompt), 0),
	methodSetLevel:              newMethodInfo(sessionMethod((*ServerSession).setLevel), 0),
	methodComplete:              newMethodInfo(serverMethod((*Server).complete), 0),
	methodDiscover:              newMethodInfo(serverMethod((*Server).discover), missingParamsOK),
	methodSubscriptionsListen:   newMethodInfo(serverMethod((*Server).subscriptionsListen), 0),
	notificationInitialized:     newMethodInfo(sessionMethod((*ServerSession).initialized), notification|missingParamsOK),
	notificationCancelled:       newMethodInfo(sessionMethod((*ServerSession).cancelled), notification|missingParamsOK),
	notificationRootsListChanged: newMethodInfo(sessionMethod(func(*ServerSession, context.Context, *RootsListChangedParams) (Result, error) {
		return nil, nil
	}), notification|missingParamsOK),
	notificationProgress: newMethodInfo(sessionMethod(func(*ServerSession, context.Context, *ProgressNotificationParams) (Result, error) {
		return nil, nil
	}), notification),
}

func serverMethod[P Params, R Result](f func(*Server, context.Context, *ServerRequest[P]) (R, error)) func(context.Context, *ServerRequest[P]) (R, error) {
	return func(ctx context.Context, req *ServerRequest[P]) (R, error) {
		return f(req.Session.server, ctx, req)
	}
}

func sessionMethod[P Params, R Result](f func(*ServerSession, context.Context, P) (R, error)) func(context.Context, *ServerRequest[P]) (R, error) {
	return func(ctx context.Context, req *ServerRequest[P]) (R, error) {
		return f(req.Session, ctx, req.Params)
	}
}

// initializeMethod requires params. A handshake that cannot be read fails
// as the handshake's own error, not as a JSON-RPC one.
func initializeMethod() methodInfo {
	info := newMethodInfo(sessionMethod((*ServerSession).initialize), 0)
	info.unmarshal = func(raw json.RawMessage) (Params, error) {
		var p *InitializeParams
		if raw != nil {
			if err := json.Unmarshal(raw, &p); err != nil {
				return nil, fmt.Errorf("unmarshaling %q into a %T: %w", raw, p, err)
			}
		}
		if p == nil {
			return nil, errors.New(`missing required "params"`)
		}
		return p, nil
	}
	return info
}

func (ss *ServerSession) initialize(ctx context.Context, p *InitializeParams) (*InitializeResult, error) {
	if p == nil {
		return nil, fmt.Errorf("%w: \"params\" must be provided", jsonrpc.ErrInvalidParams)
	}
	ss.mu.Lock()
	if ss.state.initialize != nil {
		ss.mu.Unlock()
		return nil, fmt.Errorf("duplicate %q received", methodInitialize)
	}
	ss.state.initialize = p
	ss.state.negotiated = negotiatedVersion(p.ProtocolVersion, ss.server.versions)
	negotiated := ss.state.negotiated
	ss.mu.Unlock()
	return &InitializeResult{
		ProtocolVersion: negotiated,
		Capabilities:    ss.server.capabilities(),
		Instructions:    ss.server.opts.Instructions,
		ServerInfo:      ss.server.impl,
	}, nil
}

func (ss *ServerSession) initialized(ctx context.Context, _ *InitializedParams) (Result, error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.state.initialize == nil {
		return nil, fmt.Errorf("%q before %q", notificationInitialized, methodInitialize)
	}
	if ss.state.initialized {
		return nil, fmt.Errorf("duplicate %q received", notificationInitialized)
	}
	ss.state.initialized = true
	return nil, nil
}

func (ss *ServerSession) cancelled(context.Context, *CancelledParams) (Result, error) {
	return nil, nil
}

func (ss *ServerSession) ping(context.Context, *PingParams) (*emptyResult, error) {
	return &emptyResult{}, nil
}

func (ss *ServerSession) setLevel(_ context.Context, p *SetLoggingLevelParams) (*emptyResult, error) {
	ss.mu.Lock()
	ss.state.logLevel = p.Level
	ss.mu.Unlock()
	return &emptyResult{}, nil
}

// AddReceivingCustomMethod serves a method the protocol does not define.
// It is refused for a name the protocol does.
func AddReceivingCustomMethod[P paramsPtr[T], R Result, T any](s *Server, method string, h func(context.Context, *ServerSession, P) (R, error)) error {
	if _, ok := serverMethods[method]; ok {
		return fmt.Errorf("mcp: AddReceivingCustomMethod: %q shadows a standard MCP method", method)
	}
	info := newMethodInfo(func(ctx context.Context, req *ServerRequest[P]) (R, error) {
		return h(ctx, req.Session, req.Params)
	}, missingParamsOK)
	s.mu.Lock()
	methods := maps.Clone(s.methods)
	methods[method] = info
	s.methods = methods
	s.mu.Unlock()
	return nil
}
