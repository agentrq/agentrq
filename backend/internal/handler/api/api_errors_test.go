// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/agentlink"
	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	slackctrl "github.com/agentrq/agentrq/backend/internal/controller/slack"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/gofiber/fiber/v2"
	"github.com/golang-jwt/jwt/v5"
)

// The error paths of the sign-in and workspace routes in api.go, with the
// successes nothing else reaches. The task routes have their own file.

// ── Sign-in ──────────────────────────────────────────────────────────────────

// authPathTokens is the TokenService the sign-in routes touch. A state token is
// the provider and the redirect joined, so a test can forge or check one by
// hand; each *Err field makes its call fail.
type authPathTokens struct {
	auth.TokenService
	stateErr   error
	tokenErr   error
	refreshErr error
	mcpErr     error
	claims     *auth.Claims
}

func (m *authPathTokens) CreateOAuthStateToken(redirectURL, provider string) (string, error) {
	if m.stateErr != nil {
		return "", m.stateErr
	}
	return provider + ":" + redirectURL, nil
}

func (m *authPathTokens) ValidateOAuthStateToken(tokenStr, provider string) (string, error) {
	if !strings.HasPrefix(tokenStr, provider+":") {
		return "", errors.New("state was minted for another provider")
	}
	return strings.TrimPrefix(tokenStr, provider+":"), nil
}

func (m *authPathTokens) CreateToken(userID, email, name, picture string) (string, error) {
	if m.tokenErr != nil {
		return "", m.tokenErr
	}
	return "access-token", nil
}

func (m *authPathTokens) CreateRefreshToken(userID string) (string, error) {
	if m.refreshErr != nil {
		return "", m.refreshErr
	}
	return "refresh-token", nil
}

func (m *authPathTokens) ValidateRefreshToken(string) (*auth.Claims, error) {
	return m.claims, nil
}

func (m *authPathTokens) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	return "", m.mcpErr
}

// authPathCrud answers the user lookups a sign-in makes.
type authPathCrud struct {
	crud.Controller
	err error
	// sawEmail is the email the last FindOrCreateUser was asked for.
	sawEmail string
}

func (m *authPathCrud) FindOrCreateUser(_ context.Context, req entity.FindOrCreateUserRequest) (*entity.FindOrCreateUserResponse, error) {
	m.sawEmail = req.Email
	if m.err != nil {
		return nil, m.err
	}
	return &entity.FindOrCreateUserResponse{User: entity.User{ID: 7, Email: req.Email}}, nil
}

func (m *authPathCrud) FindUserByID(context.Context, int64) (entity.User, error) {
	return entity.User{ID: 7, Email: "a@b.com"}, nil
}

func authPathApp(h *handler) *fiber.App {
	app := fiber.New()
	h.router = app
	h.registerPublicAuthRoutes()
	h.registerProtectedAuthRoutes()
	return app
}

func setCookies(resp *http.Response) string {
	return strings.Join(resp.Header.Values("Set-Cookie"), "\n")
}

func TestRootLogin_ErrorPaths(t *testing.T) {
	cases := []struct {
		name    string
		enabled bool
		secret  string
		body    string
		crudErr error
		tokens  *authPathTokens
		want    int
	}{
		{"disabled", false, "s3cret", `{"rootToken":"s3cret"}`, nil, &authPathTokens{}, http.StatusForbidden},
		{"a body that is not JSON", true, "s3cret", `{`, nil, &authPathTokens{}, http.StatusBadRequest},
		{"the wrong token", true, "s3cret", `{"rootToken":"guess"}`, nil, &authPathTokens{}, http.StatusUnauthorized},
		{"no token configured", true, "", `{"rootToken":""}`, nil, &authPathTokens{}, http.StatusUnauthorized},
		{"the user cannot be synced", true, "s3cret", `{"rootToken":"s3cret"}`, errors.New("db down"), &authPathTokens{}, http.StatusInternalServerError},
		{"the session cannot be signed", true, "s3cret", `{"rootToken":"s3cret"}`, nil, &authPathTokens{tokenErr: errors.New("no key")}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			app := authPathApp(&handler{
				rootLoginEnabled: tc.enabled,
				rootToken:        tc.secret,
				crud:             &authPathCrud{err: tc.crudErr},
				tokenSvc:         tc.tokens,
			})
			resp, body := errPathDo(t, app, http.MethodPost, "/auth/root/login", tc.body)
			if resp.StatusCode != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, body)
			}
			if strings.Contains(setCookies(resp), "at=") {
				t.Errorf("a refused sign-in set a session: %s", setCookies(resp))
			}
		})
	}
}

