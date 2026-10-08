// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func TestTokenService(t *testing.T) {
	cfg := TokenConfig{
		JWTSecret: "test-secret",
	}
	s := NewTokenService(cfg)

	t.Run("CreateAndValidateToken", func(t *testing.T) {
		userID := "user123"
		email := "user@example.com"
		name := "Test User"
		picture := "http://example.com/pic.jpg"

		token, err := s.CreateToken(userID, email, name, picture)
		if err != nil {
			t.Fatalf("failed to create token: %v", err)
		}

		claims, err := s.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate token: %v", err)
		}

		if claims.Subject != userID {
			t.Errorf("expected userID %s, got %s", userID, claims.Subject)
		}
		if claims.Email != email {
			t.Errorf("expected email %s, got %s", email, claims.Email)
		}
		if claims.Name != name {
			t.Errorf("expected name %s, got %s", name, claims.Name)
		}
		if claims.Picture != picture {
			t.Errorf("expected picture %s, got %s", picture, claims.Picture)
		}
		if !HasAudience(claims, ActorHumanAudience) {
			t.Errorf("expected human token audience to include %s, got %v", ActorHumanAudience, claims.Audience)
		}
	})

	t.Run("CreateMCPToken", func(t *testing.T) {
		userID := "user123"
		workspaceID := "ws456"

		token, err := s.CreateMCPToken(userID, workspaceID, "access")
		if err != nil {
			t.Fatalf("failed to create MCP token: %v", err)
		}

		claims, err := s.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate MCP token: %v", err)
		}

		if claims.Subject != userID {
			t.Errorf("expected userID %s, got %s", userID, claims.Subject)
		}
		if len(claims.Audience) == 0 || claims.Audience[0] != workspaceID {
			t.Errorf("expected audience %s, got %v", workspaceID, claims.Audience)
		}
		if HasAudience(claims, ActorHumanAudience) {
			t.Errorf("expected MCP access token not to include %s, got %v", ActorHumanAudience, claims.Audience)
		}
	})

	t.Run("CreateMCPRefreshTokenDoesNotIncludeHumanActor", func(t *testing.T) {
		userID := "user123"
		workspaceID := "ws456"

		token, err := s.CreateMCPToken(userID, workspaceID, "refresh")
		if err != nil {
			t.Fatalf("failed to create MCP token: %v", err)
		}

		claims, err := s.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate MCP token: %v", err)
		}

		if HasAudience(claims, ActorHumanAudience) {
			t.Errorf("expected MCP refresh token not to include %s, got %v", ActorHumanAudience, claims.Audience)
		}
	})

	t.Run("CreateOAuthCodeToken", func(t *testing.T) {
		userID := "user123"
		workspaceID := "ws456"

		token, err := s.CreateOAuthCodeToken(userID, workspaceID)
		if err != nil {
			t.Fatalf("failed to create OAuth code token: %v", err)
		}

		claims, err := s.ValidateToken(token)
		if err != nil {
			t.Fatalf("failed to validate OAuth code token: %v", err)
		}

		if claims.Subject != userID {
			t.Errorf("expected userID %s, got %s", userID, claims.Subject)
		}
		if len(claims.Audience) == 0 || claims.Audience[0] != workspaceID {
			t.Errorf("expected audience %s, got %v", workspaceID, claims.Audience)
		}

		// Check expiry is within bounds ~ 2 mins
		expiry := claims.ExpiresAt.Time
		if expiry.Sub(time.Now()) > 2*time.Minute+time.Second {
			t.Errorf("expected expiry to be <= 2 mins, got %v", expiry.Sub(time.Now()))
		}
	})

	t.Run("InvalidToken", func(t *testing.T) {
		_, err := s.ValidateToken("invalid.token.here")
		if err == nil {
			t.Error("expected error for invalid token, got nil")
		}
	})

	t.Run("ExpiredToken", func(t *testing.T) {
		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user123",
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-1 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
		tokenStr, _ := token.SignedString([]byte("test-secret"))

		_, err := s.ValidateToken(tokenStr)
		if err == nil {
			t.Error("expected error for expired token, got nil")
		}
	})

	t.Run("CreateAndValidateOAuthStateToken", func(t *testing.T) {
		redirectURL := "/dashboard"
		provider := "google"

		token, err := s.CreateOAuthStateToken(redirectURL, provider)
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		got, err := s.ValidateOAuthStateToken(token, provider)
		if err != nil {
			t.Fatalf("failed to validate state token: %v", err)
		}
		if got != redirectURL {
			t.Errorf("expected redirectURL %q, got %q", redirectURL, got)
		}
	})

	t.Run("OAuthStateTokenWrongProvider", func(t *testing.T) {
		token, err := s.CreateOAuthStateToken("/dashboard", "google")
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		_, err = s.ValidateOAuthStateToken(token, "github")
		if err == nil {
			t.Error("expected error when validating with wrong provider, got nil")
		}
	})

	t.Run("OAuthStateTokenExpiry", func(t *testing.T) {
		token, err := s.CreateOAuthStateToken("/", "google")
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		// Should be valid immediately
		_, err = s.ValidateOAuthStateToken(token, "google")
		if err != nil {
			t.Errorf("expected valid token, got: %v", err)
		}
	})

	t.Run("PanicsWhenSecretMissing", func(t *testing.T) {
		defer func() {
			if r := recover(); r == nil {
				t.Errorf("The code did not panic")
			}
		}()

		NewTokenService(TokenConfig{})
	})
}

