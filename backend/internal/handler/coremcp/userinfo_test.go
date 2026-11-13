// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

const userInfoSecret = "userinfo-test-secret"

// userInfoUser is the one user the userinfo tests' controller knows.
var userInfoUser = entity.User{ID: 4242424242, Email: "ada@example.com", Name: "Ada", Picture: "https://example.com/ada.png"}

// userInfoUsers is the one controller call the userinfo handler makes.
type userInfoUsers struct {
	crud.Controller
	users map[int64]entity.User
}

func (u userInfoUsers) FindUserByID(ctx context.Context, id int64) (entity.User, error) {
	if user, ok := u.users[id]; ok {
		return user, nil
	}
	return entity.User{}, errors.New("record not found")
}

func userInfoHandler() *handler {
	return &handler{
		crud:     userInfoUsers{users: map[int64]entity.User{userInfoUser.ID: userInfoUser}},
		tokenSvc: auth.NewTokenService(auth.TokenConfig{JWTSecret: userInfoSecret}),
	}
}

var userInfoSubject = monoflake.ID(userInfoUser.ID).String()

func mintMCP(t *testing.T, subject, resource, kind string) string {
	t.Helper()
	tok, err := auth.NewTokenService(auth.TokenConfig{JWTSecret: userInfoSecret}).CreateMCPToken(subject, resource, kind)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func signClaims(t *testing.T, method jwt.SigningMethod, key any, claims auth.Claims) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func askUserInfo(h *handler, prepare func(*http.Request)) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, "/mcp/oauth2/userinfo", nil)
	prepare(req)
	w := httptest.NewRecorder()
	h.oauthUserInfoHandler().ServeHTTP(w, req)
	return w
}

func bearer(tok string) func(*http.Request) {
	return func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }
}