// A refresh token that cannot be minted does not undo a sign-in: the person
// gets the session they would have had before refresh tokens existed.
func TestIssueSession_KeepsTheSignInWhenTheRefreshTokenFails(t *testing.T) {
	app := authPathApp(&handler{
		rootLoginEnabled: true,
		rootToken:        "s3cret",
		crud:             &authPathCrud{},
		tokenSvc:         &authPathTokens{refreshErr: errors.New("no key")},
	})

	resp, body := errPathDo(t, app, http.MethodPost, "/auth/root/login", `{"rootToken":"s3cret"}`)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
	}
	cookies := setCookies(resp)
	if !strings.Contains(cookies, "at=access-token") {
		t.Errorf("no access cookie: %s", cookies)
	}
	if strings.Contains(cookies, "rt=") {
		t.Errorf("a refresh cookie was set without a refresh token: %s", cookies)
	}
}

func TestRefreshSession_AnswersAFailedSigningWith500(t *testing.T) {
	app := authPathApp(&handler{
		crud: &authPathCrud{},
		tokenSvc: &authPathTokens{
			claims:   &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "7"}},
			tokenErr: errors.New("no key"),
		},
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/refresh", nil)
	req.Header.Set("Cookie", "rt=a-valid-refresh-token")
	resp, err := app.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	if strings.Contains(setCookies(resp), "at=") {
		t.Errorf("a failed refresh touched the session: %s", setCookies(resp))
	}
}

// Signing out expires both cookies, each at the path and domain it was set on:
// a deletion that does not match is no deletion at all.
func TestLogout_ExpiresBothCookiesWhereTheyWereSet(t *testing.T) {
	cases := []struct {
		domain, wantDomain string
	}{
		{"agentrq.example.com", "domain=.agentrq.example.com"},
		{"localhost:3000", ""},
		{"", ""},
	}
	for _, tc := range cases {
		t.Run("domain "+tc.domain, func(t *testing.T) {
			app := fiber.New()
			h := &handler{domain: tc.domain}
			app.Post("/auth/logout", h.logout())

			resp, _ := errPathDo(t, app, http.MethodPost, "/auth/logout", "")
			if resp.StatusCode != http.StatusNoContent {
				t.Fatalf("status = %d, want 204", resp.StatusCode)
			}
			cookies := resp.Header.Values("Set-Cookie")
			if len(cookies) != 2 {
				t.Fatalf("Set-Cookie = %q, want both halves", cookies)
			}
			if !strings.HasPrefix(cookies[0], "at=;") || !strings.HasPrefix(cookies[1], "rt=;") {
				t.Errorf("Set-Cookie = %q, want at then rt cleared", cookies)
			}
			if !strings.Contains(cookies[1], "path=/api/v1/auth/refresh") {
				t.Errorf("refresh cookie cleared at the wrong path: %q", cookies[1])
			}
			for _, c := range cookies {
				if tc.wantDomain == "" && strings.Contains(c, "domain=") {
					t.Errorf("cookie %q names a domain", c)
				}
				if tc.wantDomain != "" && !strings.Contains(c, tc.wantDomain) {
					t.Errorf("cookie %q, want %s", c, tc.wantDomain)
				}
			}
		})
	}
}

