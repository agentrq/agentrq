// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"context"
	"errors"
)

// ErrEmailNotVerified is returned by Exchange when the provider has no
// verified email address for the account. Accounts are matched by email, so
// such a sign-in is refused rather than trusted.
var ErrEmailNotVerified = errors.New("no verified email address")

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
