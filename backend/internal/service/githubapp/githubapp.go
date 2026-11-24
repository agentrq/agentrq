// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// Package githubapp talks to GitHub as the server's own GitHub App, so that
// skills can be imported from private repositories an account has given the
// app access to.
//
// An installation is believed only when GitHub, asked with the user's own
// token from the install's OAuth step, lists it: the installation_id GitHub's
// redirect carries is written by whoever opens the URL. And no user token is
// kept: each import mints an installation token that reads one repository and
// lapses within the hour.
package githubapp

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	requestTimeout = 30 * time.Second
	// stateTTL is how long a connect may take, choosing repositories on
	// GitHub included.
	stateTTL = 30 * time.Minute
	// maxPages caps the pages of installations read, 100 to a page.
	maxPages = 10
)

var (
	// ErrDisabled is any call while the app is not configured.
	ErrDisabled = errors.New("the GitHub App is not configured on this server")
	// ErrInvalidState is a connect whose state was not made for this account,
	// or is too old.
	ErrInvalidState = errors.New("this GitHub connection link has expired or was started by another account; choose Connect GitHub beside a skill import to start again")
	// ErrInstallationGone is an installation GitHub no longer has.
	ErrInstallationGone = errors.New("the GitHub App is no longer installed there")
	// ErrRepoNotCovered is a repository the installation was not given.
	ErrRepoNotCovered = errors.New("the GitHub App installation cannot read this repository")

	slugPattern = regexp.MustCompile(`^[A-Za-z0-9-]+$`)
	repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)
	pemParts    = regexp.MustCompile(`(?s)^(-----BEGIN [A-Z ]+-----)(.*?)(-----END [A-Z ]+-----)$`)
)

type (
	// Config is the admin's settings for the server's GitHub App.
	Config struct {
		Enabled      bool   `yaml:"enabled"`
		AppID        string `yaml:"appId"`
		Slug         string `yaml:"slug"`
		ClientID     string `yaml:"clientId"`
		ClientSecret string `yaml:"clientSecret"`
		// PrivateKey is the app's PEM key, its line breaks kept, written
		// out as "\n", or lost: it is pasted into a one-line field.
		PrivateKey string `yaml:"privateKey"`
	}

	// Installation is the app installed on one GitHub account.
	Installation struct {
		ID          int64
		Account     string
		AccountType string
	}

	// Service is the GitHub App. Every method but Enabled answers
	// ErrDisabled while it is not configured.
	Service interface {
		Enabled() bool
		// InstallURL is where a person installs the app, or changes which
		// repositories it reads; GitHub brings them back with state.
		InstallURL(ctx context.Context, state string) (string, error)
		// NewState is the state for userID's connect, and CheckState
		// whether state is one.
		NewState(ctx context.Context, userID int64) (string, error)
		CheckState(ctx context.Context, state string, userID int64) error
		// UserInstallations are the app's installations the person who
		// authorised code can reach.
		UserInstallations(ctx context.Context, code string) ([]Installation, error)
		// RepoToken is a token reading owner/repo's contents through
		// installationID, for an hour at most.
		RepoToken(ctx context.Context, installationID int64, repo string) (string, error)
	}

	service struct {
		cfg     Config
		key     *rsa.PrivateKey
		client  *http.Client
		apiBase string
		webBase string
		now     func() time.Time
	}
)

// Check refuses settings the app could not run with. Disabled, anything goes.
func Check(cfg Config) error {
	if !cfg.Enabled {
		return nil
	}
	if _, err := strconv.ParseInt(strings.TrimSpace(cfg.AppID), 10, 64); err != nil {
		return errors.New("githubApp.appId: must be the app's numeric ID")
	}
	if !slugPattern.MatchString(cfg.Slug) {
		return errors.New("githubApp.slug: must be the app's name in its URL, github.com/apps/<slug>")
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return errors.New("githubApp: clientId and clientSecret are required")
	}
	if _, err := parseKey(cfg.PrivateKey); err != nil {
		return fmt.Errorf("githubApp.privateKey: %w", err)
	}
	return nil
}

// parseKey reads the app's PEM key however a one-line field kept it: line
// breaks written out as \n, or lost, as pasting into one does.
func parseKey(pem string) (*rsa.PrivateKey, error) {
	pem = strings.TrimSpace(strings.ReplaceAll(pem, `\n`, "\n"))
	if m := pemParts.FindStringSubmatch(pem); m != nil {
		pem = m[1] + "\n" + strings.Join(strings.Fields(m[2]), "") + "\n" + m[3]
	}
	key, err := jwt.ParseRSAPrivateKeyFromPEM([]byte(pem))
	if err != nil {
		return nil, errors.New("not a PEM-encoded RSA private key")
	}
	return key, nil
}

