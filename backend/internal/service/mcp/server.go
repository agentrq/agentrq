// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package mcp is AgentRQ's Model Context Protocol server: the protocol types,
// method dispatch and the Streamable HTTP transport the workspace server and
// CoreMCP are served over.
//
// A stateful server's session id is a signed token that records how the
// session was initialized (see SessionIDs), so a session outlives the process
// that created it: after a restart, or on another instance, its client
// reopening its push stream rebuilds it.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"net/url"
	"regexp"
	"slices"
	"sync"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/mcp/jsonrpc"
)

// MethodHandler handles one received request or notification. A call
// returns exactly one of a result and an error; a notification neither.
type MethodHandler func(ctx context.Context, method string, req Request) (Result, error)

// Middleware wraps a MethodHandler.
type Middleware func(MethodHandler) MethodHandler

// ToolHandler handles tools/call for one tool. An error it returns is a
// protocol error; see AddTool for handlers whose errors are tool errors.
type ToolHandler func(context.Context, *CallToolRequest) (*CallToolResult, error)

// ResourceHandler reads a resource. A resource that does not exist is
// ResourceNotFoundError.
type ResourceHandler func(context.Context, *ReadResourceRequest) (*ReadResourceResult, error)

// PromptHandler renders a prompt.
type PromptHandler func(context.Context, *GetPromptRequest) (*GetPromptResult, error)

// ServerOptions configures a Server.
type ServerOptions struct {
	// Instructions are sent to every client in the handshake.
	Instructions string
	// Capabilities are advertised as given, with tools, prompts and
	// resources added when the server has any. Nil advertises logging.
	Capabilities *ServerCapabilities
	// SubscribeHandler and UnsubscribeHandler accept resources/subscribe and
	// resources/unsubscribe; resource subscriptions are advertised only with
	// them. Both or neither.
	SubscribeHandler   func(context.Context, *SubscribeRequest) error
	UnsubscribeHandler func(context.Context, *UnsubscribeRequest) error
	// SupportedProtocolVersions narrows the revisions this server speaks;
	// empty speaks every revision in SupportedProtocolVersions.
	SupportedProtocolVersions []string
}

// Server is an MCP server: its tools, resources and prompts, and the
// sessions of the clients connected to it.
type Server struct {
	impl     *Implementation
	opts     ServerOptions
	versions []string

	mu         sync.Mutex
	tools      map[string]*serverTool
	resources  map[string]*serverResource
	templates  map[string]*serverResourceTemplate
	prompts    map[string]*serverPrompt
	methods    map[string]methodInfo
	handler    MethodHandler
	sessions   []*ServerSession
	resSubs    map[string]map[*ServerSession]jsonrpc.ID
	listenSubs map[string]map[*ServerSession]jsonrpc.ID // list_changed notification -> listens
}

type serverTool struct {
	tool    *Tool
	handler ToolHandler
}

type serverResource struct {
	resource *Resource
	handler  ResourceHandler
}

type serverResourceTemplate struct {
	template *ResourceTemplate
	re       *regexp.Regexp
	handler  ResourceHandler
}

type serverPrompt struct {
	prompt  *Prompt
	handler PromptHandler
}

// NewServer is a server named impl, with no tools, resources or prompts yet.
func NewServer(impl *Implementation, opts *ServerOptions) *Server {
	if impl == nil {
		panic("mcp: nil Implementation")
	}
	s := &Server{
		impl:       impl,
		tools:      map[string]*serverTool{},
		resources:  map[string]*serverResource{},
		templates:  map[string]*serverResourceTemplate{},
		prompts:    map[string]*serverPrompt{},
		methods:    maps.Clone(serverMethods),
		resSubs:    map[string]map[*ServerSession]jsonrpc.ID{},
		listenSubs: map[string]map[*ServerSession]jsonrpc.ID{},
		versions:   slices.Clone(supportedProtocolVersions),
	}
	if opts != nil {
		s.opts = *opts
	}
	if (s.opts.SubscribeHandler == nil) != (s.opts.UnsubscribeHandler == nil) {
		panic("mcp: SubscribeHandler and UnsubscribeHandler go together")
	}
	if len(s.opts.SupportedProtocolVersions) > 0 {
		for _, v := range s.opts.SupportedProtocolVersions {
			if !slices.Contains(supportedProtocolVersions, v) {
				panic(fmt.Sprintf("mcp: unsupported protocol version %q", v))
			}
		}
		s.versions = slices.DeleteFunc(s.versions, func(v string) bool {
			return !slices.Contains(s.opts.SupportedProtocolVersions, v)
		})
	}
	s.handler = s.defaultHandler
	return s
}