func TestRefreshTokens(t *testing.T) {
	s := NewTokenService(TokenConfig{JWTSecret: "test-secret"})

	t.Run("round trips the user it was minted for", func(t *testing.T) {
		token, err := s.CreateRefreshToken("user123")
		if err != nil {
			t.Fatalf("CreateRefreshToken: %v", err)
		}

		claims, err := s.ValidateRefreshToken(token)
		if err != nil {
			t.Fatalf("ValidateRefreshToken: %v", err)
		}
		if claims.Subject != "user123" {
			t.Errorf("subject = %q, want user123", claims.Subject)
		}
	})

	t.Run("carries no profile details", func(t *testing.T) {
		// They would go stale over a month-long session, and the access token
		// minted from this is built from the database instead.
		token, _ := s.CreateRefreshToken("user123")
		claims, _ := s.ValidateRefreshToken(token)

		if claims.Email != "" || claims.Name != "" || claims.Picture != "" {
			t.Errorf("refresh token carries profile details: %+v", claims)
		}
	})

	t.Run("outlives an access token", func(t *testing.T) {
		refresh, _ := s.CreateRefreshToken("user123")
		access, _ := s.CreateToken("user123", "a@b.com", "A", "")

		refreshClaims, _ := s.ValidateRefreshToken(refresh)
		accessClaims, _ := s.ValidateToken(access)

		if !refreshClaims.ExpiresAt.After(accessClaims.ExpiresAt.Time) {
			t.Errorf("refresh expiry %v is not after access expiry %v",
				refreshClaims.ExpiresAt, accessClaims.ExpiresAt)
		}
	})

	t.Run("rejects an access token", func(t *testing.T) {
		// The security-critical case. Without the audience check a stolen
		// access token could be walked forward into fresh sessions forever.
		access, _ := s.CreateToken("user123", "a@b.com", "A", "")

		if _, err := s.ValidateRefreshToken(access); err == nil {
			t.Error("an access token was accepted as a refresh token")
		}
	})

	t.Run("rejects an agent refresh token", func(t *testing.T) {
		// The MCP flow mints its own "refresh" tokens for one workspace. They
		// stand for an agent, not a person, and must not open a human session.
		mcp, err := s.CreateMCPToken("user123", "workspace1", "refresh")
		if err != nil {
			t.Fatalf("CreateMCPToken: %v", err)
		}

		if _, err := s.ValidateRefreshToken(mcp); err == nil {
			t.Error("an MCP refresh token was accepted as a human refresh token")
		}
	})

	t.Run("rejects a token this server did not sign", func(t *testing.T) {
		other := NewTokenService(TokenConfig{JWTSecret: "a-different-secret"})
		foreign, _ := other.CreateRefreshToken("user123")

		if _, err := s.ValidateRefreshToken(foreign); err == nil {
			t.Error("a token signed with another secret was accepted")
		}
	})

	t.Run("rejects nonsense", func(t *testing.T) {
		for _, tokenStr := range []string{"", "not.a.token", "a.b.c"} {
			if _, err := s.ValidateRefreshToken(tokenStr); err == nil {
				t.Errorf("ValidateRefreshToken(%q) was accepted", tokenStr)
			}
		}
	})

	t.Run("rejects one that has expired", func(t *testing.T) {
		claims := Claims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user123",
				Audience:  jwt.ClaimStrings{RefreshHumanAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			},
		}
		expired, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}

		if _, err := s.ValidateRefreshToken(expired); err == nil {
			t.Error("an expired refresh token was accepted")
		}
	})
}

