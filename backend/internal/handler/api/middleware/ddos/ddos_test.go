// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package ddos

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/handler/api/middleware/clientip"
)

func serve(h http.Handler, remote string, headers map[string]string) int {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func newHandler(max int) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return New(true, max, time.Minute)(ok)
}

func TestBlocksAPeerOverTheRate(t *testing.T) {
	h := newHandler(3)
	for i := 0; i < 3; i++ {
		if code := serve(h, "203.0.113.7:4000", nil); code != http.StatusOK {
			t.Fatalf("request %d: got %d, want 200", i+1, code)
		}
	}
	if code := serve(h, "203.0.113.7:4000", nil); code != http.StatusTooManyRequests {
		t.Fatalf("request over the rate: got %d, want 429", code)
	}
	if code := serve(h, "203.0.113.7:4001", nil); code != http.StatusTooManyRequests {
		t.Fatalf("same peer, new port, while blocked: got %d, want 429", code)
	}
}

// A client on the open internet writes X-Forwarded-For itself. Varying it must
// not give each request a fresh bucket, or the limit never applies.
func TestSpoofedForwardedForDoesNotEscapeTheLimit(t *testing.T) {
	h := newHandler(3)
	blocked := 0
	for i := 0; i < 20; i++ {
		xff := map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i)}
		if serve(h, "203.0.113.7:4000", xff) == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("20 requests from one public peer in a second, each with a new X-Forwarded-For: none was blocked")
	}
}

func TestSpoofedRealIPDoesNotEscapeTheLimit(t *testing.T) {
	h := newHandler(3)
	blocked := 0
	for i := 0; i < 20; i++ {
		xri := map[string]string{"X-Real-IP": fmt.Sprintf("198.51.100.%d", i)}
		if serve(h, "203.0.113.7:4000", xri) == http.StatusTooManyRequests {
			blocked++
		}
	}
	if blocked == 0 {
		t.Fatal("20 requests from one public peer in a second, each with a new X-Real-IP: none was blocked")
	}
}

// Spoofing works the other way too: naming somebody else's address must not
// get that address blocked.
func TestSpoofedForwardedForCannotBlockSomebodyElse(t *testing.T) {
	h := newHandler(3)
	victim := "192.0.2.10"
	for i := 0; i < 10; i++ {
		serve(h, "203.0.113.7:4000", map[string]string{"X-Forwarded-For": victim})
	}
	if code := serve(h, victim+":5000", nil); code != http.StatusOK {
		t.Fatalf("victim's own first request after an attacker named it: got %d, want 200", code)
	}
}

// Behind the deployment's own proxy every request arrives from one peer; the
// X-Real-IP it sets, believed because the operator trusts it, keeps its
// clients apart.
func TestATrustedProxysClientsAreCountedApart(t *testing.T) {
	clients, err := clientip.New("10.0.0.5", "X-Real-IP")
	if err != nil {
		t.Fatal(err)
	}
	h := clients.Middleware(newHandler(3))
	for i := 0; i < 20; i++ {
		xri := map[string]string{"X-Real-IP": fmt.Sprintf("198.51.100.%d", i)}
		if code := serve(h, "10.0.0.5:4000", xri); code != http.StatusOK {
			t.Fatalf("client %d behind the proxy: got %d, want 200", i, code)
		}
	}
}
