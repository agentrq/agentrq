// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package auth

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func sessionTokens() *tokenService {
	return NewTokenService(TokenConfig{APISecret: "api-secret", MCPSecret: "mcp-secret"}).(*tokenService)
}

// An MCP session id reads back as what it recorded, for the user and
// workspace it was minted for, and it never expires.
func TestMCPSessionToken_RoundTrips(t *testing.T) {
	svc := sessionTokens()
	tok, err := svc.CreateMCPSessionToken(context.Background(), "user-1", "ws-1", MCPSession{ProtocolVersion: "2025-06-18", ClientName: "claude-code", ClientVersion: "2.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("session id %q is not a JWT", tok)
	}

	got, err := svc.ValidateMCPSessionToken(context.Background(), tok, "user-1", "ws-1")
	if err != nil {
		t.Fatalf("its own session id was refused: %v", err)
	}
	if got.ProtocolVersion != "2025-06-18" || got.ClientName != "claude-code" || got.ClientVersion != "2.1.0" {
		t.Errorf("read back %+v, want what was recorded", got)
	}
	claims := &mcpSessionClaims{}
	if _, _, err := jwt.NewParser().ParseUnverified(tok, claims); err != nil || claims.ExpiresAt != nil || claims.IssuedAt == nil {
		t.Errorf("minted exp %v, iat %v (%v); want no expiry and an issue time", claims.ExpiresAt, claims.IssuedAt, err)
	}
}

// Two sessions opened by the same client are told apart.
func TestMCPSessionToken_EverySessionIsItsOwn(t *testing.T) {
	svc := sessionTokens()
	a, _ := svc.CreateMCPSessionToken(context.Background(), "user-1", "ws-1", MCPSession{})
	b, _ := svc.CreateMCPSessionToken(context.Background(), "user-1", "ws-1", MCPSession{})
	if a == b {
		t.Fatal("two sessions got the same id")
	}
}

// A session id is honoured only for the user and workspace it was minted
// for: it cannot be carried into another workspace or account.
func TestMCPSessionToken_BoundToUserAndWorkspace(t *testing.T) {
	svc := sessionTokens()
	tok, _ := svc.CreateMCPSessionToken(context.Background(), "user-1", "ws-1", MCPSession{})
	if _, err := svc.ValidateMCPSessionToken(context.Background(), tok, "user-2", "ws-1"); err == nil {
		t.Error("another user's session id was accepted")
	}
	if _, err := svc.ValidateMCPSessionToken(context.Background(), tok, "user-1", "ws-2"); err == nil {
		t.Error("another workspace's session id was accepted")
	}
}

// Session ids and tokens are signed with different keys: a session id is no
// credential anywhere, and no token passes as a session id.
func TestMCPSessionToken_NeverATokenNorTheReverse(t *testing.T) {
	svc := sessionTokens()
	session, _ := svc.CreateMCPSessionToken(context.Background(), "user-1", "ws-1", MCPSession{})
	if _, err := svc.ValidateToken(session); err == nil {
		t.Error("a session id validated as an MCP token")
	}
	if _, err := svc.ValidateSessionToken(session); err == nil {
		t.Error("a session id validated as a person's session token")
	}

	access, _ := svc.CreateMCPToken("user-1", "ws-1", "access")
	if _, err := svc.ValidateMCPSessionToken(context.Background(), access, "user-1", "ws-1"); err == nil {
		t.Error("an access token validated as a session id")
	}
	cookie, _ := svc.CreateToken("user-1", "", "", "")
	if _, err := svc.ValidateMCPSessionToken(context.Background(), cookie, "user-1", "ws-1"); err == nil {
		t.Error("a person's session token validated as a session id")
	}

	other := NewTokenService(TokenConfig{APISecret: "api-secret", MCPSecret: "another-secret"})
	if _, err := other.ValidateMCPSessionToken(context.Background(), session, "user-1", "ws-1"); err == nil {
		t.Error("a session id verified under another MCP secret")
	}
}

// An id that has expired, minted when ids still expired, or not signed as
// one is refused; one that never expires is a session id.
func TestMCPSessionToken_RefusesBadIDs(t *testing.T) {
	svc := sessionTokens()
	sign := func(claims mcpSessionClaims, method jwt.SigningMethod) string {
		t.Helper()
		tok, err := jwt.NewWithClaims(method, claims).SignedString(svc.sessionKey)
		if err != nil {
			t.Fatal(err)
		}
		return tok
	}
	valid := jwt.RegisteredClaims{Subject: "user-1", Audience: jwt.ClaimStrings{"ws-1"}}

	expired := valid
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	notYet := valid
	notYet.ExpiresAt = jwt.NewNumericDate(time.Now().Add(time.Hour))

	for name, tok := range map[string]string{
		"expired":   sign(mcpSessionClaims{RegisteredClaims: expired}, jwt.SigningMethodHS256),
		"HS512":     sign(mcpSessionClaims{RegisteredClaims: valid}, jwt.SigningMethodHS512),
		"unsigned":  unsignedToken(t, mcpSessionClaims{RegisteredClaims: valid}),
		"not a JWT": "not-a-session",
	} {
		if _, err := svc.ValidateMCPSessionToken(context.Background(), tok, "user-1", "ws-1"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	for name, claims := range map[string]jwt.RegisteredClaims{"without expiry": valid, "not yet expired": notYet} {
		if _, err := svc.ValidateMCPSessionToken(context.Background(), sign(mcpSessionClaims{RegisteredClaims: claims}, jwt.SigningMethodHS256), "user-1", "ws-1"); err != nil {
			t.Errorf("%s: refused: %v", name, err)
		}
	}
}
