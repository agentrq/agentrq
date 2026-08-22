package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

// bindingTokenSvc extends the shared mock with a client registration whose
// redirect_uris the authorize handler is expected to enforce.
type bindingTokenSvc struct {
	mockTokenSvc
	registeredClientID string
	registeredURIs     []string
}

func (m *bindingTokenSvc) ValidateClientRegistrationToken(tokenStr string) (*auth.ClientRegistrationClaims, error) {
	if tokenStr != "" && tokenStr == m.registeredClientID {
		return &auth.ClientRegistrationClaims{RedirectURIs: m.registeredURIs}, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

// fakeCIMD stands in for the network-backed resolver so authorize-handler
// tests never make outbound requests.
type fakeCIMD struct {
	metadata map[string]*auth.ClientMetadata
	err      error
}

func (f *fakeCIMD) IsClientIDURL(clientID string) bool {
	return strings.HasPrefix(clientID, "https://")
}

func (f *fakeCIMD) Resolve(ctx context.Context, clientID string) (*auth.ClientMetadata, error) {
	if f.err != nil {
		return nil, f.err
	}
	if m, ok := f.metadata[clientID]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("no metadata document for %q", clientID)
}

func setupBindingRouter(tokenSvc auth.TokenService, cimd auth.CIMDResolver) *http.ServeMux {
	mux := http.NewServeMux()
	New(Params{
		TokenSvc: tokenSvc,
		Crud:     &mockCrud{},
		CIMD:     cimd,
		BaseURL:  "https://agentrq.com",
		Mux:      mux,
	})
	return mux
}

func authorizeRequest(clientID, redirectURI string) *http.Request {
	req := httptest.NewRequest("GET", "/mcp/12345/oauth2/authorize?client_id="+url.QueryEscape(clientID)+
		"&redirect_uri="+url.QueryEscape(redirectURI), nil)
	req.SetPathValue("workspaceID", "12345")
	req.Host = "12345.mcp.agentrq.com"
	req.AddCookie(&http.Cookie{Name: "at", Value: "valid-auth-cookie"})
	return req
}

// A client that registered redirect_uris via DCR must only be redirected to
// one of those exact URIs — otherwise registration is decorative and an
// attacker can supply any redirect the loose heuristic happens to allow.
func TestAuthorize_DCRRegisteredRedirectURIIsEnforced(t *testing.T) {
	tokenSvc := &bindingTokenSvc{
		registeredClientID: "registered-client",
		registeredURIs:     []string{"cursor://callback"},
	}
	mux := setupBindingRouter(tokenSvc, &fakeCIMD{})

	t.Run("registered redirect_uri is allowed", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, authorizeRequest("registered-client", "cursor://callback"))
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302 for a registered redirect_uri, got %d", w.Code)
		}
	})

	// Without binding, this custom-scheme URI would sail through the
	// same-origin heuristic, which only inspects http/https redirects.
	t.Run("unregistered custom-scheme redirect_uri is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, authorizeRequest("registered-client", "attacker://callback"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for an unregistered redirect_uri, got %d", w.Code)
		}
	})

	t.Run("unregistered https redirect_uri is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, authorizeRequest("registered-client", "https://agentrq.com/callback"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for an unregistered redirect_uri, got %d", w.Code)
		}
	})
}

// Without an allowlist, the redirect binding is bypassable by simply not
// presenting a recognizable client_id: control falls through to the
// same-origin heuristic, which skips every check for custom schemes. These
// cases pin that hole shut while keeping the native editors working.
func TestAuthorize_UnregisteredCustomSchemes(t *testing.T) {
	mux := setupBindingRouter(&bindingTokenSvc{}, &fakeCIMD{})

	tests := []struct {
		name        string
		clientID    string
		redirectURI string
		wantStatus  int
	}{
		// The attack: no usable client_id, arbitrary scheme.
		{"unknown scheme, no client_id", "", "attacker://callback", http.StatusBadRequest},
		{"unknown scheme, unrecognized client_id", "garbage", "attacker://callback", http.StatusBadRequest},
		{"unknown scheme, exfiltration-looking host", "", "evil://x.example.com/steal", http.StatusBadRequest},

		// Native editors keep working with no registration at all.
		{"cursor", "", "cursor://callback", http.StatusFound},
		{"vscode", "", "vscode://callback", http.StatusFound},
		{"zed", "", "zed://callback", http.StatusFound},
		// Schemes are case-insensitive per RFC 3986 §3.1.
		{"uppercase scheme still matched", "", "VSCode://callback", http.StatusFound},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, authorizeRequest(tt.clientID, tt.redirectURI))
			if w.Code != tt.wantStatus {
				t.Errorf("redirect_uri %q: got %d, want %d", tt.redirectURI, w.Code, tt.wantStatus)
			}
		})
	}
}

