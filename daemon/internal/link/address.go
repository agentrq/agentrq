// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package link is the daemon's connection to the backend.
//
// Everything in it exists because a machine daemon is a long-lived process on
// somebody else's computer: it reconnects rather than exits, it never blocks
// the terminal on the network, and it says who it is on every attempt.
package link

import (
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Path is the endpoint a daemon connects to.
const Path = "/api/v1/daemon/connect"

// SocketURL turns a profile's server URL into the socket to dial.
//
// The scheme is mapped rather than assumed: a profile enrolled against plain
// HTTP with --insecure must dial ws, and one enrolled against HTTPS must dial
// wss. Guessing either way would turn a deliberate choice into a silent
// downgrade or an unexplained failure.
func SocketURL(serverURL string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return "", fmt.Errorf("link: unusable server url: %w", err)
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	case "http":
		u.Scheme = "ws"
	default:
		return "", fmt.Errorf("link: server url must be http or https, got %q", u.Scheme)
	}
	if u.Host == "" {
		return "", fmt.Errorf("link: server url has no host")
	}
	// Whatever path the server is mounted under is kept, so a backend behind a
	// prefix works without a second setting to get wrong.
	u.Path = strings.TrimSuffix(u.Path, "/") + Path
	u.RawQuery = ""
	u.Fragment = ""
	return u.String(), nil
}

// Identity is who the daemon says it is.
//
// The token decides; the ids are routing and correlation, and the backend
// refuses a connection whose headers contradict its token.
type Identity struct {
	MachineID string
	UserID    string
	Token     string
	Version   string
}

// Headers are sent on the upgrade request.
//
// The token is a header rather than a query parameter on purpose: a URL ends
// up in access logs, in proxy logs and in error reports, and a credential in
// any of those is a credential that has leaked.
func (id Identity) Headers() http.Header {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+id.Token)
	if id.MachineID != "" {
		h.Set("X-AgentRQ-Machine-Id", id.MachineID)
	}
	if id.UserID != "" {
		h.Set("X-AgentRQ-User-Id", id.UserID)
	}
	h.Set("User-Agent", "agentrqd/"+id.Version)
	return h
}

// Backoff bounds how hard a daemon retries.
//
// Jittered so a fleet of machines that all lost the same backend does not
// return as a synchronised thundering herd the moment it comes back, and
// capped so a machine left running overnight reconnects promptly rather than
// an hour later.
const (
	BackoffBase = time.Second
	BackoffMax  = 60 * time.Second
)

// Backoff is the delay before attempt n, counting from 1.
func Backoff(attempt int, random func() float64) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	base := BackoffBase
	for i := 1; i < attempt && base < BackoffMax; i++ {
		base *= 2
	}
	if base > BackoffMax {
		base = BackoffMax
	}
	// Half the window is fixed and half is jitter, so retries spread out
	// without any of them becoming pointlessly eager.
	return base/2 + time.Duration(float64(base/2)*random())
}
