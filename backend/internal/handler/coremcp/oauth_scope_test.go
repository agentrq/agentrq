// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
)

// codeTokenSvc accepts "auth-code" as an authorization code for the
// supervisor.
type codeTokenSvc struct{ authorizeTokenSvc }

func (codeTokenSvc) ValidateToken(tokenStr string) (*auth.Claims, error) {
	if tokenStr == "auth-code" {
		return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{
			Subject:  "user-1",
			Audience: jwt.ClaimStrings{"coremcp", "authorization_code"},
		}}, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

func wellKnownRequest(path string) *http.Request {
	r := httptest.NewRequest("GET", path, nil)
	r.Host = "mcp.agentrq.com"
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}

func TestSupervisorMetadata_AdvertisesTheSupervisorScope(t *testing.T) {
	h := &handler{}
	for name, handler := range map[string]http.Handler{
		"authorization server": h.oauthMetadataHandler(),
		"protected resource":   h.oauthProtectedResourceHandler(),
	} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, wellKnownRequest("/.well-known/x"))
		var doc struct {
			ScopesSupported []string `json:"scopes_supported"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
			t.Fatalf("%s: decode: %v", name, err)
		}
		if len(doc.ScopesSupported) != 1 || doc.ScopesSupported[0] != "supervisor-mcp" {
			t.Errorf("%s metadata scopes_supported = %v, want [supervisor-mcp]", name, doc.ScopesSupported)
		}
	}
}

func TestSupervisorUnauthorized_ChallengeNamesTheScope(t *testing.T) {
	h := &handler{}
	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, httptest.NewRequest("POST", "/mcp", nil))
	if challenge := w.Header().Get("WWW-Authenticate"); !strings.Contains(challenge, `scope="supervisor-mcp"`) {
		t.Errorf(`WWW-Authenticate = %q, want it to carry scope="supervisor-mcp"`, challenge)
	}
}

func TestSupervisorToken_GrantsTheSupervisorScope(t *testing.T) {
	h := &handler{tokenSvc: codeTokenSvc{}}
	// A scope the server does not have is answered with the one it does,
	// never refused: clients send defaults such as offline_access unasked.
	form := url.Values{"grant_type": {"authorization_code"}, "code": {"auth-code"}, "scope": {"openid offline_access"}}
	req := httptest.NewRequest("POST", "/mcp/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.oauthTokenHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("token answered %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp["scope"] != "supervisor-mcp" {
		t.Errorf("token response scope = %v, want supervisor-mcp", resp["scope"])
	}
}

func TestSupervisorAuthorize_AnUnknownScopeStillReachesConsent(t *testing.T) {
	h, _ := consentHandler()
	req := supervisorAuthorizeRequest()
	q := req.URL.Query()
	q.Set("scope", "openid offline_access")
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("authorize with an unknown scope answered %d (%s), want the consent page", w.Code, w.Header().Get("Location"))
	}
}
