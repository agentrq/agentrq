// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/mustafaturan/monoflake"
)

// failingTokens is a token service whose minting fails, for the paths that
// must not hand out a half-made credential. Everything else is the embedded
// service's.
type failingTokens struct {
	auth.TokenService
}

func (failingTokens) CreateClientRegistrationToken([]string, string) (string, error) {
	return "", errors.New("signing key unavailable")
}

func (failingTokens) CreateMCPToken(string, string, string) (string, error) {
	return "", errors.New("signing key unavailable")
}

func registerRequest(method, body string) *http.Request {
	req := httptest.NewRequest(method, "/mcp/12345/oauth2/register", strings.NewReader(body))
	req.SetPathValue("workspaceID", "12345")
	return req
}

func TestRegister_RefusesAnythingButPOST(t *testing.T) {
	mux, _ := setupTestRouter()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, registerRequest(http.MethodGet, ""))

	if w.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET register answered %d, want 405", w.Code)
	}
}

// A JSON null is an empty registration, not a malformed one: it gets a client
// with nothing registered.
func TestRegister_ANullBodyRegistersAnEmptyClient(t *testing.T) {
	mux, _ := setupTestRouter()

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, registerRequest(http.MethodPost, "null"))

	if w.Code != http.StatusCreated {
		t.Fatalf("register answered %d: %s", w.Code, w.Body.String())
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	if got["client_id"] != "mocked-client-id-" || got["token_endpoint_auth_method"] != "none" {
		t.Errorf("registered %v, want a public client with no redirect_uris", got)
	}
}

func TestRegister_RefusesMalformedRedirectURIs(t *testing.T) {
	mux, _ := setupTestRouter()

	for _, body := range []string{
		`{"redirect_uris":"https://agentrq.com/callback"}`,
		`{"redirect_uris":[""]}`,
		`{"redirect_uris":[42]}`,
		`{"redirect_uris":["http://[::1"]}`,
	} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, registerRequest(http.MethodPost, body))

		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_redirect_uri") {
			t.Errorf("%s answered %d %q, want 400 invalid_redirect_uri", body, w.Code, w.Body.String())
		}
	}
}

// A client_id that could not be signed is reported, never issued blank.
func TestRegister_ReportsAClientIDThatCouldNotBeSigned(t *testing.T) {
	h := &handler{tokenSvc: failingTokens{}}

	w := httptest.NewRecorder()
	h.oauthRegisterHandler().ServeHTTP(w, registerRequest(http.MethodPost, `{"redirect_uris":["cursor://callback"]}`))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "failed to register client") {
		t.Fatalf("answered %d %q, want 400 failed to register client", w.Code, w.Body.String())
	}
}

// Both discovery documents are per workspace, so a request that names none
// finds nothing.
func TestDiscovery_ARequestNamingNoWorkspaceIsNotFound(t *testing.T) {
	h := &handler{baseURL: "https://agentrq.com"}
	for name, endpoint := range map[string]http.Handler{
		"protected resource":   h.oauthProtectedResourceHandler(),
		"authorization server": h.oauthMetadataHandler(),
	} {
		req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
		req.Host = "agentrq.com"

		w := httptest.NewRecorder()
		endpoint.ServeHTTP(w, req)

		if w.Code != http.StatusNotFound {
			t.Errorf("%s answered %d, want 404", name, w.Code)
		}
	}
}

// ownerCrud is a workspace store whose workspace may be missing, or owned by
// somebody other than the signed-in person.
type ownerCrud struct {
	mockCrud
	owner int64
	err   error
}

func (c *ownerCrud) SystemGetWorkspace(ctx context.Context, id int64) (entity.Workspace, error) {
	if c.err != nil {
		return entity.Workspace{}, c.err
	}
	return entity.Workspace{ID: id, UserID: c.owner}, nil
}

func ownerRouter(crud *ownerCrud) *http.ServeMux {
	mux := http.NewServeMux()
	New(Params{TokenSvc: &mockTokenSvc{}, Crud: crud, CIMD: &fakeCIMD{}, BaseURL: "https://agentrq.com", Mux: mux})
	return mux
}

func TestAuthorize_AnUnknownWorkspaceIsNotFound(t *testing.T) {
	mux := ownerRouter(&ownerCrud{err: errors.New("record not found")})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("", "https://agentrq.com/callback"))

	if w.Code != http.StatusNotFound {
		t.Fatalf("authorize answered %d, want 404", w.Code)
	}
}

// Signed in is not enough: the workspace has to be the signed-in person's own.
func TestAuthorize_SomebodyElsesWorkspaceIsForbidden(t *testing.T) {
	mux := ownerRouter(&ownerCrud{owner: monoflake.IDFromBase62("someoneElse").Int64()})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("", "https://agentrq.com/callback"))

	if w.Code != http.StatusForbidden {
		t.Fatalf("authorize answered %d, want 403", w.Code)
	}
}

// A client whose metadata document cannot be fetched is refused before anyone
// is asked to sign in.
func TestAuthorize_AnUnresolvableMetadataDocumentIsRefused(t *testing.T) {
	mux := setupBindingRouter(&bindingTokenSvc{}, &fakeCIMD{err: errors.New("connection refused")})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, authorizeRequest("https://client.example/metadata.json", "https://agentrq.com/callback"))

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid_client") {
		t.Fatalf("authorize answered %d %q, want 400 invalid_client", w.Code, w.Body.String())
	}
}

