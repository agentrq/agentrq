// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func validProvider() OAuth2Provider {
	return OAuth2Provider{
		ID: "gitlab", Name: "GitLab", ClientID: "cid", ClientSecret: "sec",
		AuthURL: "https://gitlab.example/oauth/authorize", TokenURL: "https://gitlab.example/oauth/token",
		UserInfoURL: "https://gitlab.example/oauth/userinfo", Scopes: []string{"openid", "email"},
		IconURL: "https://cdn.example/login-icons/0123456789abcdef",
	}
}

func TestCheckOAuth2Providers(t *testing.T) {
	if err := CheckOAuth2Providers(nil); err != nil {
		t.Fatalf("no providers: %v", err)
	}
	ok := validProvider()
	local := validProvider()
	local.ID, local.AuthURL, local.TokenURL, local.UserInfoURL, local.IconURL = "local-1", "http://localhost:9000/a", "http://127.0.0.1:9000/t", "http://[::1]:9000/u", ""
	if err := CheckOAuth2Providers([]OAuth2Provider{ok, local}); err != nil {
		t.Fatalf("valid providers: %v", err)
	}

	cases := []struct {
		name string
		edit func(p *OAuth2Provider)
		want string
	}{
		{"an empty id", func(p *OAuth2Provider) { p.ID = "" }, "auth.oauth2Providers[0].id"},
		{"an id with capitals", func(p *OAuth2Provider) { p.ID = "GitLab" }, "auth.oauth2Providers[0].id"},
		{"an id with a slash", func(p *OAuth2Provider) { p.ID = "a/b" }, "auth.oauth2Providers[0].id"},
		{"an id too long", func(p *OAuth2Provider) { p.ID = strings.Repeat("a", 33) }, "auth.oauth2Providers[0].id"},
		{"no name", func(p *OAuth2Provider) { p.Name = "" }, "auth.oauth2Providers[0].name: must be set"},
		{"no client id", func(p *OAuth2Provider) { p.ClientID = "" }, "auth.oauth2Providers[0].clientId: must be set"},
		{"no client secret", func(p *OAuth2Provider) { p.ClientSecret = "" }, "auth.oauth2Providers[0].clientSecret: must be set"},
		{"an http auth URL", func(p *OAuth2Provider) { p.AuthURL = "http://gitlab.example/a" }, "auth.oauth2Providers[0].authUrl"},
		{"a relative token URL", func(p *OAuth2Provider) { p.TokenURL = "/oauth/token" }, "auth.oauth2Providers[0].tokenUrl"},
		{"another scheme", func(p *OAuth2Provider) { p.UserInfoURL = "ftp://gitlab.example/u" }, "auth.oauth2Providers[0].userInfoUrl"},
		{"an unparsable URL", func(p *OAuth2Provider) { p.UserInfoURL = "https://%zz" }, "auth.oauth2Providers[0].userInfoUrl"},
		{"an icon that is not a link", func(p *OAuth2Provider) { p.IconURL = "javascript:alert(1)" }, "auth.oauth2Providers[0].iconUrl"},
		{"an icon with no host", func(p *OAuth2Provider) { p.IconURL = "/storage/x" }, "auth.oauth2Providers[0].iconUrl"},
	}
	for _, c := range cases {
		p := validProvider()
		c.edit(&p)
		if err := CheckOAuth2Providers([]OAuth2Provider{p}); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: err = %v, want one naming %s", c.name, err, c.want)
		}
	}

	if err := CheckOAuth2Providers([]OAuth2Provider{ok, ok}); err == nil || !strings.Contains(err.Error(), "auth.oauth2Providers[1].id") {
		t.Errorf("a repeated id: err = %v", err)
	}
}

