package ratelimit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
)

type mockTokenSvc struct {
	auth.TokenService
	validateTokenFunc func(tokenStr string) (*auth.Claims, error)
}

func (m *mockTokenSvc) ValidateToken(tokenStr string) (*auth.Claims, error) {
	return m.validateTokenFunc(tokenStr)
}

func TestRatelimitMiddleware(t *testing.T) {
	// 1. Set up a simple mock token service
	tokenSvc := &mockTokenSvc{}

	// Create a dummy next handler
	nextHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	t.Run("Disabled rate limiter", func(t *testing.T) {
		limiter := New(false, 1, 1, 1*time.Second, tokenSvc)
		handler := limiter(nextHandler)

		req := httptest.NewRequest("GET", "/", nil)
		rec := httptest.NewRecorder()

		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec.Code)
		}
	})

	t.Run("IP-based rate limit blocks over limit", func(t *testing.T) {
		// Enabled, Max IP = 1, Max User = 5, Window = 1s
		limiter := New(true, 1, 5, 1*time.Second, tokenSvc)
		handler := limiter(nextHandler)

		// 1st request from same IP
		req1 := httptest.NewRequest("GET", "/", nil)
		req1.RemoteAddr = "1.2.3.4:1234"
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Errorf("expected 1st request 200, got %d", rec1.Code)
		}

		// 2nd request from same IP (blocked)
		req2 := httptest.NewRequest("GET", "/", nil)
		req2.RemoteAddr = "1.2.3.4:1234"
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusTooManyRequests {
			t.Errorf("expected 2nd request 429, got %d", rec2.Code)
		}
	})

	t.Run("User-based rate limiting scopes correctly with actor:human audience", func(t *testing.T) {
		// Enabled, Max IP = 10, Max User = 1, Window = 1s
		limiter := New(true, 10, 1, 1*time.Second, tokenSvc)
		handler := limiter(nextHandler)

		// Set mock token validation to return a valid human token
		tokenSvc.validateTokenFunc = func(tokenStr string) (*auth.Claims, error) {
			if tokenStr == "human-token" {
				return &auth.Claims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:  "user-123",
						Audience: jwt.ClaimStrings{auth.ActorHumanAudience},
					},
				}, nil
			}
			return nil, errors.New("invalid token")
		}

		// 1st request for User 123
		req1 := httptest.NewRequest("GET", "/?token=human-token", nil)
		req1.RemoteAddr = "1.1.1.1:1234"
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec1.Code)
		}

		// 2nd request for User 123 (from a different IP) - should be blocked by User Limit (Max User = 1)
		req2 := httptest.NewRequest("GET", "/?token=human-token", nil)
		req2.RemoteAddr = "2.2.2.2:1234"
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusTooManyRequests {
			t.Errorf("expected 429 for second user request, got %d", rec2.Code)
		}
	})

	t.Run("User-based rate limiting is bypassed for non-human tokens", func(t *testing.T) {
		// Enabled, Max IP = 10, Max User = 1, Window = 1s
		limiter := New(true, 10, 1, 1*time.Second, tokenSvc)
		handler := limiter(nextHandler)

		// Set mock token validation to return an MCP/Agent token (has subject but no "actor:human" audience)
		tokenSvc.validateTokenFunc = func(tokenStr string) (*auth.Claims, error) {
			if tokenStr == "mcp-token" {
				return &auth.Claims{
					RegisteredClaims: jwt.RegisteredClaims{
						Subject:  "user-123",
						Audience: jwt.ClaimStrings{"workspace-abc", "some-mcp-scope"},
					},
				}, nil
			}
			return nil, errors.New("invalid token")
		}

		// 1st request for MCP token from IP 1.1.1.1
		req1 := httptest.NewRequest("GET", "/?token=mcp-token", nil)
		req1.RemoteAddr = "1.1.1.1:1234"
		rec1 := httptest.NewRecorder()
		handler.ServeHTTP(rec1, req1)
		if rec1.Code != http.StatusOK {
			t.Errorf("expected 200, got %d", rec1.Code)
		}

		// 2nd request for same MCP token but from IP 2.2.2.2.
		// If scoping works, it should NOT map to user:user-123 (which would block it since Max User = 1).
		// Instead, it is treated as a separate IP-based request and should succeed (since Max IP = 10).
		req2 := httptest.NewRequest("GET", "/?token=mcp-token", nil)
		req2.RemoteAddr = "2.2.2.2:1234"
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req2)
		if rec2.Code != http.StatusOK {
			t.Errorf("expected 200, got %d (might have incorrectly matched user limit)", rec2.Code)
		}
	})
}
