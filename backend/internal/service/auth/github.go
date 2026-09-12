// Copyright 2026 Contextual, Inc. https://agentrq.com

package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/github"
)

type githubService struct {
	config *oauth2.Config
}

// NewGitHub creates a GitHub OAuth service implementing the same Service interface.
func NewGitHub(clientID, clientSecret, redirectURL string) Service {
	return &githubService{
		config: &oauth2.Config{
			ClientID:     clientID,
			ClientSecret: clientSecret,
			RedirectURL:  redirectURL,
			Scopes:       []string{"user:email", "read:user"},
			Endpoint:     github.Endpoint,
		},
	}
}

func (s *githubService) GetAuthURL(state string) string {
	return s.config.AuthCodeURL(state)
}

func (s *githubService) Exchange(ctx context.Context, code string) (*User, error) {
	token, err := s.config.Exchange(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("github oauth exchange: %w", err)
	}

	client := s.config.Client(ctx, token)

	user, err := s.fetchUser(client)
	if err != nil {
		return nil, err
	}

	if user.Email == "" {
		email, err := s.fetchPrimaryEmail(client)
		if err != nil {
			return nil, err
		}
		user.Email = email
	}

	return user, nil
}

func (s *githubService) fetchUser(client *http.Client) (*User, error) {
	resp, err := client.Get("https://api.github.com/user")
	if err != nil {
		return nil, fmt.Errorf("github get user: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("github user status: %d", resp.StatusCode)
	}

	var gh struct {
		ID        int64  `json:"id"`
		Login     string `json:"login"`
		Name      string `json:"name"`
		Email     string `json:"email"`
		AvatarURL string `json:"avatar_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&gh); err != nil {
		return nil, fmt.Errorf("github decode user: %w", err)
	}

	name := gh.Name
	if name == "" {
		name = gh.Login
	}

	return &User{
		ID:      fmt.Sprintf("github:%d", gh.ID),
		Email:   gh.Email,
		Name:    name,
		Picture: gh.AvatarURL,
	}, nil
}

func (s *githubService) fetchPrimaryEmail(client *http.Client) (string, error) {
	resp, err := client.Get("https://api.github.com/user/emails")
	if err != nil {
		return "", fmt.Errorf("github get emails: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("github emails status: %d", resp.StatusCode)
	}

	var emails []struct {
		Email    string `json:"email"`
		Primary  bool   `json:"primary"`
		Verified bool   `json:"verified"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&emails); err != nil {
		return "", fmt.Errorf("github decode emails: %w", err)
	}

	for _, e := range emails {
		if e.Primary && e.Verified {
			return e.Email, nil
		}
	}
	for _, e := range emails {
		if e.Primary {
			return e.Email, nil
		}
	}
	if len(emails) > 0 {
		return emails[0].Email, nil
	}

	return "", fmt.Errorf("no email found in github account")
}
