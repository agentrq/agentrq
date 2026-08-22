package auth

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// newTestResolver builds a resolver pointed at an httptest TLS server. It
// deliberately reuses the server's own client (which trusts the test cert and
// dials loopback) instead of NewCIMDResolver's SSRF-hardened transport, which
// would — correctly — refuse to connect to 127.0.0.1. The SSRF policy itself
// is covered separately by TestIsSpecialUseIP.
func newTestResolver(ts *httptest.Server) *cimdResolver {
	client := ts.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &cimdResolver{client: client, cache: make(map[string]cimdCacheEntry)}
}

func TestIsClientIDURL(t *testing.T) {
	tests := []struct {
		name     string
		clientID string
		want     bool
	}{
		{"valid https URL with path", "https://client.example.com/app", true},
		{"valid with port", "https://client.example.com:8443/app", true},
		{"http scheme rejected", "http://client.example.com/app", false},
		{"custom scheme rejected", "cursor://callback", false},
		{"opaque DCR client id rejected", "dynamic-abc123", false},
		{"no path rejected", "https://client.example.com", false},
		{"userinfo rejected", "https://user:pass@client.example.com/app", false},
		{"fragment rejected", "https://client.example.com/app#frag", false},
		{"single-dot path segment rejected", "https://client.example.com/./app", false},
		{"double-dot path segment rejected", "https://client.example.com/../app", false},
		{"empty rejected", "", false},
	}

	r := &cimdResolver{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := r.IsClientIDURL(tt.clientID); got != tt.want {
				t.Errorf("IsClientIDURL(%q) = %v, want %v", tt.clientID, got, tt.want)
			}
		})
	}
}

// isSpecialUseIP is what keeps an attacker from pointing a client_id at
// internal infrastructure (SSRF), so its ranges are worth pinning down.
func TestIsSpecialUseIP(t *testing.T) {
	tests := []struct {
		ip   string
		want bool
	}{
		{"127.0.0.1", true},       // loopback
		{"::1", true},             // loopback v6
		{"10.0.0.1", true},        // private
		{"192.168.1.1", true},     // private
		{"172.16.0.1", true},      // private
		{"169.254.169.254", true}, // link-local (cloud metadata endpoint)
		{"0.0.0.0", true},         // unspecified
		{"224.0.0.1", true},       // multicast
		{"255.255.255.255", true}, // broadcast
		{"fd00::1", true},         // IPv6 unique-local
		{"fe80::1", true},         // IPv6 link-local

		// An IPv4-mapped v6 literal must be rejected for the same reason its
		// bare v4 form is, or it becomes a trivial bypass.
		{"::ffff:127.0.0.1", true},
		{"::ffff:169.254.169.254", true},

		// NAT64 and 6to4 wrap an arbitrary IPv4 address inside a v6 one.
		{"64:ff9b::7f00:1", true},    // -> 127.0.0.1
		{"64:ff9b::a9fe:a9fe", true}, // -> 169.254.169.254

		{"100.64.0.1", true}, // RFC 6598 shared address space
		{"198.18.0.1", true}, // benchmarking
		{"240.0.0.1", true},  // reserved for future use
		{"192.0.0.1", true},  // IETF protocol assignments

		{"93.184.216.34", false},
		{"8.8.8.8", false},
		{"2606:4700:4700::1111", false}, // public v6 resolver
	}

	for _, tt := range tests {
		t.Run(tt.ip, func(t *testing.T) {
			if got := isSpecialUseIP(net.ParseIP(tt.ip)); got != tt.want {
				t.Errorf("isSpecialUseIP(%s) = %v, want %v", tt.ip, got, tt.want)
			}
		})
	}
}

// The production resolver must refuse to connect to internal targets even
// though the client_id URL is entirely attacker-supplied. This exercises the
// real NewCIMDResolver transport, not the loopback-permitting test one.
func TestNewCIMDResolver_RefusesInternalTargets(t *testing.T) {
	// A real local listener, so a failure here means the guard let it through
	// rather than the address simply being unreachable.
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"client_id":"pwned"}`))
	}))
	defer ts.Close()

	resolver := NewCIMDResolver()
	for _, clientID := range []string{
		ts.URL + "/client",                  // 127.0.0.1:port — a live server
		"https://127.0.0.1/client",          // loopback
		"https://[::1]/client",              // loopback v6
		"https://169.254.169.254/latest",    // cloud metadata
		"https://10.0.0.1/client",           // private
		"https://[::ffff:127.0.0.1]/client", // v4-mapped bypass attempt
	} {
		t.Run(clientID, func(t *testing.T) {
			if _, err := resolver.Resolve(context.Background(), clientID); err == nil {
				t.Errorf("expected %q to be refused, but the fetch succeeded", clientID)
			}
		})
	}
}

func TestResolve_ValidDocument(t *testing.T) {
	var clientIDURL string
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"client_id": "` + clientIDURL + `",
			"client_name": "Test Client",
			"redirect_uris": ["https://client.example.com/callback", "cursor://callback"],
			"token_endpoint_auth_method": "none"
		}`))
	}))
	defer ts.Close()
	clientIDURL = ts.URL + "/client"

	metadata, err := newTestResolver(ts).Resolve(context.Background(), clientIDURL)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if metadata.ClientID != clientIDURL {
		t.Errorf("ClientID = %q, want %q", metadata.ClientID, clientIDURL)
	}
	if len(metadata.RedirectURIs) != 2 {
		t.Fatalf("expected 2 redirect_uris, got %v", metadata.RedirectURIs)
	}
	// A custom-scheme redirect is legitimate here precisely because the
	// client published it in its own metadata document.
	if metadata.RedirectURIs[1] != "cursor://callback" {
		t.Errorf("unexpected redirect_uris: %v", metadata.RedirectURIs)
	}
	if metadata.TokenEndpointAuthMethod != "none" {
		t.Errorf("TokenEndpointAuthMethod = %q, want none", metadata.TokenEndpointAuthMethod)
	}
}

