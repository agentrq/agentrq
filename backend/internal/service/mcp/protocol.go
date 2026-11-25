// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"encoding/json"
	"net/http"
)

// The wire types of the protocol revisions this server speaks. Field order
// and tags are the wire's: a client reads these, and a change to one is a
// change to what every client receives.

// Meta is the _meta object of params and results.
type Meta map[string]any

// GetMeta returns the metadata.
func (m Meta) GetMeta() map[string]any { return m }

// SetMeta replaces the metadata.
func (m *Meta) SetMeta(x map[string]any) { *m = x }

// Params is a request's or a notification's params.
type Params interface {
	GetMeta() map[string]any
	SetMeta(map[string]any)
	isParams()
	isNil() bool
}

// Result is a request's result.
type Result interface {
	GetMeta() map[string]any
	SetMeta(map[string]any)
	isResult()
}

// ParamsBase makes a struct the params of a custom method.
type ParamsBase struct {
	Meta `json:"_meta,omitempty"`
}

func (*ParamsBase) isParams()     {}
func (p *ParamsBase) isNil() bool { return p == nil }
func (*ResultBase) isResult()     {}
func (*emptyResult) isResult()    {}

// ResultBase makes a struct the result of a custom method.
type ResultBase struct {
	Meta `json:"_meta,omitempty"`
}

// emptyResult is the {} that answers ping and logging/setLevel.
type emptyResult struct{}

func (*emptyResult) GetMeta() map[string]any { return nil }
func (*emptyResult) SetMeta(map[string]any)  {}

// resultType is "complete" on every result a 2026-07-28 request receives.
type resultType string

const resultTypeComplete resultType = "complete"

// completeResultWithType carries resultType at the head of a list result.
type completeResultWithType struct {
	ResultType resultType `json:"resultType,omitempty"`
}

func (r *completeResultWithType) setResultType(rt resultType) { r.ResultType = rt }

// resultTyped is a result that says whether it is complete.
type resultTyped interface {
	setResultType(resultType)
}

// Cacheable is the cache hint of list results, resources/read and
// server/discover.
type Cacheable struct {
	TTLMs      int    `json:"ttlMs"`
	CacheScope string `json:"cacheScope"`
}

// normalize fills the scope the protocol defaults to.
func (c *Cacheable) normalize() {
	if c.CacheScope == "" {
		c.CacheScope = "public"
	}
}

// Implementation names a client or a server.
type Implementation struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Version     string `json:"version"`
	WebsiteURL  string `json:"websiteUrl,omitempty"`
	Icons       []Icon `json:"icons,omitempty"`
}

// Icon is an image naming a server, tool, resource or prompt.
type Icon struct {
	Source   string   `json:"src"`
	MIMEType string   `json:"mimeType,omitempty"`
	Sizes    []string `json:"sizes,omitempty"`
	Theme    string   `json:"theme,omitempty"`
}

// Annotations hint how a client may use a resource or content.
type Annotations struct {
	Audience     []string `json:"audience,omitempty"`
	LastModified string   `json:"lastModified,omitempty"`
	Priority     float64  `json:"priority,omitempty"`
}

// ServerCapabilities is what a server says it can do.
type ServerCapabilities struct {
	Experimental map[string]any `json:"experimental,omitempty"`
	Extensions   map[string]any `json:"extensions,omitempty"`
	// A2UI is where A2UI's MCP binding advertises the cards a side draws.
	A2UI        map[string]any          `json:"a2ui,omitempty"`
	Completions *CompletionCapabilities `json:"completions,omitempty"`
	Logging     *LoggingCapabilities    `json:"logging,omitempty"`
	Prompts     *PromptCapabilities     `json:"prompts,omitempty"`
	Resources   *ResourceCapabilities   `json:"resources,omitempty"`
	Tools       *ToolCapabilities       `json:"tools,omitempty"`
}

