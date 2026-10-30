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
	"testing"
	"time"
)

// productionDial returns the DialContext of NewCIMDResolver's hardened
// transport, so its refusals can be checked one by one.
func productionDial(t *testing.T) func(ctx context.Context, network, addr string) (any, error) {
	t.Helper()
	r, ok := NewCIMDResolver().(*cimdResolver)
	if !ok {
		t.Fatalf("NewCIMDResolver returned %T, want *cimdResolver", NewCIMDResolver())
	}
	tr, ok := r.client.Transport.(*http.Transport)
	if !ok || tr.DialContext == nil {
		t.Fatalf("resolver transport = %T, want an *http.Transport with a DialContext", r.client.Transport)
	}
	return func(ctx context.Context, network, addr string) (any, error) {
		conn, err := tr.DialContext(ctx, network, addr)
		if conn != nil {
			_ = conn.Close()
		}
		return conn, err
	}
}

func TestCIMDDial_RefusesAddressesItCannotCheck(t *testing.T) {
	dial := productionDial(t)

	t.Run("address without a port", func(t *testing.T) {
		if _, err := dial(context.Background(), "tcp", "client.example.com"); err == nil {
			t.Fatal("dialled an address with no port")
		}
	})

	t.Run("host that does not resolve", func(t *testing.T) {
		if _, err := dial(context.Background(), "tcp", ":443"); err == nil {
			t.Fatal("dialled an empty host")
		}
	})
}

func TestCIMDDial_DialsTheValidatedPublicAddress(t *testing.T) {
	// A public literal passes the special-use check and reaches the dial. The
	// context is already cancelled, so the dial fails at once with that cause
	// instead of touching the network — which proves the dial was attempted.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := productionDial(t)(ctx, "tcp", "8.8.8.8:443")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want the dial to be attempted and cancelled", err)
	}
}

// The production client, not the test one, must treat a redirect as the
// response itself. Its transport is swapped for one that trusts the test
// server, keeping its own redirect policy.
func TestNewCIMDResolver_DoesNotFollowRedirects(t *testing.T) {
	followed := false
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/elsewhere" {
			followed = true
		}
		http.Redirect(w, r, "/elsewhere", http.StatusFound)
	}))
	defer ts.Close()

	r := NewCIMDResolver().(*cimdResolver)
	r.client.Transport = ts.Client().Transport

	_, err := r.Resolve(context.Background(), ts.URL+"/client")
	if err == nil || !strings.Contains(err.Error(), "unexpected status 302") {
		t.Fatalf("err = %v, want the redirect reported as status 302", err)
	}
	if followed {
		t.Error("the resolver followed the redirect")
	}
}

func TestIsSpecialUseIP_NilFailsClosed(t *testing.T) {
	if !isSpecialUseIP(nil) {
		t.Fatal("an unparseable address was treated as public")
	}
}

func TestParseClientIDURL_UnparseableURL(t *testing.T) {
	if _, err := parseClientIDURL("https://[::1/client"); err == nil {
		t.Fatal("accepted a URL with an unterminated IPv6 host")
	}
}

func TestResolve_NilContextFailsBeforeFetching(t *testing.T) {
	var nilCtx context.Context
	r := &cimdResolver{client: http.DefaultClient, cache: map[string]cimdCacheEntry{}}
	if _, err := r.Resolve(nilCtx, "https://client.example.com/app"); err == nil {
		t.Fatal("Resolve with a nil context returned no error")
	}
}

func TestResolve_TruncatedBodyRejected(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Promise more than is sent; the server then drops the connection.
		w.Header().Set("Content-Length", "100")
		_, _ = w.Write([]byte(`{"client_id":`))
	}))
	defer ts.Close()

	_, err := newTestResolver(ts).Resolve(context.Background(), ts.URL+"/client")
	if err == nil || !strings.Contains(err.Error(), "reading body") {
		t.Fatalf("err = %v, want a body read error", err)
	}
}

func TestCacheTTLFromResponse(t *testing.T) {
	cases := []struct {
		cacheControl string
		want         time.Duration
	}{
		{"", cimdDefaultTTL},
		{"no-store", cimdDefaultTTL},
		{"max-age=abc", cimdDefaultTTL},
		{"max-age=abc, max-age=600", 600 * time.Second},
		{"public, max-age=300", 300 * time.Second},
		{"max-age=5", cimdMinTTL},
		{"max-age=999999", cimdMaxTTL},
	}
	for _, tc := range cases {
		t.Run(tc.cacheControl, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tc.cacheControl != "" {
				resp.Header.Set("Cache-Control", tc.cacheControl)
			}
			if got := cacheTTLFromResponse(resp); got != tc.want {
				t.Errorf("cacheTTLFromResponse(%q) = %v, want %v", tc.cacheControl, got, tc.want)
			}
		})
	}
}

func TestIsAllowedNativeRedirectScheme(t *testing.T) {
	cases := map[string]bool{
		"cursor":     true,
		"VSCode":     true, // schemes are case-insensitive
		"claude":     true,
		"javascript": false,
		"https":      false,
		"":           false,
	}
	for scheme, want := range cases {
		if got := IsAllowedNativeRedirectScheme(scheme); got != want {
			t.Errorf("IsAllowedNativeRedirectScheme(%q) = %v, want %v", scheme, got, want)
		}
	}
}
