package coremcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// RFC 9728 §2: the protected-resource document advertises
// "authorization_servers" — an ARRAY of issuer IDENTIFIERS, not a singular
// metadata document URL.
func TestProtectedResource_AdvertisesIssuerPerRFC9728(t *testing.T) {
	h := &handler{}

	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp", nil)
	req.Host = "mcp.agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	h.oauthProtectedResourceHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var doc map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, legacy := doc["authorization_server"]; legacy {
		t.Error(`non-standard singular "authorization_server" must not be emitted`)
	}

	servers, _ := doc["authorization_servers"].([]any)
	if len(servers) != 1 {
		t.Fatalf("authorization_servers = %v, want a single-entry array", doc["authorization_servers"])
	}
	// The issuer identifier, not the .well-known metadata URL derived from it.
	if servers[0] != "https://mcp.agentrq.com" {
		t.Errorf("authorization_servers[0] = %v, want the issuer %q", servers[0], "https://mcp.agentrq.com")
	}
	if doc["resource"] != "https://mcp.agentrq.com/mcp" {
		t.Errorf("resource = %v, want %q", doc["resource"], "https://mcp.agentrq.com/mcp")
	}
}

// The issuer in the protected-resource document must match the one the
// authorization server metadata publishes for itself (RFC 8414 §3.3).
func TestProtectedResourceIssuerMatchesMetadataIssuer(t *testing.T) {
	h := &handler{}

	newReq := func(path string) *http.Request {
		r := httptest.NewRequest("GET", path, nil)
		r.Host = "mcp.agentrq.com"
		r.Header.Set("X-Forwarded-Proto", "https")
		return r
	}

	prmW := httptest.NewRecorder()
	h.oauthProtectedResourceHandler().ServeHTTP(prmW, newReq("/.well-known/oauth-protected-resource/mcp"))
	var prm map[string]any
	if err := json.Unmarshal(prmW.Body.Bytes(), &prm); err != nil {
		t.Fatalf("decode protected resource metadata: %v", err)
	}

	asW := httptest.NewRecorder()
	h.oauthMetadataHandler().ServeHTTP(asW, newReq("/.well-known/oauth-authorization-server"))
	var as map[string]any
	if err := json.Unmarshal(asW.Body.Bytes(), &as); err != nil {
		t.Fatalf("decode authorization server metadata: %v", err)
	}

	servers, _ := prm["authorization_servers"].([]any)
	if len(servers) != 1 || servers[0] != as["issuer"] {
		t.Errorf("authorization_servers = %v, want it to name the AS issuer %v", prm["authorization_servers"], as["issuer"])
	}
}

// RFC 9728 §5.1 / MCP auth spec: a 401 MUST carry a WWW-Authenticate challenge
// naming the resource metadata URL, so clients need not guess well-known paths.
func TestUnauthorized_AdvertisesResourceMetadataChallenge(t *testing.T) {
	h := &handler{}

	req := httptest.NewRequest("POST", "/mcp", nil)
	req.Host = "mcp.agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", w.Code)
	}

	challenge := w.Header().Get("WWW-Authenticate")
	want := `resource_metadata="https://mcp.agentrq.com/.well-known/oauth-protected-resource/mcp"`
	if !strings.Contains(challenge, want) {
		t.Errorf("WWW-Authenticate = %q, want it to contain %s", challenge, want)
	}
	if !strings.HasPrefix(challenge, "Bearer ") {
		t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", challenge)
	}
}
