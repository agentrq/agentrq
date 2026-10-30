// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestContextHasAudience(t *testing.T) {
	var nilCtx context.Context
	if ContextHasAudience(nilCtx, "ws") {
		t.Error("a nil context reported an audience")
	}
	if ContextHasAudience(context.Background(), "ws") {
		t.Error("a context without MCP claims reported an audience")
	}
	ctx := context.WithValue(context.Background(), CtxKeyMCPClaims, &Claims{
		RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"ws-1", "agent"}},
	})
	if !ContextHasAudience(ctx, "agent") {
		t.Error("the claims' audience was not found")
	}
	if ContextHasAudience(ctx, "human") {
		t.Error("an audience the claims do not carry was reported")
	}
	if HasAudience(nil, "agent") {
		t.Error("nil claims reported an audience")
	}
}

// unsignedToken mints a token with alg "none", which every validator must
// refuse before it looks at the claims.
func unsignedToken(t *testing.T, claims jwt.Claims) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

func TestValidators_RefuseUnsignedTokens(t *testing.T) {
	svc := NewTokenService(TokenConfig{JWTSecret: "test-secret"})

	t.Run("session", func(t *testing.T) {
		tok := unsignedToken(t, Claims{RegisteredClaims: jwt.RegisteredClaims{Subject: "u1"}})
		if _, err := svc.ValidateToken(tok); err == nil {
			t.Fatal("an unsigned token was accepted")
		}
	})

	t.Run("oauth state", func(t *testing.T) {
		tok := unsignedToken(t, StateClaims{
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{"github"}},
			RedirectURL:      "/",
		})
		if _, err := svc.ValidateOAuthStateToken(tok, "github"); err == nil {
			t.Fatal("an unsigned state token was accepted")
		}
	})

	t.Run("client registration", func(t *testing.T) {
		tok := unsignedToken(t, ClientRegistrationClaims{
			RegisteredClaims: jwt.RegisteredClaims{Audience: jwt.ClaimStrings{DynamicClientAudience}},
			RedirectURIs:     []string{"https://evil.example.com/cb"},
		})
		if _, err := svc.ValidateClientRegistrationToken(tok); err == nil {
			t.Fatal("an unsigned client registration was accepted")
		}
	})
}

func TestValidateClientRegistrationToken_RefusesOtherTokens(t *testing.T) {
	svc := NewTokenService(TokenConfig{JWTSecret: "test-secret"})

	// A session token is signed with the same secret but is not a client_id.
	session, err := svc.CreateToken("u1", "u@example.com", "U", "")
	if err != nil {
		t.Fatal(err)
	}
	for name, tok := range map[string]string{
		"session token": session,
		"garbage":       "not-a-jwt",
	} {
		t.Run(name, func(t *testing.T) {
			if claims, err := svc.ValidateClientRegistrationToken(tok); err == nil {
				t.Fatalf("accepted %s as a client registration: %+v", name, claims)
			}
		})
	}
}
