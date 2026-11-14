// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/golang-jwt/jwt/v5"
)

// signSupervisorToken signs a supervisor token for user-1 that expires at exp,
// or never when exp is zero, with the key supervisorTokens verifies.
func signSupervisorToken(t *testing.T, tokenType string, exp time.Time) string {
	t.Helper()
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:  "user-1",
		Audience: jwt.ClaimStrings{resourceSupervisor, tokenType},
	}}
	if !exp.IsZero() {
		claims.ExpiresAt = jwt.NewNumericDate(exp)
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("supervisor-streamable-secret"))
	if err != nil {
		t.Fatalf("signing a %s token: %v", tokenType, err)
	}
	return tok
}

// expiresIn is how long from now a token supervisorTokens signed has left.
func expiresIn(t *testing.T, token string) time.Duration {
	t.Helper()
	claims, err := supervisorTokens.ValidateToken(token)
	if err != nil {
		t.Fatalf("token does not verify: %v", err)
	}
	return time.Until(claims.ExpiresAt.Time)
}

// within reports whether got is want, give or take the second a test takes.
func within(got, want time.Duration) bool {
	return got <= want && got > want-5*time.Second
}

// A refresh answers a new 8-hour access token and a new 7-day refresh token,
// so a client that keeps refreshing stays signed in, and expires_in says 8h.
func TestSupervisorToken_ARefreshIssuesAnEightHourAccessTokenAndASevenDayRefreshToken(t *testing.T) {
	h := &handler{tokenSvc: supervisorTokens}
	old := signSupervisorToken(t, "refresh", time.Now().Add(time.Hour))

	w := supervisorTokenRequest(h, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {old}}.Encode())
	if w.Code != http.StatusOK {
		t.Fatalf("refresh answered %d: %s", w.Code, w.Body.String())
	}
	var got struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int    `json:"expires_in"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding %q: %v", w.Body.String(), err)
	}
	if got.ExpiresIn != 28800 {
		t.Errorf("expires_in = %d, want 28800", got.ExpiresIn)
	}
	if left := expiresIn(t, got.AccessToken); !within(left, 8*time.Hour) {
		t.Errorf("the access token expires in %v, want 8h", left)
	}
	if got.RefreshToken == old {
		t.Error("the refresh answered the refresh token it was given, want a new one")
	}
	if left := expiresIn(t, got.RefreshToken); !within(left, 7*24*time.Hour) {
		t.Errorf("the refresh token expires in %v, want 7 days", left)
	}
}

// A refresh token signed before the lifetimes were shortened, or with no
// expiry at all, is refused, so its client signs in again.
func TestSupervisorToken_RefusesARefreshTokenThatOutlivesSevenDays(t *testing.T) {
	h := &handler{tokenSvc: supervisorTokens}
	for name, refresh := range map[string]string{
		"a 365-day token":    mustMint(t)(supervisorTokens.CreateMCPToken("user-1", resourceSupervisor, "refresh")),
		"one past 7 days":    signSupervisorToken(t, "refresh", time.Now().Add(7*24*time.Hour+2*time.Minute)),
		"one with no expiry": signSupervisorToken(t, "refresh", time.Time{}),
	} {
		w := supervisorTokenRequest(h, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}}.Encode())
		if w.Code != http.StatusUnauthorized || !strings.Contains(w.Body.String(), "invalid_grant") {
			t.Errorf("%s: answered %d %q, want 401 invalid_grant", name, w.Code, w.Body.String())
		}
	}
}

// An access token signed before the lifetimes were shortened, or with no
// expiry at all, no longer opens the server.
func TestSupervisorStreamable_ChallengesAnAccessTokenThatOutlivesEightHours(t *testing.T) {
	h := supervisorStreamHandler()
	for name, token := range map[string]string{
		"a 365-day token":    mustMint(t)(supervisorTokens.CreateMCPToken("user-1", resourceSupervisor, "access")),
		"one past 8 hours":   signSupervisorToken(t, "access", time.Now().Add(8*time.Hour+2*time.Minute)),
		"one with no expiry": signSupervisorToken(t, "access", time.Time{}),
	} {
		w := httptest.NewRecorder()
		h.streamableHandler().ServeHTTP(w, supervisorStreamRequest(token))
		if w.Code != http.StatusUnauthorized {
			t.Errorf("%s: answered %d, want 401", name, w.Code)
		}
	}
}

// A token minted by an instance whose clock runs a little ahead still works.
func TestSupervisorStreamable_AllowsAClockAMinuteAhead(t *testing.T) {
	h := supervisorStreamHandler()
	token := signSupervisorToken(t, "access", time.Now().Add(8*time.Hour+30*time.Second))

	w := httptest.NewRecorder()
	h.streamableHandler().ServeHTTP(w, supervisorStreamRequest(token))
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
}

func mustMint(t *testing.T) func(string, error) string {
	return func(tok string, err error) string {
		t.Helper()
		if err != nil {
			t.Fatalf("minting: %v", err)
		}
		return tok
	}
}
