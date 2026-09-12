// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package auth

import "context"

type User struct {
	ID      string `json:"id"`
	Sub     string `json:"sub"`
	Email   string `json:"email"`
	Name    string `json:"name"`
	Picture string `json:"picture"`
}

type Service interface {
	GetAuthURL(state string) string
	Exchange(ctx context.Context, code string) (*User, error)
}