// Registration is the escape hatch: a client that declares an unusual scheme
// can still use it, so the allowlist never blocks a new integration.
func TestAuthorize_RegisteredClientMayUseAnyScheme(t *testing.T) {
	tokenSvc := &bindingTokenSvc{
		registeredClientID: "registered-client",
		registeredURIs:     []string{"someneweditor://callback"},
	}
	mux := setupBindingRouter(tokenSvc, &fakeCIMD{})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("registered-client", "someneweditor://callback"))
	if w.Code != http.StatusFound {
		t.Fatalf("a registered redirect_uri must be honored regardless of scheme, got %d", w.Code)
	}

	// ...but registering one scheme does not open the rest.
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("registered-client", "attacker://callback"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("an unregistered scheme must still be refused, got %d", w.Code)
	}
}

// An https client_id is a Client ID Metadata Document URL: its published
// redirect_uris are what the authorize handler must enforce.
func TestAuthorize_CIMDRedirectURIIsEnforced(t *testing.T) {
	const clientID = "https://client.example.com/oauth-client"
	cimd := &fakeCIMD{metadata: map[string]*auth.ClientMetadata{
		clientID: {ClientID: clientID, RedirectURIs: []string{"https://client.example.com/callback"}},
	}}
	mux := setupBindingRouter(&bindingTokenSvc{}, cimd)

	t.Run("redirect_uri published in the document is allowed", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, authorizeRequest(clientID, "https://client.example.com/callback"))
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302 for a published redirect_uri, got %d", w.Code)
		}
		if loc := w.Header().Get("Location"); !strings.HasPrefix(loc, "https://client.example.com/callback") {
			t.Errorf("expected redirect to the client callback, got %s", loc)
		}
	})

	t.Run("redirect_uri absent from the document is rejected", func(t *testing.T) {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, authorizeRequest(clientID, "https://evil.example.com/callback"))
		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for an unpublished redirect_uri, got %d", w.Code)
		}
	})
}

// The draft requires aborting the authorization request when the client's
// metadata document can't be fetched or validated.
func TestAuthorize_CIMDResolutionFailureAborts(t *testing.T) {
	cimd := &fakeCIMD{err: fmt.Errorf("fetch failed")}
	mux := setupBindingRouter(&bindingTokenSvc{}, cimd)

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("https://client.example.com/oauth-client", "https://client.example.com/callback"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when the metadata document cannot be resolved, got %d", w.Code)
	}
}

