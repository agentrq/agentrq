// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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

	// RefreshHumanAudience marks a browser/desktop refresh token. Deliberately
	// distinct from the MCP flow's "refresh": that one is an agent credential
	// for one workspace, this one stands for a signed-in person, and a token
	// minted for either must never satisfy the other's check.
	RefreshHumanAudience = "refresh:human"

	// TerminalTicketAudience marks a credential minted for one terminal
	// socket, and nothing else.
	//
	// The terminal WebSocket cannot authenticate the way the rest of the API
	// does. Every other call is a same-origin relative URL carrying the `at`
	// cookie; the socket is necessarily an absolute URL, because Electron's
	// app:// handler forwards HTTP and not WebSocket upgrades — so on the
	// desktop app it is a cross-site request, and a SameSite=Lax cookie is
	// withheld from it by the browser. A ticket is a credential the page can
	// hold and present explicitly, which works from either build.
	TerminalTicketAudience = "terminal_ticket"

	// BrowserTicketAudience marks a credential for the Chrome extension's
	// browser socket. The extension's origin never receives the `at` cookie.
	BrowserTicketAudience = "browser_ticket"

	// OAuthConsentAudience marks the token the OAuth consent page posts back
	// when the person approves an app. Only a page rendered for that person
	// holds one, which is what stops another site submitting the approval.
	OAuthConsentAudience = "oauth_consent"
)

// OAuthConsentTTL is how long the consent page can stay open before Allow
// stops working and the app has to start over.
const OAuthConsentTTL = 10 * time.Minute

// BrowserTicketTTL is short for the terminal ticket's reason: it travels in a
// URL, and every connect mints a new one.
const BrowserTicketTTL = time.Minute

// TerminalTicketTTL is how long a terminal ticket is worth presenting.
//
// Short, because it travels in a URL query string: it is minted immediately
// before a socket is opened and has no other use. A reconnect mints another
// rather than replaying this one, which is what lets the lifetime be this
// short — see `useTerminalSession.js`, where every connection attempt calls
// back for a fresh URL.
const TerminalTicketTTL = time.Minute

// RefreshTokenTTL is how long a session survives without being used at all.
//
// Every refresh mints a new one, so someone who opens the app at least this
// often is never signed out; this is the idle window, not a session cap.
//
// Two days, kept deliberately short because these tokens are stateless: there
// is no way to revoke one before it expires, so its lifetime *is* the blast
// radius if it leaks -- and on desktop it is stored unencrypted on disk.
const RefreshTokenTTL = 2 * 24 * time.Hour

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

// DynamicClientAudience marks a token minted by the RFC 7591 dynamic client
// registration endpoint. The token itself IS the client_id: it carries the
// client's registered redirect_uris so /oauth2/authorize can validate a
// redirect_uri against the specific client that requested it, without
// needing a database to persist registrations.
const DynamicClientAudience = "dynamic_client"

type ClientRegistrationClaims struct {
	jwt.RegisteredClaims
	RedirectURIs []string `json:"redirect_uris,omitempty"`
	// ClientName is what the client called itself when it registered. Nobody
	// checks it, so the consent page labels it as unverified.
	ClientName string `json:"client_name,omitempty"`
}

// OAuthConsent is the authorization request a consent token approves. Every
// field must match when Allow is posted, so an approval for one app, redirect
// or workspace cannot be spent on another.
type OAuthConsent struct {
	// Resource is the base62 workspace ID, or "coremcp" for the supervisor.
	Resource    string `json:"res"`
	ClientID    string `json:"cid"`
	RedirectURI string `json:"ruri"`
	State       string `json:"st"`
}

type OAuthConsentClaims struct {
	jwt.RegisteredClaims
	OAuthConsent
}

