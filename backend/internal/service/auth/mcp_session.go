// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// mcpSessionKeyLabel names the key MCP session ids are signed with.
const mcpSessionKeyLabel = "agentrq mcp session"

// mcpSessionClaims are an MCP session id's claims: who opened it, on which
// workspace, and how the client introduced itself. Nothing in it grants
// anything.
type mcpSessionClaims struct {
	jwt.RegisteredClaims
	ProtocolVersion string `json:"pv,omitempty"`
	ClientName      string `json:"cn,omitempty"`
	ClientVersion   string `json:"cv,omitempty"`
}

// deriveKey is the key for one use of secret: HMAC-SHA256 of the label.
func deriveKey(secret []byte, label string) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(label))
	return mac.Sum(nil)
}

// CreateMCPSessionToken mints the id of userID's MCP session with
// workspaceID.
func (s *tokenService) CreateMCPSessionToken(userID, workspaceID string, session MCPSession) (string, error) {
	now := time.Now()
	claims := mcpSessionClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			ID:        rand.Text(),
			Subject:   userID,
			Audience:  jwt.ClaimStrings{workspaceID},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(MCPSessionTTL)),
		},
		ProtocolVersion: session.ProtocolVersion,
		ClientName:      session.ClientName,
		ClientVersion:   session.ClientVersion,
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.sessionKey)
}

// ValidateMCPSessionToken reads back an id CreateMCPSessionToken minted for
// userID's session with workspaceID. Any other id, an expired one included,
// is an error.
func (s *tokenService) ValidateMCPSessionToken(tokenStr, userID, workspaceID string) (*MCPSession, error) {
	claims := &mcpSessionClaims{}
	_, err := jwt.ParseWithClaims(tokenStr, claims, func(*jwt.Token) (any, error) {
		return s.sessionKey, nil
	},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithSubject(userID),
		jwt.WithAudience(workspaceID),
	)
	if err != nil {
		return nil, err
	}
	return &MCPSession{
		ProtocolVersion: claims.ProtocolVersion,
		ClientName:      claims.ClientName,
		ClientVersion:   claims.ClientVersion,
		ExpiresAt:       claims.ExpiresAt.Time,
	}, nil
}
