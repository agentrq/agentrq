package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	mock_pubsub "github.com/agentrq/agentrq/backend/internal/service/mocks/pubsub"
	"github.com/golang/mock/gomock"
)

func newProtocolTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	pub := mock_pubsub.NewMockService(ctrl)
	pub.EXPECT().Publish(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()

	listTasks := func(context.Context, ListTasksFilter) ([]model.Task, error) {
		return []model.Task{{Status: "completed"}}, nil
	}

	ps := NewWorkspaceServer(
		1, "user", "http://localhost",
		nil, nil, nil, listTasks, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, pub,
	)
	srv := httptest.NewServer(ps.Handler())
	t.Cleanup(srv.Close)
	return srv
}

// mcpPost issues a single JSON-RPC request, unwrapping the SSE framing the
// streamable transport uses for successful responses.
func mcpPost(t *testing.T, url string, headers map[string]string, body string) (int, []byte, http.Header) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if _, after, ok := bytes.Cut(raw, []byte("data: ")); ok {
		raw, _, _ = bytes.Cut(after, []byte("\n"))
	}
	return resp.StatusCode, raw, resp.Header
}

// discoverResult asks the server what it can serve. server/discover is defined
// by the >= 2026-07-28 revision, so the probe carries that revision's _meta and
// standard headers even though the server itself may negotiate lower.
func discoverResult(t *testing.T, srv *httptest.Server) (supportedVersions []string, raw map[string]any) {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`

	status, out, _ := mcpPost(t, srv.URL, map[string]string{
		"MCP-Protocol-Version": "2026-07-28",
		"Mcp-Method":           "server/discover",
	}, body)
	if status != http.StatusOK {
		t.Fatalf("server/discover: expected 200, got %d: %s", status, out)
	}

	var env struct {
		Error  *struct{ Message string } `json:"error"`
		Result map[string]any            `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("server/discover: decode %s: %v", out, err)
	}
	if env.Error != nil {
		t.Fatalf("server/discover returned an error: %s", env.Error.Message)
	}

	for _, v := range env.Result["supportedVersions"].([]any) {
		supportedVersions = append(supportedVersions, v.(string))
	}
	return supportedVersions, env.Result
}

// server/discover must be answerable with no session and no initialize
// handshake, and must carry the fields clients rely on to negotiate.
func TestServerDiscover_ReportsIdentityAndVersions(t *testing.T) {
	srv := newProtocolTestServer(t)
	versions, result := discoverResult(t, srv)

	if len(versions) == 0 {
		t.Error("expected server/discover to advertise at least one protocol version")
	}
	if result["capabilities"] == nil {
		t.Error("expected server/discover to report capabilities")
	}

	// Server identity travels in _meta for this revision, per the SDK's own
	// discover conformance fixture — not as a top-level serverInfo field.
	meta, _ := result["_meta"].(map[string]any)
	if meta == nil || meta["io.modelcontextprotocol/serverInfo"] == nil {
		t.Errorf("expected server identity in _meta, got %v", result["_meta"])
	}
}

// server/discover is a CacheableResult, and it is the cheap probe clients make
// before negotiating. The SDK defaults ttlMs to 0, which its own docs define as
// "immediately stale" — a hint that tells a client nothing. Everything in the
// result is fixed at construction, so it should advertise a real lifetime.
func TestServerDiscover_CarriesUsableCacheHint(t *testing.T) {
	srv := newProtocolTestServer(t)
	_, result := discoverResult(t, srv)

	ttl, ok := result["ttlMs"].(float64)
	if !ok {
		t.Fatalf("expected a numeric ttlMs, got %v", result["ttlMs"])
	}
	if ttl <= 0 {
		t.Errorf("ttlMs = %v; a zero TTL means immediately stale, which is not a usable cache hint", ttl)
	}
	if scope := result["cacheScope"]; scope != "public" {
		t.Errorf("cacheScope = %v, want public (the result carries no per-user state)", scope)
	}
}

// The result is derived entirely from construction-time configuration, so
// repeated calls within the TTL must not drift.
func TestServerDiscover_StableAcrossCalls(t *testing.T) {
	srv := newProtocolTestServer(t)

	_, first := discoverResult(t, srv)
	_, second := discoverResult(t, srv)

	a, _ := json.Marshal(first)
	b, _ := json.Marshal(second)
	if string(a) != string(b) {
		t.Errorf("server/discover drifted between calls:\n  %s\n  %s", a, b)
	}
}