// Every token shares one signing secret, so the session check is the only
// thing between an agent's workspace token and the whole account.
func TestSessionToken(t *testing.T) {
	s := NewTokenService(TokenConfig{JWTSecret: "test-secret"})

	t.Run("accepts a signed-in person's access token", func(t *testing.T) {
		token, _ := s.CreateToken("user123", "a@b.com", "A", "")

		claims, err := s.ValidateSessionToken(token)
		if err != nil {
			t.Fatalf("ValidateSessionToken: %v", err)
		}
		if claims.Subject != "user123" || claims.Email != "a@b.com" {
			t.Errorf("claims = %+v", claims)
		}
	})

	others := map[string]func() (string, error){
		"workspace MCP access token":  func() (string, error) { return s.CreateMCPToken("user123", "workspace1", "access") },
		"workspace MCP refresh token": func() (string, error) { return s.CreateMCPToken("user123", "workspace1", "refresh") },
		"supervisor MCP access token": func() (string, error) { return s.CreateMCPToken("user123", "coremcp", "access") },
		"OAuth authorization code":    func() (string, error) { return s.CreateOAuthCodeToken("user123", "workspace1") },
		"human refresh token":         func() (string, error) { return s.CreateRefreshToken("user123") },
		"terminal ticket":             func() (string, error) { return s.CreateTerminalTicket("user123", "42") },
		"browser ticket":              func() (string, error) { return s.CreateBrowserTicket("user123") },
		"OAuth consent token": func() (string, error) {
			return s.CreateOAuthConsentToken("user123", OAuthConsent{Resource: "workspace1", ClientID: "c"})
		},
	}
	for name, mint := range others {
		t.Run("rejects a "+name, func(t *testing.T) {
			token, err := mint()
			if err != nil {
				t.Fatalf("mint: %v", err)
			}
			if _, err := s.ValidateSessionToken(token); err == nil {
				t.Errorf("a %s was accepted as a session", name)
			}
		})
	}

	t.Run("rejects a token with no audience", func(t *testing.T) {
		claims := Claims{RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "user123",
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour)),
		}}
		token, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte("test-secret"))

		if _, err := s.ValidateSessionToken(token); err == nil {
			t.Error("a token with no audience was accepted as a session")
		}
	})

	t.Run("rejects nonsense", func(t *testing.T) {
		if _, err := s.ValidateSessionToken("not.a.token"); err == nil {
			t.Error("nonsense was accepted")
		}
	})
}

