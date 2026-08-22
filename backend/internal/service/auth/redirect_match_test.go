package auth

import "testing"

func TestRedirectURIMatches(t *testing.T) {
	tests := []struct {
		name       string
		registered string
		requested  string
		want       bool
	}{
		// The regression this function exists for: Claude Code publishes a
		// portless loopback URI in its Client ID Metadata Document and then
		// listens on an OS-assigned ephemeral port.
		{"claude code ephemeral port on localhost", "http://localhost/callback", "http://localhost:3118/callback", true},
		{"claude code ephemeral port on 127.0.0.1", "http://127.0.0.1/callback", "http://127.0.0.1:54321/callback", true},
		{"registered port replaced by another", "http://localhost:1234/callback", "http://localhost:5678/callback", true},
		{"requested drops the registered port", "http://localhost:1234/callback", "http://localhost/callback", true},
		{"ipv6 loopback ephemeral port", "http://[::1]/callback", "http://[::1]:8080/callback", true},

		// Exact matches, loopback or not.
		{"identical strings", "https://app.example.com/cb", "https://app.example.com/cb", true},
		{"identical custom scheme", "cursor://callback", "cursor://callback", true},

		// The port carve-out must not become a redirect-to-anywhere hole.
		{"loopback registration cannot escape to a public host", "http://localhost/callback", "http://evil.example.com/callback", false},
		{"loopback registration cannot escape via port syntax", "http://localhost/callback", "http://evil.example.com:3118/callback", false},
		{"public registration is not relaxed by port", "https://app.example.com/cb", "https://app.example.com:8443/cb", false},
		{"decimal-encoded loopback is not localhost", "http://localhost/callback", "http://2130706433/callback", false},
		{"lookalike host is not loopback", "http://localhost/callback", "http://localhost.evil.com/callback", false},
		{"subdomain of loopback name is not loopback", "http://localhost/callback", "http://a.localhost/callback", false},

		// Only the port is relaxed — every other component still binds.
		{"path must match", "http://localhost/callback", "http://localhost:3118/other", false},
		{"path traversal is not a match", "http://localhost/callback", "http://localhost:3118/callback/../evil", false},
		{"query must match", "http://localhost/callback", "http://localhost:3118/callback?next=evil", false},
		{"scheme must match", "http://localhost/callback", "https://localhost:3118/callback", false},
		{"userinfo must match", "http://localhost/callback", "http://user:pw@localhost:3118/callback", false},
		{"localhost and 127.0.0.1 are not interchangeable", "http://localhost/callback", "http://127.0.0.1:3118/callback", false},

		// 127.0.0.0/8 is all loopback, but the host still has to be the same one.
		{"different loopback IPs do not match", "http://127.0.0.1/cb", "http://127.0.0.2:99/cb", false},
		{"same non-standard loopback IP matches on any port", "http://127.0.0.2/cb", "http://127.0.0.2:99/cb", true},

		// Malformed input must fail closed rather than panic.
		{"unparseable requested", "http://localhost/callback", "http://[::1/callback", false},
		{"unparseable registered", "http://[::1/callback", "http://localhost/callback", false},
		{"empty requested", "http://localhost/callback", "", false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := RedirectURIMatches(tc.registered, tc.requested); got != tc.want {
				t.Errorf("RedirectURIMatches(%q, %q) = %v, want %v",
					tc.registered, tc.requested, got, tc.want)
			}
		})
	}
}

func TestAnyRedirectURIMatches(t *testing.T) {
	// Exactly what Claude Code publishes.
	claudeCode := []string{"http://localhost/callback", "http://127.0.0.1/callback"}

	if !AnyRedirectURIMatches(claudeCode, "http://localhost:3118/callback") {
		t.Error("expected the ephemeral-port localhost callback to match")
	}
	if !AnyRedirectURIMatches(claudeCode, "http://127.0.0.1:3118/callback") {
		t.Error("expected the ephemeral-port 127.0.0.1 callback to match")
	}
	if AnyRedirectURIMatches(claudeCode, "http://evil.example.com:3118/callback") {
		t.Error("a non-loopback host must not match a loopback registration")
	}
	if AnyRedirectURIMatches(nil, "http://localhost:3118/callback") {
		t.Error("no registered URIs must match nothing")
	}
}
