// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package gitlabapp

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testConfig() Config {
	return Config{Enabled: true, Host: " GitLab.Example ", ClientID: " cid ", ClientSecret: "shh"}
}

var testNow = time.Date(2026, 11, 27, 6, 0, 0, 0, time.UTC)

func newTest(t *testing.T, h http.HandlerFunc) *service {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s := New(testConfig(), "https://agentrq.example/").(*service)
	s.base, s.client, s.now = srv.URL, srv.Client(), func() time.Time { return testNow }
	return s
}

func TestCheck(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  Config
		want string
	}{
		{"disabled with nothing set", Config{}, ""},
		{"complete", testConfig(), ""},
		{"disabled with a bad host", Config{Host: "https://gitlab.com"}, "gitlabApp.host"},
		{"no client id", Config{Enabled: true, ClientSecret: "s"}, "clientId and clientSecret"},
		{"no client secret", Config{Enabled: true, ClientID: "c", ClientSecret: " "}, "clientId and clientSecret"},
	} {
		err := Check(tc.cfg)
		if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
			t.Errorf("%s: %v, want %q", tc.name, err, tc.want)
		}
	}
}

func TestDisabled(t *testing.T) {
	ctx := context.Background()
	for _, cfg := range []Config{{}, {Enabled: true, Host: "gitlab.example"}} {
		s := New(cfg, "https://a.example")
		if s.Enabled() {
			t.Fatalf("%+v must be off", cfg)
		}
		if _, err := s.AuthorizeURL(ctx, "x"); !errors.Is(err, ErrDisabled) {
			t.Error("AuthorizeURL")
		}
		if _, err := s.NewState(ctx, 1); !errors.Is(err, ErrDisabled) {
			t.Error("NewState")
		}
		if err := s.CheckState(ctx, "x", 1); !errors.Is(err, ErrDisabled) {
			t.Error("CheckState")
		}
		if _, err := s.Exchange(ctx, "c"); !errors.Is(err, ErrDisabled) {
			t.Error("Exchange")
		}
		if _, err := s.Refresh(ctx, "r"); !errors.Is(err, ErrDisabled) {
			t.Error("Refresh")
		}
		if _, err := s.Username(ctx, "a"); !errors.Is(err, ErrDisabled) {
			t.Error("Username")
		}
		if err := s.Revoke(ctx, "a"); !errors.Is(err, ErrDisabled) {
			t.Error("Revoke")
		}
	}
	// The host is there whether or not connecting is on: public imports read it.
	if h := New(Config{}, "").Host(); h != "gitlab.com" {
		t.Errorf("default host %q", h)
	}
}

func TestAuthorizeURL(t *testing.T) {
	s := New(testConfig(), "https://agentrq.example/")
	if !s.Enabled() || s.Host() != "gitlab.example" {
		t.Fatalf("enabled %v host %q", s.Enabled(), s.Host())
	}
	got, err := s.AuthorizeURL(context.Background(), "st&ate")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(got)
	q := u.Query()
	if u.Scheme+"://"+u.Host+u.Path != "https://gitlab.example/oauth/authorize" || q.Get("client_id") != "cid" || q.Get("redirect_uri") != "https://agentrq.example/gitlab/callback" ||
		q.Get("response_type") != "code" || q.Get("scope") != "read_api" || q.Get("state") != "st&ate" {
		t.Errorf("authorize URL %s", got)
	}
}

func TestState(t *testing.T) {
	s := newTest(t, nil)
	ctx := context.Background()
	state, err := s.NewState(ctx, 42)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckState(ctx, state, 42); err != nil {
		t.Fatalf("own state: %v", err)
	}
	for name, bad := range map[string]string{
		"another account's": state, "no dots": "abc", "a forged mac": state[:strings.LastIndexByte(state, '.')] + ".forged",
	} {
		uid := int64(42)
		if name == "another account's" {
			uid = 7
		}
		if err := s.CheckState(ctx, bad, uid); !errors.Is(err, ErrInvalidState) {
			t.Errorf("%s: %v", name, err)
		}
	}
	// One signed for an expiry that is not a number, or long past.
	odd := "42.soon"
	if err := s.CheckState(ctx, odd+"."+s.mac(odd), 42); !errors.Is(err, ErrInvalidState) {
		t.Errorf("odd expiry: %v", err)
	}
	s.now = func() time.Time { return testNow.Add(stateTTL + time.Second) }
	if err := s.CheckState(ctx, state, 42); !errors.Is(err, ErrInvalidState) {
		t.Errorf("expired: %v", err)
	}
}

func tokenServer(t *testing.T, status int, body string, saw *url.Values) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		r.ParseForm()
		*saw = r.PostForm
		w.WriteHeader(status)
		w.Write([]byte(body))
	}
}

