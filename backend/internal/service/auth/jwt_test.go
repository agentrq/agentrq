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
