// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

// Errors from resolving a server URL.
var (
	ErrNoServer     = errors.New("config: no server URL given")
	ErrBadServer    = errors.New("config: server URL is not usable")
	ErrInsecureHTTP = errors.New("config: refusing plain HTTP to a remote server")
)

// ServerURL is a checked address for an AgentRQ server, together with what had
// to be waived to accept it.
type ServerURL struct {
	// URL is the normalised address: scheme and host, no trailing slash.
	URL string
	// Loopback reports whether this address is the local machine.
	Loopback bool
	// Insecure reports that plain HTTP was accepted for a remote server
	// because it was explicitly allowed. Carried onwards so that every later
	// start can say so out loud.
	Insecure bool
}

// ResolveServer normalises and checks a server address.
//
// The rule, decided for this daemon: **TLS is required and verified unless the
// server is loopback.** There is no network to intercept on loopback, so
// demanding a certificate there would only push people towards a blanket
// "skip verification" flag — the worst of both worlds. Anywhere else, plain
// HTTP has to be asked for by name.
//
// allowInsecure is the --insecure flag. It permits plain HTTP to a remote host
// and nothing else: it never disables certificate verification for an https
// URL, because "the certificate is wrong" and "there is no certificate" are
// different problems and only one of them is ever deliberate.
func ResolveServer(raw string, allowInsecure bool) (ServerURL, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ServerURL{}, ErrNoServer
	}

	// A bare host is what people type. Defaulting it to https rather than http
	// means the safe reading is the one that needs no thought.
	if !strings.Contains(trimmed, "://") {
		trimmed = "https://" + trimmed
	}

	u, err := url.Parse(trimmed)
	if err != nil {
		return ServerURL{}, fmt.Errorf("%w: %q: %v", ErrBadServer, raw, err)
	}
	if u.Host == "" {
		return ServerURL{}, fmt.Errorf("%w: %q has no host", ErrBadServer, raw)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ServerURL{}, fmt.Errorf("%w: scheme %q is not http or https", ErrBadServer, u.Scheme)
	}

	loopback := isLoopback(u.Hostname())

	if u.Scheme == "http" && !loopback && !allowInsecure {
		return ServerURL{}, fmt.Errorf(
			"%w: %s. Use https, or pass --insecure to accept plain HTTP to this host",
			ErrInsecureHTTP, u.Host)
	}

	// Path, query and fragment are not part of a server address. Keeping them
	// would produce URLs like https://host/foo/api/v1/... the first time
	// something joined a path onto it.
	normalised := u.Scheme + "://" + u.Host

	return ServerURL{
		URL:      normalised,
		Loopback: loopback,
		// Only a waiver counts as insecure. Plain HTTP to loopback is the
		// documented normal case and should not make the daemon shout.
		Insecure: u.Scheme == "http" && !loopback,
	}, nil
}

// isLoopback reports whether a hostname names this machine.
func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	// A bracketed IPv6 literal arrives here already unwrapped by Hostname().
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

// InsecureWarning is the line printed on every start of an insecurely enrolled
// profile — not once at enrolment.
//
// A security decision that becomes invisible after first boot has stopped being
// a decision. Whoever runs this next, or finds it running on a machine they
// inherited, should be told without having to go looking.
func InsecureWarning(p Profile) string {
	if !p.Insecure {
		return ""
	}
	return fmt.Sprintf(
		"WARNING: profile %q talks to %s over plain HTTP. "+
			"Its machine token and everything typed into its terminals cross the network unencrypted.",
		p.ID, p.ServerURL)
}
