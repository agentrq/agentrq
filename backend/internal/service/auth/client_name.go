// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ClientNameMaxRunes caps the name the consent page shows, so a client cannot
// push the rest of the page out of view with a long one.
const ClientNameMaxRunes = 80

// ErrInvalidClientName is wrapped by every error ValidateClientName returns.
var ErrInvalidClientName = errors.New("invalid client_name")

// ValidateClientName checks a client's self-declared RFC 7591 "client_name"
// before the consent page shows it, and returns it with its whitespace
// collapsed, or "" when the client gave none.
//
// A bad name is refused rather than cleaned up. Control and invisible format
// characters have no place in a name, and one of them is an attack: a
// right-to-left override (U+202E) makes "moc.elgoog" read as "google.com".
func ValidateClientName(raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	name, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: must be a string", ErrInvalidClientName)
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidClientName)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", fmt.Errorf("%w: must not contain control or invisible formatting characters", ErrInvalidClientName)
		}
	}
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > ClientNameMaxRunes {
		return "", fmt.Errorf("%w: must be at most %d characters", ErrInvalidClientName, ClientNameMaxRunes)
	}
	return name, nil
}