// Refusing a pre-handshake tools/list is correct, not a spec deviation, and
// this test exists to stop someone "fixing" it.
//
// A third-party conformance suite reported that a version-less tools/list
// "must be served, not refused". The official SDK's own conformance fixture
// (mcp/testdata/conformance/server/lifecycle.txtar, "rejects non-ping requests
// until 'initialized' is received") asserts the exact opposite, expecting
// precisely the error below. Serving such a request would mean failing the
// reference suite to satisfy a third-party one.
func TestPreHandshakeRequestIsRefused(t *testing.T) {
	srv := newProtocolTestServer(t)

	status, out, _ := mcpPost(t, srv.URL, nil, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if status != http.StatusOK {
		t.Fatalf("expected a JSON-RPC error carried over HTTP 200, got %d: %s", status, out)
	}

	var env struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if env.Error == nil {
		t.Fatal("expected tools/list before initialize to be refused")
	}
	if !strings.Contains(env.Error.Message, "invalid during session initialization") {
		t.Errorf("unexpected refusal reason: %q", env.Error.Message)
	}
}

// The bug behind this test: the server advertised nothing usable and clients
// blind-tried the newest revision, getting a plain-text HTTP 400 that isn't
// JSON-RPC at all — so every subsequent assertion saw an undefined result.
// Guard the invariant directly: every version we advertise must actually
// complete a handshake and echo that same version back.
func TestEveryAdvertisedVersionCompletesHandshake(t *testing.T) {
	srv := newProtocolTestServer(t)
	versions, _ := discoverResult(t, srv)

	for _, version := range versions {
		t.Run(version, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version +
				`","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`

			status, out, hdr := mcpPost(t, srv.URL, map[string]string{"MCP-Protocol-Version": version}, body)
			if status != http.StatusOK {
				t.Fatalf("initialize: expected 200, got %d: %s", status, out)
			}

			var env struct {
				Error  *struct{ Message string } `json:"error"`
				Result struct {
					ProtocolVersion string `json:"protocolVersion"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out, &env); err != nil {
				t.Fatalf("decode %s: %v", out, err)
			}
			if env.Error != nil {
				t.Fatalf("initialize failed: %s", env.Error.Message)
			}
			if env.Result.ProtocolVersion != version {
				t.Errorf("initialize settled on %q, want the advertised %q",
					env.Result.ProtocolVersion, version)
			}

			session := hdr.Get("Mcp-Session-Id")
			if session == "" {
				t.Fatal("expected a session id from initialize")
			}

			// The handshake must actually yield a usable session: this is what
			// the conformance suite could not get past.
			status, out, _ = mcpPost(t, srv.URL,
				map[string]string{"MCP-Protocol-Version": version, "Mcp-Session-Id": session},
				`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
			if status != http.StatusOK {
				t.Fatalf("tools/list: expected 200, got %d: %s", status, out)
			}

			var listEnv struct {
				Error  *struct{ Message string } `json:"error"`
				Result struct {
					Tools []struct{ Name string } `json:"tools"`
				} `json:"result"`
			}
			if err := json.Unmarshal(out, &listEnv); err != nil {
				t.Fatalf("decode %s: %v", out, err)
			}
			if listEnv.Error != nil {
				t.Fatalf("tools/list failed: %s", listEnv.Error.Message)
			}
			if len(listEnv.Result.Tools) == 0 {
				t.Error("expected tools/list to return the registered tools")
			}
		})
	}
}

// tools/call must return a schema-conformant CallToolResult: "content" is
// required by the spec, so it must be present and non-empty.
func TestToolsCall_ReturnsSchemaConformantResult(t *testing.T) {
	srv := newProtocolTestServer(t)

	const version = "2025-06-18"
	status, out, hdr := mcpPost(t, srv.URL, map[string]string{"MCP-Protocol-Version": version},
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"`+version+
			`","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`)
	if status != http.StatusOK {
		t.Fatalf("initialize: got %d: %s", status, out)
	}
	session := hdr.Get("Mcp-Session-Id")

	headers := map[string]string{"MCP-Protocol-Version": version, "Mcp-Session-Id": session}
	mcpPost(t, srv.URL, headers, `{"jsonrpc":"2.0","method":"notifications/initialized"}`)

	// getWorkspace needs no arguments, so it exercises the result shape rather
	// than argument validation.
	status, out, _ = mcpPost(t, srv.URL, headers,
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"getWorkspace","arguments":{}}}`)
	if status != http.StatusOK {
		t.Fatalf("tools/call: expected 200, got %d: %s", status, out)
	}

	var env struct {
		Error  *struct{ Message string } `json:"error"`
		Result *struct {
			Content []struct {
				Type string `json:"type"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if env.Error != nil {
		t.Fatalf("tools/call returned an error: %s", env.Error.Message)
	}
	if env.Result == nil {
		t.Fatal("tools/call returned no result")
	}
	if len(env.Result.Content) == 0 {
		t.Fatal(`CallToolResult is missing the schema-required "content" field`)
	}
	if env.Result.Content[0].Type == "" {
		t.Error("expected each content block to carry a type")
	}
}