func TestGetAuthenticatedUser_WithoutASignedInUser(t *testing.T) {
	h := &handler{}
	app := fiber.New()
	app.Get("/anonymous", h.getAuthenticatedUser())
	app.Get("/odd-locals", func(c *fiber.Ctx) error {
		c.Locals("user_id", "u1")
		c.Locals("user_email", 42) // not a string: read as missing
		return h.getAuthenticatedUser()(c)
	})

	if resp, body := errPathDo(t, app, http.MethodGet, "/anonymous", ""); resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (body %s)", resp.StatusCode, body)
	}

	resp, body := errPathDo(t, app, http.MethodGet, "/odd-locals", "")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var user map[string]string
	if err := json.Unmarshal([]byte(body), &user); err != nil {
		t.Fatal(err)
	}
	if user["id"] != "u1" || user["email"] != "" {
		t.Errorf("user = %v, want id u1 and no email", user)
	}
}

// Both providers' login routes: the state carries where to return to, falling
// back to the app's root under its base path.
func TestOAuthLogin(t *testing.T) {
	type provider struct {
		name string
		path string
		auth func(h *handler, svc auth.Service)
	}
	providers := []provider{
		{"google", "/auth/google/login", func(h *handler, svc auth.Service) { h.auth = svc }},
		{"github", "/auth/github/login", func(h *handler, svc auth.Service) { h.githubAuth = svc; h.githubClientID = "gh-client" }},
	}
	for _, p := range providers {
		t.Run(p.name, func(t *testing.T) {
			newApp := func(basePath string, tokens *authPathTokens) *fiber.App {
				h := &handler{tokenSvc: tokens, basePath: basePath}
				p.auth(h, &mockAuthService{})
				return authPathApp(h)
			}

			t.Run("defaults to the root", func(t *testing.T) {
				resp, _ := errPathDo(t, newApp("", &authPathTokens{}), http.MethodGet, p.path, "")
				if resp.StatusCode != http.StatusFound {
					t.Fatalf("status = %d, want 302", resp.StatusCode)
				}
				if loc := resp.Header.Get("Location"); loc != "https://google.com/auth?state="+p.name+":/" {
					t.Errorf("Location = %q", loc)
				}
			})

			t.Run("defaults to the root under the base path", func(t *testing.T) {
				resp, _ := errPathDo(t, newApp("/agentrq", &authPathTokens{}), http.MethodGet, p.path, "")
				if loc := resp.Header.Get("Location"); loc != "https://google.com/auth?state="+p.name+":/agentrq/" {
					t.Errorf("Location = %q", loc)
				}
			})

			t.Run("keeps a same-origin redirect", func(t *testing.T) {
				resp, _ := errPathDo(t, newApp("", &authPathTokens{}), http.MethodGet, p.path+"?redirect_url=/workspaces", "")
				if loc := resp.Header.Get("Location"); loc != "https://google.com/auth?state="+p.name+":/workspaces" {
					t.Errorf("Location = %q", loc)
				}
			})

			t.Run("answers 500 when no state can be made", func(t *testing.T) {
				resp, body := errPathDo(t, newApp("", &authPathTokens{stateErr: errors.New("no key")}), http.MethodGet, p.path, "")
				if resp.StatusCode != http.StatusInternalServerError {
					t.Fatalf("status = %d, want 500 (body %s)", resp.StatusCode, body)
				}
			})
		})
	}

	t.Run("github is refused when it is not configured", func(t *testing.T) {
		app := authPathApp(&handler{tokenSvc: &authPathTokens{}})
		if resp, body := errPathDo(t, app, http.MethodGet, "/auth/github/login", ""); resp.StatusCode != http.StatusForbidden {
			t.Fatalf("status = %d, want 403 (body %s)", resp.StatusCode, body)
		}
	})
}

