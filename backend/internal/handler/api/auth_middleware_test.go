// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v2"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
)

// The `at` cookie is only a header the client sends, so the middleware must
// refuse every token that is not a signed-in person's session. An agent's
// workspace MCP token used to pass here, and opened the owner's whole account.
func TestAuthMiddleware_AcceptsOnlyASession(t *testing.T) {
	tokens := auth.NewTokenService(auth.TokenConfig{JWTSecret: "test-secret"})
	h := &handler{tokenSvc: tokens}
	app := fiber.New()
	app.Use(h.authMiddleware())
	app.Get("/auth/user", h.getAuthenticatedUser())

	get := func(t *testing.T, cookie string) int {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/auth/user", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: "at", Value: cookie})
		}
		res, err := app.Test(req)
		if err != nil {
			t.Fatal(err)
		}
		return res.StatusCode
	}

	t.Run("a session is let in", func(t *testing.T) {
		session, _ := tokens.CreateToken("user-1", "a@b.com", "A", "")
		if got := get(t, session); got != http.StatusOK {
			t.Errorf("status = %d, want 200", got)
		}
	})

	t.Run("no cookie is refused", func(t *testing.T) {
		if got := get(t, ""); got != http.StatusUnauthorized {
			t.Errorf("status = %d, want 401", got)
		}
	})

	others := map[string]func() (string, error){
		"workspace MCP token":  func() (string, error) { return tokens.CreateMCPToken("user-1", "ws1", "access") },
		"supervisor MCP token": func() (string, error) { return tokens.CreateMCPToken("user-1", "coremcp", "access") },
		"refresh token":        func() (string, error) { return tokens.CreateRefreshToken("user-1") },
		"terminal ticket":      func() (string, error) { return tokens.CreateTerminalTicket("user-1", "42") },
		"browser ticket":       func() (string, error) { return tokens.CreateBrowserTicket("user-1") },
	}
	for name, mint := range others {
		t.Run("a "+name+" is refused", func(t *testing.T) {
			token, err := mint()
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			if got := get(t, token); got != http.StatusUnauthorized {
				t.Errorf("status = %d, want 401", got)
			}
		})
	}
}
