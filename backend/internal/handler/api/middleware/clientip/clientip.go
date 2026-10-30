// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Package clientip names the client a request came from, for every throttle
// that counts by address.
package clientip

import (
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"

	zlog "github.com/rs/zerolog/log"
)

// _forwardedFor is read right to left, unlike the single-address headers.
const _forwardedFor = "X-Forwarded-For"

// Resolver believes a client-address header only from the proxies the
// operator lists. Guessing them instead, as "any private peer", let anybody
// spoof the header wherever the network hides the client behind a private
// address, such as Docker's gateway.
type Resolver struct {
	trusted []*net.IPNet
	header  string
	warn    sync.Once
}

// New takes the trusted proxies as a comma-separated list of CIDRs or single
// addresses; empty trusts no one, so no header is ever read.
func New(trustedProxies, header string) (*Resolver, error) {
	r := &Resolver{header: http.CanonicalHeaderKey(strings.TrimSpace(header))}
	if r.header == "" {
		r.header = "X-Real-Ip"
	}
	for _, s := range strings.Split(trustedProxies, ",") {
		if s = strings.TrimSpace(s); s == "" {
			continue
		}
		if !strings.Contains(s, "/") {
			ip := net.ParseIP(s)
			if ip == nil {
				return nil, fmt.Errorf("trusted proxy %q is neither an address nor a CIDR", s)
			}
			bits := 128
			if ip.To4() != nil {
				bits = 32
			}
			s = fmt.Sprintf("%s/%d", s, bits)
		}
		_, n, err := net.ParseCIDR(s)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q is neither an address nor a CIDR", s)
		}
		r.trusted = append(r.trusted, n)
	}
	return r, nil
}

func (r *Resolver) isTrusted(ip net.IP) bool {
	for _, n := range r.trusted {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// Client names the client behind peer. A header that is missing or not an
// address leaves the peer itself.
func (r *Resolver) Client(peer net.IP, h http.Header) net.IP {
	if !r.isTrusted(peer) {
		if len(r.trusted) == 0 && (peer.IsLoopback() || peer.IsPrivate()) &&
			(h.Get(r.header) != "" || h.Get(_forwardedFor) != "") {
			r.warn.Do(func() {
				zlog.Warn().Str("peer", peer.String()).Msg("[middleware/clientip] a proxy names its clients, but no proxy is trusted: every client behind it shares one limit; set AGENTRQ_TRUSTED_PROXIES")
			})
		}
		return peer
	}
	if r.header == _forwardedFor {
		// The client writes the leftmost entries; each trusted proxy appends
		// the address it heard from. The first untrusted one from the right
		// is the client.
		entries := strings.Split(strings.Join(h.Values(_forwardedFor), ","), ",")
		client := peer
		for i := len(entries) - 1; i >= 0; i-- {
			ip := net.ParseIP(strings.TrimSpace(entries[i]))
			if ip == nil {
				return client
			}
			client = ip
			if !r.isTrusted(ip) {
				break
			}
		}
		return client
	}
	if ip := net.ParseIP(strings.TrimSpace(h.Get(r.header))); ip != nil {
		return ip
	}
	return peer
}

// Middleware puts the client in RemoteAddr, so everything after it, the
// throttles included, counts the client and never reads a header itself.
func (r *Resolver) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		host, port, err := net.SplitHostPort(req.RemoteAddr)
		if err != nil {
			host, port = req.RemoteAddr, "0"
		}
		if peer := net.ParseIP(host); peer != nil {
			req.RemoteAddr = net.JoinHostPort(r.Client(peer, req.Header).String(), port)
		}
		next.ServeHTTP(w, req)
	})
}

// FromRequest names the client a request came from: its peer, as the
// Middleware left it.
func FromRequest(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