func TestOAuthCallback(t *testing.T) {
	type provider struct {
		name string
		path string
		auth func(h *handler, svc auth.Service)
	}
	providers := []provider{
		{"google", "/auth/google/callback", func(h *handler, svc auth.Service) { h.auth = svc }},
		{"github", "/auth/github/callback", func(h *handler, svc auth.Service) { h.githubAuth = svc }},
	}
	exchanged := func(context.Context, string) (*auth.User, error) {
		return &auth.User{Sub: "sub-1", Email: "a@b.com", Name: "Ada"}, nil
	}

	for _, p := range providers {
		t.Run(p.name, func(t *testing.T) {
			newApp := func(basePath string, exchange func(context.Context, string) (*auth.User, error), crudErr error, tokens *authPathTokens) (*fiber.App, *authPathCrud) {
				ctrl := &authPathCrud{err: crudErr}
				h := &handler{tokenSvc: tokens, crud: ctrl, basePath: basePath}
				p.auth(h, &mockAuthService{exchangeFunc: exchange})
				return authPathApp(h), ctrl
			}

			failures := []struct {
				name     string
				exchange func(context.Context, string) (*auth.User, error)
				crudErr  error
				tokens   *authPathTokens
			}{
				{"a code the provider refuses", func(context.Context, string) (*auth.User, error) { return nil, errors.New("bad code") }, nil, &authPathTokens{}},
				{"a user that cannot be synced", exchanged, errors.New("db down"), &authPathTokens{}},
				{"a session that cannot be signed", exchanged, nil, &authPathTokens{tokenErr: errors.New("no key")}},
			}
			for _, f := range failures {
				t.Run(f.name, func(t *testing.T) {
					app, _ := newApp("", f.exchange, f.crudErr, f.tokens)
					resp, body := errPathDo(t, app, http.MethodGet, p.path+"?code=c1", "")
					if resp.StatusCode != http.StatusInternalServerError {
						t.Fatalf("status = %d, want 500 (body %s)", resp.StatusCode, body)
					}
					if ct := resp.Header.Get("Content-Type"); ct != _mimeJSON {
						t.Errorf("Content-Type = %q, want %q", ct, _mimeJSON)
					}
					if strings.Contains(setCookies(resp), "at=") {
						t.Errorf("a failed sign-in set a session: %s", setCookies(resp))
					}
				})
			}

			t.Run("signs in and returns to the root under the base path", func(t *testing.T) {
				app, ctrl := newApp("/agentrq", exchanged, nil, &authPathTokens{})
				resp, _ := errPathDo(t, app, http.MethodGet, p.path+"?code=c1", "")
				if resp.StatusCode != http.StatusFound {
					t.Fatalf("status = %d, want 302", resp.StatusCode)
				}
				if loc := resp.Header.Get("Location"); loc != "/agentrq/" {
					t.Errorf("Location = %q, want /agentrq/", loc)
				}
				if ctrl.sawEmail != "a@b.com" {
					t.Errorf("synced %q, want the provider's email", ctrl.sawEmail)
				}
				cookies := setCookies(resp)
				if !strings.Contains(cookies, "at=access-token") || !strings.Contains(cookies, "rt=refresh-token") {
					t.Errorf("Set-Cookie = %s, want both halves of a session", cookies)
				}
			})

			t.Run("returns where the state says", func(t *testing.T) {
				app, _ := newApp("", exchanged, nil, &authPathTokens{})
				state := url.QueryEscape(p.name + ":/workspaces")
				resp, _ := errPathDo(t, app, http.MethodGet, p.path+"?code=c1&state="+state, "")
				if loc := resp.Header.Get("Location"); loc != "/workspaces" {
					t.Errorf("Location = %q, want /workspaces", loc)
				}
			})

			t.Run("ignores a state it cannot read", func(t *testing.T) {
				app, _ := newApp("", exchanged, nil, &authPathTokens{})
				resp, _ := errPathDo(t, app, http.MethodGet, p.path+"?code=c1&state=forged", "")
				if loc := resp.Header.Get("Location"); loc != "/" {
					t.Errorf("Location = %q, want /", loc)
				}
			})
		})
	}
}

