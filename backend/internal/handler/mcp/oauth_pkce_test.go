// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/handler/oauthconsent/oauthconsenttest"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

// A verifier and its S256 challenge, worked out independently of this code.
const (
	pkceVerifier  = "dBjftJeZ4CVP-mJ0kjHnc6Pe_6e3ITt5FdeB7-xSjsk"
	pkceChallenge = "Qllz9EE2koVDgFtnbz1fqtEX4WJlP3FUeE6J4yP4c5s"
)

// pkceTokens signs codes for real, so the challenge travels inside the code
// from authorize to token exactly as it does in production.
type pkceTokens struct{ auth.TokenService }

func (pkceTokens) ValidateSessionToken(tokenStr string) (*auth.Claims, error) {
	if tokenStr == "valid-auth-cookie" {
		return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: monoflake.IDFromBase62("user123").String()}}, nil
	}
	return nil, jwt.ErrSignatureInvalid
}

func pkceRouter() *http.ServeMux {
	return setupBindingRouter(pkceTokens{auth.NewTokenService(auth.TokenConfig{JWTSecret: "pkce-test-secret"})}, &fakeCIMD{})
}

// workspaceCode walks an authorize request through the consent page and
// returns the code it redirects with, or the response that refused it.
func workspaceCode(t *testing.T, mux *http.ServeMux, challenge, method string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	req := authorizeRequest("", "cursor://callback")
	q := req.URL.Query()
	if challenge != "" {
		q.Set("code_challenge", challenge)
	}
	if method != "" {
		q.Set("code_challenge_method", method)
	}
	req.URL.RawQuery = q.Encode()

	w := oauthconsenttest.Allow(mux, req)
	if w.Code != http.StatusFound {
		return "", w
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("redirect: %v", err)
	}
	return loc.Query().Get("code"), w
}

func workspaceToken(mux *http.ServeMux, code, verifier string) *httptest.ResponseRecorder {
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}}
	if verifier != "" {
		form.Set("code_verifier", verifier)
	}
	req := httptest.NewRequest("POST", "/mcp/12345/oauth2/token", strings.NewReader(form.Encode()))
	req.SetPathValue("workspaceID", "12345")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)
	return w
}

func TestWorkspacePKCE_TheCodeIsRedeemedOnlyWithItsVerifier(t *testing.T) {
	mux := pkceRouter()

	for name, tc := range map[string]struct {
		verifier string
		want     int
	}{
		"its verifier":       {pkceVerifier, http.StatusOK},
		"another verifier":   {strings.Repeat("a", 43), http.StatusUnauthorized},
		"no verifier at all": {"", http.StatusUnauthorized},
	} {
		t.Run(name, func(t *testing.T) {
			code, w := workspaceCode(t, mux, pkceChallenge, "S256")
			if code == "" {
				t.Fatalf("authorize answered %d: %s", w.Code, w.Body.String())
			}
			if got := workspaceToken(mux, code, tc.verifier); got.Code != tc.want {
				t.Errorf("token answered %d, want %d: %s", got.Code, tc.want, got.Body.String())
			}
		})
	}
}

// Clients that never sent PKCE keep signing in, and a verifier cannot be
// sent to a code that was not bound to one.
func TestWorkspacePKCE_ACodeWithoutAChallenge(t *testing.T) {
	mux := pkceRouter()

	code, w := workspaceCode(t, mux, "", "")
	if code == "" {
		t.Fatalf("authorize answered %d: %s", w.Code, w.Body.String())
	}
	if got := workspaceToken(mux, code, ""); got.Code != http.StatusOK {
		t.Errorf("token without PKCE answered %d: %s", got.Code, got.Body.String())
	}
	if got := workspaceToken(mux, code, pkceVerifier); got.Code != http.StatusUnauthorized {
		t.Errorf("a verifier for an unbound code answered %d, want 401", got.Code)
	}
}

func TestWorkspacePKCE_OnlyS256IsAccepted(t *testing.T) {
	if code, w := workspaceCode(t, pkceRouter(), pkceChallenge, "plain"); code != "" || w.Code != http.StatusBadRequest {
		t.Errorf("a plain challenge answered %d, want 400", w.Code)
	}
}

func TestWorkspaceMetadata_AdvertisesS256(t *testing.T) {
	mux, _ := setupTestRouter()
	req := httptest.NewRequest("GET", "/mcp/12345/.well-known/oauth-authorization-server", nil)
	req.SetPathValue("workspaceID", "12345")
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	var doc struct {
		Methods []string `json:"code_challenge_methods_supported"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &doc); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(doc.Methods) != 1 || doc.Methods[0] != "S256" {
		t.Errorf("code_challenge_methods_supported = %v, want [S256]", doc.Methods)
	}
}