// AddReceivingMiddleware wraps the handling of every received message. The
// first middleware runs first: AddReceivingMiddleware(a, b) handles with
// a(b(handler)).
func (s *Server) AddReceivingMiddleware(middleware ...Middleware) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range slices.Backward(middleware) {
		s.handler = m(s.handler)
	}
}

func (s *Server) receivingHandler() MethodHandler {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handler
}

// defaultHandler runs the method a message names, after the middleware.
func (s *Server) defaultHandler(ctx context.Context, method string, req Request) (Result, error) {
	s.mu.Lock()
	info, ok := s.methods[method]
	s.mu.Unlock()
	if !ok {
		return nil, jsonrpc.ErrNotHandled
	}
	return info.handle(ctx, req)
}

// AddTool adds a tool, or replaces the one with its name. h receives the raw
// arguments: most tools want the typed AddTool.
func (s *Server) AddTool(t *Tool, h ToolHandler) {
	checkToolInputSchema(t)
	s.mu.Lock()
	s.tools[t.Name] = &serverTool{tool: t, handler: h}
	s.mu.Unlock()
	s.changed(notificationToolListChanged)
}

// AddResource adds a resource, or replaces the one with its URI.
func (s *Server) AddResource(r *Resource, h ResourceHandler) {
	if _, err := url.Parse(r.URI); err != nil {
		panic(err)
	}
	s.mu.Lock()
	s.resources[r.URI] = &serverResource{resource: r, handler: h}
	s.mu.Unlock()
	s.changed(notificationResourceListChanged)
}

// AddResourceTemplate adds a resource template, or replaces the one with its
// URI template.
func (s *Server) AddResourceTemplate(t *ResourceTemplate, h ResourceHandler) {
	re, err := compileURITemplate(t.URITemplate)
	if err != nil {
		panic(fmt.Errorf("URI template %q is invalid: %w", t.URITemplate, err))
	}
	s.mu.Lock()
	s.templates[t.URITemplate] = &serverResourceTemplate{template: t, re: re, handler: h}
	s.mu.Unlock()
	s.changed(notificationResourceListChanged)
}

// AddPrompt adds a prompt, or replaces the one with its name.
func (s *Server) AddPrompt(p *Prompt, h PromptHandler) {
	s.mu.Lock()
	s.prompts[p.Name] = &serverPrompt{prompt: p, handler: h}
	s.mu.Unlock()
	s.changed(notificationPromptListChanged)
}

// Tools lists the server's tools in name order.
func (s *Server) Tools() []*Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Tool, 0, len(s.tools))
	for _, name := range slices.Sorted(maps.Keys(s.tools)) {
		out = append(out, s.tools[name].tool)
	}
	return out
}

// Sessions yields the sessions connected now, oldest first.
func (s *Server) Sessions() iter.Seq[*ServerSession] {
	s.mu.Lock()
	sessions := slices.Clone(s.sessions)
	s.mu.Unlock()
	return slices.Values(sessions)
}

func (s *Server) addSession(ss *ServerSession) {
	s.mu.Lock()
	s.sessions = append(s.sessions, ss)
	s.mu.Unlock()
}

func (s *Server) removeSession(ss *ServerSession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessions = slices.DeleteFunc(s.sessions, func(x *ServerSession) bool { return x == ss })
	for _, subs := range s.resSubs {
		delete(subs, ss)
	}
	for _, subs := range s.listenSubs {
		delete(subs, ss)
	}
}

// capabilities are the configured ones, with tools, prompts and resources
// added for what the server has.
func (s *Server) capabilities() *ServerCapabilities {
	s.mu.Lock()
	defer s.mu.Unlock()
	var caps *ServerCapabilities
	if s.opts.Capabilities != nil {
		caps = s.opts.Capabilities.clone()
	} else {
		caps = &ServerCapabilities{Logging: &LoggingCapabilities{}}
	}
	if len(s.tools) > 0 && caps.Tools == nil {
		caps.Tools = &ToolCapabilities{ListChanged: true}
	}
	if len(s.prompts) > 0 && caps.Prompts == nil {
		caps.Prompts = &PromptCapabilities{ListChanged: true}
	}
	if len(s.resources) > 0 || len(s.templates) > 0 {
		if caps.Resources == nil {
			caps.Resources = &ResourceCapabilities{ListChanged: true}
		}
		if s.opts.SubscribeHandler != nil {
			caps.Resources.Subscribe = true
		}
	}
	return caps
}