// userInfoServer is a provider's token and userinfo endpoints on the
// loopback interface.
func userInfoServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			if err := r.ParseForm(); err != nil || r.Form.Get("code") != "c1" {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"tok","token_type":"Bearer"}`))
		case "/userinfo":
			if r.Header.Get("Authorization") != "Bearer tok" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func providerAt(srv *httptest.Server) Service {
	p := validProvider()
	p.AuthURL, p.TokenURL, p.UserInfoURL = srv.URL+"/authorize", srv.URL+"/token", srv.URL+"/userinfo"
	return NewOAuth2(p, "https://app.example/api/v1/auth/oauth2/gitlab/callback")
}

func TestOAuth2AuthURL(t *testing.T) {
	got, err := url.Parse(NewOAuth2(validProvider(), "https://app.example/cb").GetAuthURL("st"))
	if err != nil {
		t.Fatal(err)
	}
	q := got.Query()
	if got.Host != "gitlab.example" || got.Path != "/oauth/authorize" || q.Get("state") != "st" || q.Get("client_id") != "cid" ||
		q.Get("redirect_uri") != "https://app.example/cb" || q.Get("scope") != "openid email" || q.Get("response_type") != "code" {
		t.Errorf("auth URL = %s", got)
	}
}

func TestOAuth2Exchange(t *testing.T) {
	ctx := context.Background()

	t.Run("reads the verified user", func(t *testing.T) {
		srv := userInfoServer(t, http.StatusOK, `{"sub":"42","email":"a@b.com","email_verified":true,"name":"Ada","picture":"https://p/x.png"}`)
		u, err := providerAt(srv).Exchange(ctx, "c1")
		if err != nil {
			t.Fatal(err)
		}
		if *u != (User{ID: "gitlab:42", Sub: "42", Email: "a@b.com", Name: "Ada", Picture: "https://p/x.png"}) {
			t.Errorf("user = %+v", *u)
		}
	})

	t.Run("takes a verified claim sent as a string, and names the user by username or email", func(t *testing.T) {
		srv := userInfoServer(t, http.StatusOK, `{"sub":"1","email":"a@b.com","email_verified":"true","preferred_username":"ada"}`)
		if u, err := providerAt(srv).Exchange(ctx, "c1"); err != nil || u.Name != "ada" {
			t.Errorf("user = %+v, %v; want the username", u, err)
		}
		srv = userInfoServer(t, http.StatusOK, `{"sub":"1","email":"a@b.com","email_verified":true}`)
		if u, err := providerAt(srv).Exchange(ctx, "c1"); err != nil || u.Name != "a@b.com" {
			t.Errorf("user = %+v, %v; want the email", u, err)
		}
	})

	t.Run("refuses an email the provider has not verified", func(t *testing.T) {
		for _, body := range []string{
			`{"sub":"1","email":"a@b.com","email_verified":false}`,
			`{"sub":"1","email":"a@b.com","email_verified":"false"}`,
			`{"sub":"1","email":"a@b.com"}`,
			`{"sub":"1","email_verified":true}`,
		} {
			srv := userInfoServer(t, http.StatusOK, body)
			if _, err := providerAt(srv).Exchange(ctx, "c1"); !errors.Is(err, ErrEmailNotVerified) {
				t.Errorf("%s: err = %v, want ErrEmailNotVerified", body, err)
			}
		}
	})

	t.Run("fails on a refused code, a failing or garbled userinfo", func(t *testing.T) {
		srv := userInfoServer(t, http.StatusOK, `{}`)
		if _, err := providerAt(srv).Exchange(ctx, "bad"); err == nil || !strings.Contains(err.Error(), "gitlab oauth exchange") {
			t.Errorf("refused code: err = %v", err)
		}
		srv = userInfoServer(t, http.StatusInternalServerError, `{}`)
		if _, err := providerAt(srv).Exchange(ctx, "c1"); err == nil || !strings.Contains(err.Error(), "userinfo status: 500") {
			t.Errorf("failing userinfo: err = %v", err)
		}
		srv = userInfoServer(t, http.StatusOK, `not json`)
		if _, err := providerAt(srv).Exchange(ctx, "c1"); err == nil || !strings.Contains(err.Error(), "decode userinfo") {
			t.Errorf("garbled userinfo: err = %v", err)
		}
	})

	t.Run("fails when userinfo cannot be reached", func(t *testing.T) {
		srv := userInfoServer(t, http.StatusOK, `{}`)
		p := validProvider()
		p.TokenURL, p.UserInfoURL = srv.URL+"/token", "http://127.0.0.1:1/userinfo"
		if _, err := NewOAuth2(p, "https://app.example/cb").Exchange(ctx, "c1"); err == nil || !strings.Contains(err.Error(), "get userinfo") {
			t.Errorf("err = %v", err)
		}
	})
}
