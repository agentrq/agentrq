package mcp

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
)

// newTestWorkspaceServer builds a WorkspaceServer with just enough wiring to
// exercise the HTTP/MCP transport layer. The callback funcs are left nil
// since these tests never reach a tool handler.
func newTestWorkspaceServer(t *testing.T) *WorkspaceServer {
	t.Helper()
	return NewWorkspaceServer(
		1, "user", "http://localhost",
		nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		eventbus.New(), nil, nil, "icon", "name", "desc", nil, nil, nil, nil,
	)
}

func postJSONRPC(t *testing.T, url, body string, headers map[string]string) *http.Response {
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
	return resp
}

// server/discover must be answerable with no session or handshake at all,
// and must report the fields conformance suites check: supportedVersions,
// ttlMs, capabilities and server identity.
func TestServerDiscover_NoSessionRequired(t *testing.T) {
	ps := newTestWorkspaceServer(t)
	srv := httptest.NewServer(ps.Handler())
	defer srv.Close()

	resp := postJSONRPC(t, srv.URL, `{"jsonrpc":"2.0","id":7,"method":"server/discover","params":{}}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var env struct {
		ID     int `json:"id"`
		Result struct {
			SupportedVersions []string       `json:"supportedVersions"`
			TTLMs             int64          `json:"ttlMs"`
			Capabilities      map[string]any `json:"capabilities"`
			ServerInfo        map[string]any `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if env.ID != 7 {
		t.Errorf("expected id to be echoed back as 7, got %d", env.ID)
	}
	if len(env.Result.SupportedVersions) == 0 {
		t.Error("expected supportedVersions to be non-empty")
	}
	if env.Result.TTLMs <= 0 {
		t.Error("expected a positive ttlMs cache hint")
	}
	if env.Result.Capabilities == nil {
		t.Error("expected capabilities to be reported")
	}
	if env.Result.ServerInfo["name"] == "" || env.Result.ServerInfo["name"] == nil {
		t.Error("expected serverInfo.name to be set")
	}

	cacheControl := resp.Header.Get("Cache-Control")
	if cacheControl == "" {
		t.Error("expected a Cache-Control header for the CacheableResult")
	}
}

// server/discover must return the same content on repeated calls within its
// TTL, since it's derived from static configuration rather than per-request
// state.
func TestServerDiscover_StableAcrossCalls(t *testing.T) {
	ps := newTestWorkspaceServer(t)
	srv := httptest.NewServer(ps.Handler())
	defer srv.Close()

	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{}}`

	resp1 := postJSONRPC(t, srv.URL, body, nil)
	defer resp1.Body.Close()
	var out1 map[string]any
	if err := json.NewDecoder(resp1.Body).Decode(&out1); err != nil {
		t.Fatal(err)
	}

	resp2 := postJSONRPC(t, srv.URL, body, nil)
	defer resp2.Body.Close()
	var out2 map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&out2); err != nil {
		t.Fatal(err)
	}

	b1, _ := json.Marshal(out1["result"])
	b2, _ := json.Marshal(out2["result"])
	if string(b1) != string(b2) {
		t.Errorf("expected stable result across calls, got %s vs %s", b1, b2)
	}
}

// A version-less call with no prior initialize handshake must still be
// served on the server's default protocol version, not refused.
func TestVersionNegotiation_VersionlessRequestServedOnDefault(t *testing.T) {
	ps := newTestWorkspaceServer(t)
	srv := httptest.NewServer(ps.Handler())
	defer srv.Close()

	resp := postJSONRPC(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`, nil)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, err := decodeSSEOrJSON(resp)
	if err != nil {
		t.Fatal(err)
	}

	var env struct {
		Error  *struct{ Message string } `json:"error"`
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if env.Error != nil {
		t.Fatalf("expected tools/list to be served, got error: %s", env.Error.Message)
	}
	if len(env.Result.Tools) == 0 {
		t.Error("expected the registered tools to be listed")
	}
}

// A caller that already completed a real initialize handshake must be
// unaffected by the auto-initialize compatibility path.
func TestVersionNegotiation_ExistingSessionUnaffected(t *testing.T) {
	ps := newTestWorkspaceServer(t)
	srv := httptest.NewServer(ps.Handler())
	defer srv.Close()

	initResp := postJSONRPC(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26","capabilities":{},"clientInfo":{"name":"test","version":"1.0.0"}}}`, nil)
	defer initResp.Body.Close()
	sessionID := initResp.Header.Get("Mcp-Session-Id")
	if sessionID == "" {
		t.Fatal("expected a session id from a real initialize call")
	}
	if _, err := decodeSSEOrJSON(initResp); err != nil {
		t.Fatal(err)
	}

	resp := postJSONRPC(t, srv.URL, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`, map[string]string{"Mcp-Session-Id": sessionID})
	defer resp.Body.Close()
	body, err := decodeSSEOrJSON(resp)
	if err != nil {
		t.Fatal(err)
	}

	var env struct {
		Error *struct{ Message string } `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		t.Fatalf("decode %s: %v", body, err)
	}
	if env.Error != nil {
		t.Fatalf("expected tools/list to succeed on the real session, got error: %s", env.Error.Message)
	}
	if got := resp.Header.Get("Mcp-Session-Id"); got != "" && got != sessionID {
		t.Errorf("expected the real session id to be preserved, got %q", got)
	}
}

// decodeSSEOrJSON reads a streamable-HTTP response body, unwrapping the
// "event: message\ndata: ...\n\n" framing if present.
func decodeSSEOrJSON(resp *http.Response) ([]byte, error) {
	buf := new(bytes.Buffer)
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		return nil, err
	}
	b := buf.Bytes()
	if _, after, ok := bytes.Cut(b, []byte("data: ")); ok {
		line, _, _ := bytes.Cut(after, []byte("\n"))
		return line, nil
	}
	return b, nil
}