// signedOutReturn follows a signed-out authorize request to the login page and
// returns where that page is told to send the person back to.
func signedOutReturn(t *testing.T, h http.Handler, req *http.Request) string {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusFound {
		t.Fatalf("authorize answered %d, want a redirect to the login page", w.Code)
	}
	loc, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatalf("Location is not a URL: %v", err)
	}
	return loc.Query().Get("redirect_url")
}

// The way back from the login page is on the host the request came to, over
// plain http only when nothing says the request was secure.
func TestAuthorize_SignedOutReturnURL(t *testing.T) {
	h := (&handler{tokenSvc: &mockTokenSvc{}, crud: &mockCrud{}, cimd: &fakeCIMD{}, baseURL: "https://agentrq.com"}).oauthAuthorizeHandler()
	padded := monoflake.ID(monoflake.IDFromBase62("12345").Int64()).String()

	t.Run("plain http on a local host", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp/12345/oauth2/authorize?redirect_uri=/callback", nil)
		req.SetPathValue("workspaceID", "12345")
		req.Host = "localhost:8080"

		want := "http://localhost:8080/mcp/12345/oauth2/authorize?redirect_uri=/callback"
		if got := signedOutReturn(t, h, req); got != want {
			t.Errorf("return URL %q, want %q", got, want)
		}
	})

	// On the workspace's own subdomain the workspace is the origin, so its path
	// prefix is dropped — down to the root when nothing follows it.
	t.Run("the subdomain drops the workspace prefix", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+padded+"/oauth2/authorize?redirect_uri=/callback", nil)
		req.SetPathValue("workspaceID", padded)
		req.Host = "ws.mcp.agentrq.com"

		want := "https://ws.mcp.agentrq.com/oauth2/authorize?redirect_uri=/callback"
		if got := signedOutReturn(t, h, req); got != want {
			t.Errorf("return URL %q, want %q", got, want)
		}
	})

	t.Run("the bare prefix becomes the root", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/mcp/"+padded+"?redirect_uri=/callback", nil)
		req.SetPathValue("workspaceID", padded)
		req.Host = "ws.mcp.agentrq.com"

		want := "https://ws.mcp.agentrq.com/?redirect_uri=/callback"
		if got := signedOutReturn(t, h, req); got != want {
			t.Errorf("return URL %q, want %q", got, want)
		}
	})
}

func tokenRequest(form url.Values) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/mcp/12345/oauth2/token", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func TestToken_RefusesAFormThatDoesNotParse(t *testing.T) {
	h := &handler{tokenSvc: streamTokens}
	req := httptest.NewRequest(http.MethodPost, "/mcp/12345/oauth2/token", strings.NewReader("grant_type=%zz"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	w := httptest.NewRecorder()
	h.oauthTokenHandler().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "invalid form") {
		t.Fatalf("answered %d %q, want 400 invalid form", w.Code, w.Body.String())
	}
}

// Each grant is redeemed only with the kind of token it is for.
func TestToken_RefusesTheWrongGrant(t *testing.T) {
	h := &handler{tokenSvc: streamTokens}
	access := mintWorkspaceToken(t, streamWorkspaceID, "access")

	cases := []struct {
		name string
		form url.Values
		code int
		want string
	}{
		{"an unsupported grant", url.Values{"grant_type": {"client_credentials"}}, http.StatusBadRequest, "unsupported_grant_type"},
		{"a code that does not verify", url.Values{"grant_type": {"authorization_code"}, "code": {"forged"}}, http.StatusUnauthorized, "invalid_grant"},
		{"an access token presented as a code", url.Values{"grant_type": {"authorization_code"}, "code": {access}}, http.StatusUnauthorized, "invalid_grant"},
		{"an access token presented as a refresh token", url.Values{"grant_type": {"refresh_token"}, "refresh_token": {access}}, http.StatusUnauthorized, "invalid_grant"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			h.oauthTokenHandler().ServeHTTP(w, tokenRequest(tc.form))

			if w.Code != tc.code || !strings.Contains(w.Body.String(), tc.want) {
				t.Fatalf("answered %d %q, want %d %s", w.Code, w.Body.String(), tc.code, tc.want)
			}
		})
	}
}

// A refresh token buys a fresh access token for the same workspace.
func TestToken_ARefreshTokenIsExchanged(t *testing.T) {
	h := &handler{tokenSvc: streamTokens}
	refresh := mintWorkspaceToken(t, streamWorkspaceID, "refresh")

	w := httptest.NewRecorder()
	h.oauthTokenHandler().ServeHTTP(w, tokenRequest(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}))

	if w.Code != http.StatusOK {
		t.Fatalf("refresh answered %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	claims, err := streamTokens.ValidateToken(got.AccessToken)
	if err != nil {
		t.Fatalf("the new access token does not verify: %v", err)
	}
	if !auth.HasAudience(claims, streamWorkspaceID) || !auth.HasAudience(claims, "access") || claims.Subject != streamUser {
		t.Errorf("new access token is for %v / %s, want workspace %s access for %s", claims.Audience, claims.Subject, streamWorkspaceID, streamUser)
	}
}

func TestToken_ReportsAnAccessTokenThatCouldNotBeSigned(t *testing.T) {
	refresh := mintWorkspaceToken(t, streamWorkspaceID, "refresh")
	h := &handler{tokenSvc: failingTokens{streamTokens}}

	w := httptest.NewRecorder()
	h.oauthTokenHandler().ServeHTTP(w, tokenRequest(url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}))

	if w.Code != http.StatusInternalServerError || !strings.Contains(w.Body.String(), "server_error") {
		t.Fatalf("answered %d %q, want 500 server_error", w.Code, w.Body.String())
	}
}
