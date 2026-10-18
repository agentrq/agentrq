// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"testing"
)

// claude-code refuses a --session-id that is not a valid UUID.
var uuidPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-5[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestConversationIDIsAStableUUIDPerSession(t *testing.T) {
	a, b := ConversationID(7), ConversationID(8)
	if !uuidPattern.MatchString(a) || !uuidPattern.MatchString(b) {
		t.Fatalf("not version 5 UUIDs: %q %q", a, b)
	}
	if a == b {
		t.Error("two sessions share a conversation")
	}
	if ConversationID(7) != a {
		t.Error("the id changed between calls, so a restore would look for the wrong conversation")
	}
}

func saveConversation(t *testing.T, dir string, sessionID uint64) {
	t.Helper()
	project := filepath.Join(dir, "projects", "-home-someone-app")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ConversationID(sessionID)+".jsonl"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestConversationArgs(t *testing.T) {
	claude := t.TempDir()
	saveConversation(t, claude, 7)
	s := New((&recordingStarter{}).start, 0, 0)
	s.ClaudeDir = claude
	id7, id8 := ConversationID(7), ConversationID(8)

	cases := map[string]struct {
		req  Request
		want []string
	}{
		"a launch names its conversation":         {Request{ID: 7, Kind: KindClaudeCode}, []string{"--session-id", id7}},
		"a restore resumes a saved conversation":  {Request{ID: 7, Kind: KindClaudeCode, Resume: true}, []string{"--resume", id7}},
		"a restore with nothing saved starts one": {Request{ID: 8, Kind: KindClaudeCode, Resume: true}, []string{"--session-id", id8}},
		"the gateway has no conversation to name": {Request{ID: 7, Kind: KindACPGateway, Resume: true}, nil},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			if got := s.conversationArgs(c.req); !slices.Equal(got, c.want) {
				t.Errorf("args = %v, want %v", got, c.want)
			}
		})
	}
}

// Where claude-code keeps them unless told otherwise, and where it is told.
func TestConversationsAreLookedForWhereClaudeCodeKeepsThem(t *testing.T) {
	s := New((&recordingStarter{}).start, 0, 0)

	config := t.TempDir()
	saveConversation(t, config, 7)
	t.Setenv("CLAUDE_CONFIG_DIR", config)
	if !s.conversationSaved(ConversationID(7)) {
		t.Error("not found under $CLAUDE_CONFIG_DIR")
	}

	home := t.TempDir()
	saveConversation(t, filepath.Join(home, ".claude"), 7)
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if !s.conversationSaved(ConversationID(7)) {
		t.Error("not found under ~/.claude")
	}
	if s.conversationSaved(ConversationID(8)) {
		t.Error("found a conversation that was never saved")
	}

	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	t.Setenv("home", "")
	if s.conversationSaved(ConversationID(7)) {
		t.Error("found a conversation with no home to keep it in")
	}
}

// The process is started with them, not just told about them.
func TestAClaudeCodeLaunchStartsItsConversation(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.ClaudeDir = t.TempDir()
	if _, err := s.Start(t.Context(), "work", claudeRequest(t, 7)); err != nil {
		t.Fatal(err)
	}
	spec, _ := st.last()
	if !slices.Equal(spec.Argv[len(spec.Argv)-2:], []string{"--session-id", ConversationID(7)}) {
		t.Errorf("argv = %v", spec.Argv)
	}
}
