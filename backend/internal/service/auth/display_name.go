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

// DisplayNameMaxRunes caps a name the consent page shows, so it cannot push
// the rest of the page out of view.
const DisplayNameMaxRunes = 80

// ErrInvalidDisplayName is wrapped by every error ValidateDisplayName returns.
var ErrInvalidDisplayName = errors.New("invalid name")

// ValidateDisplayName checks a name before the consent page shows it — a
// client's self-declared RFC 7591 "client_name", or a workspace's name — and
// returns it with its whitespace collapsed, or "" when there is none.
//
// A bad name is refused rather than cleaned up. Control and invisible format
// characters have no place in a name, and one of them is an attack: a
// right-to-left override (U+202E) makes "moc.elgoog" read as "google.com".
func ValidateDisplayName(raw any) (string, error) {
	if raw == nil {
		return "", nil
	}
	name, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("%w: must be a string", ErrInvalidDisplayName)
	}
	if !utf8.ValidString(name) {
		return "", fmt.Errorf("%w: must be valid UTF-8", ErrInvalidDisplayName)
	}
	for _, r := range name {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return "", fmt.Errorf("%w: must not contain control or invisible formatting characters", ErrInvalidDisplayName)
		}
	}
	name = strings.Join(strings.Fields(name), " ")
	if utf8.RuneCountInString(name) > DisplayNameMaxRunes {
		return "", fmt.Errorf("%w: must be at most %d characters", ErrInvalidDisplayName, DisplayNameMaxRunes)
	}
	return name, nil
}
