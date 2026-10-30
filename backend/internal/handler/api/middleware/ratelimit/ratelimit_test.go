// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package ratelimit

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/handler/api/middleware/clientip"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
)

// tokens accepts "tok-<user>" and nothing else; the middleware calls only
// ValidateToken.
type tokens struct{ auth.TokenService }

func (tokens) ValidateToken(s string) (*auth.Claims, error) {
	if len(s) > 4 && s[:4] == "tok-" {
		return &auth.Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: s[4:]}}, nil
	}
	return nil, errors.New("invalid")
}

func newHandler(perIP, perUser int) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })
	return New(true, perIP, perUser, time.Minute, tokens{})(ok)
}

func serve(h http.Handler, remote string, headers map[string]string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func TestLimitsAnAnonymousPeer(t *testing.T) {
	h := newHandler(2, 100)
	for i := 0; i < 2; i++ {
		if w := serve(h, "203.0.113.7:4000", nil); w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d", i+1, w.Code)
		}
	}
	if w := serve(h, "203.0.113.7:4000", nil); w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request: got %d, want 429", w.Code)
	}
}

func TestLimitsAUserAcrossAddresses(t *testing.T) {
	h := newHandler(100, 2)
	bearer := map[string]string{"Authorization": "Bearer tok-alice"}
	for i := 0; i < 2; i++ {
		if w := serve(h, fmt.Sprintf("203.0.113.%d:4000", i), bearer); w.Code != http.StatusOK {
			t.Fatalf("request %d: got %d", i+1, w.Code)
		}
	}
	if w := serve(h, "203.0.113.99:4000", bearer); w.Code != http.StatusTooManyRequests {
		t.Fatalf("third request as alice: got %d, want 429", w.Code)
	}
}

func TestRemainingHeaderCountsDown(t *testing.T) {
	h := newHandler(5, 100)
	for want := 4; want >= 0; want-- {
		w := serve(h, "203.0.113.7:4000", nil)
		if got := w.Header().Get("X-RateLimit-Remaining"); got != fmt.Sprint(want) {
			t.Fatalf("X-RateLimit-Remaining = %q, want %d", got, want)
		}
	}
}

// An anonymous client writes X-Forwarded-For itself; a new value per request
// must not reset its quota.
func TestSpoofedForwardedForDoesNotEscapeTheIPLimit(t *testing.T) {
	h := newHandler(2, 100)
	limited := 0
	for i := 0; i < 20; i++ {
		xff := map[string]string{"X-Forwarded-For": fmt.Sprintf("198.51.100.%d", i)}
		if serve(h, "203.0.113.7:4000", xff).Code == http.StatusTooManyRequests {
			limited++
		}
	}
	if limited == 0 {
		t.Fatal("20 anonymous requests from one public peer, each with a new X-Forwarded-For: none was limited")
	}
}

func TestSpoofedForwardedForCannotExhaustSomebodyElsesQuota(t *testing.T) {
	h := newHandler(2, 100)
	victim := "192.0.2.10"
	for i := 0; i < 10; i++ {
		serve(h, "203.0.113.7:4000", map[string]string{"X-Forwarded-For": victim})
	}
	if w := serve(h, victim+":5000", nil); w.Code != http.StatusOK {
		t.Fatalf("victim's first request after an attacker named it: got %d, want 200", w.Code)
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
	h := clients.Middleware(newHandler(2, 100))
	for i := 0; i < 20; i++ {
		xri := map[string]string{"X-Real-IP": fmt.Sprintf("198.51.100.%d", i)}
		if w := serve(h, "10.0.0.5:4000", xri); w.Code != http.StatusOK {
			t.Fatalf("client %d behind the proxy: got %d, want 200", i, w.Code)
		}
	}
}
