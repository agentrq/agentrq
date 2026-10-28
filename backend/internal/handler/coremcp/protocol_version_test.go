// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/config"
)

// The deliberate mirror of controller/mcp's protocol_version_test.go, which
// pins the opposite posture for the per-workspace server. The two servers
// disagree on purpose: that one pushes to its agent over the SSE stream and so
// must keep sessions, this one pushes nothing and so need not. Neither should
// be flipped without the other's test failing.

func newStatelessTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	// The streamable handler directly, not the authenticating wrapper: the
	// bearer check in coremcp.go is the same before and after this change, and
	// what is under test is the transport beneath it.
	srv := httptest.NewServer(NewServer(&mockEventCrud{}, "https://agentrq.example", nil).Handler())
	t.Cleanup(srv.Close)
	return srv
}

// corePost issues a single JSON-RPC request, unwrapping the SSE framing the
// streamable transport uses for successful responses.
func corePost(t *testing.T, url string, headers map[string]string, body string) (int, []byte, http.Header) {
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

// coreDiscover asks the server what it can serve. server/discover is defined by
// the >= 2026-07-28 revision, so the probe carries that revision's _meta.
func coreDiscover(t *testing.T, srv *httptest.Server) []string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":1,"method":"server/discover","params":{"_meta":{` +
		`"io.modelcontextprotocol/protocolVersion":"2026-07-28",` +
		`"io.modelcontextprotocol/clientCapabilities":{}}}}`

	status, out, _ := corePost(t, srv.URL, map[string]string{
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

	var versions []string
	for _, v := range env.Result["supportedVersions"].([]any) {
		versions = append(versions, v.(string))
	}
	return versions
}

// What "stateless" actually buys a client: no handshake, no session header, no
// state to lose. A tools/call with neither is the whole point, and it is what
// the per-workspace server refuses.
func TestCoreMCPServesRequestsWithoutASession(t *testing.T) {
	srv := newStatelessTestServer(t)

	status, out, hdr := corePost(t, srv.URL, nil,
		`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	if status != http.StatusOK {
		t.Fatalf("tools/list: expected 200, got %d: %s", status, out)
	}
	if sess := hdr.Get("Mcp-Session-Id"); sess != "" {
		t.Errorf("stateless server handed out a session id %q", sess)
	}

	var env struct {
		Error  *struct{ Message string } `json:"error"`
		Result struct {
			Tools []struct{ Name string } `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if env.Error != nil {
		t.Fatalf("tools/list without a handshake was refused: %s", env.Error.Message)
	}
	if len(env.Result.Tools) == 0 {
		t.Error("expected tools/list to return the registered tools")
	}
}

// The reason for the change. The SDK serves revision 2026-07-28 only on a
// stateless transport, so this assertion fails the moment somebody turns the
// flag back off.
func TestCoreMCPServesTheModernRevision(t *testing.T) {
	srv := newStatelessTestServer(t)

	var found bool
	for _, v := range coreDiscover(t, srv) {
		if v == "2026-07-28" {
			found = true
		}
	}
	if !found {
		t.Errorf("2026-07-28 is not advertised; the transport is no longer stateless")
	}
}

// Going stateless is additive, not a swap: the SDK's transport "supports every
// legacy SDK protocol version", and only gates 2026-07-28 behind statelessness.
// A client that never heard of the new revision must be unaffected, which is
// what makes this safe to deploy in front of clients we cannot upgrade.
func TestCoreMCPStillServesLegacyRevisions(t *testing.T) {
	srv := newStatelessTestServer(t)

	for _, version := range coreDiscover(t, srv) {
		if version >= "2026-07-28" {
			// Initialize is retired from this revision on; it is covered by
			// TestCoreMCPServesTheModernRevision instead.
			continue
		}
		t.Run(version, func(t *testing.T) {
			body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"` + version +
				`","capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`

			status, out, _ := corePost(t, srv.URL, map[string]string{"MCP-Protocol-Version": version}, body)
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
		})
	}
}

// What a client loses, recorded so nobody debugs it twice: a stateless
// transport answers 405 to the standalone SSE stream and to the session
// teardown. Neither costs this server anything — it has never pushed a
// notification and has no session to tear down — but a client that opens a GET
// stream on connect will see this, and both SDKs treat it as permitted.
func TestCoreMCPRefusesTheStreamAndTeardown(t *testing.T) {
	srv := newStatelessTestServer(t)

	for _, method := range []string{http.MethodGet, http.MethodDelete} {
		req, err := http.NewRequest(method, srv.URL, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Accept", "text/event-stream")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: got %d, want 405: %s", method, resp.StatusCode, body)
		}
		// RFC 9110 §15.5.6 requires it, and a client with no Allow header
		// cannot tell a refused method from a broken server.
		if allow := resp.Header.Get("Allow"); allow != "POST" {
			t.Errorf("%s: Allow = %q, want POST", method, allow)
		}
	}
}

// serverInfo.version is how a client tells which release it reached, so it is
// the app's own release rather than a fixed placeholder.
func TestCoreMCPInitializeAdvertisesTheRelease(t *testing.T) {
	srv := newStatelessTestServer(t)
	body := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-11-25",` +
		`"capabilities":{},"clientInfo":{"name":"probe","version":"1"}}}`

	status, out, _ := corePost(t, srv.URL, map[string]string{"MCP-Protocol-Version": "2025-11-25"}, body)
	if status != http.StatusOK {
		t.Fatalf("initialize: expected 200, got %d: %s", status, out)
	}
	var env struct {
		Result struct {
			ServerInfo struct{ Version string } `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode %s: %v", out, err)
	}
	if got := env.Result.ServerInfo.Version; got != config.Release() {
		t.Errorf("serverInfo.version = %q, want the release %q", got, config.Release())
	}
}