// AddExtension advertises an extension and its settings.
func (c *ServerCapabilities) AddExtension(name string, settings map[string]any) {
	if c.Extensions == nil {
		c.Extensions = map[string]any{}
	}
	if settings == nil {
		settings = map[string]any{}
	}
	c.Extensions[name] = settings
}

// changed tells every session a list it may have read has changed. Only a
// change after a client connected reaches anyone: the servers here register
// everything before they serve.
func (s *Server) changed(notification string) {
	s.mu.Lock()
	var legacy []*ServerSession
	for _, ss := range s.sessions {
		if !ss.speaksModern() {
			legacy = append(legacy, ss)
		}
	}
	listens := maps.Clone(s.listenSubs[notification])
	s.mu.Unlock()
	if len(legacy) == 0 && len(listens) == 0 {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		for _, ss := range legacy {
			_ = ss.Notify(ctx, notification, struct{}{})
		}
		for ss, id := range listens {
			_ = ss.Notify(ctx, notification, map[string]any{"_meta": map[string]any{MetaKeySubscriptionID: id.Raw()}})
		}
	}()
}

// ResourceUpdated tells the sessions that subscribed to params.URI, and the
// listens that follow it, that it changed.
func (s *Server) ResourceUpdated(ctx context.Context, params *ResourceUpdatedNotificationParams) error {
	s.mu.Lock()
	subs := maps.Clone(s.resSubs[params.URI])
	s.mu.Unlock()
	notifyCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for ss, id := range subs {
		p := *params
		if ss.speaksModern() {
			meta := maps.Clone(p.Meta)
			if meta == nil {
				meta = Meta{}
			}
			meta[MetaKeySubscriptionID] = id.Raw()
			p.Meta = meta
		}
		_ = ss.Notify(notifyCtx, notificationResourceUpdated, &p)
	}
	return nil
}

