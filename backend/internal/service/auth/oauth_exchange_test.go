// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"golang.org/x/oauth2"
)

// fakeProvider stands in for an OAuth provider's token and API hosts. The
// providers' URLs are fixed in the source, so the test hands the oauth2
// package an HTTP client (through the context, which is how oauth2 is told
// which client to use) whose transport sends every request to this server
// instead, keeping the host it was meant for in X-Original-Host.
type fakeProvider struct {
	t      *testing.T
	server *httptest.Server

	mu     sync.Mutex
	routes map[string]http.HandlerFunc // "host/path" -> handler
	hits   []string
	failOn string // "host/path" whose request fails before it is sent
}

func newFakeProvider(t *testing.T) *fakeProvider {
	t.Helper()
	p := &fakeProvider{t: t, routes: map[string]http.HandlerFunc{}}
	p.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("X-Original-Host") + r.URL.Path
		p.mu.Lock()
		p.hits = append(p.hits, key)
		h, ok := p.routes[key]
		p.mu.Unlock()
		if !ok {
			t.Errorf("unexpected request to %s", key)
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}))
	t.Cleanup(p.server.Close)
	return p
}

func (p *fakeProvider) handle(hostPath string, h http.HandlerFunc) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.routes[hostPath] = h
}

func (p *fakeProvider) json(hostPath string, status int, body string) {
	p.handle(hostPath, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
}

func (p *fakeProvider) RoundTrip(req *http.Request) (*http.Response, error) {
	key := req.URL.Host + req.URL.Path
	if p.failOn == key {
		return nil, errors.New("connection refused")
	}
	out := req.Clone(req.Context())
	out.Header.Set("X-Original-Host", req.URL.Host)
	out.URL.Scheme = "http"
	out.URL.Host = strings.TrimPrefix(p.server.URL, "http://")
	out.Host = ""
	return http.DefaultTransport.RoundTrip(out)
}

// ctx returns a context that makes the oauth2 package use this fake for the
// token exchange and for the client it builds from the token.
func (p *fakeProvider) ctx() context.Context {
	return context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Transport: p})
}

func (p *fakeProvider) hitList() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.hits...)
}

const (
	githubToken  = "github.com/login/oauth/access_token"
	githubUser   = "api.github.com/user"
	githubEmails = "api.github.com/user/emails"
	googleToken  = "oauth2.googleapis.com/token"
	googleUser   = "www.googleapis.com/oauth2/v2/userinfo"
	tokenJSON    = `{"access_token":"gho_test","token_type":"bearer"}`
)

func requireBearer(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Authorization"); got != "Bearer gho_test" {
		t.Errorf("Authorization = %q, want the exchanged token", got)
	}
}

func TestGitHubExchange_UserWithPublicEmail(t *testing.T) {
	p := newFakeProvider(t)
	p.handle(githubToken, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("parse token form: %v", err)
		}
		if got := r.Form.Get("code"); got != "the-code" {
			t.Errorf("code = %q, want the-code", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(tokenJSON))
	})
	p.handle(githubUser, func(w http.ResponseWriter, r *http.Request) {
		requireBearer(t, r)
		_, _ = w.Write([]byte(`{"id":42,"login":"octo","name":"Octo Cat","email":"octo@example.com","avatar_url":"https://avatars.example.com/42"}`))
	})

	user, err := NewGitHub("cid", "secret", "http://localhost/cb").Exchange(p.ctx(), "the-code")
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	want := User{ID: "github:42", Email: "octo@example.com", Name: "Octo Cat", Picture: "https://avatars.example.com/42"}
	if *user != want {
		t.Errorf("user = %+v, want %+v", *user, want)
	}
	// A public email is enough: the emails endpoint must not be asked.
	for _, hit := range p.hitList() {
		if hit == githubEmails {
			t.Errorf("fetched %s although the profile carried an email", githubEmails)
		}
	}
}

func TestGitHubExchange_FallsBackToTheEmailsList(t *testing.T) {
	cases := []struct {
		name   string
		emails string
		want   string
	}{
		{
			name:   "primary and verified wins over an earlier verified one",
			emails: `[{"email":"old@example.com","primary":false,"verified":true},{"email":"main@example.com","primary":true,"verified":true}]`,
			want:   "main@example.com",
		},
		{
			name:   "unverified primary when no primary is verified",
			emails: `[{"email":"other@example.com","primary":false,"verified":true},{"email":"main@example.com","primary":true,"verified":false}]`,
			want:   "main@example.com",
		},
		{
			name:   "first address when none is primary",
			emails: `[{"email":"first@example.com"},{"email":"second@example.com"}]`,
			want:   "first@example.com",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newFakeProvider(t)
			p.json(githubToken, http.StatusOK, tokenJSON)
			// No name either, so the login stands in for it.
			p.json(githubUser, http.StatusOK, `{"id":7,"login":"octo","email":""}`)
			p.handle(githubEmails, func(w http.ResponseWriter, r *http.Request) {
				requireBearer(t, r)
				_, _ = w.Write([]byte(tc.emails))
			})

			user, err := NewGitHub("cid", "secret", "").Exchange(p.ctx(), "code")
			if err != nil {
				t.Fatalf("Exchange: %v", err)
			}
			if user.Email != tc.want {
				t.Errorf("email = %q, want %q", user.Email, tc.want)
			}
			if user.Name != "octo" {
				t.Errorf("name = %q, want the login when the profile has no name", user.Name)
			}
			if user.ID != "github:7" {
				t.Errorf("id = %q, want github:7", user.ID)
			}
		})
	}
}

