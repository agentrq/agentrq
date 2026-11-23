// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func sessionTokens() *tokenService {
	return NewTokenService(TokenConfig{APISecret: "api-secret", MCPSecret: "mcp-secret"}).(*tokenService)
}

// An MCP session id reads back as what it recorded, for the user and
// workspace it was minted for, until it expires.
func TestMCPSessionToken_RoundTrips(t *testing.T) {
	svc := sessionTokens()
	before := time.Now()
	tok, err := svc.CreateMCPSessionToken("user-1", "ws-1", MCPSession{ProtocolVersion: "2025-06-18", ClientName: "claude-code", ClientVersion: "2.1.0"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(tok, ".") != 2 {
		t.Fatalf("session id %q is not a JWT", tok)
	}

	got, err := svc.ValidateMCPSessionToken(tok, "user-1", "ws-1")
	if err != nil {
		t.Fatalf("its own session id was refused: %v", err)
	}
	if got.ProtocolVersion != "2025-06-18" || got.ClientName != "claude-code" || got.ClientVersion != "2.1.0" {
		t.Errorf("read back %+v, want what was recorded", got)
	}
	if want := before.Add(MCPSessionTTL); got.ExpiresAt.Before(want.Add(-time.Second)) || got.ExpiresAt.After(want.Add(time.Minute)) {
		t.Errorf("expires %v, want about %v", got.ExpiresAt, want)
	}
}

// Two sessions opened by the same client are told apart.
func TestMCPSessionToken_EverySessionIsItsOwn(t *testing.T) {
	svc := sessionTokens()
	a, _ := svc.CreateMCPSessionToken("user-1", "ws-1", MCPSession{})
	b, _ := svc.CreateMCPSessionToken("user-1", "ws-1", MCPSession{})
	if a == b {
		t.Fatal("two sessions got the same id")
	}
}

// A session id is honoured only for the user and workspace it was minted
// for: it cannot be carried into another workspace or account.
func TestMCPSessionToken_BoundToUserAndWorkspace(t *testing.T) {
	svc := sessionTokens()
	tok, _ := svc.CreateMCPSessionToken("user-1", "ws-1", MCPSession{})
	if _, err := svc.ValidateMCPSessionToken(tok, "user-2", "ws-1"); err == nil {
		t.Error("another user's session id was accepted")
	}
	if _, err := svc.ValidateMCPSessionToken(tok, "user-1", "ws-2"); err == nil {
		t.Error("another workspace's session id was accepted")
	}
}

// Session ids and tokens are signed with different keys: a session id is no
// credential anywhere, and no token passes as a session id.
func TestMCPSessionToken_NeverATokenNorTheReverse(t *testing.T) {
	svc := sessionTokens()
	session, _ := svc.CreateMCPSessionToken("user-1", "ws-1", MCPSession{})
	if _, err := svc.ValidateToken(session); err == nil {
		t.Error("a session id validated as an MCP token")
	}
	if _, err := svc.ValidateSessionToken(session); err == nil {
		t.Error("a session id validated as a person's session token")
	}

	access, _ := svc.CreateMCPToken("user-1", "ws-1", "access")
	if _, err := svc.ValidateMCPSessionToken(access, "user-1", "ws-1"); err == nil {
		t.Error("an access token validated as a session id")
	}
	cookie, _ := svc.CreateToken("user-1", "", "", "")
	if _, err := svc.ValidateMCPSessionToken(cookie, "user-1", "ws-1"); err == nil {
		t.Error("a person's session token validated as a session id")
	}

	other := NewTokenService(TokenConfig{APISecret: "api-secret", MCPSecret: "another-secret"})
	if _, err := other.ValidateMCPSessionToken(session, "user-1", "ws-1"); err == nil {
		t.Error("a session id verified under another MCP secret")
	}
}

// An id that has expired, never expires, or is not signed as one is
// refused.
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
	valid := jwt.RegisteredClaims{Subject: "user-1", Audience: jwt.ClaimStrings{"ws-1"}, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}

	expired := valid
	expired.ExpiresAt = jwt.NewNumericDate(time.Now().Add(-time.Minute))
	forever := valid
	forever.ExpiresAt = nil

	for name, tok := range map[string]string{
		"expired":        sign(mcpSessionClaims{RegisteredClaims: expired}, jwt.SigningMethodHS256),
		"without expiry": sign(mcpSessionClaims{RegisteredClaims: forever}, jwt.SigningMethodHS256),
		"HS512":          sign(mcpSessionClaims{RegisteredClaims: valid}, jwt.SigningMethodHS512),
		"unsigned":       unsignedToken(t, mcpSessionClaims{RegisteredClaims: valid}),
		"not a JWT":      "not-a-session",
	} {
		if _, err := svc.ValidateMCPSessionToken(tok, "user-1", "ws-1"); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	if _, err := svc.ValidateMCPSessionToken(sign(mcpSessionClaims{RegisteredClaims: valid}, jwt.SigningMethodHS256), "user-1", "ws-1"); err != nil {
		t.Errorf("the control id was refused: %v", err)
	}
}
