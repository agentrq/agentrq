package api

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/security"
	"github.com/gofiber/fiber/v2"
)

func TestGoogleOAuth_CSRF(t *testing.T) {
	app := fiber.New()
	tokenKey := "test-token-key-32-bytes-long-123"
	h := &handler{
		tokenKey:   tokenKey,
		sslEnabled: false,
		baseURL:    "http://localhost:3000",
		auth:       &mockAuthService{},
	}

	app.Get("/google/login", h.googleLogin())
	app.Get("/google/callback", h.googleCallback())

	t.Run("Login sets cookie and signed state", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/google/login?redirect_url=/dashboard", nil)
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusFound {
			t.Errorf("Expected status 302, got %d", resp.StatusCode)
		}

		// Check cookie
		cookieHeader := resp.Header.Get("Set-Cookie")
		if !strings.Contains(cookieHeader, "oauth_state=") {
			t.Errorf("Expected oauth_state cookie, got %s", cookieHeader)
		}

		// Check redirect URL for state
		loc := resp.Header.Get("Location")
		if !strings.Contains(loc, "state=") {
			t.Errorf("Expected state parameter in redirect, got %s", loc)
		}
	})

	t.Run("Callback rejects invalid signature", func(t *testing.T) {
		req := httptest.NewRequest("GET", "/google/callback?code=123&state=bad.sig", nil)
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("Expected status 400, got %d", resp.StatusCode)
		}
	})

	t.Run("Callback rejects missing nonce cookie", func(t *testing.T) {
		stateData := "/dashboard:nonce123"
		sig := security.Sign(stateData, tokenKey)
		state := fmt.Sprintf("%s.%s", stateData, sig)

		req := httptest.NewRequest("GET", "/google/callback?code=123&state="+state, nil)
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})

	t.Run("Callback rejects nonce mismatch", func(t *testing.T) {
		stateData := "/dashboard:nonce123"
		sig := security.Sign(stateData, tokenKey)
		state := fmt.Sprintf("%s.%s", stateData, sig)

		req := httptest.NewRequest("GET", "/google/callback?code=123&state="+state, nil)
		req.Header.Set("Cookie", "oauth_state=different-nonce")
		resp, _ := app.Test(req)

		if resp.StatusCode != http.StatusForbidden {
			t.Errorf("Expected status 403, got %d", resp.StatusCode)
		}
	})
}
