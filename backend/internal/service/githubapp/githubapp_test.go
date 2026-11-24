// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package githubapp

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

var testKey = func() *rsa.PrivateKey {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return k
}()

func testPEM() string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(testKey)}))
}

func testConfig() Config {
	return Config{Enabled: true, AppID: " 42 ", Slug: "agentrq-skills", ClientID: "Iv1.abc", ClientSecret: "shh", PrivateKey: testPEM()}
}

func newTest(t *testing.T, h http.Handler) *service {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	s := New(testConfig()).(*service)
	s.apiBase, s.webBase = srv.URL, srv.URL
	return s
}

func TestCheck(t *testing.T) {
	ok := testConfig()
	oneLine := ok
	oneLine.PrivateKey = strings.ReplaceAll(ok.PrivateKey, "\n", `\n`)
	pasted := ok
	pasted.PrivateKey = strings.ReplaceAll(ok.PrivateKey, "\n", "")
	spaced := ok
	spaced.PrivateKey = strings.ReplaceAll(ok.PrivateKey, "\n", " ")
	for _, tc := range []struct {
		name   string
		mutate func(*Config)
		want   string
	}{
		{"disabled with nothing set", func(c *Config) { *c = Config{} }, ""},
		{"complete", func(*Config) {}, ""},
		{"a one-line key", func(c *Config) { *c = oneLine }, ""},
		{"a key pasted without its line breaks", func(c *Config) { *c = pasted }, ""},
		{"a key pasted with spaces for line breaks", func(c *Config) { *c = spaced }, ""},
		{"an app id that is not a number", func(c *Config) { c.AppID = "abc" }, "appId"},
		{"a slug with a slash", func(c *Config) { c.Slug = "a/b" }, "slug"},
		{"no client secret", func(c *Config) { c.ClientSecret = "" }, "clientSecret"},
		{"a key that is not PEM", func(c *Config) { c.PrivateKey = "nope" }, "privateKey"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := testConfig()
			tc.mutate(&c)
			err := Check(c)
			if tc.want == "" && err != nil || tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)) {
				t.Fatalf("Check = %v, want %q", err, tc.want)
			}
		})
	}
}

func TestDisabled(t *testing.T) {
	ctx := context.Background()
	for _, cfg := range []Config{{}, func() Config { c := testConfig(); c.Enabled = false; return c }(), func() Config { c := testConfig(); c.PrivateKey = "x"; return c }()} {
		s := New(cfg)
		if s.Enabled() {
			t.Fatalf("%+v is enabled", cfg)
		}
		if _, err := s.InstallURL(ctx, "x"); !errors.Is(err, ErrDisabled) {
			t.Fatal(err)
		}
		if _, err := s.NewState(ctx, 1); !errors.Is(err, ErrDisabled) {
			t.Fatal(err)
		}
		if err := s.CheckState(ctx, "x", 1); !errors.Is(err, ErrDisabled) {
			t.Fatal(err)
		}
		if _, err := s.UserInstallations(ctx, "c"); !errors.Is(err, ErrDisabled) {
			t.Fatal(err)
		}
		if _, err := s.RepoToken(ctx, 1, "r"); !errors.Is(err, ErrDisabled) {
			t.Fatal(err)
		}
	}
}

func TestInstallURL(t *testing.T) {
	s := New(testConfig())
	u, err := s.InstallURL(context.Background(), "1.2.a+b")
	if err != nil || u != "https://github.com/apps/agentrq-skills/installations/new?state=1.2.a%2Bb" {
		t.Fatalf("InstallURL = %q, %v", u, err)
	}
}

func TestState(t *testing.T) {
	ctx := context.Background()
	s := New(testConfig()).(*service)
	now := time.Unix(1_800_000_000, 0)
	s.now = func() time.Time { return now }
	state, err := s.NewState(ctx, 77)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.CheckState(ctx, state, 77); err != nil {
		t.Fatalf("own state refused: %v", err)
	}

	other := New(func() Config { c := testConfig(); c.ClientSecret = "other"; return c }())
	forged, _ := other.NewState(ctx, 77)
	for name, tc := range map[string]struct {
		state string
		user  int64
	}{
		"another account":     {state, 78},
		"another server":      {forged, 77},
		"no mac":              {"77", 77},
		"a changed user":      {strings.Replace(state, "77.", "78.", 1), 78},
		"an expiry not a num": {"77.x." + s.mac("77.x"), 77},
	} {
		if err := s.CheckState(ctx, tc.state, tc.user); !errors.Is(err, ErrInvalidState) {
			t.Errorf("%s: CheckState = %v", name, err)
		}
	}
	now = now.Add(stateTTL + time.Second)
	if err := s.CheckState(ctx, state, 77); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expired state: %v", err)
	}
}

