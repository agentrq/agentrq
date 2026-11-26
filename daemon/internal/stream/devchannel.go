// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"strings"
	"unicode"
)

// DevChannelPrompt reports whether Claude Code's "Loading development
// channels" warning is on screen with its first choice, "I am using this for
// local development", selected, and the channels it lists (#1446).
func (s *Screen) DevChannelPrompt() (channels []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var warned, selected, listed bool
	for _, line := range s.linesLocked() {
		switch {
		case strings.Contains(line, "Loading development channels"):
			warned = true
		case strings.HasPrefix(line, "Channels:"):
			listed = true
			channels = strings.FieldsFunc(strings.TrimPrefix(line, "Channels:"), func(r rune) bool {
				return r == ',' || unicode.IsSpace(r)
			})
		case strings.HasPrefix(line, "❯") && strings.Contains(line, "I am using this for local development"):
			selected = true
		}
	}
	return channels, warned && listed && selected
}
