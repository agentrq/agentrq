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

// devChannelPrompt is what Claude Code 2.1.332 printed on a 100×30 terminal
// for `--dangerously-load-development-channels server:devtest`, as recorded.
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
		{"the recorded warning", 100, recorded, []string{"server:devtest"}, true},
		{"wrapped on a narrow terminal", 40,
			[]byte("WARNING: Loading development channels\r\nChannels: server:a-very-long-workspace-server-name\r\n❯ 1. I am using this for local development\r\n  2. Exit"),
			[]string{"server:a-very-long-workspace-server-name"}, true},
		{"several channels", 100,
			bytes.Replace(recorded, []byte("server:devtest"), []byte("server:devtest, plugin:x@y"), 1),
			[]string{"server:devtest", "plugin:x@y"}, true},
		{"Exit selected", 100,
			[]byte("WARNING: Loading development channels\r\nChannels: server:devtest\r\n  1. I am using this for local development\r\n❯ 2. Exit"),
			[]string{"server:devtest"}, false},
		{"no channel list", 100,
			[]byte("WARNING: Loading development channels\r\n❯ 1. I am using this for local development"), nil, false},
		{"not the warning", 100,
			[]byte("Channels: server:devtest\r\n❯ 1. I am using this for local development"), []string{"server:devtest"}, false},
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
