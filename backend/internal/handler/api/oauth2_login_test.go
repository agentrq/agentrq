// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/gofiber/fiber/v2"
)

// oauth2App is the sign-in routes with one provider the operator added,
// gitlab, signing in through exchange.
func oauth2App(basePath string, exchange func(context.Context, string) (*auth.User, error), crudErr error, tokens *authPathTokens) (*fiber.App, *authPathCrud) {
	ctrl := &authPathCrud{err: crudErr}
	h := &handler{tokenSvc: tokens, crud: ctrl, basePath: basePath, oauth2Logins: []OAuth2Login{
		{ID: "gitlab", Name: "GitLab", IconURL: "https://cdn.example/login-icons/abc", Service: &mockAuthService{exchangeFunc: exchange}},
	}}
	return authPathApp(h), ctrl
}

func TestOAuth2Login(t *testing.T) {
	t.Run("redirects to the provider with a state for it", func(t *testing.T) {
		app, _ := oauth2App("/agentrq", nil, nil, &authPathTokens{})
		resp, _ := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/login?redirect_url=/workspaces", "")
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want 302", resp.StatusCode)
		}
		if loc := resp.Header.Get("Location"); loc != "https://google.com/auth?state=oauth2:gitlab:/workspaces" {
			t.Errorf("Location = %q", loc)
		}
	})

	t.Run("defaults to the root under the base path", func(t *testing.T) {
		app, _ := oauth2App("/agentrq", nil, nil, &authPathTokens{})
		resp, _ := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/login", "")
		if loc := resp.Header.Get("Location"); loc != "https://google.com/auth?state=oauth2:gitlab:/agentrq/" {
			t.Errorf("Location = %q", loc)
		}
	})

	t.Run("answers 404 for a provider nobody added", func(t *testing.T) {
		app, _ := oauth2App("", nil, nil, &authPathTokens{})
		for _, path := range []string{"/auth/oauth2/okta/login", "/auth/oauth2/okta/callback"} {
			if resp, body := errPathDo(t, app, http.MethodGet, path, ""); resp.StatusCode != http.StatusNotFound {
				t.Errorf("%s: status = %d, want 404 (body %s)", path, resp.StatusCode, body)
			}
		}
	})

	t.Run("answers 500 when no state can be made", func(t *testing.T) {
		app, _ := oauth2App("", nil, nil, &authPathTokens{stateErr: errors.New("no key")})
		if resp, body := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/login", ""); resp.StatusCode != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500 (body %s)", resp.StatusCode, body)
		}
	})
}

func TestOAuth2Callback(t *testing.T) {
	exchanged := func(context.Context, string) (*auth.User, error) {
		return &auth.User{Sub: "s1", Email: "a@b.com", Name: "Ada"}, nil
	}
	state := "&state=" + url.QueryEscape("oauth2:gitlab:/workspaces")

	t.Run("signs in and returns where the state says", func(t *testing.T) {
		app, ctrl := oauth2App("", exchanged, nil, &authPathTokens{})
		resp, body := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/callback?code=c1"+state, "")
		if resp.StatusCode != http.StatusFound {
			t.Fatalf("status = %d, want 302 (body %s)", resp.StatusCode, body)
		}
		if loc := resp.Header.Get("Location"); loc != "/workspaces" {
			t.Errorf("Location = %q, want /workspaces", loc)
		}
		if ctrl.sawEmail != "a@b.com" {
			t.Errorf("synced %q, want the provider's email", ctrl.sawEmail)
		}
		if cookies := setCookies(resp); !strings.Contains(cookies, "at=access-token") || !strings.Contains(cookies, "rt=refresh-token") {
			t.Errorf("Set-Cookie = %s, want both halves of a session", cookies)
		}
	})

	t.Run("refuses a missing state, or one made for another provider", func(t *testing.T) {
		for _, q := range []string{"", "&state=" + url.QueryEscape("github:/"), "&state=" + url.QueryEscape("oauth2:okta:/")} {
			app, ctrl := oauth2App("", exchanged, nil, &authPathTokens{})
			resp, body := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/callback?code=c1"+q, "")
			if resp.StatusCode != http.StatusBadRequest {
				t.Errorf("state %q: status = %d, want 400 (body %s)", q, resp.StatusCode, body)
			}
			if ctrl.sawEmail != "" || strings.Contains(setCookies(resp), "at=") {
				t.Errorf("state %q: signed in anyway", q)
			}
		}
	})

	t.Run("sends an unverified email back to the login page", func(t *testing.T) {
		refused := func(context.Context, string) (*auth.User, error) {
			return nil, fmt.Errorf("wrapped: %w", auth.ErrEmailNotVerified)
		}
		app, ctrl := oauth2App("/agentrq", refused, nil, &authPathTokens{})
		resp, _ := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/callback?code=c1"+state, "")
		if loc, want := resp.Header.Get("Location"), "/agentrq/login?error=oauth2EmailUnverified"; resp.StatusCode != http.StatusFound || loc != want {
			t.Errorf("status %d, Location = %q, want 302 to %q", resp.StatusCode, loc, want)
		}
		if ctrl.sawEmail != "" {
			t.Errorf("synced %q, want no account looked up", ctrl.sawEmail)
		}
	})

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
			app, _ := oauth2App("", f.exchange, f.crudErr, f.tokens)
			resp, body := errPathDo(t, app, http.MethodGet, "/auth/oauth2/gitlab/callback?code=c1"+state, "")
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
}

// The login page reads the added providers from the auth config: never null,
// and never anything but what a button needs.
func TestAuthConfigListsOAuth2Providers(t *testing.T) {
	get := func(t *testing.T, h *handler) map[string]any {
		t.Helper()
		app := fiber.New()
		app.Get("/auth/config", h.authConfig())
		resp, err := app.Test(httptest.NewRequest(http.MethodGet, "/auth/config", nil))
		if err != nil {
			t.Fatal(err)
		}
		var body map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		return body
	}
	if got := get(t, &handler{})["oauth2Providers"]; fmt.Sprint(got) != "[]" {
		t.Errorf("oauth2Providers = %v, want an empty list", got)
	}
	h := &handler{oauth2Logins: []OAuth2Login{{ID: "gitlab", Name: "GitLab", IconURL: "https://x/i", Service: &mockAuthService{}}}}
	got, _ := json.Marshal(get(t, h)["oauth2Providers"])
	if want := `[{"iconUrl":"https://x/i","id":"gitlab","name":"GitLab"}]`; string(got) != want {
		t.Errorf("oauth2Providers = %s, want %s", got, want)
	}
}