// A terminal ticket is a bearer credential that travels in a URL query
// string, so what it is *not* valid for is the whole design.
func TestTerminalTicket(t *testing.T) {
	svc := NewTokenService(TokenConfig{JWTSecret: "test-secret"})
	other := NewTokenService(TokenConfig{JWTSecret: "another-secret"})

	ticket, err := svc.CreateTerminalTicket("user-1", "500")
	if err != nil {
		t.Fatal(err)
	}

	claims, err := svc.ValidateTerminalTicket(ticket, "500")
	if err != nil {
		t.Fatalf("a freshly minted ticket was refused: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q, want the person it was minted for", claims.Subject)
	}

	t.Run("not for another session", func(t *testing.T) {
		// Without this the ticket would be "any terminal this person can
		// reach", for as long as it lives, rather than the one they asked for.
		if _, err := svc.ValidateTerminalTicket(ticket, "501"); err == nil {
			t.Fatal("a ticket minted for session 500 opened 501")
		}
	})

	t.Run("no session named", func(t *testing.T) {
		// A caller with no session id must not be able to skip the check by
		// passing nothing. "" is in no audience, but relying on that would
		// make this depend on how the audience list happens to be built.
		if _, err := svc.ValidateTerminalTicket(ticket, ""); err == nil {
			t.Fatal("a ticket validated against no session at all")
		}
	})

	t.Run("an access token is not a ticket", func(t *testing.T) {
		// The reason the marker audience exists. The `at` cookie is good for
		// 24 hours and for everything; if it satisfied this check, a
		// credential that belongs in a cookie would also work in a URL — and
		// URLs are logged by proxies and kept in shell history.
		at, err := svc.CreateToken("user-1", "a@b.c", "A", "")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ValidateTerminalTicket(at, "500"); err == nil {
			t.Fatal("an access token was accepted as a terminal ticket")
		}
	})

	t.Run("an MCP token for the session's number is not a ticket", func(t *testing.T) {
		// Audiences are a flat list, and an MCP token's audience is an id. A
		// check that only looked for the session id in the audience would
		// accept this — the marker is what separates the two.
		mcp, err := svc.CreateMCPToken("user-1", "500", "access")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ValidateTerminalTicket(mcp, "500"); err == nil {
			t.Fatal("an MCP token was accepted as a terminal ticket")
		}
	})

	t.Run("another server's ticket", func(t *testing.T) {
		theirs, err := other.CreateTerminalTicket("user-1", "500")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := svc.ValidateTerminalTicket(theirs, "500"); err == nil {
			t.Fatal("a ticket signed with another secret was accepted")
		}
	})

	t.Run("nonsense", func(t *testing.T) {
		if _, err := svc.ValidateTerminalTicket("not-a-token", "500"); err == nil {
			t.Fatal("expected a refusal")
		}
	})
}

func TestBrowserTicket(t *testing.T) {
	svc := NewTokenService(TokenConfig{JWTSecret: "test-secret"})
	other := NewTokenService(TokenConfig{JWTSecret: "another-secret"})

	ticket, err := svc.CreateBrowserTicket("user-1")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ValidateBrowserTicket(ticket)
	if err != nil {
		t.Fatalf("a freshly minted ticket was refused: %v", err)
	}
	if claims.Subject != "user-1" {
		t.Errorf("subject = %q, want user-1", claims.Subject)
	}
	if left := time.Until(claims.ExpiresAt.Time); left > BrowserTicketTTL || left < BrowserTicketTTL-5*time.Second {
		t.Errorf("expires in %v, want about %v", left, BrowserTicketTTL)
	}

	refused := map[string]func() (string, error){
		"terminal ticket": func() (string, error) { return svc.CreateTerminalTicket("user-1", "500") },
		"access token":    func() (string, error) { return svc.CreateToken("user-1", "a@b.c", "A", "") },
		"other secret":    func() (string, error) { return other.CreateBrowserTicket("user-1") },
	}
	for name, mint := range refused {
		t.Run(name, func(t *testing.T) {
			tok, err := mint()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := svc.ValidateBrowserTicket(tok); err == nil {
				t.Fatalf("a %s opened the browser socket", name)
			}
		})
	}

	t.Run("not a terminal ticket", func(t *testing.T) {
		if _, err := svc.ValidateTerminalTicket(ticket, "500"); err == nil {
			t.Fatal("a browser ticket opened a terminal")
		}
	})
}

func TestClientRegistrationTokenCarriesName(t *testing.T) {
	svc := NewTokenService(TokenConfig{JWTSecret: "test-secret"})

	clientID, err := svc.CreateClientRegistrationToken([]string{"http://localhost/callback"}, "Claude Code")
	if err != nil {
		t.Fatal(err)
	}
	claims, err := svc.ValidateClientRegistrationToken(clientID)
	if err != nil {
		t.Fatalf("a freshly registered client_id was refused: %v", err)
	}
	if claims.ClientName != "Claude Code" {
		t.Errorf("the registration carried the name %q, want Claude Code", claims.ClientName)
	}
}

