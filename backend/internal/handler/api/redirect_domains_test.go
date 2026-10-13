// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"io"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v2"
	"gopkg.in/yaml.v3"
)

func TestRedirectDomains_UnmarshalYAML(t *testing.T) {
	tests := []struct {
		name string
		doc  string
		want RedirectDomains
	}{
		{"yaml list", "d: [app.example.com, ' docs.example.com ']", RedirectDomains{"app.example.com", "docs.example.com"}},
		{"comma separated, as the env var gives it", "d: 'app.example.com, docs.example.com,'", RedirectDomains{"app.example.com", "docs.example.com"}},
		{"empty env var", "d: ''", nil},
		{"unset env var", "d:", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var cfg struct {
				D RedirectDomains `yaml:"d"`
			}
			if err := yaml.Unmarshal([]byte(tt.doc), &cfg); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			if !reflect.DeepEqual(cfg.D, tt.want) {
				t.Errorf("got %#v, want %#v", cfg.D, tt.want)
			}
		})
	}
}

func TestRedirectDomains_UnmarshalYAML_RefusesOtherShapes(t *testing.T) {
	for _, doc := range []string{"d: {a: b}", "d: [{a: b}]"} {
		var cfg struct {
			D RedirectDomains `yaml:"d"`
		}
		if err := yaml.Unmarshal([]byte(doc), &cfg); err == nil {
			t.Errorf("Unmarshal(%q) = nil error, want one", doc)
		}
	}
}

func TestRedirectHosts(t *testing.T) {
	hosts, err := redirectHosts([]string{"App.Example.com", "localhost:8080"})
	if err != nil {
		t.Fatalf("redirectHosts: %v", err)
	}
	want := map[string]struct{}{"app.example.com": {}, "localhost:8080": {}}
	if !reflect.DeepEqual(hosts, want) {
		t.Errorf("got %v, want %v", hosts, want)
	}

	for _, bad := range []string{"https://app.example.com", "app.example.com/path", "user@app.example.com", "app.example.com?x=1", "app.example.com#x", "bad host", "%zz"} {
		if _, err := redirectHosts([]string{bad}); err == nil || !strings.Contains(err.Error(), "auth.allowedRedirectDomains") {
			t.Errorf("redirectHosts(%q) error = %v, want an auth.allowedRedirectDomains error", bad, err)
		}
	}
}

func TestNew_RefusesABadRedirectDomain(t *testing.T) {
	app := fiber.New()
	_, err := New(Params{Router: app.Group(_routeBasePath), RedirectDomains: []string{"https://app.example.com"}})
	if err == nil {
		t.Fatal("New accepted a redirect domain with a scheme")
	}
}

func TestSanitizeRedirectURL_AllowedDomains(t *testing.T) {
	hosts, err := redirectHosts([]string{"app.example.com", "localhost:8080"})
	if err != nil {
		t.Fatal(err)
	}
	h := &handler{baseURL: "http://localhost:3000", basePath: "/agentrq", redirectHosts: hosts}

	tests := []struct {
		input string
		want  string
	}{
		{"https://app.example.com/after-login?x=1", "https://app.example.com/after-login?x=1"},
		{"http://APP.example.com/", "http://APP.example.com/"},
		{"http://localhost:8080/cb", "http://localhost:8080/cb"},
		{"https://app.example.com.evil.com/", "/agentrq/"},
		{"https://evil.com/?https://app.example.com", "/agentrq/"},
		{"https://app.example.com:444/", "/agentrq/"},
		{"javascript://app.example.com/%0aalert(1)", "/agentrq/"},
	}
	for _, tt := range tests {
		if got := h.sanitizeRedirectURL(tt.input); got != tt.want {
			t.Errorf("sanitizeRedirectURL(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestAuthConfig_ListsAllowedRedirectDomains(t *testing.T) {
	for _, tt := range []struct {
		domains []string
		want    []any
	}{
		{nil, []any{}},
		{[]string{"app.example.com"}, []any{"app.example.com"}},
	} {
		app := fiber.New()
		h := &handler{redirectDomains: tt.domains}
		app.Get("/auth/config", h.authConfig())

		resp, err := app.Test(httptest.NewRequest("GET", "/auth/config", nil))
		if err != nil {
			t.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		var got map[string]any
		if err := json.Unmarshal(body, &got); err != nil {
			t.Fatalf("decode %s: %v", body, err)
		}
		if !reflect.DeepEqual(got["allowedRedirectDomains"], tt.want) {
			t.Errorf("allowedRedirectDomains = %#v, want %#v", got["allowedRedirectDomains"], tt.want)
		}
	}
}
