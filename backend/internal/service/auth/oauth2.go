// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"regexp"

	"golang.org/x/oauth2"
)

// OAuth2Provider is a sign-in provider the operator adds on the admin page,
// besides Google and GitHub: any OAuth2 server whose userinfo endpoint
// answers with the OpenID Connect claims.
type OAuth2Provider struct {
	// ID names the provider in its sign-in URLs: /auth/oauth2/<id>/login.
	ID           string   `yaml:"id"`
	Name         string   `yaml:"name"`
	ClientID     string   `yaml:"clientId"`
	ClientSecret string   `yaml:"clientSecret"`
	AuthURL      string   `yaml:"authUrl"`
	TokenURL     string   `yaml:"tokenUrl"`
	UserInfoURL  string   `yaml:"userInfoUrl"`
	Scopes       []string `yaml:"scopes"`
	// IconURL is shown on the sign-in button; empty shows a generic one.
	IconURL string `yaml:"iconUrl"`
}

var oauth2ProviderID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// CheckOAuth2Providers refuses a provider the server could not sign anyone
// in with, naming the setting at fault.
func CheckOAuth2Providers(providers []OAuth2Provider) error {
	seen := map[string]bool{}
	for i, p := range providers {
		at := fmt.Sprintf("auth.oauth2Providers[%d]", i)
		if !oauth2ProviderID.MatchString(p.ID) {
			return fmt.Errorf("%s.id: %q must be 1-32 lowercase letters, digits or '-'", at, p.ID)
		}
		if seen[p.ID] {
			return fmt.Errorf("%s.id: %q is used by another provider", at, p.ID)
		}
		seen[p.ID] = true
		for _, f := range [][2]string{{"name", p.Name}, {"clientId", p.ClientID}, {"clientSecret", p.ClientSecret}} {
			if f[1] == "" {
				return fmt.Errorf("%s.%s: must be set", at, f[0])
			}
		}
		for _, f := range [][2]string{{"authUrl", p.AuthURL}, {"tokenUrl", p.TokenURL}, {"userInfoUrl", p.UserInfoURL}} {
			if !secureURL(f[1]) {
				return fmt.Errorf("%s.%s: %q must be an https URL (http only for localhost)", at, f[0], f[1])
			}
		}
		if u, err := url.Parse(p.IconURL); p.IconURL != "" && (err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "") {
			return fmt.Errorf("%s.iconUrl: %q must be an http(s) URL", at, p.IconURL)
		}
	}
	return nil
}

// secureURL is an https URL, or an http one on the loopback interface, where
// a provider under test runs. Codes and tokens cross these.
func secureURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && ip.IsLoopback())
}

type oauth2Service struct {
	id          string
	config      *oauth2.Config
	userInfoURL string
}

// NewOAuth2 signs in with p, which the server has checked with
// CheckOAuth2Providers.
func NewOAuth2(p OAuth2Provider, redirectURL string) Service {
	return &oauth2Service{
		id:          p.ID,
		userInfoURL: p.UserInfoURL,
		config: &oauth2.Config{
			ClientID:     p.ClientID,
			ClientSecret: p.ClientSecret,
			RedirectURL:  redirectURL,
			Scopes:       p.Scopes,
			Endpoint:     oauth2.Endpoint{AuthURL: p.AuthURL, TokenURL: p.TokenURL},
		},
	}
}

func (s *oauth2Service) GetAuthURL(state string) string {
	return s.config.AuthCodeURL(state)
}

func (s *oauth2Service) Exchange(ctx context.Context, code string) (*User, error) {
	token, err := s.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("%s oauth exchange: %w", s.id, err)
	}
	resp, err := s.config.Client(ctx, token).Get(s.userInfoURL)
	if err != nil {
		return nil, fmt.Errorf("%s get userinfo: %w", s.id, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s userinfo status: %d", s.id, resp.StatusCode)
	}

	var info struct {
		Sub               string `json:"sub"`
		Email             string `json:"email"`
		EmailVerified     any    `json:"email_verified"`
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		Picture           string `json:"picture"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("%s decode userinfo: %w", s.id, err)
	}
	// Accounts are matched by email: only an address the provider says it
	// has verified signs anyone in. Some send the claim as a string.
	if info.Email == "" || (info.EmailVerified != true && info.EmailVerified != "true") {
		return nil, ErrEmailNotVerified
	}

	name := info.Name
	if name == "" {
		name = info.PreferredUsername
	}
	if name == "" {
		name = info.Email
	}
	return &User{
		ID:      s.id + ":" + info.Sub,
		Sub:     info.Sub,
		Email:   info.Email,
		Name:    name,
		Picture: info.Picture,
	}, nil
}