type TokenService interface {
	CreateToken(userID, email, name, picture string) (string, error)
	CreateRefreshToken(userID string) (string, error)
	ValidateRefreshToken(tokenStr string) (*Claims, error)
	CreateMCPToken(userID, workspaceID, tokenType string) (string, error)
	CreateOAuthCodeToken(userID, workspaceID string) (string, error)
	CreateTerminalTicket(userID, sessionID string) (string, error)
	ValidateTerminalTicket(tokenStr, sessionID string) (*Claims, error)
	CreateBrowserTicket(userID string) (string, error)
	ValidateBrowserTicket(tokenStr string) (*Claims, error)
	CreateOAuthStateToken(redirectURL, provider string) (string, error)
	CreateClientRegistrationToken(redirectURIs []string, clientName string) (string, error)
	CreateOAuthConsentToken(userID string, consent OAuthConsent) (string, error)
	ValidateOAuthConsentToken(tokenStr, userID string, consent OAuthConsent) error
	ValidateToken(tokenStr string) (*Claims, error)
	ValidateSessionToken(tokenStr string) (*Claims, error)
	ValidateOAuthStateToken(tokenStr, provider string) (redirectURL string, err error)
	ValidateClientRegistrationToken(tokenStr string) (*ClientRegistrationClaims, error)
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

// CreateRefreshToken mints the long-lived half of a session.
//
// It carries no email, name or picture: those go stale, and a refresh token is
// only ever exchanged for an access token that is minted from the database at
// that moment. Carrying them would mean a month-old name reappearing after a
// refresh.
func (s *tokenService) CreateRefreshToken(userID string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(RefreshTokenTTL)),
			Audience:  jwt.ClaimStrings{RefreshHumanAudience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ValidateRefreshToken accepts only a token minted by CreateRefreshToken.
//
// The audience check is the point: without it an access token would be
// accepted here, and a stolen 24-hour access token could be walked forward
// indefinitely into fresh sessions.
func (s *tokenService) ValidateRefreshToken(tokenStr string) (*Claims, error) {
	claims, err := s.ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if !HasAudience(claims, RefreshHumanAudience) {
		return nil, errors.New("not a refresh token")
	}
	return claims, nil
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

// CreateClientRegistrationToken mints the client_id returned by the RFC 7591
// dynamic client registration endpoint. The client_id IS the token: it's a
// signed, stateless credential carrying the client's registered
// redirect_uris, so /oauth2/authorize can later validate a redirect_uri
// against the specific client that registered it without persisting
// anything server-side.
func (s *tokenService) CreateClientRegistrationToken(redirectURIs []string, clientName string) (string, error) {
	claims := ClientRegistrationClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   "dynamic_client",
			Audience:  jwt.ClaimStrings{DynamicClientAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(10 * 365 * 24 * time.Hour)), // effectively long-lived
		},
		RedirectURIs: redirectURIs,
		ClientName:   clientName,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ValidateClientRegistrationToken parses a client_id minted by
// CreateClientRegistrationToken. It returns an error for any client_id that
// wasn't issued by this server's DCR endpoint (e.g. a legacy or third-party
// client_id), which callers should treat as "not a recognized dynamic
// client" rather than a hard authentication failure.
func (s *tokenService) ValidateClientRegistrationToken(tokenStr string) (*ClientRegistrationClaims, error) {
	token, err := jwt.ParseWithClaims(tokenStr, &ClientRegistrationClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithAudience(DynamicClientAudience))
	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*ClientRegistrationClaims)
	if !ok || !token.Valid {
		return nil, errors.New("invalid client registration token")
	}
	return claims, nil
}

// CreateOAuthConsentToken mints the token the consent page posts back with
// Allow, bound to the person it was shown to and to the request it describes.
func (s *tokenService) CreateOAuthConsentToken(userID string, consent OAuthConsent) (string, error) {
	claims := OAuthConsentClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			Audience:  jwt.ClaimStrings{OAuthConsentAudience},
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(OAuthConsentTTL)),
		},
		OAuthConsent: consent,
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ValidateOAuthConsentToken accepts only a token minted by
// CreateOAuthConsentToken for this person and exactly this request.
func (s *tokenService) ValidateOAuthConsentToken(tokenStr, userID string, consent OAuthConsent) error {
	token, err := jwt.ParseWithClaims(tokenStr, &OAuthConsentClaims{}, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return s.secret, nil
	}, jwt.WithAudience(OAuthConsentAudience), jwt.WithSubject(userID))
	if err != nil {
		return err
	}

	if token.Claims.(*OAuthConsentClaims).OAuthConsent != consent {
		return errors.New("consent token was issued for a different authorization request")
	}
	return nil
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

// CreateTerminalTicket mints the credential a browser presents when it opens
// a session's terminal socket.
//
// Scoped to one session as well as one person: the audience carries the
// session id, so a ticket for a terminal somebody may watch cannot be replayed
// against a different one. `sessionID` is the numeric form, because that is
// what the socket handler has in its hand — it is given a uint64 by the router
// and never sees the base62 spelling.
func (s *tokenService) CreateTerminalTicket(userID, sessionID string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(TerminalTicketTTL)),
			Audience:  jwt.ClaimStrings{sessionID, TerminalTicketAudience},
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(s.secret)
}

// ValidateTerminalTicket accepts only a ticket minted by CreateTerminalTicket
// for this exact session.
//
// Both audience checks are load-bearing and for different reasons. Without the
// marker, the 24-hour `at` access token would satisfy this too, and a
// credential that belongs in a cookie would become one that works in a URL —
// so it could be logged by a proxy, land in somebody's shell history and still
// open every terminal on the account. Without the session id, one ticket would
// open any session its holder can reach, for a full minute after it was minted
// for one of them.
func (s *tokenService) ValidateTerminalTicket(tokenStr, sessionID string) (*Claims, error) {
	claims, err := s.ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if !HasAudience(claims, TerminalTicketAudience) {
		return nil, errors.New("not a terminal ticket")
	}
	if sessionID == "" || !HasAudience(claims, sessionID) {
		return nil, errors.New("terminal ticket is for another session")
	}
	return claims, nil
}

// CreateBrowserTicket mints the credential the extension presents when it
// opens the browser socket.
func (s *tokenService) CreateBrowserTicket(userID string) (string, error) {
	claims := Claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(BrowserTicketTTL)),
			Audience:  jwt.ClaimStrings{BrowserTicketAudience},
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

// ValidateBrowserTicket accepts only a ticket minted by CreateBrowserTicket;
// without the audience check the 24-hour access token would work in a URL.
func (s *tokenService) ValidateBrowserTicket(tokenStr string) (*Claims, error) {
	claims, err := s.ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if !HasAudience(claims, BrowserTicketAudience) {
		return nil, errors.New("not a browser ticket")
	}
	return claims, nil
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

// ValidateSessionToken accepts only a signed-in person's access token, the
// `at` cookie that CreateToken mints. Every other token is signed with the same
// secret, so without the audience check an agent's workspace MCP token, a
// refresh token or a ticket would pass for the whole account.
func (s *tokenService) ValidateSessionToken(tokenStr string) (*Claims, error) {
	claims, err := s.ValidateToken(tokenStr)
	if err != nil {
		return nil, err
	}
	if !HasAudience(claims, ActorHumanAudience) {
		return nil, errors.New("not a session token")
	}
	return claims, nil
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
