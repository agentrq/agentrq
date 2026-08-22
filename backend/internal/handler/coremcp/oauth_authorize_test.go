package coremcp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
)

// authorizeTokenSvc is the minimum TokenService the authorize handler touches:
// a valid session cookie and (unused here) DCR client registrations.
type authorizeTokenSvc struct{}

func (authorizeTokenSvc) CreateToken(userID, email, name, picture string) (string, error) {
	return "", nil
}
func (authorizeTokenSvc) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	return "mcp-token", nil
}
func (authorizeTokenSvc) CreateOAuthCodeToken(userID, workspaceID string) (string, error) {
	return "code-token", nil
}
func (authorizeTokenSvc) CreateOAuthStateToken(redirectURL, provider string) (string, error) {
	return "", nil
}
func (authorizeTokenSvc) CreateClientRegistrationToken(redirectURIs []string) (string, error) {
	return "", nil
}
func (authorizeTokenSvc) ValidateToken(tokenStr string) (*auth.Claims, error) {
	if tokenStr == "valid-auth-cookie" {
		return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1"}}, nil
	}
	return nil, jwt.ErrSignatureInvalid
}
func (authorizeTokenSvc) ValidateOAuthStateToken(tokenStr, provider string) (string, error) {
	return "", nil
}
func (authorizeTokenSvc) ValidateClientRegistrationToken(tokenStr string) (*auth.ClientRegistrationClaims, error) {
	return nil, jwt.ErrSignatureInvalid
}

// stubCIMD resolves client_id metadata documents without any network access.
type stubCIMD struct {
	metadata map[string]*auth.ClientMetadata
}

func (s *stubCIMD) IsClientIDURL(clientID string) bool {
	return strings.HasPrefix(clientID, "https://")
}

func (s *stubCIMD) Resolve(ctx context.Context, clientID string) (*auth.ClientMetadata, error) {
	if m, ok := s.metadata[clientID]; ok {
		return m, nil
	}
	return nil, fmt.Errorf("no metadata document for %q", clientID)
}

// Regression for the reported break: logging in to the supervisor MCP
// (coremcp) from Claude Code failed with
//
//	invalid redirect_uri: not registered for this client_id
//
// Claude Code publishes PORTLESS loopback redirect_uris in its Client ID
// Metadata Document and then listens on an ephemeral port the OS assigns at
// request time, so exact string matching can never match. RFC 8252 §7.3
// requires allowing any port for loopback redirect URIs.
func TestAuthorize_LoopbackRedirectURIIgnoresPort(t *testing.T) {
	const claudeCodeClientID = "https://claude.ai/oauth/claude-code-client-metadata"

	h := &handler{
		tokenSvc: authorizeTokenSvc{},
		baseURL:  "https://mcp.agentrq.com",
		// Verbatim from https://claude.ai/oauth/claude-code-client-metadata.
		cimd: &stubCIMD{metadata: map[string]*auth.ClientMetadata{
			claudeCodeClientID: {
				RedirectURIs: []string{"http://localhost/callback", "http://127.0.0.1/callback"},
			},
		}},
	}

	authorize := func(redirectURI string) *httptest.ResponseRecorder {
		// The query string from the original bug report.
		q := url.Values{
			"response_type":         {"code"},
			"client_id":             {claudeCodeClientID},
			"code_challenge":        {"0NaoYO97xvCieUzBNq5AbkQuWRqFS12KVo_FHqxCJ1w"},
			"code_challenge_method": {"S256"},
			"redirect_uri":          {redirectURI},
			"state":                 {"Bjq7O78iY2Uj4qLsiV-_O9Un5GvoMil6dEA3rwQa9LM"},
			"resource":              {"https://mcp.agentrq.com/mcp"},
		}
		req := httptest.NewRequest("GET", "/mcp/oauth2/authorize?"+q.Encode(), nil)
		req.Host = "mcp.agentrq.com"
		req.Header.Set("X-Forwarded-Proto", "https")
		req.AddCookie(&http.Cookie{Name: "at", Value: "valid-auth-cookie"})

		w := httptest.NewRecorder()
		h.oauthAuthorizeHandler().ServeHTTP(w, req)
		return w
	}

	t.Run("the exact redirect_uri from the bug report is accepted", func(t *testing.T) {
		w := authorize("http://localhost:3118/callback")
		if w.Code != http.StatusFound {
			t.Fatalf("expected 302, got %d: %s", w.Code, strings.TrimSpace(w.Body.String()))
		}
		loc := w.Header().Get("Location")
		if !strings.HasPrefix(loc, "http://localhost:3118/callback?") {
			t.Errorf("expected a redirect back to the requested callback, got %q", loc)
		}
		if !strings.Contains(loc, "code=") {
			t.Errorf("expected an authorization code in %q", loc)
		}
	})

	for _, redirectURI := range []string{
		"http://localhost:54321/callback",
		"http://127.0.0.1:3118/callback",
		"http://localhost/callback", // exact match still works
	} {
		t.Run("allowed "+redirectURI, func(t *testing.T) {
			if w := authorize(redirectURI); w.Code != http.StatusFound {
				t.Errorf("expected 302 for %q, got %d: %s",
					redirectURI, w.Code, strings.TrimSpace(w.Body.String()))
			}
		})
	}

	// Relaxing the port must not relax the host — these are the cases that
	// would turn the fix into an open redirect.
	for _, redirectURI := range []string{
		"http://evil.example.com:3118/callback",
		"http://localhost.evil.com/callback",
		"http://localhost:3118/other",
		"https://localhost:3118/callback",
		"attacker://callback",
	} {
		t.Run("rejected "+redirectURI, func(t *testing.T) {
			if w := authorize(redirectURI); w.Code != http.StatusBadRequest {
				t.Errorf("expected 400 for %q, got %d", redirectURI, w.Code)
			}
		})
	}
}