// clone is a copy that changing does not change c.
func (c *ServerCapabilities) clone() *ServerCapabilities {
	cp := *c
	cp.Experimental = shallowClone(c.Experimental)
	cp.Extensions = shallowClone(c.Extensions)
	cp.A2UI = shallowClone(c.A2UI)
	cp.Completions = shallowClonePtr(c.Completions)
	cp.Logging = shallowClonePtr(c.Logging)
	cp.Prompts = shallowClonePtr(c.Prompts)
	cp.Resources = shallowClonePtr(c.Resources)
	cp.Tools = shallowClonePtr(c.Tools)
	return &cp
}

func shallowClone(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	cp := make(map[string]any, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func shallowClonePtr[T any](p *T) *T {
	if p == nil {
		return nil
	}
	cp := *p
	return &cp
}

// CompletionCapabilities is the completions capability.
type CompletionCapabilities struct{}

// LoggingCapabilities is the logging capability.
type LoggingCapabilities struct{}

// PromptCapabilities is the prompts capability.
type PromptCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourceCapabilities is the resources capability.
type ResourceCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
	Subscribe   bool `json:"subscribe,omitempty"`
}

// ToolCapabilities is the tools capability.
type ToolCapabilities struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ClientCapabilities is what a client says it can do. Only its shape is
// kept: the server never asks a client for anything.
type ClientCapabilities struct {
	Experimental map[string]any  `json:"experimental,omitempty"`
	Extensions   map[string]any  `json:"extensions,omitempty"`
	Roots        json.RawMessage `json:"roots,omitempty"`
	Sampling     json.RawMessage `json:"sampling,omitempty"`
	Elicitation  json.RawMessage `json:"elicitation,omitempty"`
}

// InitializeParams is the client's half of the handshake.
type InitializeParams struct {
	Meta            `json:"_meta,omitempty"`
	Capabilities    *ClientCapabilities `json:"capabilities"`
	ClientInfo      *Implementation     `json:"clientInfo"`
	ProtocolVersion string              `json:"protocolVersion"`
}

// InitializeResult is the server's half of the handshake.
type InitializeResult struct {
	Meta            `json:"_meta,omitempty"`
	Capabilities    *ServerCapabilities `json:"capabilities"`
	Instructions    string              `json:"instructions,omitempty"`
	ProtocolVersion string              `json:"protocolVersion"`
	ServerInfo      *Implementation     `json:"serverInfo"`
}

// InitializedParams are notifications/initialized' params.
type InitializedParams struct {
	Meta `json:"_meta,omitempty"`
}

// PingParams are ping's params.
type PingParams struct {
	Meta `json:"_meta,omitempty"`
}

// CancelledParams are notifications/cancelled' params.
type CancelledParams struct {
	Meta      `json:"_meta,omitempty"`
	Reason    string `json:"reason,omitempty"`
	RequestID any    `json:"requestId"`
}

// ProgressNotificationParams are notifications/progress' params.
type ProgressNotificationParams struct {
	Meta          `json:"_meta,omitempty"`
	ProgressToken any     `json:"progressToken"`
	Message       string  `json:"message,omitempty"`
	Progress      float64 `json:"progress"`
	Total         float64 `json:"total,omitempty"`
}

// RootsListChangedParams are notifications/roots/list_changed' params.
type RootsListChangedParams struct {
	Meta `json:"_meta,omitempty"`
}

// SetLoggingLevelParams are logging/setLevel's params.
type SetLoggingLevelParams struct {
	Meta  `json:"_meta,omitempty"`
	Level string `json:"level"`
}

// LoggingMessageParams are notifications/message' params.
type LoggingMessageParams struct {
	Meta   `json:"_meta,omitempty"`
	Data   any    `json:"data"`
	Level  string `json:"level"`
	Logger string `json:"logger,omitempty"`
}

// CompleteParams are completion/complete's params.
type CompleteParams struct {
	Meta     `json:"_meta,omitempty"`
	Argument struct {
		Name  string `json:"name"`
		Value string `json:"value"`
	} `json:"argument"`
	Context json.RawMessage `json:"context,omitempty"`
	Ref     json.RawMessage `json:"ref"`
}

// Tool is a tool's listing.
type Tool struct {
	Meta         `json:"_meta,omitempty"`
	Annotations  *ToolAnnotations `json:"annotations,omitempty"`
	Description  string           `json:"description,omitempty"`
	InputSchema  any              `json:"inputSchema"`
	Name         string           `json:"name"`
	OutputSchema any              `json:"outputSchema,omitempty"`
	Title        string           `json:"title,omitempty"`
	Icons        []Icon           `json:"icons,omitempty"`
}

// ToolAnnotations hint how a client may run a tool.
type ToolAnnotations struct {
	DestructiveHint *bool  `json:"destructiveHint,omitempty"`
	IdempotentHint  bool   `json:"idempotentHint"`
	OpenWorldHint   *bool  `json:"openWorldHint,omitempty"`
	ReadOnlyHint    bool   `json:"readOnlyHint"`
	Title           string `json:"title,omitempty"`
}

// ListToolsParams are tools/list's params.
type ListToolsParams struct {
	Meta   `json:"_meta,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// ListToolsResult is tools/list's result.
type ListToolsResult struct {
	completeResultWithType
	Meta `json:"_meta,omitempty"`
	Cacheable
	NextCursor string  `json:"nextCursor,omitempty"`
	Tools      []*Tool `json:"tools"`
}

// CallToolParams are tools/call's params, as a client builds them.
type CallToolParams struct {
	Meta      `json:"_meta,omitempty"`
	Name      string `json:"name"`
	Arguments any    `json:"arguments,omitempty"`
}

// CallToolParamsRaw are tools/call's params, as the server receives them.
type CallToolParamsRaw struct {
	Meta      `json:"_meta,omitempty"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

// CallToolResult is tools/call's result.
type CallToolResult struct {
	Meta              `json:"_meta,omitempty"`
	Content           []Content `json:"content"`
	StructuredContent any       `json:"structuredContent,omitempty"`
	IsError           bool      `json:"isError,omitempty"`

	resultType resultType
	err        error
}

// SetError marks the result as a failed call: err's text becomes the content
// unless content was already given.
func (r *CallToolResult) SetError(err error) {
	if len(r.Content) == 0 {
		r.Content = []Content{&TextContent{Text: err.Error()}}
	}
	r.IsError = true
	r.err = err
}

// GetError is the error SetError recorded.
func (r *CallToolResult) GetError() error { return r.err }

func (r *CallToolResult) setResultType(rt resultType) { r.resultType = rt }

// MarshalJSON writes resultType last, after the fields.
func (r *CallToolResult) MarshalJSON() ([]byte, error) {
	type plain CallToolResult
	return json.Marshal(struct {
		plain
		ResultType resultType `json:"resultType,omitempty"`
	}{plain(*r), r.resultType})
}

// UnmarshalJSON reads content blocks into their types.
func (r *CallToolResult) UnmarshalJSON(data []byte) error {
	type plain CallToolResult
	var w struct {
		plain
		Content    []*wireContent `json:"content"`
		ResultType resultType     `json:"resultType"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	content, err := contentsFromWire(w.Content)
	if err != nil {
		return err
	}
	*r = CallToolResult(w.plain)
	r.Content = content
	r.resultType = w.ResultType
	return nil
}

// Resource is a resource's listing.
type Resource struct {
	Meta        `json:"_meta,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
	Description string       `json:"description,omitempty"`
	MIMEType    string       `json:"mimeType,omitempty"`
	Name        string       `json:"name"`
	Size        int64        `json:"size,omitempty"`
	Title       string       `json:"title,omitempty"`
	URI         string       `json:"uri"`
	Icons       []Icon       `json:"icons,omitempty"`
}

// ResourceTemplate is a family of resources named by a URI template.
type ResourceTemplate struct {
	Meta        `json:"_meta,omitempty"`
	Annotations *Annotations `json:"annotations,omitempty"`
	Description string       `json:"description,omitempty"`
	MIMEType    string       `json:"mimeType,omitempty"`
	Name        string       `json:"name"`
	Title       string       `json:"title,omitempty"`
	URITemplate string       `json:"uriTemplate"`
	Icons       []Icon       `json:"icons,omitempty"`
}

// ResourceContents is what a read returns for one resource.
type ResourceContents struct {
	URI      string `json:"uri"`
	MIMEType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
	Blob     []byte `json:"blob,omitzero"`
	Meta     Meta   `json:"_meta,omitempty"`
}

// ListResourcesParams are resources/list's params.
type ListResourcesParams struct {
	Meta   `json:"_meta,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// ListResourcesResult is resources/list's result.
type ListResourcesResult struct {
	completeResultWithType
	Meta `json:"_meta,omitempty"`
	Cacheable
	NextCursor string      `json:"nextCursor,omitempty"`
	Resources  []*Resource `json:"resources"`
}

// ListResourceTemplatesParams are resources/templates/list's params.
type ListResourceTemplatesParams struct {
	Meta   `json:"_meta,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// ListResourceTemplatesResult is resources/templates/list's result.
type ListResourceTemplatesResult struct {
	completeResultWithType
	Meta `json:"_meta,omitempty"`
	Cacheable
	NextCursor        string              `json:"nextCursor,omitempty"`
	ResourceTemplates []*ResourceTemplate `json:"resourceTemplates"`
}

// ReadResourceParams are resources/read's params.
type ReadResourceParams struct {
	Meta `json:"_meta,omitempty"`
	URI  string `json:"uri"`
}

// ReadResourceResult is resources/read's result.
type ReadResourceResult struct {
	Meta `json:"_meta,omitempty"`
	Cacheable
	Contents []*ResourceContents `json:"contents"`

	resultType resultType
}

func (r *ReadResourceResult) setResultType(rt resultType) { r.resultType = rt }

// MarshalJSON writes resultType last, after the fields.
func (r *ReadResourceResult) MarshalJSON() ([]byte, error) {
	type plain ReadResourceResult
	return json.Marshal(struct {
		plain
		ResultType resultType `json:"resultType,omitempty"`
	}{plain(*r), r.resultType})
}

// UnmarshalJSON reads resultType back.
func (r *ReadResourceResult) UnmarshalJSON(data []byte) error {
	type plain ReadResourceResult
	var w struct {
		plain
		ResultType resultType `json:"resultType"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*r = ReadResourceResult(w.plain)
	r.resultType = w.ResultType
	return nil
}

// SubscribeParams are resources/subscribe's params.
type SubscribeParams struct {
	Meta `json:"_meta,omitempty"`
	URI  string `json:"uri"`
}

// UnsubscribeParams are resources/unsubscribe's params.
type UnsubscribeParams struct {
	Meta `json:"_meta,omitempty"`
	URI  string `json:"uri"`
}

// ResourceUpdatedNotificationParams are notifications/resources/updated'
// params.
type ResourceUpdatedNotificationParams struct {
	Meta `json:"_meta,omitempty"`
	URI  string `json:"uri"`
}

// Prompt is a prompt's listing.
type Prompt struct {
	Meta        `json:"_meta,omitempty"`
	Arguments   []*PromptArgument `json:"arguments,omitempty"`
	Description string            `json:"description,omitempty"`
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Icons       []Icon            `json:"icons,omitempty"`
}

// PromptArgument is one of a prompt's arguments.
type PromptArgument struct {
	Name        string `json:"name"`
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// PromptMessage is one message of a prompt.
type PromptMessage struct {
	Content Content `json:"content"`
	Role    string  `json:"role"`
}

// UnmarshalJSON reads the content block into its type.
func (m *PromptMessage) UnmarshalJSON(data []byte) error {
	var w struct {
		Content *wireContent `json:"content"`
		Role    string       `json:"role"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	content, err := contentFromWire(w.Content)
	if err != nil {
		return err
	}
	m.Content, m.Role = content, w.Role
	return nil
}

// ListPromptsParams are prompts/list's params.
type ListPromptsParams struct {
	Meta   `json:"_meta,omitempty"`
	Cursor string `json:"cursor,omitempty"`
}

// ListPromptsResult is prompts/list's result.
type ListPromptsResult struct {
	completeResultWithType
	Meta `json:"_meta,omitempty"`
	Cacheable
	NextCursor string    `json:"nextCursor,omitempty"`
	Prompts    []*Prompt `json:"prompts"`
}

// GetPromptParams are prompts/get's params.
type GetPromptParams struct {
	Meta      `json:"_meta,omitempty"`
	Arguments map[string]string `json:"arguments,omitempty"`
	Name      string            `json:"name"`
}

// GetPromptResult is prompts/get's result.
type GetPromptResult struct {
	Meta        `json:"_meta,omitempty"`
	Description string           `json:"description,omitempty"`
	Messages    []*PromptMessage `json:"messages"`

	resultType resultType
}

func (r *GetPromptResult) setResultType(rt resultType) { r.resultType = rt }

// MarshalJSON writes resultType last, after the fields.
func (r *GetPromptResult) MarshalJSON() ([]byte, error) {
	type plain GetPromptResult
	return json.Marshal(struct {
		plain
		ResultType resultType `json:"resultType,omitempty"`
	}{plain(*r), r.resultType})
}

// UnmarshalJSON reads resultType back.
func (r *GetPromptResult) UnmarshalJSON(data []byte) error {
	type plain GetPromptResult
	var w struct {
		plain
		ResultType resultType `json:"resultType"`
	}
	if err := json.Unmarshal(data, &w); err != nil {
		return err
	}
	*r = GetPromptResult(w.plain)
	r.resultType = w.ResultType
	return nil
}

// DiscoverParams are server/discover's params.
type DiscoverParams struct {
	Meta `json:"_meta,omitempty"`
}

// DiscoverResult is server/discover's result.
type DiscoverResult struct {
	completeResultWithType
	Meta `json:"_meta,omitempty"`
	Cacheable
	SupportedVersions []string            `json:"supportedVersions"`
	Capabilities      *ServerCapabilities `json:"capabilities"`
	Instructions      string              `json:"instructions,omitempty"`
}

// NotificationSubscriptions is what a subscriptions/listen asks to hear.
type NotificationSubscriptions struct {
	ToolsListChanged      bool     `json:"toolsListChanged,omitempty"`
	PromptsListChanged    bool     `json:"promptsListChanged,omitempty"`
	ResourcesListChanged  bool     `json:"resourcesListChanged,omitempty"`
	ResourceSubscriptions []string `json:"resourceSubscriptions,omitempty"`
}

// SubscriptionsListenParams are subscriptions/listen's params.
type SubscriptionsListenParams struct {
	Meta          `json:"_meta,omitempty"`
	Notifications *NotificationSubscriptions `json:"notifications"`
}

// subscriptionsAcknowledgedParams are what a listen was granted.
type subscriptionsAcknowledgedParams struct {
	Meta          `json:"_meta,omitempty"`
	Notifications NotificationSubscriptions `json:"notifications"`
}

// SubscriptionsListenResult ends a listen.
type SubscriptionsListenResult struct {
	completeResultWithType
	Meta `json:"_meta"`
}

// UnsupportedProtocolVersionData is the data of a -32022 error: what the
// server can speak instead.
type UnsupportedProtocolVersionData struct {
	Supported []string `json:"supported"`
	Requested string   `json:"requested"`
}

// RequestExtra is what the transport knows about a request.
type RequestExtra struct {
	Header http.Header
}

func (*InitializeParams) isParams()                  {}
func (p *InitializeParams) isNil() bool              { return p == nil }
func (*InitializedParams) isParams()                 {}
func (p *InitializedParams) isNil() bool             { return p == nil }
func (*PingParams) isParams()                        {}
func (p *PingParams) isNil() bool                    { return p == nil }
func (*CancelledParams) isParams()                   {}
func (p *CancelledParams) isNil() bool               { return p == nil }
func (*ProgressNotificationParams) isParams()        {}
func (p *ProgressNotificationParams) isNil() bool    { return p == nil }
func (*RootsListChangedParams) isParams()            {}
func (p *RootsListChangedParams) isNil() bool        { return p == nil }
func (*SetLoggingLevelParams) isParams()             {}
func (p *SetLoggingLevelParams) isNil() bool         { return p == nil }
func (*LoggingMessageParams) isParams()              {}
func (p *LoggingMessageParams) isNil() bool          { return p == nil }
func (*CompleteParams) isParams()                    {}
func (p *CompleteParams) isNil() bool                { return p == nil }
func (*ListToolsParams) isParams()                   {}
func (p *ListToolsParams) isNil() bool               { return p == nil }
func (*CallToolParams) isParams()                    {}
func (p *CallToolParams) isNil() bool                { return p == nil }
func (*CallToolParamsRaw) isParams()                 {}
func (p *CallToolParamsRaw) isNil() bool             { return p == nil }
func (*ListResourcesParams) isParams()               {}
func (p *ListResourcesParams) isNil() bool           { return p == nil }
func (*ListResourceTemplatesParams) isParams()       {}
func (p *ListResourceTemplatesParams) isNil() bool   { return p == nil }
func (*ReadResourceParams) isParams()                {}
func (p *ReadResourceParams) isNil() bool            { return p == nil }
func (*SubscribeParams) isParams()                   {}
func (p *SubscribeParams) isNil() bool               { return p == nil }
func (*UnsubscribeParams) isParams()                 {}
func (p *UnsubscribeParams) isNil() bool             { return p == nil }
func (*ResourceUpdatedNotificationParams) isParams() {}
func (p *ResourceUpdatedNotificationParams) isNil() bool {
	return p == nil
}
func (*ListPromptsParams) isParams()                   {}
func (p *ListPromptsParams) isNil() bool               { return p == nil }
func (*GetPromptParams) isParams()                     {}
func (p *GetPromptParams) isNil() bool                 { return p == nil }
func (*DiscoverParams) isParams()                      {}
func (p *DiscoverParams) isNil() bool                  { return p == nil }
func (*SubscriptionsListenParams) isParams()           {}
func (p *SubscriptionsListenParams) isNil() bool       { return p == nil }
func (*subscriptionsAcknowledgedParams) isParams()     {}
func (p *subscriptionsAcknowledgedParams) isNil() bool { return p == nil }

func (*InitializeResult) isResult()            {}
func (*ListToolsResult) isResult()             {}
func (*CallToolResult) isResult()              {}
func (*ListResourcesResult) isResult()         {}
func (*ListResourceTemplatesResult) isResult() {}
func (*ReadResourceResult) isResult()          {}
func (*ListPromptsResult) isResult()           {}
func (*GetPromptResult) isResult()             {}
func (*DiscoverResult) isResult()              {}
func (*SubscriptionsListenResult) isResult()   {}

// The custom-wire Results above take Meta from their embedded field, so they
// satisfy Result; emptyResult answers with nothing.
var (
	_ Result = (*CallToolResult)(nil)
	_ Result = (*ReadResourceResult)(nil)
	_ Result = (*GetPromptResult)(nil)
	_ Result = (*emptyResult)(nil)
	_ Params = (*LoggingMessageParams)(nil)
)
