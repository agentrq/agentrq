// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/mustafaturan/monoflake"
)

// A workspace's MCP server must take only an access token minted for that
// workspace: every token shares one signing secret, so the audience is all
// that keeps one workspace's agent out of another's.
func TestIdentifyUser_OnlyThisWorkspacesAccessToken(t *testing.T) {
	const secret = "test-secret"
	tokens := auth.NewTokenService(auth.TokenConfig{JWTSecret: secret})
	h := &handler{tokenSvc: tokens}
	const ws, other int64 = 500, 501
	wsID := monoflake.ID(ws).String()

	mint := func(token string, err error) string {
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		return token
	}

	if got := h.identifyUser(context.Background(), ws, mint(tokens.CreateMCPToken("user-1", wsID, "access"))); got != "user-1" {
		t.Errorf("this workspace's access token: got %q, want user-1", got)
	}

	noAudience, err := jwt.NewWithClaims(jwt.SigningMethodHS256, auth.Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   "user-1",
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
	}}).SignedString([]byte(secret))
	refused := map[string]string{
		"another workspace's access token": mint(tokens.CreateMCPToken("user-1", monoflake.ID(other).String(), "access")),
		"this workspace's refresh token":   mint(tokens.CreateMCPToken("user-1", wsID, "refresh")),
		"an authorization code":            mint(tokens.CreateOAuthCodeToken("user-1", wsID)),
		"a supervisor access token":        mint(tokens.CreateMCPToken("user-1", "coremcp", "access")),
		"a person's session":               mint(tokens.CreateToken("user-1", "a@b.com", "A", "")),
		"a token with no audience":         mint(noAudience, err),
		"nonsense":                         "not.a.token",
		"nothing":                          "",
	}
	for name, token := range refused {
		if got := h.identifyUser(context.Background(), ws, token); got != "" {
			t.Errorf("%s was accepted as %q", name, got)
		}
	}
}