// Registration must hand back a client_id that actually carries the
// redirect_uris, and must reject malformed metadata per RFC 7591 §3.2.2.
func TestRegister_RFC7591(t *testing.T) {
	mux := setupBindingRouter(&bindingTokenSvc{}, &fakeCIMD{})

	t.Run("valid registration returns a client_id and no-store", func(t *testing.T) {
		body := strings.NewReader(`{"redirect_uris":["https://client.example.com/callback"],"client_name":"Test"}`)
		req := httptest.NewRequest("POST", "/mcp/12345/oauth2/register", body)
		req.SetPathValue("workspaceID", "12345")
		req.Host = "12345.mcp.agentrq.com"

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d (%s)", w.Code, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Errorf("expected Cache-Control: no-store, got %q", cc)
		}

		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out["client_id"] == "" || out["client_id"] == nil {
			t.Error("expected a client_id in the registration response")
		}
		// Public clients only: DCR never issues a secret.
		if out["token_endpoint_auth_method"] != "none" {
			t.Errorf("expected token_endpoint_auth_method=none, got %v", out["token_endpoint_auth_method"])
		}
	})

	t.Run("malformed redirect_uris is rejected with invalid_redirect_uri", func(t *testing.T) {
		body := strings.NewReader(`{"redirect_uris":"not-an-array"}`)
		req := httptest.NewRequest("POST", "/mcp/12345/oauth2/register", body)
		req.SetPathValue("workspaceID", "12345")
		req.Host = "12345.mcp.agentrq.com"

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out["error"] != "invalid_redirect_uri" {
			t.Errorf("expected error=invalid_redirect_uri, got %v", out["error"])
		}
	})

	t.Run("malformed JSON is rejected with invalid_client_metadata", func(t *testing.T) {
		req := httptest.NewRequest("POST", "/mcp/12345/oauth2/register", strings.NewReader(`{oops`))
		req.SetPathValue("workspaceID", "12345")
		req.Host = "12345.mcp.agentrq.com"

		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Fatalf("expected 400, got %d", w.Code)
		}
		var out map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out["error"] != "invalid_client_metadata" {
			t.Errorf("expected error=invalid_client_metadata, got %v", out["error"])
		}
	})
}

// RFC 8414 §3.1 puts the well-known suffix between the host and the issuer's
// path, and §3.3 requires the issuer to match the URL used to fetch it.
func TestMetadata_RFC8414IssuerAndWellKnownPath(t *testing.T) {
	mux := setupBindingRouter(&bindingTokenSvc{}, &fakeCIMD{})

	req := httptest.NewRequest("GET", "/.well-known/oauth-authorization-server/mcp/12345", nil)
	req.Host = "agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected the RFC 8414 well-known path to be served, got %d", w.Code)
	}

	var meta map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
		t.Fatalf("decode: %v", err)
	}

	// Without the workspace segment every workspace would claim the same
	// issuer identity.
	wantIssuer := "https://agentrq.com/mcp/" + monoflake.IDFromBase62("12345").String()
	if meta["issuer"] != wantIssuer {
		t.Errorf("issuer = %v, want %s", meta["issuer"], wantIssuer)
	}
	if meta["client_id_metadata_document_supported"] != true {
		t.Error("expected client_id_metadata_document_supported to be advertised")
	}
	methods, _ := meta["token_endpoint_auth_methods_supported"].([]any)
	if len(methods) != 1 || methods[0] != "none" {
		t.Errorf("expected token_endpoint_auth_methods_supported=[none], got %v", meta["token_endpoint_auth_methods_supported"])
	}
}

// RFC 9728 §2: the protected-resource document advertises
// "authorization_servers" — an ARRAY of issuer IDENTIFIERS, not a singular
// metadata document URL — and the RFC 8414 URL a client derives from that
// issuer must resolve to the metadata handler.
func TestProtectedResource_AdvertisesIssuerPerRFC9728(t *testing.T) {
	mux := setupBindingRouter(&bindingTokenSvc{}, &fakeCIMD{})

	req := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource/mcp/12345", nil)
	req.SetPathValue("workspaceID", "12345")
	req.Host = "agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

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
	issuer, _ := servers[0].(string)
	wantIssuer := "https://agentrq.com/mcp/" + monoflake.IDFromBase62("12345").String()
	if issuer != wantIssuer {
		t.Errorf("authorization_servers[0] = %q, want issuer %q", issuer, wantIssuer)
	}

	wantResource := wantIssuer
	if doc["resource"] != wantResource {
		t.Errorf("resource = %v, want %q", doc["resource"], wantResource)
	}

	// A client derives the metadata URL from the issuer itself (RFC 8414 §3.1:
	// the well-known suffix goes between host and the issuer's path). That
	// derived URL must resolve.
	derived := strings.Replace(issuer, "https://agentrq.com",
		"/.well-known/oauth-authorization-server", 1)
	followReq := httptest.NewRequest("GET", derived, nil)
	followReq.Host = "agentrq.com"
	followW := httptest.NewRecorder()
	mux.ServeHTTP(followW, followReq)
	if followW.Code != http.StatusOK {
		t.Errorf("metadata URL %q derived from the advertised issuer returned %d, expected it to resolve", derived, followW.Code)
	}
}

