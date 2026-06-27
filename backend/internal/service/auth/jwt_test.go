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
		nonce := "test-nonce"

		token, err := s.CreateOAuthStateToken(redirectURL, provider, nonce)
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		got, err := s.ValidateOAuthStateToken(token, provider, nonce)
		if err != nil {
			t.Fatalf("failed to validate state token: %v", err)
		}
		if got != redirectURL {
			t.Errorf("expected redirectURL %q, got %q", redirectURL, got)
		}
	})

	t.Run("OAuthStateTokenWrongProvider", func(t *testing.T) {
		token, err := s.CreateOAuthStateToken("/dashboard", "google", "")
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		_, err = s.ValidateOAuthStateToken(token, "github", "")
		if err == nil {
			t.Error("expected error when validating with wrong provider, got nil")
		}
	})

	t.Run("OAuthStateTokenNonceMismatch", func(t *testing.T) {
		token, err := s.CreateOAuthStateToken("/", "google", "nonce-1")
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		_, err = s.ValidateOAuthStateToken(token, "google", "nonce-2")
		if err == nil {
			t.Error("expected error when validating with mismatched nonce, got nil")
		}
	})

	t.Run("OAuthStateTokenExpiry", func(t *testing.T) {
		token, err := s.CreateOAuthStateToken("/", "google", "")
		if err != nil {
			t.Fatalf("failed to create state token: %v", err)
		}

		// Should be valid immediately
		_, err = s.ValidateOAuthStateToken(token, "google", "")
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
