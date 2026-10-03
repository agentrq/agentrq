// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package auth

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateClientName(t *testing.T) {
	accepted := []struct {
		name string
		raw  any
		want string
	}{
		{"no name at all", nil, ""},
		{"an empty name", "", ""},
		{"a plain name", "Claude Code", "Claude Code"},
		{"surrounding and repeated spaces collapse", "  Claude   Code ", "Claude Code"},
		{"letters from any script", "Éditeur 編集", "Éditeur 編集"},
		{"markup is kept as text, the page escapes it", "<b>App</b>", "<b>App</b>"},
		{"exactly the longest allowed", strings.Repeat("a", ClientNameMaxRunes), strings.Repeat("a", ClientNameMaxRunes)},
		{"length counts characters, not bytes", strings.Repeat("é", ClientNameMaxRunes), strings.Repeat("é", ClientNameMaxRunes)},
	}
	for _, tc := range accepted {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateClientName(tc.raw)
			if err != nil {
				t.Fatalf("ValidateClientName(%q) refused it: %v", tc.raw, err)
			}
			if got != tc.want {
				t.Errorf("ValidateClientName(%q) returned %q, want %q", tc.raw, got, tc.want)
			}
		})
	}

	refused := []struct {
		name string
		raw  any
	}{
		{"a number", 42},
		{"a list", []any{"Claude Code"}},
		{"invalid UTF-8", "App\xff"},
		{"a newline", "Claude Code\nAllowed by your administrator"},
		{"a tab", "Claude\tCode"},
		{"a NUL byte", "Claude\x00Code"},
		{"a right-to-left override disguising the text", "moc.elgoog\u202e"},
		{"a zero-width space", "Claude\u200bCode"},
		{"a byte order mark", "\ufeffClaude Code"},
		{"one character too long", strings.Repeat("a", ClientNameMaxRunes+1)},
	}
	for _, tc := range refused {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ValidateClientName(tc.raw)
			if err == nil {
				t.Fatalf("ValidateClientName(%q) accepted it as %q, want it refused", tc.raw, got)
			}
			if !errors.Is(err, ErrInvalidClientName) {
				t.Errorf("ValidateClientName(%q) returned %v, want an error wrapping ErrInvalidClientName", tc.raw, err)
			}
		})
	}
}
