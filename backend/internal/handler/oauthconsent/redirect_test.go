// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsent

import (
	"errors"
	"net/url"
	"strings"
	"testing"
)

func TestCheckRedirectURI(t *testing.T) {
	const base = "https://agentrq.com"
	registered := []string{"https://client.example.com/callback", "http://localhost/callback"}

	accepted := []struct {
		name       string
		requested  string
		registered []string
		want       string
	}{
		{"a registered address", "https://client.example.com/callback", registered, "https://client.example.com/callback"},
		{"a registered loopback address on any port", "http://localhost:3118/callback", registered, "http://localhost:3118/callback"},
		{"the only registered address, when none is named", "", []string{"https://client.example.com/callback"}, "https://client.example.com/callback"},
		{"a registered custom scheme", "someneweditor://callback", []string{"someneweditor://callback"}, "someneweditor://callback"},
		{"a path on this server", "/settings/callback", nil, "/settings/callback"},
		{"this server's own origin", "https://agentrq.com/callback", nil, "https://agentrq.com/callback"},
		{"a loopback address", "http://127.0.0.1:3118/callback", nil, "http://127.0.0.1:3118/callback"},
		{"an IPv6 loopback address", "http://[::1]:3118/callback", nil, "http://[::1]:3118/callback"},
		{"a known native app's scheme", "cursor://callback", nil, "cursor://callback"},
		{"a native app scheme whose host is not a hostname", "vscode://oauth_callback", nil, "vscode://oauth_callback"},
	}
	for _, tc := range accepted {
		t.Run("accepted: "+tc.name, func(t *testing.T) {
			got, err := CheckRedirectURI(tc.requested, tc.registered, base)
			if err != nil {
				t.Fatalf("CheckRedirectURI(%q) refused it: %v", tc.requested, err)
			}
			if got != tc.want {
				t.Errorf("CheckRedirectURI(%q) returned %q, want %q", tc.requested, got, tc.want)
			}
		})
	}

	refused := []struct {
		name       string
		requested  string
		registered []string
		baseURL    string
		want       error
	}{
		{"no address and nothing registered", "", nil, base, ErrRedirectMissing},
		{"no address and two registered", "", registered, base, ErrRedirectMissing},
		{"an address the client did not register", "https://evil.example.com/callback", registered, base, ErrRedirectNotRegistered},
		{"a registered host on another path", "https://client.example.com/other", registered, base, ErrRedirectNotRegistered},
		{"an unparseable address", "http://%zz", nil, base, ErrRedirectMalformed},
		{"a fragment", "https://client.example.com/callback#x", registered, base, ErrRedirectFragment},
		{"an empty fragment", "https://agentrq.com/callback#", nil, base, ErrRedirectFragment},
		{"a user name", "https://agentrq.com@evil.example.com/callback", nil, base, ErrRedirectUserinfo},
		{"a host that is not a hostname", "https://exa_mple.com/callback", []string{"https://exa_mple.com/callback"}, base, ErrRedirectHost},
		{"a host longer than DNS allows", "https://" + strings.Repeat("a.", 127) + "com/callback", nil, base, ErrRedirectHost},
		{"a scheme-relative address", "//evil.example.com/callback", nil, base, ErrRedirectRelative},
		{"a backslash path", "/\\evil.example.com", nil, base, ErrRedirectRelative},
		{"a bare relative path", "callback", nil, base, ErrRedirectRelative},
		{"an unknown custom scheme", "evilapp://callback", nil, base, ErrRedirectScheme},
		{"plain http to another host", "http://agentrq.com/callback", nil, base, ErrRedirectInsecure},
		{"another site", "https://evil.example.com/callback", nil, base, ErrRedirectHostMismatch},
		{"a lookalike of localhost", "https://localhost.evil.com/callback", nil, base, ErrRedirectHostMismatch},
		{"an unparseable base URL", "https://agentrq.com/callback", nil, "http://%zz", ErrRedirectHostMismatch},
	}
	for _, tc := range refused {
		t.Run("refused: "+tc.name, func(t *testing.T) {
			got, err := CheckRedirectURI(tc.requested, tc.registered, tc.baseURL)
			if !errors.Is(err, tc.want) {
				t.Errorf("CheckRedirectURI(%q) returned %q, %v, want the error %v", tc.requested, got, err, tc.want)
			}
		})
	}
}

func TestLoginURLCarriesTheReturnAddress(t *testing.T) {
	back := "https://mcp.agentrq.com/mcp/oauth2/authorize?client_id=a&state=b c"
	got := LoginURL("https://agentrq.com", back)

	loc, err := url.Parse(got)
	if err != nil {
		t.Fatalf("LoginURL returned %q, not a URL: %v", got, err)
	}
	if prefix := "https://agentrq.com/login?"; !strings.HasPrefix(got, prefix) {
		t.Errorf("LoginURL = %q, want it to start with %q", got, prefix)
	}
	if r := loc.Query().Get("redirect_url"); r != back {
		t.Errorf("redirect_url = %q, want %q", r, back)
	}
}