func TestGitHubExchange_Errors(t *testing.T) {
	cases := []struct {
		name    string
		setup   func(p *fakeProvider)
		wantErr string
	}{
		{
			name:    "token endpoint refuses the code",
			setup:   func(p *fakeProvider) { p.json(githubToken, http.StatusBadRequest, `{"error":"bad_verification_code"}`) },
			wantErr: "github oauth exchange",
		},
		{
			name: "user request fails",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.failOn = githubUser
			},
			wantErr: "github get user",
		},
		{
			name: "user endpoint answers non-200",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.json(githubUser, http.StatusUnauthorized, `{"message":"Bad credentials"}`)
			},
			wantErr: "github user status: 401",
		},
		{
			name: "user body is not JSON",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.json(githubUser, http.StatusOK, `<html>`)
			},
			wantErr: "github decode user",
		},
		{
			name: "emails request fails",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.json(githubUser, http.StatusOK, `{"id":1,"login":"octo"}`)
				p.failOn = githubEmails
			},
			wantErr: "github get emails",
		},
		{
			name: "emails endpoint answers non-200",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.json(githubUser, http.StatusOK, `{"id":1,"login":"octo"}`)
				p.json(githubEmails, http.StatusForbidden, `{}`)
			},
			wantErr: "github emails status: 403",
		},
		{
			name: "emails body is not JSON",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.json(githubUser, http.StatusOK, `{"id":1,"login":"octo"}`)
				p.json(githubEmails, http.StatusOK, `{"not":"a list"}`)
			},
			wantErr: "github decode emails",
		},
		{
			name: "account has no email at all",
			setup: func(p *fakeProvider) {
				p.json(githubToken, http.StatusOK, tokenJSON)
				p.json(githubUser, http.StatusOK, `{"id":1,"login":"octo"}`)
				p.json(githubEmails, http.StatusOK, `[]`)
			},
			wantErr: "no email found in github account",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newFakeProvider(t)
			tc.setup(p)
			user, err := NewGitHub("cid", "secret", "").Exchange(p.ctx(), "code")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
			if user != nil {
				t.Errorf("user = %+v, want nil on error", user)
			}
		})
	}
}

func TestGoogleExchange(t *testing.T) {
	t.Run("returns the userinfo profile", func(t *testing.T) {
		p := newFakeProvider(t)
		p.json(googleToken, http.StatusOK, tokenJSON)
		p.handle(googleUser, func(w http.ResponseWriter, r *http.Request) {
			requireBearer(t, r)
			_, _ = w.Write([]byte(`{"id":"g-1","email":"g@example.com","name":"Gee","picture":"https://p.example.com/g"}`))
		})
		user, err := NewGoogle("cid", "secret", "").Exchange(p.ctx(), "code")
		if err != nil {
			t.Fatalf("Exchange: %v", err)
		}
		want := User{ID: "g-1", Email: "g@example.com", Name: "Gee", Picture: "https://p.example.com/g"}
		if *user != want {
			t.Errorf("user = %+v, want %+v", *user, want)
		}
	})

	cases := []struct {
		name    string
		setup   func(p *fakeProvider)
		wantErr string
	}{
		{
			name:    "token endpoint refuses the code",
			setup:   func(p *fakeProvider) { p.json(googleToken, http.StatusBadRequest, `{"error":"invalid_grant"}`) },
			wantErr: "google oauth exchange",
		},
		{
			name: "userinfo request fails",
			setup: func(p *fakeProvider) {
				p.json(googleToken, http.StatusOK, tokenJSON)
				p.failOn = googleUser
			},
			wantErr: "google get userinfo",
		},
		{
			name: "userinfo answers non-200",
			setup: func(p *fakeProvider) {
				p.json(googleToken, http.StatusOK, tokenJSON)
				p.json(googleUser, http.StatusUnauthorized, `{}`)
			},
			wantErr: "google userinfo status: 401",
		},
		{
			name: "userinfo body is not JSON",
			setup: func(p *fakeProvider) {
				p.json(googleToken, http.StatusOK, tokenJSON)
				p.json(googleUser, http.StatusOK, `nope`)
			},
			wantErr: "google decode userinfo",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := newFakeProvider(t)
			tc.setup(p)
			user, err := NewGoogle("cid", "secret", "").Exchange(p.ctx(), "code")
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("err = %v, want one containing %q", err, tc.wantErr)
			}
			if user != nil {
				t.Errorf("user = %+v, want nil on error", user)
			}
		})
	}
}
