package clientip

import (
	"net/http"
	"testing"
)

func TestExtract_IgnoresForwardedHeadersFromUntrustedPeer(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "203.0.113.10:12345",
		Header: http.Header{
			"X-Forwarded-For": []string{"198.51.100.99"},
		},
	}

	if got := Extract(r); got != "203.0.113.10" {
		t.Fatalf("expected direct peer IP, got %q", got)
	}
}

func TestExtract_UsesForwardedHeadersFromTrustedPeer(t *testing.T) {
	r := &http.Request{
		RemoteAddr: "127.0.0.1:12345",
		Header: http.Header{
			"X-Forwarded-For": []string{"198.51.100.99, 10.0.0.2"},
		},
	}

	if got := Extract(r); got != "198.51.100.99" {
		t.Fatalf("expected forwarded client IP, got %q", got)
	}
}