// The draft requires the document's client_id to match the URL it was
// fetched from; without this a client could host a document claiming to be
// someone else.
func TestResolve_ClientIDMismatchRejected(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"client_id": "https://someone-else.example.com/client"}`))
	}))
	defer ts.Close()

	_, err := newTestResolver(ts).Resolve(context.Background(), ts.URL+"/client")
	if err == nil {
		t.Fatal("expected a client_id mismatch to be rejected")
	}
}

func TestResolve_ForbiddenFieldsRejected(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"client_secret", `{"client_id": "%s", "client_secret": "shhh"}`},
		{"client_secret_expires_at", `{"client_id": "%s", "client_secret_expires_at": 0}`},
		{"client_secret_basic auth", `{"client_id": "%s", "token_endpoint_auth_method": "client_secret_basic"}`},
		{"client_secret_post auth", `{"client_id": "%s", "token_endpoint_auth_method": "client_secret_post"}`},
		{"client_secret_jwt auth", `{"client_id": "%s", "token_endpoint_auth_method": "client_secret_jwt"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var clientIDURL string
			ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, tt.body, clientIDURL)
			}))
			defer ts.Close()
			clientIDURL = ts.URL + "/client"

			if _, err := newTestResolver(ts).Resolve(context.Background(), clientIDURL); err == nil {
				t.Errorf("expected %s to be rejected", tt.name)
			}
		})
	}
}

func TestResolve_NonOKStatusRejected(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer ts.Close()

	if _, err := newTestResolver(ts).Resolve(context.Background(), ts.URL+"/client"); err == nil {
		t.Fatal("expected a non-200 response to be rejected")
	}
}

// The draft forbids following redirects when fetching the document.
func TestResolve_RedirectNotFollowed(t *testing.T) {
	var clientIDURL string
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/client" {
			http.Redirect(w, r, "/elsewhere", http.StatusFound)
			return
		}
		w.Write([]byte(`{"client_id": "` + clientIDURL + `"}`))
	}))
	defer ts.Close()
	clientIDURL = ts.URL + "/client"

	if _, err := newTestResolver(ts).Resolve(context.Background(), clientIDURL); err == nil {
		t.Fatal("expected a redirect to be treated as a fetch failure")
	}
}

func TestResolve_InvalidJSONRejected(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json at all`))
	}))
	defer ts.Close()

	if _, err := newTestResolver(ts).Resolve(context.Background(), ts.URL+"/client"); err == nil {
		t.Fatal("expected a malformed document to be rejected")
	}
}

func TestResolve_NonURLClientIDRejectedWithoutFetch(t *testing.T) {
	fetched := false
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetched = true
	}))
	defer ts.Close()

	if _, err := newTestResolver(ts).Resolve(context.Background(), "dynamic-abc123"); err == nil {
		t.Fatal("expected a non-URL client_id to be rejected")
	}
	if fetched {
		t.Error("a non-URL client_id must not trigger an outbound fetch")
	}
}

func TestResolve_CachesSuccessfulFetch(t *testing.T) {
	var clientIDURL string
	fetches := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		w.Write([]byte(`{"client_id": "` + clientIDURL + `"}`))
	}))
	defer ts.Close()
	clientIDURL = ts.URL + "/client"

	r := newTestResolver(ts)
	for range 3 {
		if _, err := r.Resolve(context.Background(), clientIDURL); err != nil {
			t.Fatalf("Resolve: %v", err)
		}
	}
	if fetches != 1 {
		t.Errorf("expected the document to be fetched once and cached, got %d fetches", fetches)
	}
}

// Errors must never be cached, so a client that fixes its document isn't
// locked out for the cache lifetime.
func TestResolve_DoesNotCacheErrors(t *testing.T) {
	var clientIDURL string
	fetches := 0
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetches++
		if fetches == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"client_id": "` + clientIDURL + `"}`))
	}))
	defer ts.Close()
	clientIDURL = ts.URL + "/client"

	r := newTestResolver(ts)
	if _, err := r.Resolve(context.Background(), clientIDURL); err == nil {
		t.Fatal("expected the first fetch to fail")
	}
	if _, err := r.Resolve(context.Background(), clientIDURL); err != nil {
		t.Fatalf("expected a retry after an error to re-fetch and succeed, got %v", err)
	}
}