// listTools answers tools/list: every tool, in name order.
func (s *Server) listTools(ctx context.Context, req *ListToolsRequest) (*ListToolsResult, error) {
	if err := checkCursor(req.Params.cursor()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	res := &ListToolsResult{Tools: []*Tool{}}
	for _, name := range slices.Sorted(maps.Keys(s.tools)) {
		res.Tools = append(res.Tools, s.tools[name].tool)
	}
	s.mu.Unlock()
	res.Cacheable.normalize()
	return res, nil
}

func (s *Server) callTool(ctx context.Context, req *CallToolRequest) (*CallToolResult, error) {
	s.mu.Lock()
	st, ok := s.tools[req.Params.Name]
	s.mu.Unlock()
	if !ok {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown tool %q", req.Params.Name)}
	}
	res, err := st.handler(ctx, req)
	if err != nil || res == nil {
		return res, err
	}
	req.Session.markResultType(res)
	if res.Content == nil {
		cp := *res
		cp.Content = []Content{}
		res = &cp
	}
	return res, nil
}

func (s *Server) listResources(ctx context.Context, req *ListResourcesRequest) (*ListResourcesResult, error) {
	if err := checkCursor(req.Params.cursor()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	res := &ListResourcesResult{Resources: []*Resource{}}
	for _, uri := range slices.Sorted(maps.Keys(s.resources)) {
		res.Resources = append(res.Resources, s.resources[uri].resource)
	}
	s.mu.Unlock()
	res.Cacheable.normalize()
	return res, nil
}

func (s *Server) listResourceTemplates(ctx context.Context, req *ListResourceTemplatesRequest) (*ListResourceTemplatesResult, error) {
	if err := checkCursor(req.Params.cursor()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	res := &ListResourceTemplatesResult{ResourceTemplates: []*ResourceTemplate{}}
	for _, t := range slices.Sorted(maps.Keys(s.templates)) {
		res.ResourceTemplates = append(res.ResourceTemplates, s.templates[t].template)
	}
	s.mu.Unlock()
	res.Cacheable.normalize()
	return res, nil
}

// readResource reads a listed resource, or one a template matches. An
// unknown URI is not found, whether or not it exists.
func (s *Server) readResource(ctx context.Context, req *ReadResourceRequest) (*ReadResourceResult, error) {
	uri := req.Params.URI
	handler, mimeType, ok := s.lookupResource(uri)
	if !ok {
		return nil, ResourceNotFoundError(uri)
	}
	res, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}
	if res == nil || res.Contents == nil {
		return nil, fmt.Errorf("reading resource %s: read handler returned nil information", uri)
	}
	req.Session.markResultType(res)
	res.Cacheable.normalize()
	for _, c := range res.Contents {
		if c.URI == "" {
			c.URI = uri
		}
		if c.MIMEType == "" {
			c.MIMEType = mimeType
		}
	}
	return res, nil
}

func (s *Server) lookupResource(uri string) (ResourceHandler, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r, ok := s.resources[uri]; ok {
		return r.handler, r.resource.MIMEType, true
	}
	for _, key := range slices.Sorted(maps.Keys(s.templates)) {
		t := s.templates[key]
		if t.re.MatchString(uri) {
			return t.handler, t.template.MIMEType, true
		}
	}
	return nil, "", false
}

// ResourceNotFoundError is the error a read of a missing resource answers.
func ResourceNotFoundError(uri string) error {
	data, _ := json.Marshal(map[string]string{"uri": uri})
	return &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: "Resource not found", Data: data}
}

func (s *Server) listPrompts(ctx context.Context, req *ListPromptsRequest) (*ListPromptsResult, error) {
	if err := checkCursor(req.Params.cursor()); err != nil {
		return nil, err
	}
	s.mu.Lock()
	res := &ListPromptsResult{Prompts: []*Prompt{}}
	for _, name := range slices.Sorted(maps.Keys(s.prompts)) {
		res.Prompts = append(res.Prompts, s.prompts[name].prompt)
	}
	s.mu.Unlock()
	res.Cacheable.normalize()
	return res, nil
}

func (s *Server) getPrompt(ctx context.Context, req *GetPromptRequest) (*GetPromptResult, error) {
	s.mu.Lock()
	p, ok := s.prompts[req.Params.Name]
	s.mu.Unlock()
	if !ok {
		return nil, &jsonrpc.Error{Code: jsonrpc.CodeInvalidParams, Message: fmt.Sprintf("unknown prompt %q", req.Params.Name)}
	}
	res, err := p.handler(ctx, req)
	if err == nil && res != nil {
		req.Session.markResultType(res)
	}
	return res, err
}

func (s *Server) complete(ctx context.Context, req *CompleteRequest) (*emptyResult, error) {
	if len(req.Params.Ref) == 0 || string(req.Params.Ref) == "null" {
		return nil, fmt.Errorf("%w: missing required 'ref' field", jsonrpc.ErrInvalidParams)
	}
	if req.Params.Argument.Name == "" {
		return nil, fmt.Errorf("%w: missing required 'argument.name' field", jsonrpc.ErrInvalidParams)
	}
	return nil, jsonrpc.ErrMethodNotFound
}

// discover answers server/discover: what the transport serving this request
// can speak, and what the server offers.
func (s *Server) discover(ctx context.Context, req *ServerRequest[*DiscoverParams]) (*DiscoverResult, error) {
	versions := req.Session.supportedVersions()
	// On a transport that serves 2026-07-28 the discovering client is now
	// known by what it said, as a handshake would have made it known. A
	// stateful transport keeps nothing: the session it made for this request
	// is never handed out.
	if slices.ContainsFunc(versions, func(v string) bool { return v >= ProtocolVersion20260728 }) {
		var meta Meta
		if req.Params != nil {
			meta = req.Params.Meta
		}
		init := &InitializeParams{}
		init.ProtocolVersion, _ = meta[MetaKeyProtocolVersion].(string)
		_ = remarshal(meta[MetaKeyClientInfo], &init.ClientInfo)
		_ = remarshal(meta[MetaKeyClientCapabilities], &init.Capabilities)
		req.Session.setInitialize(init)
	}
	res := &DiscoverResult{
		SupportedVersions: versions,
		Capabilities:      s.capabilities(),
		Instructions:      s.opts.Instructions,
	}
	res.Cacheable.normalize()
	return res, nil
}

func (s *Server) subscribe(ctx context.Context, req *SubscribeRequest) (*emptyResult, error) {
	id, ok := requestIDFrom(ctx, req.Session)
	if !ok {
		return nil, fmt.Errorf("%w: subscribe requires a request ID", jsonrpc.ErrInvalidRequest)
	}
	if s.opts.SubscribeHandler == nil {
		return nil, fmt.Errorf("%w: server does not support resource subscriptions", jsonrpc.ErrMethodNotFound)
	}
	if err := s.opts.SubscribeHandler(ctx, req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.resSubs[req.Params.URI] == nil {
		s.resSubs[req.Params.URI] = map[*ServerSession]jsonrpc.ID{}
	}
	s.resSubs[req.Params.URI][req.Session] = id
	return &emptyResult{}, nil
}

func (s *Server) unsubscribe(ctx context.Context, req *UnsubscribeRequest) (*emptyResult, error) {
	if s.opts.UnsubscribeHandler == nil {
		return nil, jsonrpc.ErrMethodNotFound
	}
	if err := s.opts.UnsubscribeHandler(ctx, req); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if subs, ok := s.resSubs[req.Params.URI]; ok {
		delete(subs, req.Session)
		if len(subs) == 0 {
			delete(s.resSubs, req.Params.URI)
		}
	}
	return &emptyResult{}, nil
}

// subscriptionsListen answers subscriptions/listen: it acknowledges what the
// server will send, then holds the request open, notifying on its stream,
// until the client goes away. A listen for nothing returns at once.
func (s *Server) subscriptionsListen(ctx context.Context, req *SubscriptionsListenRequest) (*SubscriptionsListenResult, error) {
	id, ok := requestIDFrom(ctx, req.Session)
	if !ok {
		return nil, fmt.Errorf("%w: subscriptions/listen requires a request ID", jsonrpc.ErrInvalidRequest)
	}
	if req.Params.Notifications == nil {
		return nil, fmt.Errorf("%w: missing required 'notifications' field", jsonrpc.ErrInvalidParams)
	}
	allowed := s.allowedSubscriptions(req.Params.Notifications)
	listChanged := map[string]bool{
		notificationToolListChanged:     allowed.ToolsListChanged,
		notificationPromptListChanged:   allowed.PromptsListChanged,
		notificationResourceListChanged: allowed.ResourcesListChanged,
	}
	s.mu.Lock()
	for n, on := range listChanged {
		if on {
			if s.listenSubs[n] == nil {
				s.listenSubs[n] = map[*ServerSession]jsonrpc.ID{}
			}
			s.listenSubs[n][req.Session] = id
		}
	}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		for n := range listChanged {
			delete(s.listenSubs[n], req.Session)
		}
		s.mu.Unlock()
	}()

	for _, uri := range allowed.ResourceSubscriptions {
		sub := &SubscribeRequest{Session: req.Session, Params: &SubscribeParams{URI: uri, Meta: req.Params.GetMeta()}}
		if _, err := s.subscribe(ctx, sub); err != nil {
			return nil, err
		}
		defer s.unsubscribe(ctx, &UnsubscribeRequest{Session: req.Session, Params: &UnsubscribeParams{URI: uri, Meta: req.Params.GetMeta()}})
	}

	ack := &subscriptionsAcknowledgedParams{Notifications: allowed, Meta: Meta{MetaKeySubscriptionID: id.Raw()}}
	if err := req.Session.Notify(ctx, notificationSubscriptionsAck, ack); err != nil {
		return nil, fmt.Errorf("sending subscriptions/acknowledged: %w", err)
	}
	if len(allowed.ResourceSubscriptions) > 0 || allowed.ToolsListChanged || allowed.PromptsListChanged || allowed.ResourcesListChanged {
		<-ctx.Done()
	}
	return &SubscriptionsListenResult{Meta: Meta{MetaKeySubscriptionID: id.Raw()}}, nil
}

// allowedSubscriptions is the part of what a listen wants that the server
// can send.
func (s *Server) allowedSubscriptions(want *NotificationSubscriptions) NotificationSubscriptions {
	caps := s.capabilities()
	var agreed NotificationSubscriptions
	agreed.ToolsListChanged = want.ToolsListChanged && caps.Tools != nil && caps.Tools.ListChanged
	agreed.PromptsListChanged = want.PromptsListChanged && caps.Prompts != nil && caps.Prompts.ListChanged
	agreed.ResourcesListChanged = want.ResourcesListChanged && caps.Resources != nil && caps.Resources.ListChanged
	if len(want.ResourceSubscriptions) > 0 && caps.Resources != nil && caps.Resources.Subscribe {
		agreed.ResourceSubscriptions = slices.Clone(want.ResourceSubscriptions)
	}
	return agreed
}

// checkCursor refuses a cursor: every list here fits on one page, so no
// cursor was ever handed out.
func checkCursor(cursor string) error {
	if cursor != "" {
		return jsonrpc.ErrInvalidParams
	}
	return nil
}

func (p *ListToolsParams) cursor() string {
	if p == nil {
		return ""
	}
	return p.Cursor
}

func (p *ListResourcesParams) cursor() string {
	if p == nil {
		return ""
	}
	return p.Cursor
}

func (p *ListResourceTemplatesParams) cursor() string {
	if p == nil {
		return ""
	}
	return p.Cursor
}

func (p *ListPromptsParams) cursor() string {
	if p == nil {
		return ""
	}
	return p.Cursor
}