func TestUserInstallations(t *testing.T) {
	var pages atomic.Int32
	s := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/login/oauth/access_token":
			_ = r.ParseForm()
			if r.Form.Get("client_id") != "Iv1.abc" || r.Form.Get("client_secret") != "shh" || r.Form.Get("code") != "good" {
				fmt.Fprint(w, `{"error":"bad_verification_code"}`)
				return
			}
			fmt.Fprint(w, `{"access_token":"ghu_user"}`)
		case "/user/installations":
			if r.Header.Get("Authorization") != "Bearer ghu_user" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			pages.Add(1)
			var list []map[string]any
			if r.URL.Query().Get("page") == "1" {
				for i := range 100 {
					list = append(list, map[string]any{"id": 1000 + i, "app_id": 7, "account": map[string]string{"login": "x"}})
				}
				list[0] = map[string]any{"id": 9, "app_id": 42, "account": map[string]string{"login": "acme", "type": "Organization"}}
			} else {
				list = []map[string]any{{"id": 10, "app_id": 42, "account": map[string]string{"login": "me", "type": "User"}}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"installations": list})
		}
	}))
	got, err := s.UserInstallations(context.Background(), "good")
	if err != nil {
		t.Fatal(err)
	}
	want := []Installation{{9, "acme", "Organization"}, {10, "me", "User"}}
	if fmt.Sprint(got) != fmt.Sprint(want) || pages.Load() != 2 {
		t.Fatalf("got %v in %d pages", got, pages.Load())
	}
	if _, err := s.UserInstallations(context.Background(), "bad"); err == nil || !strings.Contains(err.Error(), "bad_verification_code") {
		t.Fatalf("bad code: %v", err)
	}
	if _, err := s.UserInstallations(context.Background(), ""); err == nil || !strings.Contains(err.Error(), "Request user authorization") {
		t.Fatalf("no code: %v", err)
	}
}

func TestUserInstallationsFailures(t *testing.T) {
	for name, h := range map[string]http.HandlerFunc{
		"exchange not 200": func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusInternalServerError) },
		"listing refused": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/user/installations" {
				w.WriteHeader(http.StatusForbidden)
				return
			}
			fmt.Fprint(w, `{"access_token":"t"}`)
		},
		"listing garbled": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/user/installations" {
				fmt.Fprint(w, `{`)
				return
			}
			fmt.Fprint(w, `{"access_token":"t"}`)
		},
	} {
		s := newTest(t, h)
		if _, err := s.UserInstallations(context.Background(), "c"); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
	s := New(testConfig()).(*service)
	s.webBase, s.apiBase = "http://127.0.0.1:1", "http://127.0.0.1:1"
	if _, err := s.UserInstallations(context.Background(), "c"); err == nil || !strings.Contains(err.Error(), "reach GitHub") {
		t.Fatalf("unreachable: %v", err)
	}
	if _, err := s.RepoToken(context.Background(), 1, "r"); err == nil || !strings.Contains(err.Error(), "reach GitHub") {
		t.Fatalf("unreachable: %v", err)
	}
	s.webBase = "://bad"
	if _, err := s.UserInstallations(context.Background(), "c"); err == nil {
		t.Fatal("bad base")
	}
	s.apiBase = "://bad"
	if _, err := s.RepoToken(context.Background(), 1, "r"); err == nil {
		t.Fatal("bad base")
	}
}

func TestRepoToken(t *testing.T) {
	var seen map[string]any
	s := newTest(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok, err := jwt.ParseWithClaims(strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "), &jwt.RegisteredClaims{}, func(*jwt.Token) (any, error) {
			return &testKey.PublicKey, nil
		}, jwt.WithValidMethods([]string{"RS256"}), jwt.WithIssuer("42"))
		if err != nil || !tok.Valid {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &seen)
		switch r.URL.Path {
		case "/app/installations/9/access_tokens":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `{"token":"ghs_repo"}`)
		case "/app/installations/404/access_tokens":
			w.WriteHeader(http.StatusNotFound)
		case "/app/installations/422/access_tokens":
			w.WriteHeader(http.StatusUnprocessableEntity)
		case "/app/installations/500/access_tokens":
			w.WriteHeader(http.StatusInternalServerError)
		case "/app/installations/1/access_tokens":
			w.WriteHeader(http.StatusCreated)
			fmt.Fprint(w, `nope`)
		}
	}))
	ctx := context.Background()
	tok, err := s.RepoToken(ctx, 9, "skills")
	if err != nil || tok != "ghs_repo" {
		t.Fatalf("RepoToken = %q, %v", tok, err)
	}
	if fmt.Sprint(seen) != "map[permissions:map[contents:read metadata:read] repositories:[skills]]" {
		t.Fatalf("asked for %v", seen)
	}
	for id, want := range map[int64]error{404: ErrInstallationGone, 422: ErrRepoNotCovered, 0: ErrRepoNotCovered} {
		if _, err := s.RepoToken(ctx, id, "skills"); !errors.Is(err, want) {
			t.Errorf("%d: %v", id, err)
		}
	}
	if _, err := s.RepoToken(ctx, 9, "../x"); !errors.Is(err, ErrRepoNotCovered) {
		t.Errorf("bad repo: %v", err)
	}
	if _, err := s.RepoToken(ctx, 500, "skills"); err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("500: %v", err)
	}
	if _, err := s.RepoToken(ctx, 1, "skills"); err == nil || !strings.Contains(err.Error(), "could not be read") {
		t.Errorf("garbled: %v", err)
	}
	s.key = &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: big.NewInt(15), E: 3}, D: big.NewInt(3)}
	if _, err := s.RepoToken(ctx, 9, "skills"); err == nil {
		t.Error("a key too small to sign with signed")
	}
}
