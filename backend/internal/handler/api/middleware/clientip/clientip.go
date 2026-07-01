package clientip

import (
	"net"
	"net/http"
	"strings"
)

// Extract returns the client IP for rate limiting. Forwarded headers are only
// trusted when the direct peer is a local/private reverse proxy.
func Extract(r *http.Request) string {
	remote := remoteIP(r.RemoteAddr)
	if !isTrustedProxyPeer(remote) {
		return remote
	}

	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		for _, part := range strings.Split(xff, ",") {
			ip := strings.TrimSpace(part)
			if net.ParseIP(ip) != nil {
				return ip
			}
		}
	}
	if xri := strings.TrimSpace(r.Header.Get("X-Real-IP")); xri != "" && net.ParseIP(xri) != nil {
		return xri
	}
	return remote
}

func remoteIP(remoteAddr string) string {
	ip, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return ip
}

func isTrustedProxyPeer(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate()
}
