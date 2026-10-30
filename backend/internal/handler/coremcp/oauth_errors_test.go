// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// mintFailingTokens verifies with the embedded service but cannot sign
// anything, for the paths that must not hand out a half-made credential.
type mintFailingTokens struct {
	auth.TokenService
}

func (mintFailingTokens) CreateClientRegistrationToken([]string, string) (string, error) {
	return "", errors.New("signing key unavailable")
}

func (mintFailingTokens) CreateMCPToken(string, string, string) (string, error) {
	return "", errors.New("signing key unavailable")
}

func supervisorRegister(h *handler, method, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.oauthRegisterHandler().ServeHTTP(w, httptest.NewRequest(method, "/mcp/oauth2/register", strings.NewReader(body)))
	return w
}

func TestSupervisorRegister_RefusesAnythingButPOST(t *testing.T) {
	h, _ := consentHandler()
	if w := supervisorRegister(h, http.MethodGet, ""); w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET register answered %d, want 405", w.Code)
	}
}

func TestSupervisorRegister_RefusesABodyThatIsNotJSON(t *testing.T) {
	h, _ := consentHandler()
	w := supervisorRegister(h, http.MethodPost, "redirect_uris=cursor://callback")
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_client_metadata") {
		t.Fatalf("answered %d %q, want 400 invalid_client_metadata", w.Code, w.Body.String())
	}
}

// A JSON null is an empty registration: a public client with nothing
// registered, not a malformed request.
func TestSupervisorRegister_ANullBodyRegistersAnEmptyClient(t *testing.T) {
	h, _ := consentHandler()
	w := supervisorRegister(h, http.MethodPost, "null")
	if w.Code != http.StatusCreated {
		t.Fatalf("register answered %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	if got["token_endpoint_auth_method"] != "none" {
		t.Errorf("registered %v, want a public client", got)
	}
}

func TestSupervisorRegister_RefusesMalformedRedirectURIs(t *testing.T) {
	h, _ := consentHandler()
	for _, body := range []string{
		`{"redirect_uris":"cursor://callback"}`,
		`{"redirect_uris":[""]}`,
		`{"redirect_uris":[42]}`,
		`{"redirect_uris":["http://[::1"]}`,
	} {
		w := supervisorRegister(h, http.MethodPost, body)
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_redirect_uri") {
			t.Errorf("%s answered %d %q, want 400 invalid_redirect_uri", body, w.Code, w.Body.String())
		}
	}
}

func TestSupervisorRegister_ReportsAClientIDThatCouldNotBeSigned(t *testing.T) {
	h := &handler{tokenSvc: mintFailingTokens{}}
	w := supervisorRegister(h, http.MethodPost, `{"redirect_uris":["cursor://callback"]}`)
	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "failed to register client") {
		t.Fatalf("answered %d %q, want 400 failed to register client", w.Code, w.Body.String())
	}
}

// A client whose metadata document cannot be fetched is refused before anyone
// is asked to sign in.
func TestSupervisorAuthorize_AnUnresolvableMetadataDocumentIsRefused(t *testing.T) {
	h, _ := consentHandler()
	req := supervisorAuthorizeRequest()
	q := req.URL.Query()
	q.Set("client_id", "https://unknown.example/metadata.json")
	req.URL.RawQuery = q.Encode()

	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_client") {
		t.Fatalf("authorize answered %d %q, want 400 invalid_client", w.Code, w.Body.String())
	}
}

// Signed out on a plain-http local host, the way back from the login page is
// plain http too.
func TestSupervisorAuthorize_SignedOutOnALocalHostReturnsOverHTTP(t *testing.T) {
	h, _ := consentHandler()
	req := httptest.NewRequest(http.MethodGet, "/mcp/oauth2/authorize?redirect_uri=/callback", nil)
	req.Host = "localhost:8080"

	w := httptest.NewRecorder()
	h.oauthAuthorizeHandler().ServeHTTP(w, req)

	if w.Code != http.StatusFound {
		t.Fatalf("authorize answered %d, want a redirect to the login page", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location is not a URL: %v", err)
	}
	if got, want := loc.Query().Get("redirect_url"), "http://localhost:8080/mcp/oauth2/authorize?redirect_uri=/callback"; got != want {
		t.Errorf("return URL %q, want %q", got, want)
	}
}

func supervisorTokenRequest(h *handler, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/mcp/oauth2/token", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	h.oauthTokenHandler().ServeHTTP(w, req)
	return w
}

// Each grant is redeemed only with a supervisor token of the kind it is for.
func TestSupervisorToken_RefusesTheWrongGrant(t *testing.T) {
	h := &handler{tokenSvc: supervisorTokens}
	access := mintSupervisorToken(t, "user-1", "coremcp", "access")
	workspaceRefresh := mintSupervisorToken(t, "user-1", "000000003D7", "refresh")

	cases := []struct {
		name string
		body string
		code int
		want string
	}{
		{"a form that does not parse", "grant_type=%zz", http.StatusBadRequest, "invalid form"},
		{"an unsupported grant", url.Values{"grant_type": {"client_credentials"}}.Encode(), http.StatusBadRequest, "unsupported_grant_type"},
		{"a refresh token that does not verify", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {"forged"}}.Encode(), http.StatusUnauthorized, "invalid_grant"},
		{"a workspace's refresh token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {workspaceRefresh}}.Encode(), http.StatusUnauthorized, "invalid_grant"},
		{"an access token presented as a code", url.Values{"grant_type": {"authorization_code"}, "code": {access}}.Encode(), http.StatusUnauthorized, "invalid_grant"},
		{"an access token presented as a refresh token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {access}}.Encode(), http.StatusUnauthorized, "invalid_grant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := supervisorTokenRequest(h, tc.body)
			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("answered %d %q, want %d %s", w.Code, w.Body.String(), tc.code, tc.want)
			}
		})
	}
}

// A supervisor refresh token buys a fresh supervisor access token.
func TestSupervisorToken_ARefreshTokenIsExchanged(t *testing.T) {
	h := &handler{tokenSvc: supervisorTokens}
	refresh := mintSupervisorToken(t, "user-1", "coremcp", "refresh")

	w := supervisorTokenRequest(h, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}.Encode())

	if w.Code != http.StatusOK {
		t.Fatalf("refresh answered %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	claims, err := supervisorTokens.ValidateToken(got.AccessToken)
	if err != nil {
		t.Fatalf("the new access token does not verify: %v", err)
	}
	if !auth.HasAudience(claims, "coremcp") || !auth.HasAudience(claims, "access") || claims.Subject != "user-1" {
		t.Errorf("new access token is for %v / %s, want a supervisor access token for user-1", claims.Audience, claims.Subject)
	}
}

func TestSupervisorToken_ReportsAnAccessTokenThatCouldNotBeSigned(t *testing.T) {
	h := &handler{tokenSvc: mintFailingTokens{supervisorTokens}}
	refresh := mintSupervisorToken(t, "user-1", "coremcp", "refresh")

	w := supervisorTokenRequest(h, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}.Encode())

	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "server_error") {
		t.Fatalf("answered %d %q, want 500 server_error", w.Code, w.Body.String())
	}
}
