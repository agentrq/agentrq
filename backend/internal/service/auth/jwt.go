package auth

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type ctxKey uint8

const (
	CtxKeyMCPClaims    ctxKey = 1
	ActorHumanAudience        = "actor:human"
)

type TokenConfig struct {
	JWTSecret string `yaml:"jwt_secret"`
}

type Claims struct {
	jwt.RegisteredClaims
	Email   string `json:"email,omitempty"`
	Name    string `json:"name,omitempty"`
	Picture string `json:"picture,omitempty"`
}

type StateClaims struct {
	jwt.RegisteredClaims
	RedirectURL string `json:"rurl,omitempty"`
}

type TokenService interface {
	CreateToken(userID, email, name, picture string) (string, error)
	CreateMCPToken(userID, workspaceID, tokenType string) (string, error)
	CreateOAuthCodeToken(userID, workspaceID string) (string, error)
	CreateOAuthStateToken(redirectURL, provider string) (string, error)
	ValidateToken(tokenStr string) (*Claims, error)
	ValidateOAuthStateToken(tokenStr, provider string) (redirectURL string, err error)
}

type tokenService struct {
	secret []byte
}

func NewTokenService(cfg TokenConfig) TokenService {
	if cfg.JWTSecret == "" {
		// Critical: fallback to an empty secret is not allowed.
		// In production, the app should fail to start if JWTSecret is missing.
		panic("situational security: JWT secret is required but not provided in configuration")
	}
	return &tokenService{
		secret: []byte(cfg.JWTSecret),
	}
}

func (s *tokenService) CreateToken(userID, email, name, picture string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			Audience:  jwt.ClaimStrings{ActorHumanAudience},
		},
		Email:   email,
		Name:    name,
		Picture: picture,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *tokenService) CreateMCPToken(userID, workspaceID, tokenType string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(365 * 24 * time.Hour)), // 365 days
		},
	}

	if workspaceID != "" {
		claims.Audience = jwt.ClaimStrings{workspaceID}
	}
	if tokenType != "" {
		claims.Audience = append(claims.Audience, tokenType)
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func HasAudience(claims *Claims, audience string) bool {
	if claims == nil {
		return false
	}
	for _, aud := range claims.Audience {
		if aud == audience {
			return true
		}
	}
	return false
}

func ContextHasAudience(ctx context.Context, audience string) bool {
	if ctx == nil {
		return false
	}
	claims, _ := ctx.Value(CtxKeyMCPClaims).(*Claims)
	return HasAudience(claims, audience)
}

func (s *tokenService) CreateOAuthCodeToken(userID, workspaceID string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Minute)), // 2 minutes short lived
		},
	}

	if workspaceID != "" {
		claims.Audience = jwt.ClaimStrings{workspaceID}
	}
	claims.Audience = append(claims.Audience, "authorization_code")

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *tokenService) CreateOAuthStateToken(redirectURL, provider string) (string, error) {
	now := time.Now()
	claims := StateClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "oauth_state",
			Audience:  jwt.ClaimStrings{provider},
			ExpiresAt: jwt.NewNumericDate(now.Add(3 * time.Minute)),
			NotBefore: jwt.NewNumericDate(now.Add(-2 * time.Second)),
		},
		RedirectURL: redirectURL,
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

func (s *tokenService) ValidateOAuthStateToken(tokenStr, provider string) (string, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &StateClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithAudience(provider))
	if err != nil {
		return "", err
	}

	claims, ok := token.Claims.(*StateClaims)
	if !ok || !token.Valid {
		return "", errors.New("invalid state token")
	}
	return claims.RedirectURL, nil
}

func (s *tokenService) ValidateToken(tokenStr string) (*Claims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		// Validate the signing method to prevent algorithm confusion attacks
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, errors.New("invalid token")
}
