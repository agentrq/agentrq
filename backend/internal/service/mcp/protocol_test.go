// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// A text block always carries "text", even empty, because the protocol
// requires it.
func TestTextContentAlwaysWritesText(t *testing.T) {
	data, err := (&TextContent{}).MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"type":"text","text":""}` {
		t.Errorf("empty text block = %s", data)
	}
	data, _ = (&TextContent{Text: "<hi>", Meta: Meta{"k": 1}, Annotations: &Annotations{Priority: 1}}).MarshalJSON()
	if string(data) != `{"type":"text","text":"\u003chi\u003e","_meta":{"k":1},"annotations":{"priority":1}}` {
		t.Errorf("text block = %s", data)
	}
	(&TextContent{}).isContent()
}

// Only text content is read back; anything else is refused by type.
func TestContentFromWire(t *testing.T) {
	if _, err := contentFromWire(nil); err == nil {
		t.Error("a missing block was read")
	}
	if _, err := contentFromWire(&wireContent{Type: "image"}); err == nil || !strings.Contains(err.Error(), `"image"`) {
		t.Errorf("an image block = %v, want it refused by type", err)
	}
	c, err := contentFromWire(&wireContent{Type: "text", Text: "x", Meta: Meta{"a": 1}, Annotations: &Annotations{}})
	if err != nil || c.(*TextContent).Text != "x" {
		t.Errorf("a text block read as %v, %v", c, err)
	}
	if _, err := contentsFromWire([]*wireContent{{Type: "text"}, {Type: "audio"}}); err == nil {
		t.Error("a list with an unknown block was read")
	}
}

// Meta is read and replaced whole.
func TestMeta(t *testing.T) {
	var m Meta
	m.SetMeta(map[string]any{"a": 1})
	if m.GetMeta()["a"] != 1 {
		t.Errorf("meta = %v", m)
	}
	var e emptyResult
	e.SetMeta(map[string]any{"a": 1})
	if e.GetMeta() != nil {
		t.Error("the empty result kept metadata")
	}
	var c completeResultWithType
	c.setResultType(resultTypeComplete)
	if c.ResultType != resultTypeComplete {
		t.Error("resultType not set")
	}
}

// Every params type says whether it is nil, which is how a typed nil is
// told apart from params that were sent.
func TestParamsNil(t *testing.T) {
	params := []Params{
		(*ParamsBase)(nil), (*InitializeParams)(nil), (*InitializedParams)(nil), (*PingParams)(nil),
		(*CancelledParams)(nil), (*ProgressNotificationParams)(nil), (*RootsListChangedParams)(nil),
		(*SetLoggingLevelParams)(nil), (*LoggingMessageParams)(nil), (*CompleteParams)(nil),
		(*ListToolsParams)(nil), (*CallToolParams)(nil), (*CallToolParamsRaw)(nil),
		(*ListResourcesParams)(nil), (*ListResourceTemplatesParams)(nil), (*ReadResourceParams)(nil),
		(*SubscribeParams)(nil), (*UnsubscribeParams)(nil), (*ResourceUpdatedNotificationParams)(nil),
		(*ListPromptsParams)(nil), (*GetPromptParams)(nil), (*DiscoverParams)(nil),
		(*SubscriptionsListenParams)(nil), (*subscriptionsAcknowledgedParams)(nil),
	}
	for _, p := range params {
		p.isParams()
		if !p.isNil() {
			t.Errorf("%T: a nil pointer is not nil", p)
		}
		set := reflect.New(reflect.TypeOf(p).Elem()).Interface().(Params)
		if set.isNil() {
			t.Errorf("%T: a value is nil", p)
		}
	}
	results := []Result{
		&ResultBase{}, &emptyResult{}, &InitializeResult{}, &ListToolsResult{}, &CallToolResult{},
		&ListResourcesResult{}, &ListResourceTemplatesResult{}, &ReadResourceResult{}, &ListPromptsResult{},
		&GetPromptResult{}, &DiscoverResult{}, &SubscriptionsListenResult{},
	}
	for _, r := range results {
		r.isResult()
	}
}

// The protocol's default scope is filled in, and a scope given is kept.
func TestCacheableNormalize(t *testing.T) {
	var c Cacheable
	c.normalize()
	if c.CacheScope != "public" {
		t.Errorf("scope = %q, want public", c.CacheScope)
	}
	c = Cacheable{CacheScope: "private"}
	c.normalize()
	if c.CacheScope != "private" {
		t.Errorf("scope = %q, want private kept", c.CacheScope)
	}
}

// A copy of the capabilities can be changed without changing the original.
func TestServerCapabilitiesClone(t *testing.T) {
	orig := &ServerCapabilities{
		Experimental: map[string]any{"a": 1},
		Extensions:   map[string]any{"b": 2},
		Completions:  &CompletionCapabilities{},
		Logging:      &LoggingCapabilities{},
		Prompts:      &PromptCapabilities{ListChanged: true},
		Resources:    &ResourceCapabilities{},
		Tools:        &ToolCapabilities{},
	}
	cp := orig.clone()
	cp.Experimental["a"] = 9
	cp.Extensions["c"] = 3
	cp.Prompts.ListChanged = false
	cp.Resources.Subscribe = true
	cp.Tools.ListChanged = true
	if orig.Experimental["a"] != 1 || len(orig.Extensions) != 1 || !orig.Prompts.ListChanged || orig.Resources.Subscribe || orig.Tools.ListChanged {
		t.Errorf("changing the clone changed the original: %+v", orig)
	}
	empty := (&ServerCapabilities{}).clone()
	if empty.Experimental != nil || empty.Tools != nil {
		t.Errorf("a clone of nothing grew fields: %+v", empty)
	}
}

// A failed call carries its error's text, unless content was already given.
func TestCallToolResultSetError(t *testing.T) {
	var r CallToolResult
	r.SetError(errors.New("boom"))
	if !r.IsError || r.Content[0].(*TextContent).Text != "boom" || r.GetError().Error() != "boom" {
		t.Errorf("result = %+v", r)
	}
	r = CallToolResult{Content: []Content{&TextContent{Text: "friendly"}}}
	r.SetError(errors.New("boom"))
	if r.Content[0].(*TextContent).Text != "friendly" {
		t.Error("given content was replaced by the error")
	}
}

// resultType goes after every other field, and only when set; it reads back.
func TestCustomWireResults(t *testing.T) {
	ctr := &CallToolResult{Content: []Content{&TextContent{Text: "a"}}, IsError: true}
	data, _ := json.Marshal(ctr)
	if string(data) != `{"content":[{"type":"text","text":"a"}],"isError":true}` {
		t.Errorf("call result = %s", data)
	}
	ctr.setResultType(resultTypeComplete)
	data, _ = json.Marshal(ctr)
	if string(data) != `{"content":[{"type":"text","text":"a"}],"isError":true,"resultType":"complete"}` {
		t.Errorf("call result with type = %s", data)
	}
	var back CallToolResult
	if err := json.Unmarshal(data, &back); err != nil || back.resultType != resultTypeComplete || back.Content[0].(*TextContent).Text != "a" || !back.IsError {
		t.Errorf("call result read back as %+v, %v", back, err)
	}
	if err := json.Unmarshal([]byte(`{"content":5}`), &back); err == nil {
		t.Error("a malformed call result was read")
	}
	if err := json.Unmarshal([]byte(`{"content":[{"type":"image"}]}`), &back); err == nil {
		t.Error("a call result with an unknown block was read")
	}

	rr := &ReadResourceResult{Contents: []*ResourceContents{{URI: "x://a", Text: "t"}}}
	rr.setResultType(resultTypeComplete)
	data, _ = json.Marshal(rr)
	if string(data) != `{"ttlMs":0,"cacheScope":"","contents":[{"uri":"x://a","text":"t"}],"resultType":"complete"}` {
		t.Errorf("read result = %s", data)
	}
	var rback ReadResourceResult
	if err := json.Unmarshal(data, &rback); err != nil || rback.resultType != resultTypeComplete || rback.Contents[0].URI != "x://a" {
		t.Errorf("read result read back as %+v, %v", rback, err)
	}
	if err := json.Unmarshal([]byte(`{"contents":5}`), &rback); err == nil {
		t.Error("a malformed read result was read")
	}

	gr := &GetPromptResult{Description: "d", Messages: []*PromptMessage{{Content: &TextContent{Text: "m"}, Role: "user"}}}
	gr.setResultType(resultTypeComplete)
	data, _ = json.Marshal(gr)
	if string(data) != `{"description":"d","messages":[{"content":{"type":"text","text":"m"},"role":"user"}],"resultType":"complete"}` {
		t.Errorf("prompt result = %s", data)
	}
	var gback GetPromptResult
	if err := json.Unmarshal(data, &gback); err != nil || gback.resultType != resultTypeComplete || gback.Messages[0].Content.(*TextContent).Text != "m" {
		t.Errorf("prompt result read back as %+v, %v", gback, err)
	}
	if err := json.Unmarshal([]byte(`{"messages":5}`), &gback); err == nil {
		t.Error("a malformed prompt result was read")
	}
	var msg PromptMessage
	if err := json.Unmarshal([]byte(`[1]`), &msg); err == nil {
		t.Error("a malformed prompt message was read")
	}
	if err := json.Unmarshal([]byte(`{"role":"user"}`), &msg); err == nil {
		t.Error("a prompt message without content was read")
	}
}

// The revisions on offer are 2026-07-28, 2025-11-25 and 2025-06-18, newest
// first, and a caller cannot change them.
func TestSupportedProtocolVersions(t *testing.T) {
	got := SupportedProtocolVersions()
	if !slices.Equal(got, []string{"2026-07-28", "2025-11-25", "2025-06-18"}) {
		t.Errorf("supported = %v", got)
	}
	got[0] = "x"
	if SupportedProtocolVersions()[0] != "2026-07-28" {
		t.Error("changing the returned list changed what is supported")
	}
}

// A handshake settles on the client's revision when it is spoken, else on
// the newest one a handshake can have.
func TestNegotiatedVersion(t *testing.T) {
	all := supportedProtocolVersions
	cases := []struct {
		client    string
		supported []string
		want      string
	}{
		{"2025-06-18", all, "2025-06-18"},
		{"2025-11-25", all, "2025-11-25"},
		{"2024-11-05", all, "2025-11-25"},
		{"2026-07-28", all, "2025-11-25"},
		{"2025-06-18", []string{"2026-07-28"}, "2025-11-25"},
	}
	for _, c := range cases {
		if got := negotiatedVersion(c.client, c.supported); got != c.want {
			t.Errorf("negotiatedVersion(%q, %v) = %q, want %q", c.client, c.supported, got, c.want)
		}
	}
}

// Only a stateless transport serves 2026-07-28.
func TestTransportVersions(t *testing.T) {
	if got := transportVersions(supportedProtocolVersions, true); !slices.Equal(got, supportedProtocolVersions) {
		t.Errorf("stateless = %v", got)
	}
	if got := transportVersions(supportedProtocolVersions, false); !slices.Equal(got, []string{"2025-11-25", "2025-06-18"}) {
		t.Errorf("stateful = %v", got)
	}
	if supportedProtocolVersions[0] != "2026-07-28" {
		t.Error("filtering changed the supported list")
	}
}
