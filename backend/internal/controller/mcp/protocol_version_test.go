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

// Refusing a version-less server/discover is correct, not a spec deviation,
// and this test exists to stop someone "fixing" it.
//
// A third-party conformance suite reported:
//
//	a version-less server/discover must be served, not refused:
//	{"code":-32601,"message":"method not found: \"server/discover\""}
//
// Checked directly against the prescriptive spec text (not just the SDK),
// at https://modelcontextprotocol.io/specification/2026-07-28/basic/transports/streamable-http#protocol-version-header:
//
//	"A server that supports clients implementing protocol versions earlier
//	than 2025-06-18 (which did not define the MCP-Protocol-Version header)
//	MAY treat a request that omits the header as protocol version
//	2025-03-26."
//
// This server does support those earlier clients (it advertises
// 2024-11-05 through 2025-11-25 — see TestEveryAdvertisedVersionCompletesHandshake)
// and takes exactly that option: a header-less request is treated as
// speaking protocol 2025-03-26. server/discover is itself a >= 2026-07-28
// addition (https://modelcontextprotocol.io/specification/2026-07-28/server/discover),
// so under the assumed 2025-03-26 dialect the method plainly does not
// exist — "method not found" is the only spec-consistent answer, exactly
// as it would be for any other unknown method name.
//
// That also explains the HTTP status: SEP-2575 mandates 404 for
// MethodNotFound, but only ">= 2026-07-28" — see extractErrorStatus in the
// SDK (mcp/streamable.go). A request assumed to be 2025-03-26 predates that
// rule, so the error rides back on a plain 200, the pre-2026-07-28
// convention. This isn't the SDK improvising: it's the two spec rules
// (header-omission fallback, and the version-gated 404) composing
// correctly, and this test asserts both halves so neither regresses
// silently.
//
// The spec's own example request for server/discover
// (https://modelcontextprotocol.io/specification/2026-07-28/server/discover#request)
// always carries the full `_meta` envelope (protocolVersion, clientInfo,
// clientCapabilities) — there is no version-less example, and the SDK's
// conformance fixture (mcp/testdata/conformance/server/discover.txtar)
// agrees. TestServerDiscover_ReportsIdentityAndVersions above already
// proves discover works with no session and no initialize handshake, using
// exactly that envelope; the only thing this server declines to do is
// guess at a version nobody asked for. Serving a bare call would mean
// inventing behavior neither the spec nor the reference implementation
// defines, to satisfy a third-party suite.
func TestVersionlessDiscoverIsRefused(t *testing.T) {
	srv := newProtocolTestServer(t)

	status, out, _ := mcpPost(t, srv.URL, nil, `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`)
	// 200, not 404: a header-less request is assumed to speak the pre-2026-07-28
	// dialect (see the fallback rule cited above), whose error convention embeds
	// the JSON-RPC error in a 200 response rather than using SEP-2575's
	// version-gated 404-for-MethodNotFound.
	if status != http.StatusOK {
		t.Fatalf("expected a JSON-RPC error carried over HTTP 200, got %d: %s", status, out)
	}

	var env struct {
		Error *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if env.Error == nil {
		t.Fatal("expected a version-less server/discover call to be refused")
	}
	if !strings.Contains(env.Error.Message, "method not found") {
		t.Errorf("unexpected refusal reason: %q", env.Error.Message)
	}

	// The remedy is on the client: carry the >= 2026-07-28 `_meta` envelope,
	// which always succeeds — see TestServerDiscover_ReportsIdentityAndVersions.
	versions, _ := discoverResult(t, srv)
	if len(versions) == 0 {
		t.Error("expected the properly-tagged discover call to still succeed")
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

// Rejecting a second initialize on an already-initialized session is correct,
// not a spec deviation, and this test exists to stop someone "fixing" it.
//
// A third-party conformance suite reported the handshake failing with
//
//	target does not answer initialize: {"code":0,"message":"duplicate \"initialize\" received"}
//
// against the case "the handshake settles on a revision inside the supported
// window". That invariant is real, and it already holds — see
// TestEveryAdvertisedVersionCompletesHandshake, which proves every advertised
// revision completes a handshake, echoes its own version back, and yields a
// working session. The error only appears when a client sends initialize a
// SECOND time while reusing the Mcp-Session-Id from the first handshake, which
// the MCP lifecycle does not permit: a session is initialized exactly once, and
// a new handshake needs a new session (omit the header).
//
// The official SDK asserts this exact behavior, verbatim, in
// mcp/server_test.go ("second initialize error = %v, want duplicate
// initialize"). Accepting a repeat initialize would mean failing the reference
// implementation's own tests to satisfy a third-party suite, and would silently
// mask a real client bug: re-initializing an established session discards the
// negotiated state that every later request depends on.
func TestDuplicateInitializeOnSameSessionIsRefused(t *testing.T) {
	srv := newProtocolTestServer(t)

	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{` +
		`"protocolVersion":"2025-11-25","capabilities":{},` +
		`"clientInfo":{"name":"probe","version":"1"}}}`

	status, out, hdr := mcpPost(t, srv.URL, nil, initialize)
	if status != http.StatusOK {
		t.Fatalf("first initialize: expected 200, got %d: %s", status, out)
	}
	session := hdr.Get("Mcp-Session-Id")
	if session == "" {
		t.Fatal("expected a session id from the first initialize")
	}

	// Same session, second handshake: must be refused.
	status, out, _ = mcpPost(t, srv.URL, map[string]string{"Mcp-Session-Id": session}, initialize)
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
		t.Fatal("expected a repeat initialize on the same session to be refused")
	}
	if !strings.Contains(env.Error.Message, `duplicate "initialize" received`) {
		t.Errorf("unexpected refusal reason: %q", env.Error.Message)
	}

	// The remedy a client applies is to drop the session id, not to give up:
	// a fresh handshake must still succeed while the old session lives on.
	status, out, hdr = mcpPost(t, srv.URL, nil, initialize)
	if status != http.StatusOK {
		t.Fatalf("fresh initialize: expected 200, got %d: %s", status, out)
	}
	if fresh := hdr.Get("Mcp-Session-Id"); fresh == "" || fresh == session {
		t.Errorf("expected a new session id distinct from %q, got %q", session, fresh)
	}
	// Decode into a fresh value: unmarshalling a response with no "error" key
	// over the previous struct would leave the old non-nil pointer in place.
	var freshEnv struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(out, &freshEnv); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if freshEnv.Error != nil {
		t.Errorf("fresh initialize failed: %s", freshEnv.Error.Message)
	}
}