// New mounts the push routes behind the session check only when push is
// configured, and a request's session claims reach the controller's context.
func TestNew_PushRoutesAndSessionClaims(t *testing.T) {
	tokens := auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"})
	session, err := tokens.CreateToken("user-1", "a@b.com", "Ada", "")
	if err != nil {
		t.Fatal(err)
	}
	ctrl := &wsPathCrud{}

	get := func(t *testing.T, app *fiber.App, path string) (*http.Response, string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(&http.Cookie{Name: "at", Value: session})
		resp, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(resp.Body)
		return resp, string(b)
	}

	app := fiber.New()
	if _, err := New(Params{
		Crud:     ctrl,
		TokenSvc: tokens,
		Router:   app.Group(_routeBasePath),
		PushCtrl: &mockPushController{vapidPublicKey: "vapid-key"},
	}); err != nil {
		t.Fatal(err)
	}

	resp, body := get(t, app, "/api/v1/push/vapid-public-key")
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "vapid-key") {
		t.Errorf("push route: %d %s", resp.StatusCode, body)
	}

	resp, body = get(t, app, "/api/v1/workspaces")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("list: %d %s", resp.StatusCode, body)
	}
	claims, _ := ctrl.ctx.Value(auth.CtxKeyMCPClaims).(*auth.Claims)
	if claims == nil || claims.Subject != "user-1" {
		t.Errorf("claims in the controller's context = %+v, want the session's", claims)
	}

	bare := fiber.New()
	if _, err := New(Params{Crud: ctrl, TokenSvc: tokens, Router: bare.Group(_routeBasePath)}); err != nil {
		t.Fatal(err)
	}
	if resp, _ := get(t, bare, "/api/v1/push/vapid-public-key"); resp.StatusCode != http.StatusNotFound {
		t.Errorf("push route without push: %d, want 404", resp.StatusCode)
	}
}

// ── Workspaces ───────────────────────────────────────────────────────────────

// wsPathCrud answers every call the workspace routes make: with err when it
// is set, otherwise with ws.
type wsPathCrud struct {
	crud.Controller
	err error
	ws  entity.Workspace

	// ctx is the context the last call was made with.
	ctx         context.Context
	statsRq     entity.GetWorkspaceStatsRequest
	userStatsRq entity.GetUserStatsRequest
}

func (m *wsPathCrud) CreateWorkspace(ctx context.Context, _ entity.CreateWorkspaceRequest) (*entity.CreateWorkspaceResponse, error) {
	m.ctx = ctx
	if m.err != nil {
		return nil, m.err
	}
	return &entity.CreateWorkspaceResponse{Workspace: m.ws}, nil
}

func (m *wsPathCrud) GetWorkspace(ctx context.Context, _ entity.GetWorkspaceRequest) (*entity.GetWorkspaceResponse, error) {
	m.ctx = ctx
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetWorkspaceResponse{Workspace: m.ws}, nil
}

func (m *wsPathCrud) ListWorkspaces(ctx context.Context, _ entity.ListWorkspacesRequest) (*entity.ListWorkspacesResponse, error) {
	m.ctx = ctx
	if m.err != nil {
		return nil, m.err
	}
	return &entity.ListWorkspacesResponse{}, nil
}

func (m *wsPathCrud) DeleteWorkspace(ctx context.Context, _ entity.DeleteWorkspaceRequest) error {
	m.ctx = ctx
	return m.err
}

func (m *wsPathCrud) UnarchiveWorkspace(ctx context.Context, _ entity.UnarchiveWorkspaceRequest) error {
	m.ctx = ctx
	return m.err
}

func (m *wsPathCrud) UpdateWorkspace(ctx context.Context, _ entity.UpdateWorkspaceRequest) (*entity.UpdateWorkspaceResponse, error) {
	m.ctx = ctx
	if m.err != nil {
		return nil, m.err
	}
	return &entity.UpdateWorkspaceResponse{Workspace: m.ws}, nil
}

func (m *wsPathCrud) GetDetailedWorkspaceStats(ctx context.Context, rq entity.GetWorkspaceStatsRequest) (*entity.GetDetailedWorkspaceStatsResponse, error) {
	m.ctx = ctx
	m.statsRq = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetDetailedWorkspaceStatsResponse{}, nil
}

func (m *wsPathCrud) GetDetailedUserStats(ctx context.Context, rq entity.GetUserStatsRequest) (*entity.GetDetailedUserStatsResponse, error) {
	m.ctx = ctx
	m.userStatsRq = rq
	if m.err != nil {
		return nil, m.err
	}
	return &entity.GetDetailedUserStatsResponse{}, nil
}

