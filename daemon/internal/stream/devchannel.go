// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"strings"
	"unicode"
)

// DevChannelPrompt reports whether Claude Code's "Loading development
// channels" warning is on screen — its title, then its channel list, then its
// first choice, "I am using this for local development", selected — and the
// channels it lists (#1446).
//
// Claude Code wraps a long list onto the rows below with line breaks of its
// own, which [Screen.linesLocked] cannot join, so every row up to the blank one
// after "Channels:" is read as part of it. A layout with no such blank row reads
// the menu as channels too, which no session matches.
func (s *Screen) DevChannelPrompt() (channels []string, ok bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lines := s.linesLocked()
	warned, listed := false, false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		switch {
		case !warned:
			warned = strings.Contains(line, "Loading development channels")
		case !listed:
			if !strings.HasPrefix(line, "Channels:") {
				continue
			}
			listed = true
			channels = channelFields(strings.TrimPrefix(line, "Channels:"))
			for i+1 < len(lines) && lines[i+1] != "" {
				i++
				channels = append(channels, channelFields(lines[i])...)
			}
		case strings.HasPrefix(line, "❯") && strings.Contains(line, "I am using this for local development"):
			return channels, true
		}
	}
	return channels, false
}

// channelFields splits a run of a channel list into its entries.
func channelFields(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || unicode.IsSpace(r) })
}