func TestUserInfo_ASupervisorAccessTokenNamesItsUser(t *testing.T) {
	for _, resource := range []string{resourceSupervisor, resourceSupervisorReadOnly} {
		t.Run(resource, func(t *testing.T) {
			w := askUserInfo(userInfoHandler(), bearer(mintMCP(t, userInfoSubject, resource, "access")))
			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body %s", w.Code, w.Body)
			}
			if got := w.Header().Get("Cache-Control"); got != "no-store" {
				t.Errorf("Cache-Control = %q, want no-store", got)
			}
			var got map[string]string
			if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"sub": userInfoSubject, "id": userInfoSubject,
				"email": userInfoUser.Email, "name": userInfoUser.Name, "picture": userInfoUser.Picture,
			}
			if len(got) != len(want) {
				t.Errorf("body = %v, want exactly %v", got, want)
			}
			for k, v := range want {
				if got[k] != v {
					t.Errorf("%s = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestUserInfo_RefusesEveryOtherCredentialAlike(t *testing.T) {
	future := jwt.NewNumericDate(time.Now().Add(time.Hour))
	supervisorClaims := func(exp *jwt.NumericDate) auth.Claims {
		return auth.Claims{RegisteredClaims: jwt.RegisteredClaims{
			Subject: userInfoSubject, ExpiresAt: exp, Audience: jwt.ClaimStrings{resourceSupervisor, "access"},
		}}
	}
	session, err := auth.NewTokenService(auth.TokenConfig{JWTSecret: userInfoSecret}).CreateToken(userInfoSubject, userInfoUser.Email, "", "")
	if err != nil {
		t.Fatal(err)
	}
	code, err := auth.NewTokenService(auth.TokenConfig{JWTSecret: userInfoSecret}).CreateOAuthCodeToken(userInfoSubject, resourceSupervisor, "")
	if err != nil {
		t.Fatal(err)
	}
	valid := mintMCP(t, userInfoSubject, resourceSupervisor, "access")

	cases := map[string]func(*http.Request){
		"no Authorization header": func(*http.Request) {},
		"an empty Bearer":         bearer(""),
		"another scheme":          func(r *http.Request) { r.Header.Set("Authorization", "Basic "+valid) },
		"the token as the at cookie": func(r *http.Request) {
			r.AddCookie(&http.Cookie{Name: "at", Value: valid})
		},
		"the token as a query parameter": func(r *http.Request) {
			r.URL.RawQuery = "token=" + valid
		},
		"a malformed token": bearer("not-a-jwt"),
		"an expired token": bearer(signClaims(t, jwt.SigningMethodHS256, []byte(userInfoSecret),
			supervisorClaims(jwt.NewNumericDate(time.Now().Add(-time.Minute))))),
		"another secret's signature": bearer(signClaims(t, jwt.SigningMethodHS256, []byte("some-other-secret"),
			supervisorClaims(future))),
		"alg none": bearer(signClaims(t, jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType,
			supervisorClaims(future))),
		"a workspace token":          bearer(mintMCP(t, userInfoSubject, "0lvOHHLz23Q", "access")),
		"a supervisor refresh token": bearer(mintMCP(t, userInfoSubject, resourceSupervisor, "refresh")),
		"a supervisor code":          bearer(code),
		"a session token":            bearer(session),
		"a token with no subject":    bearer(mintMCP(t, "", resourceSupervisor, "access")),
		"a deleted user":             bearer(mintMCP(t, monoflake.ID(777).String(), resourceSupervisor, "access")),
	}
	for name, prepare := range cases {
		t.Run(name, func(t *testing.T) {
			w := askUserInfo(userInfoHandler(), prepare)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401; body %s", w.Code, w.Body)
			}
			if got := w.Header().Get("WWW-Authenticate"); got != `Bearer error="invalid_token"` {
				t.Errorf("WWW-Authenticate = %q", got)
			}
			if w.Body.Len() != 0 {
				t.Errorf("body = %q, want none: the reason is never told", w.Body)
			}
		})
	}
}

func TestUserInfo_AnswersOnlyGetAndPost(t *testing.T) {
	h := userInfoHandler()
	tok := mintMCP(t, userInfoSubject, resourceSupervisor, "access")
	for method, want := range map[string]int{http.MethodPost: http.StatusOK, http.MethodPut: http.StatusMethodNotAllowed} {
		req := httptest.NewRequest(method, "/mcp/oauth2/userinfo", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		w := httptest.NewRecorder()
		h.oauthUserInfoHandler().ServeHTTP(w, req)
		if w.Code != want {
			t.Errorf("%s: status = %d, want %d", method, w.Code, want)
		}
	}
}

// The metadata advertises the endpoint, and it is served there, on both the
// local path and the mcp. host.
func TestUserInfo_IsAdvertisedWhereItIsServed(t *testing.T) {
	mux := http.NewServeMux()
	if _, err := New(Params{
		Mux: mux, Crud: userInfoUsers{users: map[int64]entity.User{userInfoUser.ID: userInfoUser}},
		TokenSvc: auth.NewTokenService(auth.TokenConfig{JWTSecret: userInfoSecret}),
		BaseURL:  "https://agentrq.com", Domain: "agentrq.com",
	}); err != nil {
		t.Fatal(err)
	}
	tok := mintMCP(t, userInfoSubject, resourceSupervisor, "access")

	for _, host := range []string{"agentrq.com", "mcp.agentrq.com"} {
		t.Run(host, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/.well-known/oauth-authorization-server", nil)
			req.Host = host
			req.Header.Set("X-Forwarded-Proto", "https")
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			var meta map[string]any
			if err := json.Unmarshal(w.Body.Bytes(), &meta); err != nil {
				t.Fatalf("metadata: %v", err)
			}
			endpoint, _ := meta["userinfo_endpoint"].(string)
			if endpoint == "" {
				t.Fatalf("no userinfo_endpoint in %v", meta)
			}

			req = httptest.NewRequest(http.MethodGet, endpoint, nil)
			req.Header.Set("Authorization", "Bearer "+tok)
			w = httptest.NewRecorder()
			mux.ServeHTTP(w, req)
			if w.Code != http.StatusOK {
				t.Fatalf("GET %s = %d, want 200; body %s", endpoint, w.Code, w.Body)
			}
		})
	}
}

// The CoreMCP endpoint and userinfo share this one check, so neither can
// accept a token the other refuses.
func TestAccessResource_IsASupervisorAccessTokenForSomeone(t *testing.T) {
	claims := func(subject string, aud ...string) *auth.Claims {
		return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: subject, Audience: aud}}
	}
	cases := []struct {
		name   string
		claims *auth.Claims
		want   string
	}{
		{"read and write", claims("u", resourceSupervisor, "access"), resourceSupervisor},
		{"read only", claims("u", resourceSupervisorReadOnly, "access"), resourceSupervisorReadOnly},
		{"no claims", nil, ""},
		{"no subject", claims("", resourceSupervisor, "access"), ""},
		{"a refresh token", claims("u", resourceSupervisor, "refresh"), ""},
		{"a code", claims("u", resourceSupervisor, "authorization_code"), ""},
		{"a workspace", claims("u", "0lvOHHLz23Q", "access"), ""},
	}
	for _, c := range cases {
		if got := accessResource(c.claims); got != c.want {
			t.Errorf("%s: accessResource = %q, want %q", c.name, got, c.want)
		}
	}
}