// wsPathSlack is a Slack controller that records the channel calls.
type wsPathSlack struct {
	slackctrl.Controller
	err    error
	cfg    *entity.SlackConfig
	cfgErr error

	set     *entity.SetWorkspaceSlackChannelRequest
	removed *entity.RemoveWorkspaceSlackChannelRequest
}

func (m *wsPathSlack) SetWorkspaceChannel(_ context.Context, req entity.SetWorkspaceSlackChannelRequest) error {
	m.set = &req
	return m.err
}

func (m *wsPathSlack) RemoveWorkspaceChannel(_ context.Context, req entity.RemoveWorkspaceSlackChannelRequest) error {
	m.removed = &req
	return m.err
}

func (m *wsPathSlack) GetWorkspaceSlackConfig(context.Context, int64) (*entity.SlackConfig, error) {
	return m.cfg, m.cfgErr
}

// wsPathApp mounts the workspace routes behind a stand-in for the auth
// middleware.
func wsPathApp(h *handler) *fiber.App {
	app := fiber.New()
	app.Use(func(c *fiber.Ctx) error {
		c.Locals("user_id", errPathUser)
		return c.Next()
	})
	h.router = app
	if err := h.registerWorkspaceRoutes(); err != nil {
		panic(err)
	}
	return app
}

func TestWorkspaceRoutes_RefuseWhatTheyCannotRead(t *testing.T) {
	app := wsPathApp(&handler{crud: &wsPathCrud{}, mcpManager: &fakeMCPManager{}})

	cases := []struct {
		name, method, path, body string
	}{
		{"create without a name", http.MethodPost, "/workspaces", `{"workspace":{}}`},
		{"get a bad id", http.MethodGet, "/workspaces/!", ""},
		{"delete a bad id", http.MethodDelete, "/workspaces/!", ""},
		{"update a bad id", http.MethodPatch, "/workspaces/!", `{"workspace":{"name":"n"}}`},
		{"update that is not JSON", http.MethodPatch, "/workspaces/" + errPathWorkspace, `{`},
		{"archive a bad id", http.MethodPost, "/workspaces/!/archive", ""},
		{"unarchive a bad id", http.MethodPost, "/workspaces/!/unarchive", ""},
		{"token for a bad id", http.MethodGet, "/workspaces/!/token", ""},
		{"stats for a bad id", http.MethodGet, "/workspaces/!/stats", ""},
		{"slack channel for a bad id", http.MethodPut, "/workspaces/!/slack", `{"channelId":"C1","channelName":"#ops"}`},
		{"slack removal for a bad id", http.MethodDelete, "/workspaces/!/slack", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, body := errPathDo(t, app, tc.method, tc.path, tc.body)
			if resp.StatusCode != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d, want 422 (body %s)", resp.StatusCode, body)
			}
			if body != string(_invalidPayload) {
				t.Errorf("body = %s, want the invalid-payload error", body)
			}
		})
	}
}

func TestWorkspaceRoutes_AnswerAControllerFailureWithItsStatus(t *testing.T) {
	ws := "/workspaces/" + errPathWorkspace
	cases := []struct {
		name, method, path, body string
	}{
		{"create", http.MethodPost, "/workspaces", `{"workspace":{"name":"n"}}`},
		{"get", http.MethodGet, ws, ""},
		{"list", http.MethodGet, "/workspaces", ""},
		{"delete", http.MethodDelete, ws, ""},
		{"update", http.MethodPatch, ws, `{"workspace":{"name":"n"}}`},
		{"unarchive", http.MethodPost, ws + "/unarchive", ""},
		{"stats", http.MethodGet, ws + "/stats", ""},
		{"account stats", http.MethodGet, "/stats", ""},
	}
	errs := []struct {
		err  error
		want int
	}{
		{errPathDBDown, http.StatusInternalServerError},
		{base.ErrNotFound, http.StatusNotFound},
	}
	for _, tc := range cases {
		for _, e := range errs {
			t.Run(tc.name+"/"+e.err.Error(), func(t *testing.T) {
				mgr := &fakeMCPManager{server: &fakeWorkspaceServer{}}
				app := wsPathApp(&handler{crud: &wsPathCrud{err: e.err}, mcpManager: mgr})

				resp, body := errPathDo(t, app, tc.method, tc.path, tc.body)
				if resp.StatusCode != e.want {
					t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, e.want, body)
				}
				if ct := resp.Header.Get("Content-Type"); ct != _mimeJSON {
					t.Errorf("Content-Type = %q, want %q", ct, _mimeJSON)
				}
				if len(mgr.removed) != 0 {
					t.Errorf("servers %v were dropped for a change that failed", mgr.removed)
				}
			})
		}
	}
}

