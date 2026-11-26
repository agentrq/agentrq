// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package stream

import (
	"bytes"
	"os"
	"slices"
	"testing"
)

// devChannelPrompt is what Claude Code 2.1.332 printed on a 100×30 terminal for
// `--dangerously-load-development-channels server:agentrq-workspace`, as
// recorded.
func devChannelPrompt(t *testing.T) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/devchannel-prompt.bin")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDevChannelPrompt(t *testing.T) {
	recorded := devChannelPrompt(t)
	cases := []struct {
		name   string
		cols   int
		out    []byte
		want   []string
		wantOK bool
	}{
		{"nothing printed", 100, nil, nil, false},
		{"the recorded warning", 100, recorded, []string{"server:agentrq-workspace"}, true},
		{"wrapped by the terminal", 40,
			[]byte("WARNING: Loading development channels\r\nChannels: server:a-very-long-workspace-server-name\r\n\r\n❯ 1. I am using this for local development\r\n  2. Exit"),
			[]string{"server:a-very-long-workspace-server-name"}, true},
		{"wrapped by Claude Code", 40,
			[]byte("WARNING: Loading development channels\r\nChannels: server:agentrq-workspace\r\n          plugin:x@y\r\n\r\n❯ 1. I am using this for local development\r\n  2. Exit"),
			[]string{"server:agentrq-workspace", "plugin:x@y"}, true},
		{"several channels", 100,
			bytes.Replace(recorded, []byte("server:agentrq-workspace"), []byte("server:agentrq-workspace, plugin:x@y"), 1),
			[]string{"server:agentrq-workspace", "plugin:x@y"}, true},
		{"no blank row before the menu", 100,
			[]byte("WARNING: Loading development channels\r\nChannels: server:agentrq-workspace\r\n❯ 1. I am using this for local development"),
			[]string{"server:agentrq-workspace", "❯", "1.", "I", "am", "using", "this", "for", "local", "development"}, false},
		{"Exit selected", 100,
			[]byte("WARNING: Loading development channels\r\nChannels: server:agentrq-workspace\r\n\r\n  1. I am using this for local development\r\n❯ 2. Exit"),
			[]string{"server:agentrq-workspace"}, false},
		{"no channel list", 100,
			[]byte("WARNING: Loading development channels\r\n❯ 1. I am using this for local development"), nil, false},
		{"not the warning", 100,
			[]byte("Channels: server:agentrq-workspace\r\n\r\n❯ 1. I am using this for local development"), nil, false},
		{"out of order", 100,
			[]byte("❯ 1. I am using this for local development\r\nChannels: server:agentrq-workspace\r\n\r\nWARNING: Loading development channels"), nil, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := NewScreen(c.cols, 30)
			if _, err := s.Write(c.out); err != nil {
				t.Fatal(err)
			}
			got, ok := s.DevChannelPrompt()
			if ok != c.wantOK || !slices.Equal(got, c.want) {
				t.Fatalf("DevChannelPrompt() = %q, %v; want %q, %v", got, ok, c.want, c.wantOK)
			}
		})
	}
}