func TestOAuthConsentToken(t *testing.T) {
	svc := NewTokenService(TokenConfig{JWTSecret: "test-secret"})
	other := NewTokenService(TokenConfig{JWTSecret: "another-secret"})
	consent := OAuthConsent{Resource: "ws1", ClientID: "client-1", RedirectURI: "http://localhost:3118/callback", State: "s1"}

	token, err := svc.CreateOAuthConsentToken("user-1", consent)
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.ValidateOAuthConsentToken(token, "user-1", consent); err != nil {
		t.Fatalf("the token the page was rendered with was refused: %v", err)
	}

	// Each field is something an attacker would want to swap after the
	// person looked at the page.
	changed := map[string]OAuthConsent{
		"another workspace":    {Resource: "ws2", ClientID: "client-1", RedirectURI: consent.RedirectURI, State: "s1"},
		"the supervisor":       {Resource: "coremcp", ClientID: "client-1", RedirectURI: consent.RedirectURI, State: "s1"},
		"another client":       {Resource: "ws1", ClientID: "client-2", RedirectURI: consent.RedirectURI, State: "s1"},
		"another redirect":     {Resource: "ws1", ClientID: "client-1", RedirectURI: "https://evil.example.com/callback", State: "s1"},
		"another state":        {Resource: "ws1", ClientID: "client-1", RedirectURI: consent.RedirectURI, State: "s2"},
		"an empty request":     {},
		"the redirect dropped": {Resource: "ws1", ClientID: "client-1", State: "s1"},
	}
	for name, request := range changed {
		t.Run("refused for "+name, func(t *testing.T) {
			if err := svc.ValidateOAuthConsentToken(token, "user-1", request); err == nil {
				t.Fatalf("a consent token shown for %+v approved %+v", consent, request)
			}
		})
	}

	t.Run("refused for another person", func(t *testing.T) {
		if err := svc.ValidateOAuthConsentToken(token, "user-2", consent); err == nil {
			t.Fatal("a consent token minted for user-1 was accepted for user-2")
		}
	})

	refused := map[string]func() (string, error){
		"a token signed with another secret": func() (string, error) { return other.CreateOAuthConsentToken("user-1", consent) },
		"an access token":                    func() (string, error) { return svc.CreateToken("user-1", "a@b.c", "A", "") },
		"an authorization code":              func() (string, error) { return svc.CreateOAuthCodeToken("user-1", "ws1") },
		"a browser ticket":                   func() (string, error) { return svc.CreateBrowserTicket("user-1") },
		"an empty string":                    func() (string, error) { return "", nil },
	}
	for name, mint := range refused {
		t.Run("refused for "+name, func(t *testing.T) {
			tok, err := mint()
			if err != nil {
				t.Fatal(err)
			}
			if err := svc.ValidateOAuthConsentToken(tok, "user-1", consent); err == nil {
				t.Fatalf("%s was accepted as consent", name)
			}
		})
	}

	t.Run("refused once expired", func(t *testing.T) {
		expired := jwt.NewWithClaims(jwt.SigningMethodHS256, OAuthConsentClaims{
			RegisteredClaims: jwt.RegisteredClaims{
				Subject:   "user-1",
				Audience:  jwt.ClaimStrings{OAuthConsentAudience},
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Minute)),
			},
			OAuthConsent: consent,
		})
		tok, err := expired.SignedString([]byte("test-secret"))
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.ValidateOAuthConsentToken(tok, "user-1", consent); err == nil {
			t.Fatal("a consent token that expired a minute ago was accepted")
		}
	})

	t.Run("refused when signed with another algorithm", func(t *testing.T) {
		none := jwt.NewWithClaims(jwt.SigningMethodNone, OAuthConsentClaims{
			RegisteredClaims: jwt.RegisteredClaims{Subject: "user-1", Audience: jwt.ClaimStrings{OAuthConsentAudience}},
			OAuthConsent:     consent,
		})
		tok, err := none.SignedString(jwt.UnsafeAllowNoneSignatureType)
		if err != nil {
			t.Fatal(err)
		}
		if err := svc.ValidateOAuthConsentToken(tok, "user-1", consent); err == nil {
			t.Fatal("an unsigned consent token was accepted")
		}
	})

	t.Run("lasts as long as the page may stay open", func(t *testing.T) {
		parsed, _, err := jwt.NewParser().ParseUnverified(token, &OAuthConsentClaims{})
		if err != nil {
			t.Fatal(err)
		}
		exp := parsed.Claims.(*OAuthConsentClaims).ExpiresAt.Time
		if left := time.Until(exp); left > OAuthConsentTTL || left < OAuthConsentTTL-5*time.Second {
			t.Errorf("the consent token expires in %v, want about %v", left, OAuthConsentTTL)
		}
	})
}