func TestDeleteWorkspace_DropsItsServer(t *testing.T) {
	mgr := &fakeMCPManager{}
	app := wsPathApp(&handler{crud: &wsPathCrud{}, mcpManager: mgr})

	resp, body := errPathDo(t, app, http.MethodDelete, "/workspaces/"+errPathWorkspace, "")
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204 (body %s)", resp.StatusCode, body)
	}
	if len(mgr.removed) != 1 || mgr.removed[0] != 1 {
		t.Errorf("removed %v, want the deleted workspace's server", mgr.removed)
	}
}

func TestGetWorkspaceToken_AnswersAFailedMintWith500(t *testing.T) {
	app := wsPathApp(&handler{
		crud:     &wsPathCrud{ws: entity.Workspace{ID: 1}},
		tokenSvc: &authPathTokens{mcpErr: errors.New("no key")},
	})
	resp, body := errPathDo(t, app, http.MethodGet, "/workspaces/"+errPathWorkspace+"/token", "")
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (body %s)", resp.StatusCode, body)
	}
}

func TestStats_PassTheRangeThrough(t *testing.T) {
	t.Run("a workspace", func(t *testing.T) {
		ctrl := &wsPathCrud{}
		app := wsPathApp(&handler{crud: ctrl})
		resp, body := errPathDo(t, app, http.MethodGet, "/workspaces/"+errPathWorkspace+"/stats?range=custom&from=100&to=200", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
		}
		want := entity.GetWorkspaceStatsRequest{ID: 1, UserID: errPathUser, Range: "custom", From: 100, To: 200}
		if ctrl.statsRq != want {
			t.Errorf("asked for %+v, want %+v", ctrl.statsRq, want)
		}
	})

	t.Run("the account, defaulting to a week", func(t *testing.T) {
		ctrl := &wsPathCrud{}
		app := wsPathApp(&handler{crud: ctrl})
		resp, body := errPathDo(t, app, http.MethodGet, "/stats", "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
		}
		want := entity.GetUserStatsRequest{UserID: errPathUser, Range: "7d"}
		if ctrl.userStatsRq != want {
			t.Errorf("asked for %+v, want %+v", ctrl.userStatsRq, want)
		}
	})

	t.Run("the account, without a caller", func(t *testing.T) {
		h := &handler{crud: &wsPathCrud{}}
		app := fiber.New()
		app.Get("/stats", h.getUserStats())
		if resp, body := errPathDo(t, app, http.MethodGet, "/stats", ""); resp.StatusCode != http.StatusUnprocessableEntity {
			t.Fatalf("status = %d, want 422 (body %s)", resp.StatusCode, body)
		}
	})
}