// RFC 9728 §5.1 / MCP auth spec: a 401 from the MCP endpoint MUST carry a
// WWW-Authenticate challenge naming the resource metadata URL, so clients do
// not have to guess well-known paths.
func TestUnauthorized_AdvertisesResourceMetadataChallenge(t *testing.T) {
	mux := setupBindingRouter(&bindingTokenSvc{}, &fakeCIMD{})

	req := httptest.NewRequest("POST", "/mcp/12345", nil)
	req.SetPathValue("workspaceID", "12345")
	req.Host = "agentrq.com"
	req.Header.Set("X-Forwarded-Proto", "https")

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a token, got %d", w.Code)
	}

	challenge := w.Header().Get("WWW-Authenticate")
	wsPath := "/mcp/" + monoflake.IDFromBase62("12345").String()
	wantMetadata := `resource_metadata="https://agentrq.com/.well-known/oauth-protected-resource` + wsPath + `"`
	if !strings.Contains(challenge, wantMetadata) {
		t.Errorf("WWW-Authenticate = %q, want it to contain %s", challenge, wantMetadata)
	}
	if !strings.HasPrefix(challenge, "Bearer ") {
		t.Errorf("WWW-Authenticate = %q, want a Bearer challenge", challenge)
	}

	// The advertised metadata URL must resolve.
	followReq := httptest.NewRequest("GET", "/.well-known/oauth-protected-resource"+wsPath, nil)
	followReq.Host = "agentrq.com"
	followW := httptest.NewRecorder()
	mux.ServeHTTP(followW, followReq)
	if followW.Code != http.StatusOK {
		t.Errorf("advertised resource_metadata URL returned %d, expected it to resolve", followW.Code)
	}
}

// Regression: Claude Code publishes PORTLESS loopback redirect_uris in its
// Client ID Metadata Document and then listens on an ephemeral port the OS
// assigns at request time. Exact string matching rejected every such client
// with "invalid redirect_uri: not registered for this client_id", which broke
// `claude mcp add` against this server entirely. RFC 8252 §7.3 requires the
// authorization server to allow any port for loopback redirect URIs.
func TestAuthorize_LoopbackRedirectURIIgnoresPort(t *testing.T) {
	const claudeCodeClientID = "https://claude.ai/oauth/claude-code-client-metadata"

	// Verbatim from https://claude.ai/oauth/claude-code-client-metadata.
	cimd := &fakeCIMD{metadata: map[string]*auth.ClientMetadata{
		claudeCodeClientID: {
			RedirectURIs: []string{"http://localhost/callback", "http://127.0.0.1/callback"},
		},
	}}
	mux := setupBindingRouter(&bindingTokenSvc{}, cimd)

	allowed := []string{
		"http://localhost:3118/callback", // the port from the original report
		"http://localhost:54321/callback",
		"http://127.0.0.1:3118/callback",
		"http://localhost/callback", // still matches exactly
	}
	for _, redirectURI := range allowed {
		t.Run("allowed "+redirectURI, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, authorizeRequest(claudeCodeClientID, redirectURI))
			if w.Code != http.StatusFound {
				t.Errorf("expected 302 for loopback redirect_uri %q, got %d: %s",
					redirectURI, w.Code, strings.TrimSpace(w.Body.String()))
			}
		})
	}

	// Relaxing the port must not relax the host: these are the cases that
	// would turn the fix into an open redirect.
	rejected := []string{
		"http://evil.example.com:3118/callback",
		"http://localhost.evil.com/callback",
		"http://localhost:3118/other",
		"https://localhost:3118/callback",
	}
	for _, redirectURI := range rejected {
		t.Run("rejected "+redirectURI, func(t *testing.T) {
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, authorizeRequest(claudeCodeClientID, redirectURI))
			if w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %q, got %d", redirectURI, w.Code)
			}
		})
	}
}
