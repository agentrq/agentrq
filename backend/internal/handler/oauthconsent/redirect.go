// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package oauthconsent

import (
	"errors"
	"net"
	"net/url"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"golang.org/x/net/idna"
)

// The reasons a redirect_uri is refused, worded for whoever reads the 400.
var (
	ErrRedirectMissing       = errors.New("invalid redirect_uri: required unless the client registered exactly one")
	ErrRedirectNotRegistered = errors.New("invalid redirect_uri: not registered for this client_id")
	ErrRedirectMalformed     = errors.New("invalid redirect_uri")
	ErrRedirectFragment      = errors.New("invalid redirect_uri: must not contain a fragment")
	ErrRedirectUserinfo      = errors.New("invalid redirect_uri: must not contain a user name or password")
	ErrRedirectHost          = errors.New("invalid redirect_uri: host is not a valid hostname")
	ErrRedirectRelative      = errors.New("invalid redirect_uri: relative path must start with /")
	ErrRedirectScheme        = errors.New("invalid redirect_uri: unrecognized custom scheme; register the redirect_uri to use it")
	ErrRedirectInsecure      = errors.New("invalid redirect_uri: https required for non-localhost")
	ErrRedirectHostMismatch  = errors.New("invalid redirect_uri: host mismatch")
)

// CheckRedirectURI decides where an authorization response may go, and
// returns that address: requested itself, or the one address the client
// registered when it named none (RFC 6749 §3.1.2.3).
//
// A client that registered redirect_uris — at /oauth2/register, or in its
// metadata document — is held to them exactly, except that a loopback URI
// ignores its port (RFC 8252 §7.3). One that registered none may use this
// server's own origin, a loopback address, or a known native app's scheme.
// Either way the address must be one the consent page can show truthfully.
func CheckRedirectURI(requested string, registered []string, baseURL string) (string, error) {
	if requested == "" {
		if len(registered) != 1 {
			return "", ErrRedirectMissing
		}
		requested = registered[0]
	}

	u, err := url.Parse(requested)
	if err != nil {
		return "", ErrRedirectMalformed
	}
	// RFC 6749 §3.1.2: the redirection endpoint MUST NOT include a fragment.
	if strings.Contains(requested, "#") {
		return "", ErrRedirectFragment
	}
	if u.User != nil {
		return "", ErrRedirectUserinfo
	}
	if isWeb(u) {
		if _, ok := displayHost(u); !ok {
			return "", ErrRedirectHost
		}
	}

	if len(registered) > 0 {
		if !auth.AnyRedirectURIMatches(registered, requested) {
			return "", ErrRedirectNotRegistered
		}
		return requested, nil
	}

	if !u.IsAbs() {
		if strings.HasPrefix(requested, "/") && !strings.HasPrefix(requested, "//") && !strings.HasPrefix(requested, "/\\") {
			return requested, nil
		}
		return "", ErrRedirectRelative
	}

	if !isWeb(u) {
		// A private-use scheme has no origin to check, so an unregistered
		// one is accepted only for a known native client. Accepting any would
		// let a caller name an unknown client_id and have the code delivered
		// to a scheme of their choosing.
		if !auth.IsAllowedNativeRedirectScheme(u.Scheme) {
			return "", ErrRedirectScheme
		}
		return requested, nil
	}

	if isLoopback(u) {
		return requested, nil
	}
	if u.Scheme != "https" {
		return "", ErrRedirectInsecure
	}
	base, err := url.Parse(baseURL)
	if err != nil || u.Host != base.Host {
		return "", ErrRedirectHostMismatch
	}
	return requested, nil
}

// isValidRedirect is CheckRedirectURI as a yes or no, for the moment the
// code is sent: Serve re-checks the address itself rather than trust that
// every caller did.
func isValidRedirect(redirectURI string, req Request) bool {
	checked, err := CheckRedirectURI(redirectURI, req.RegisteredRedirectURIs, req.BaseURL)
	return err == nil && checked == redirectURI
}

// isWeb is true for an http or https address, the only kind with a host
// worth showing; a native app's custom scheme has none.
func isWeb(u *url.URL) bool {
	return u.Scheme == "http" || u.Scheme == "https"
}

func isLoopback(u *url.URL) bool {
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

// displayHost is the host as the consent page shows it: international names
// in their ASCII (xn--) form, so a lookalike such as a Cyrillic "о" in
// "gооgle.com" cannot pass for the real one. ok is false for a host that is
// not a valid hostname or IP address.
func displayHost(u *url.URL) (host string, ok bool) {
	host = u.Hostname()
	if net.ParseIP(host) == nil {
		ascii, err := idna.Lookup.ToASCII(host)
		if err != nil || ascii == "" || len(ascii) > 253 {
			return "", false
		}
		host = ascii
	}
	if port := u.Port(); port != "" {
		host = net.JoinHostPort(host, port)
	}
	return host, true
}