func TestExchange(t *testing.T) {
	var saw url.Values
	s := newTest(t, tokenServer(t, 200, `{"access_token":"a","refresh_token":"r","expires_in":7200}`, &saw))
	tok, err := s.Exchange(context.Background(), "code1")
	if err != nil || tok.Access != "a" || tok.Refresh != "r" || !tok.Expiry.Equal(testNow.Add(2*time.Hour)) {
		t.Fatalf("exchange: %+v, %v", tok, err)
	}
	if saw.Get("grant_type") != "authorization_code" || saw.Get("code") != "code1" || saw.Get("client_id") != "cid" || saw.Get("client_secret") != "shh" ||
		saw.Get("redirect_uri") != "https://agentrq.example/gitlab/callback" {
		t.Errorf("form %v", saw)
	}
	if _, err := s.Exchange(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "did not send an authorization code") {
		t.Errorf("no code: %v", err)
	}

	for _, tc := range []struct {
		status int
		body   string
		want   string
	}{
		{400, `{"error":"invalid_grant"}`, "refused the authorization code (400)"},
		{200, `{`, "could not be read"},
		{200, `{"access_token":"a"}`, "could not be read"},
	} {
		s := newTest(t, tokenServer(t, tc.status, tc.body, &saw))
		if _, err := s.Exchange(context.Background(), "c"); err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d %s: %v", tc.status, tc.body, err)
		}
	}
}

func TestRefresh(t *testing.T) {
	var saw url.Values
	s := newTest(t, tokenServer(t, 200, `{"access_token":"a2","refresh_token":"r2"}`, &saw))
	tok, err := s.Refresh(context.Background(), "r1")
	// With no lifetime stated it is renewed again soon.
	if err != nil || tok.Access != "a2" || tok.Refresh != "r2" || !tok.Expiry.Equal(testNow.Add(time.Minute)) {
		t.Fatalf("refresh: %+v, %v", tok, err)
	}
	if saw.Get("grant_type") != "refresh_token" || saw.Get("refresh_token") != "r1" {
		t.Errorf("form %v", saw)
	}
	for status, want := range map[int]error{400: ErrRevoked, 401: ErrRevoked} {
		s := newTest(t, tokenServer(t, status, `{}`, &saw))
		if _, err := s.Refresh(context.Background(), "r"); !errors.Is(err, want) {
			t.Errorf("%d: %v", status, err)
		}
	}
	s = newTest(t, tokenServer(t, 503, ``, &saw))
	if _, err := s.Refresh(context.Background(), "r"); err == nil || !strings.Contains(err.Error(), "answered 503 renewing") {
		t.Errorf("503: %v", err)
	}
	s = newTest(t, tokenServer(t, 200, `{`, &saw))
	if _, err := s.Refresh(context.Background(), "r"); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("garbage: %v", err)
	}
}

func TestUsername(t *testing.T) {
	s := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v4/user" || r.Header.Get("Authorization") != "Bearer a" {
			w.WriteHeader(401)
			return
		}
		w.Write([]byte(`{"username":"ann"}`))
	})
	if u, err := s.Username(context.Background(), "a"); err != nil || u != "ann" {
		t.Fatalf("username: %q, %v", u, err)
	}
	if _, err := s.Username(context.Background(), "b"); err == nil || !strings.Contains(err.Error(), "answered 401") {
		t.Errorf("refused: %v", err)
	}
	s = newTest(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`{}`)) })
	if _, err := s.Username(context.Background(), "a"); err == nil {
		t.Error("an answer with no username must fail")
	}
}

func TestRevoke(t *testing.T) {
	var saw url.Values
	status := 200
	s := newTest(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/revoke" {
			t.Errorf("unexpected %s", r.URL.Path)
		}
		r.ParseForm()
		saw = r.PostForm
		w.WriteHeader(status)
	})
	if err := s.Revoke(context.Background(), "r1"); err != nil || saw.Get("token") != "r1" || saw.Get("client_id") != "cid" || saw.Get("client_secret") != "shh" {
		t.Fatalf("revoke: %v, %v", err, saw)
	}
	status = 500
	if err := s.Revoke(context.Background(), "r1"); err == nil || !strings.Contains(err.Error(), "answered 500 revoking") {
		t.Errorf("refused: %v", err)
	}
}

func TestUnreachable(t *testing.T) {
	s := newTest(t, nil)
	s.base = "http://127.0.0.1:1"
	ctx := context.Background()
	if _, err := s.Exchange(ctx, "c"); err == nil || !strings.Contains(err.Error(), "reach GitLab") {
		t.Errorf("exchange: %v", err)
	}
	if _, err := s.Username(ctx, "a"); err == nil || !strings.Contains(err.Error(), "reach GitLab") {
		t.Errorf("username: %v", err)
	}
	if err := s.Revoke(ctx, "a"); err == nil || !strings.Contains(err.Error(), "reach GitLab") {
		t.Errorf("revoke: %v", err)
	}
	if _, err := s.Refresh(ctx, "r"); err == nil || !strings.Contains(err.Error(), "reach GitLab") {
		t.Errorf("refresh: %v", err)
	}
	s.base = "http://bad host"
	if _, err := s.Username(ctx, "a"); err == nil {
		t.Error("username with a bad base")
	}
	if err := s.Revoke(ctx, "a"); err == nil {
		t.Error("revoke with a bad base")
	}
}