// New returns the app cfg configures; one disabled, or with settings Check
// refuses, answers every call with ErrDisabled.
func New(cfg Config) Service {
	s := &service{
		client:  &http.Client{Timeout: requestTimeout},
		apiBase: "https://api.github.com",
		webBase: "https://github.com",
		now:     time.Now,
	}
	if Check(cfg) != nil || !cfg.Enabled {
		return s
	}
	s.key, _ = parseKey(cfg.PrivateKey)
	cfg.AppID = strings.TrimSpace(cfg.AppID)
	s.cfg = cfg
	return s
}

func (s *service) Enabled() bool { return s.key != nil }

func (s *service) InstallURL(_ context.Context, state string) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	return s.webBase + "/apps/" + s.cfg.Slug + "/installations/new?state=" + url.QueryEscape(state), nil
}

// A state is <user>.<expiry>.<mac>, the mac keyed by the client secret: only
// this server can make one, and only for the account that started.
func (s *service) NewState(_ context.Context, userID int64) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	payload := strconv.FormatInt(userID, 10) + "." + strconv.FormatInt(s.now().Add(stateTTL).Unix(), 10)
	return payload + "." + s.mac(payload), nil
}

func (s *service) CheckState(_ context.Context, state string, userID int64) error {
	if !s.Enabled() {
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
	m.Write([]byte("agentrq-github-app-state|" + payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s *service) UserInstallations(ctx context.Context, code string) ([]Installation, error) {
	if !s.Enabled() {
		return nil, ErrDisabled
	}
	token, err := s.exchange(ctx, code)
	if err != nil {
		return nil, err
	}
	out := []Installation{}
	for page := 1; page <= maxPages; page++ {
		var body struct {
			Installations []struct {
				ID      int64 `json:"id"`
				AppID   int64 `json:"app_id"`
				Account struct {
					Login string `json:"login"`
					Type  string `json:"type"`
				} `json:"account"`
			} `json:"installations"`
		}
		status, err := s.call(ctx, http.MethodGet, "/user/installations?per_page=100&page="+strconv.Itoa(page), token, nil, &body)
		if err != nil {
			return nil, err
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("GitHub answered %d listing your installations", status)
		}
		for _, in := range body.Installations {
			if strconv.FormatInt(in.AppID, 10) == s.cfg.AppID && in.Account.Login != "" {
				out = append(out, Installation{ID: in.ID, Account: in.Account.Login, AccountType: in.Account.Type})
			}
		}
		if len(body.Installations) < 100 {
			break
		}
	}
	return out, nil
}

// exchange turns the install's OAuth code into the person's token, which is
// used once, to list their installations, and then dropped.
func (s *service) exchange(ctx context.Context, code string) (string, error) {
	if code == "" {
		return "", errors.New("GitHub did not send an authorization code; turn on \"Request user authorization (OAuth) during installation\" in the GitHub App's settings")
	}
	form := url.Values{"client_id": {s.cfg.ClientID}, "client_secret": {s.cfg.ClientSecret}, "code": {code}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webBase+"/login/oauth/access_token", strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "agentrq-github-app")
	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil || resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("GitHub answered %d to the authorization code", resp.StatusCode)
	}
	if body.AccessToken == "" {
		return "", fmt.Errorf("GitHub refused the authorization code (%s); choose Connect GitHub beside a skill import to start again", body.Error)
	}
	return body.AccessToken, nil
}

func (s *service) RepoToken(ctx context.Context, installationID int64, repo string) (string, error) {
	if !s.Enabled() {
		return "", ErrDisabled
	}
	if installationID <= 0 || !repoPattern.MatchString(repo) {
		return "", ErrRepoNotCovered
	}
	now := s.now()
	appJWT, err := jwt.NewWithClaims(jwt.SigningMethodRS256, jwt.RegisteredClaims{
		Issuer: s.cfg.AppID,
		// GitHub allows for clock drift a minute back, and ten at most ahead.
		IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		ExpiresAt: jwt.NewNumericDate(now.Add(9 * time.Minute)),
	}).SignedString(s.key)
	if err != nil {
		return "", err
	}
	reqBody, _ := json.Marshal(map[string]any{
		"repositories": []string{repo},
		"permissions":  map[string]string{"contents": "read", "metadata": "read"},
	})
	var body struct {
		Token string `json:"token"`
	}
	status, err := s.call(ctx, http.MethodPost, "/app/installations/"+strconv.FormatInt(installationID, 10)+"/access_tokens", appJWT, reqBody, &body)
	switch {
	case err != nil:
		return "", err
	case status == http.StatusNotFound:
		return "", ErrInstallationGone
	case status == http.StatusUnprocessableEntity:
		return "", ErrRepoNotCovered
	case status != http.StatusCreated || body.Token == "":
		return "", fmt.Errorf("GitHub answered %d minting a token for the GitHub App", status)
	}
	return body.Token, nil
}

// call sends one API request with a bearer token, and decodes a 2xx answer
// into out.
func (s *service) call(ctx context.Context, method, p, token string, body []byte, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, method, s.apiBase+p, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "agentrq-github-app")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("reach GitHub: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		if err := json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out); err != nil {
			return 0, fmt.Errorf("GitHub sent an answer that could not be read: %w", err)
		}
	}
	return resp.StatusCode, nil
}
