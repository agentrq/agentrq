// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package gitlabapp talks to GitLab as an OAuth application the server's
// admin registered, so that skills can be imported from the private projects
// of the person who connects it.
//
// GitLab has no installation tokens to mint per import, as GitHub's App has:
// an access token lapses after two hours and only its refresh token renews
// it. So unlike GitHub's, a connection is kept, its tokens encrypted at rest,
// and it reads with the read_api scope alone.
package gitlabapp

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/agentrq/agentrq/backend/internal/service/skillimport"
)

const (
	requestTimeout = 30 * time.Second
	// stateTTL is how long a connect may take, signing in to GitLab included.
	stateTTL = 30 * time.Minute
	// Scope is all an import needs: read access to the API, which serves a
	// project's archive and files.
	Scope = "read_api"
	// CallbackPath is where GitLab returns a person, in the web app.
	CallbackPath = "/gitlab/callback"
)

var (
	// ErrDisabled is any call while the application is not configured.
	ErrDisabled = errors.New("connecting GitLab is not configured on this server")
	// ErrInvalidState is a connect whose state was not made for this
	// account, or is too old.
	ErrInvalidState = errors.New("this GitLab connection link has expired or was started by another account; choose Connect GitLab beside a skill import to start again")
	// ErrRevoked is a refresh GitLab refused: the person revoked the
	// application, or the token was used up.
	ErrRevoked = errors.New("GitLab no longer accepts this connection")
)

type (
	// Config is the admin's settings for the server's GitLab application.
	// Host is read even while it is off: public imports use it too.
	Config struct {
		Enabled      bool   `yaml:"enabled"`
		Host         string `yaml:"host"`
		ClientID     string `yaml:"clientId"`
		ClientSecret string `yaml:"clientSecret"`
	}

	// Token is what GitLab grants: an access token, until Expiry, and the
	// refresh token that renews it.
	Token struct {
		Access  string
		Refresh string
		Expiry  time.Time
	}

	// Service is the GitLab application. Every method but Enabled and Host
	// answers ErrDisabled while it is not configured.
	Service interface {
		Enabled() bool
		// Host is the GitLab skills are imported from.
		Host() string
		// AuthorizeURL is where a person approves the application; GitLab
		// brings them back to the web app with state.
		AuthorizeURL(ctx context.Context, state string) (string, error)
		// NewState is the state for userID's connect, and CheckState
		// whether state is one.
		NewState(ctx context.Context, userID int64) (string, error)
		CheckState(ctx context.Context, state string, userID int64) error
		// Exchange turns the code GitLab returned into a token, and Refresh
		// renews one.
		Exchange(ctx context.Context, code string) (Token, error)
		Refresh(ctx context.Context, refreshToken string) (Token, error)
		// Username is whose token it is.
		Username(ctx context.Context, accessToken string) (string, error)
		// Revoke tells GitLab a token is no longer wanted.
		Revoke(ctx context.Context, token string) error
	}

	service struct {
		cfg         Config
		enabled     bool
		redirectURI string
		client      *http.Client
		base        string
		now         func() time.Time
	}
)

// Check refuses settings the application could not run with. Disabled, only
// the host is checked, since public imports read it.
func Check(cfg Config) error {
	if err := skillimport.CheckGitLabHost(cfg.Host); err != nil {
		return fmt.Errorf("gitlabApp.host: %w", err)
	}
	if !cfg.Enabled {
		return nil
	}
	if strings.TrimSpace(cfg.ClientID) == "" || strings.TrimSpace(cfg.ClientSecret) == "" {
		return errors.New("gitlabApp: clientId and clientSecret are required")
	}
	return nil
}

// New returns the application cfg configures, returning people to
// baseURL's callback; one disabled, or with settings Check refuses, answers
// every call but Host with ErrDisabled.
func New(cfg Config, baseURL string) Service {
	cfg.Host = skillimport.GitLabHost(cfg.Host)
	cfg.ClientID, cfg.ClientSecret = strings.TrimSpace(cfg.ClientID), strings.TrimSpace(cfg.ClientSecret)
	return &service{
		cfg:         cfg,
		enabled:     cfg.Enabled && Check(cfg) == nil,
		redirectURI: strings.TrimRight(baseURL, "/") + CallbackPath,
		client:      &http.Client{Timeout: requestTimeout},
		base:        "https://" + cfg.Host,
		now:         time.Now,
	}
}

func (s *service) Enabled() bool { return s.enabled }

func (s *service) Host() string { return s.cfg.Host }

