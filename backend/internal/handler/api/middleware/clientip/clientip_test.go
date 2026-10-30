// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package clientip

import (
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rs/zerolog"
	zlog "github.com/rs/zerolog/log"
)

// resolve runs a request from remote through the middleware and returns the
// RemoteAddr the next handler sees.
func resolve(t *testing.T, r *Resolver, remote string, headers map[string]string) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = remote
	for k, v := range headers {
		req.Header.Add(k, v)
	}
	var seen string
	r.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		seen = req.RemoteAddr
	})).ServeHTTP(httptest.NewRecorder(), req)
	return seen
}

func mustNew(t *testing.T, trusted, header string) *Resolver {
	t.Helper()
	r, err := New(trusted, header)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// With nothing trusted no header is believed, not even from a private or
// loopback peer: that is what Docker's gateway looks like when it hides the
// real client.
func TestNothingTrusted_NoHeaderIsBelieved(t *testing.T) {
	r := mustNew(t, "", "")
	for name, tc := range map[string]struct {
		remote  string
		headers map[string]string
		want    string
	}{
		"a public peer is itself":            {"203.0.113.7:4000", nil, "203.0.113.7:4000"},
		"a public peer's X-Real-IP":          {"203.0.113.7:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "203.0.113.7:4000"},
		"a public peer's X-Forwarded-For":    {"203.0.113.7:4000", map[string]string{"X-Forwarded-For": "198.51.100.2"}, "203.0.113.7:4000"},
		"Docker's gateway's X-Real-IP":       {"172.17.0.1:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "172.17.0.1:4000"},
		"a loopback peer's X-Real-IP":        {"127.0.0.1:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "127.0.0.1:4000"},
		"an IPv6 peer keeps its brackets":    {"[2001:db8::1]:4000", nil, "[2001:db8::1]:4000"},
		"an address without a port gets one": {"203.0.113.7", nil, "203.0.113.7:0"},
		"a peer that is not an IP is left":   {"pipe", map[string]string{"X-Real-IP": "198.51.100.2"}, "pipe"},
	} {
		if got := resolve(t, r, tc.remote, tc.headers); got != tc.want {
			t.Errorf("%s: RemoteAddr %q, want %q", name, got, tc.want)
		}
	}
}

func TestTrustedProxy_SingleAddressHeader(t *testing.T) {
	for name, tc := range map[string]struct {
		trusted, header, remote string
		headers                 map[string]string
		want                    string
	}{
		"a trusted proxy names the client":       {"127.0.0.1", "", "127.0.0.1:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "198.51.100.2:4000"},
		"a CIDR is trusted throughout":           {"10.0.0.0/8, 172.17.0.1", "", "10.1.2.3:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "198.51.100.2:4000"},
		"an IPv6 proxy names an IPv6 client":     {"::1", "", "[::1]:4000", map[string]string{"X-Real-IP": "2001:db8::9"}, "[2001:db8::9]:4000"},
		"another private peer is not trusted":    {"127.0.0.1", "", "10.0.0.5:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "10.0.0.5:4000"},
		"a trusted proxy with no header":         {"127.0.0.1", "", "127.0.0.1:4000", nil, "127.0.0.1:4000"},
		"a header that is not an address":        {"127.0.0.1", "", "127.0.0.1:4000", map[string]string{"X-Real-IP": "evil:1"}, "127.0.0.1:4000"},
		"the configured header, and only it":     {"127.0.0.1", "CF-Connecting-IP", "127.0.0.1:4000", map[string]string{"X-Real-IP": "198.51.100.2"}, "127.0.0.1:4000"},
		"Cloudflare's header":                    {"173.245.48.0/20", "cf-connecting-ip", "173.245.48.9:4000", map[string]string{"CF-Connecting-IP": "198.51.100.2"}, "198.51.100.2:4000"},
		"X-Forwarded-For is not read by default": {"127.0.0.1", "", "127.0.0.1:4000", map[string]string{"X-Forwarded-For": "198.51.100.2"}, "127.0.0.1:4000"},
	} {
		if got := resolve(t, mustNew(t, tc.trusted, tc.header), tc.remote, tc.headers); got != tc.want {
			t.Errorf("%s: RemoteAddr %q, want %q", name, got, tc.want)
		}
	}
}

// The client writes X-Forwarded-For's leftmost entries; only what trusted
// proxies appended can be believed.
func TestTrustedProxy_ForwardedForIsReadFromTheRight(t *testing.T) {
	r := mustNew(t, "10.0.0.0/8", "X-Forwarded-For")
	for name, tc := range map[string]struct {
		remote, xff, want string
	}{
		"one proxy":                          {"10.0.0.5:4000", "198.51.100.2", "198.51.100.2:4000"},
		"a spoofed entry left of the client": {"10.0.0.5:4000", "192.0.2.1, 198.51.100.2", "198.51.100.2:4000"},
		"a chain of trusted proxies":         {"10.0.0.5:4000", "192.0.2.1, 198.51.100.2, 10.0.0.9", "198.51.100.2:4000"},
		"every entry trusted":                {"10.0.0.5:4000", "10.0.0.8, 10.0.0.9", "10.0.0.8:4000"},
		"garbage stops the walk":             {"10.0.0.5:4000", "198.51.100.2, junk, 10.0.0.9", "10.0.0.9:4000"},
		"garbage at once leaves the peer":    {"10.0.0.5:4000", "junk", "10.0.0.5:4000"},
		"an untrusted peer is itself":        {"203.0.113.7:4000", "198.51.100.2", "203.0.113.7:4000"},
	} {
		if got := resolve(t, r, tc.remote, map[string]string{"X-Forwarded-For": tc.xff}); got != tc.want {
			t.Errorf("%s: RemoteAddr %q, want %q", name, got, tc.want)
		}
	}
	// A chain may arrive as several headers rather than one list.
	h := http.Header{}
	h.Add("X-Forwarded-For", "192.0.2.1")
	h.Add("X-Forwarded-For", "198.51.100.2")
	if got := r.Client(net.ParseIP("10.0.0.5"), h).String(); got != "198.51.100.2" {
		t.Errorf("split across headers: client %q, want 198.51.100.2", got)
	}
}

func TestNew_RefusesWhatIsNotAnAddress(t *testing.T) {
	for _, s := range []string{"proxy.local", "10.0.0.0/33", "10.0.0.1, nope"} {
		if _, err := New(s, ""); err == nil {
			t.Errorf("New(%q) accepted it", s)
		}
	}
}

// A private proxy that names its clients while nothing is trusted is the
// misconfiguration that gives everybody one limit; it is logged once.
func TestUntrustedPrivateProxy_IsWarnedAboutOnce(t *testing.T) {
	var out strings.Builder
	prev := zlog.Logger
	zlog.Logger = zerolog.New(&out)
	t.Cleanup(func() { zlog.Logger = prev })

	r := mustNew(t, "", "")
	resolve(t, r, "203.0.113.7:4000", map[string]string{"X-Real-IP": "198.51.100.2"})
	resolve(t, r, "172.17.0.1:4000", nil)
	if out.Len() != 0 {
		t.Fatalf("warned for a public peer or a private one naming nobody: %s", out.String())
	}
	resolve(t, r, "172.17.0.1:4000", map[string]string{"X-Forwarded-For": "198.51.100.2"})
	resolve(t, r, "127.0.0.1:4000", map[string]string{"X-Real-IP": "198.51.100.2"})
	if n := strings.Count(out.String(), "AGENTRQ_TRUSTED_PROXIES"); n != 1 {
		t.Fatalf("warned %d times, want once: %s", n, out.String())
	}

	out.Reset()
	resolve(t, mustNew(t, "127.0.0.1", ""), "10.0.0.5:4000", map[string]string{"X-Real-IP": "198.51.100.2"})
	if out.Len() != 0 {
		t.Fatalf("warned although a proxy is trusted: %s", out.String())
	}
}

func TestFromRequest(t *testing.T) {
	for remote, want := range map[string]string{
		"203.0.113.7:4000":   "203.0.113.7",
		"[2001:db8::1]:4000": "2001:db8::1",
		"pipe":               "pipe",
	} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Real-IP", "198.51.100.2")
		if got := FromRequest(req); got != want {
			t.Errorf("FromRequest(%q) = %q, want %q", remote, got, want)
		}
	}
}