func TestWorkspaceSlackChannel(t *testing.T) {
	path := "/workspaces/" + errPathWorkspace + "/slack"
	body := `{"channelId":"C1","channelName":"#ops"}`

	t.Run("set", func(t *testing.T) {
		cases := []struct {
			name  string
			body  string
			slack *wsPathSlack
			want  int
		}{
			{"a body that is not JSON", `{`, &wsPathSlack{}, http.StatusBadRequest},
			{"no channel name", `{"channelId":"C1"}`, &wsPathSlack{}, http.StatusBadRequest},
			{"slack not configured", body, nil, http.StatusBadRequest},
			{"the controller fails", body, &wsPathSlack{err: errPathDBDown}, http.StatusInternalServerError},
			{"the channel is set", body, &wsPathSlack{}, http.StatusOK},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				h := &handler{}
				if tc.slack != nil {
					h.slackCtrl = tc.slack
				}
				resp, b := errPathDo(t, wsPathApp(h), http.MethodPut, path, tc.body)
				if resp.StatusCode != tc.want {
					t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, b)
				}
			})
		}

		slack := &wsPathSlack{}
		errPathDo(t, wsPathApp(&handler{slackCtrl: slack}), http.MethodPut, path, body)
		want := entity.SetWorkspaceSlackChannelRequest{WorkspaceID: 1, UserID: errPathUser, ChannelID: "C1", ChannelName: "#ops"}
		if slack.set == nil || *slack.set != want {
			t.Errorf("asked for %+v, want %+v", slack.set, want)
		}
	})

	t.Run("remove", func(t *testing.T) {
		cases := []struct {
			name  string
			slack *wsPathSlack
			want  int
		}{
			{"slack not configured", nil, http.StatusBadRequest},
			{"the controller fails", &wsPathSlack{err: errPathDBDown}, http.StatusInternalServerError},
			{"the channel is removed", &wsPathSlack{}, http.StatusNoContent},
		}
		for _, tc := range cases {
			t.Run(tc.name, func(t *testing.T) {
				h := &handler{}
				if tc.slack != nil {
					h.slackCtrl = tc.slack
				}
				resp, b := errPathDo(t, wsPathApp(h), http.MethodDelete, path, "")
				if resp.StatusCode != tc.want {
					t.Fatalf("status = %d, want %d (body %s)", resp.StatusCode, tc.want, b)
				}
				if tc.slack != nil && (tc.slack.removed == nil || tc.slack.removed.WorkspaceID != 1) {
					t.Errorf("removed %+v, want workspace 1", tc.slack.removed)
				}
			})
		}
	})
}

// A workspace carries its Slack channel when Slack has one for it, and is
// served without one when the lookup fails.
func TestGetWorkspace_CarriesItsSlackChannel(t *testing.T) {
	get := func(t *testing.T, slack *wsPathSlack) string {
		t.Helper()
		app := wsPathApp(&handler{
			crud:       &wsPathCrud{ws: entity.Workspace{ID: 1, Name: "n"}},
			mcpManager: &fakeMCPManager{},
			slackCtrl:  slack,
		})
		resp, body := errPathDo(t, app, http.MethodGet, "/workspaces/"+errPathWorkspace, "")
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
		}
		return body
	}

	if body := get(t, &wsPathSlack{cfg: &entity.SlackConfig{Enabled: true, ChannelID: "C1", ChannelName: "#ops"}}); !strings.Contains(body, `"channelName":"#ops"`) {
		t.Errorf("body = %s, want the channel", body)
	}
	if body := get(t, &wsPathSlack{cfgErr: errPathDBDown}); strings.Contains(body, `"slack"`) {
		t.Errorf("body = %s, want no Slack section", body)
	}
}

func TestAgentModelsEntity(t *testing.T) {
	if agentModelsEntity(nil) != nil {
		t.Error("nil snapshot became a list of models")
	}

	got := agentModelsEntity(&agentlink.ModelsSnapshot{
		ConfigID:     "model",
		CurrentModel: "m2",
		CanSet:       true,
		Models: []agentlink.Model{
			{ID: "m1", Name: "One", Description: "first", Group: "a"},
			{ID: "m2", Name: "Two", Current: true},
		},
	})
	if got == nil || got.ConfigID != "model" || got.CurrentModel != "m2" || !got.CanSet {
		t.Fatalf("got %+v", got)
	}
	want := []entity.AgentModel{
		{ID: "m1", Name: "One", Description: "first", Group: "a"},
		{ID: "m2", Name: "Two", Current: true},
	}
	if len(got.Models) != len(want) || got.Models[0] != want[0] || got.Models[1] != want[1] {
		t.Errorf("models = %+v, want %+v", got.Models, want)
	}

	// Willing to switch but naming no option to switch through: no picker.
	if agentModelsEntity(&agentlink.ModelsSnapshot{CanSet: true}).CanSet {
		t.Error("a snapshot with no config option was offered as selectable")
	}
}