func (s *service) AuthorizeURL(_ context.Context, state string) (string, error) {
	if !s.enabled {
		return "", ErrDisabled
	}
	q := url.Values{
		"client_id": {s.cfg.ClientID}, "redirect_uri": {s.redirectURI}, "response_type": {"code"},
		"scope": {Scope}, "state": {state},
	}
	return s.base + "/oauth/authorize?" + q.Encode(), nil
}

// A state is <user>.<expiry>.<mac>, the mac keyed by the client secret: only
// this server can make one, and only for the account that started.
func (s *service) NewState(_ context.Context, userID int64) (string, error) {
	if !s.enabled {
		return "", ErrDisabled
	}
	payload := strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(s.now().Add(stateTTL).Unix(), 10)
	return payload + "." + s.mac(payload), nil
}

func (s *service) CheckState(_ context.Context, state string, userID int64) error {
	if !s.enabled {
		return ErrDisabled
	}
	i := strings.LastIndexByte(state, '.')
	if i < 0 {
		return ErrInvalidState
	}
	payload, mac := state[:i], state[i+1:]
	if subtle.ConstantTimeCompare([]byte(mac), []byte(s.mac(payload))) != 1 {
		return ErrInvalidState
	}
	uid, exp, _ := strings.Cut(payload, ".")
	expiry, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || uid != strconv.FormatInt(userID, 10) || s.now().Unix() > expiry {
		return ErrInvalidState
	}
	return nil
}

func (s *service) mac(payload string) string {
	m := hmac.New(sha256.New, []byte(s.cfg.ClientSecret))
	m.Write([]byte("agentrq-gitlab-app-state|" + payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *service) Exchange(ctx context.Context, code string) (Token, error) {
	if !s.enabled {
		return Token{}, ErrDisabled
	}
	if code == "" {
		return Token{}, errors.New("GitLab did not send an authorization code; choose Connect GitLab beside a skill import to start again")
	}
	t, status, err := s.token(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code}})
	if err == nil && status != http.StatusOK {
		err = fmt.Errorf("GitLab refused the authorization code (%d); choose Connect GitLab beside a skill import to start again", status)
	}
	return t, err
}

func (s *service) Refresh(ctx context.Context, refreshToken string) (Token, error) {
	if !s.enabled {
		return Token{}, ErrDisabled
	}
	t, status, err := s.token(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refreshToken}})
	switch {
	case err != nil:
		return Token{}, err
	case status == http.StatusBadRequest || status == http.StatusUnauthorized:
		return Token{}, ErrRevoked
	case status != http.StatusOK:
		return Token{}, fmt.Errorf("GitLab answered %d renewing the connection", status)
	}
	return t, nil
}

// token asks GitLab's token endpoint for a grant.
func (s *service) token(ctx context.Context, form url.Values) (Token, int, error) {
	form.Set("client_id", s.cfg.ClientID)
	form.Set("client_secret", s.cfg.ClientSecret)
	form.Set("redirect_uri", s.redirectURI)
	resp, err := s.post(ctx, "/oauth/token", form)
	if err != nil {
		return Token{}, 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Token{}, resp.StatusCode, nil
	}
	var body struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		ExpiresIn    int64  `json:"expires_in"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil || body.AccessToken == "" || body.RefreshToken == "" {
		return Token{}, 0, errors.New("GitLab sent a token that could not be read")
	}
	if body.ExpiresIn <= 0 {
		// GitLab grants two hours; one it does not state is renewed soon.
		body.ExpiresIn = 60
	}
	return Token{Access: body.AccessToken, Refresh: body.RefreshToken, Expiry: s.now().Add(time.Duration(body.ExpiresIn) * time.Second)}, http.StatusOK, nil
}

func (s *service) Username(ctx context.Context, accessToken string) (string, error) {
	if !s.enabled {
		return "", ErrDisabled
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.base+"/api/v4/user", nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("User-Agent", "agentrq-gitlab-app")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach GitLab: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		Username string `json:"username"`
	}
	if resp.StatusCode != http.StatusOK || json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body) != nil || body.Username == "" {
		return "", fmt.Errorf("GitLab answered %d naming who connected", resp.StatusCode)
	}
	return body.Username, nil
}

func (s *service) Revoke(ctx context.Context, token string) error {
	if !s.enabled {
		return ErrDisabled
	}
	resp, err := s.post(ctx, "/oauth/revoke", url.Values{"client_id": {s.cfg.ClientID}, "client_secret": {s.cfg.ClientSecret}, "token": {token}})
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitLab answered %d revoking the connection", resp.StatusCode)
	}
	return nil
}

func (s *service) post(ctx context.Context, p string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.base+p, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agentrq-gitlab-app")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("reach GitLab: %w", err)
	}
	return resp, nil
}
