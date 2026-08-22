package auth

import (
	"net"
	"net/url"
	"strings"
)

// RedirectURIMatches reports whether a requested redirect_uri satisfies one the
// client registered, either in a Client ID Metadata Document or via RFC 7591
// dynamic client registration.
//
// Matching is exact (RFC 6749 §3.1.2.3) with one carve-out that RFC 8252 §7.3
// makes mandatory:
//
//	"The authorization server MUST allow any port to be specified at the time
//	of the request for loopback IP redirect URIs, to accommodate clients that
//	obtain an available ephemeral port from the operating system at the time of
//	the request."
//
// A native client cannot know its port when it publishes its metadata, so it
// registers a portless loopback URI and listens on whatever the OS hands it.
// Claude Code registers "http://localhost/callback" and then serves the
// callback on e.g. "http://localhost:3118/callback"; requiring string equality
// rejects every such client outright.
//
// Only the port is relaxed, and only when BOTH URIs are loopback. Scheme, host,
// path, query and userinfo must still match, so this cannot send a code to
// another host: a non-loopback registration can never be widened, and a
// loopback registration can only ever match another loopback address.
func RedirectURIMatches(registered, requested string) bool {
	if registered == requested {
		return true
	}

	reg, err := url.Parse(registered)
	if err != nil {
		return false
	}
	req, err := url.Parse(requested)
	if err != nil {
		return false
	}

	// Requiring both sides to be loopback is what keeps this from being a
	// redirect-to-anywhere hole: a client that registered a public https URI
	// gains nothing, and one that registered loopback cannot escape loopback.
	if !isLoopbackHost(reg.Hostname()) || !isLoopbackHost(req.Hostname()) {
		return false
	}

	// Hostnames must still agree — "localhost" and "127.0.0.1" are not treated
	// as interchangeable. Clients that want both register both, as Claude Code
	// does, and RFC 8252 §8.3 recommends the IP literal anyway.
	return strings.EqualFold(reg.Scheme, req.Scheme) &&
		strings.EqualFold(reg.Hostname(), req.Hostname()) &&
		reg.EscapedPath() == req.EscapedPath() &&
		reg.RawQuery == req.RawQuery &&
		reg.User.String() == req.User.String()
}

// AnyRedirectURIMatches reports whether requested matches any of the registered
// redirect URIs.
func AnyRedirectURIMatches(registered []string, requested string) bool {
	for _, candidate := range registered {
		if RedirectURIMatches(candidate, requested) {
			return true
		}
	}
	return false
}

// isLoopbackHost reports whether host is a loopback destination: the literal
// name "localhost", or any IP the stdlib considers loopback (127.0.0.0/8, ::1).
// host is expected to come from url.Hostname(), which strips the port and the
// brackets around an IPv6 literal.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
