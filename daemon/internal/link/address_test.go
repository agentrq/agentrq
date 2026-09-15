// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package link

import (
	"strings"
	"testing"
	"time"
)

func TestSocketURL(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"https becomes wss", "https://agentrq.example", "wss://agentrq.example" + Path},
		// A profile enrolled with --insecure must dial ws. Upgrading it here
		// would turn a deliberate choice into an unexplained failure.
		{"http stays plain", "http://127.0.0.1:3099", "ws://127.0.0.1:3099" + Path},
		{"a path prefix is kept", "https://example.com/agentrq/", "wss://example.com/agentrq" + Path},
		{"a query is dropped", "https://example.com?a=1", "wss://example.com" + Path},
		{"surrounding space is ignored", "  https://example.com  ", "wss://example.com" + Path},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SocketURL(tc.in)
			if err != nil {
				t.Fatalf("SocketURL(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("SocketURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSocketURLRefusals(t *testing.T) {
	for _, in := range []string{"ftp://example.com", "example.com", "", "https://", "ht tp://x"} {
		if got, err := SocketURL(in); err == nil {
			t.Errorf("SocketURL(%q) = %q, want an error", in, got)
		}
	}
}

// The token is a header, never a query parameter: a URL ends up in access
// logs, proxy logs and error reports.
func TestHeadersCarryTheTokenAndTheClaim(t *testing.T) {
	h := Identity{MachineID: "m1", UserID: "u1", Token: "secret", Version: "1.2.3"}.Headers()

	if got := h.Get("Authorization"); got != "Bearer secret" {
		t.Errorf("Authorization = %q", got)
	}
	if got := h.Get("X-AgentRQ-Machine-Id"); got != "m1" {
		t.Errorf("machine header = %q", got)
	}
	if got := h.Get("X-AgentRQ-User-Id"); got != "u1" {
		t.Errorf("user header = %q", got)
	}
	if got := h.Get("User-Agent"); !strings.Contains(got, "1.2.3") {
		t.Errorf("User-Agent = %q, want the version in it", got)
	}
}

// A profile enrolled before the user id was recorded still connects; the
// backend treats an absent claim as no claim rather than as a contradiction.
func TestAnUnknownIdentityIsOmittedRatherThanSentEmpty(t *testing.T) {
	h := Identity{Token: "secret"}.Headers()
	if _, ok := h["X-Agentrq-User-Id"]; ok {
		t.Error("an empty user id was sent as a header")
	}
	if _, ok := h["X-Agentrq-Machine-Id"]; ok {
		t.Error("an empty machine id was sent as a header")
	}
}

func TestBackoffGrowsAndIsCapped(t *testing.T) {
	// The fixed half, with no jitter, is the easiest thing to reason about.
	zero := func() float64 { return 0 }
	if got := Backoff(1, zero); got != BackoffBase/2 {
		t.Errorf("first retry = %v", got)
	}
	if got := Backoff(2, zero); got != BackoffBase {
		t.Errorf("second retry = %v", got)
	}
	if got := Backoff(50, func() float64 { return 1 }); got != BackoffMax {
		t.Errorf("a long outage = %v, want the cap %v", got, BackoffMax)
	}
	// Counting from zero or below is the caller's slip, not a reason to
	// hammer the server.
	if got := Backoff(0, zero); got != BackoffBase/2 {
		t.Errorf("Backoff(0) = %v", got)
	}
}

// A fleet that all lost the same backend must not return as one herd.
func TestBackoffIsJittered(t *testing.T) {
	seen := map[time.Duration]bool{}
	for i := 0; i < 10; i++ {
		v := float64(i) / 10
		seen[Backoff(4, func() float64 { return v })] = true
	}
	if len(seen) < 5 {
		t.Errorf("jitter produced only %d distinct delays", len(seen))
	}
}
