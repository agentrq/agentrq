// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestCommandLineQuotesOnlyWhatNeedsIt(t *testing.T) {
	got := CommandLine("/home/me/My Projects/app", []string{"claude", "--name", "ops", "server:agentrq-workspace", "it's", ""})
	want := `$ cd '/home/me/My Projects/app' && claude --name ops server:agentrq-workspace 'it'\''s' ''`
	if got != want {
		t.Errorf("CommandLine = %q, want %q", got, want)
	}
}

// The terminal says what the agent runs as, in the folder it runs in.
func TestALaunchNamesItsCommand(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	sess, err := s.Start(t.Context(), "work", claudeRequest(t, 1))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	spec, _ := st.last()
	if got, want := sess.Notices(), []string{CommandLine(spec.Dir, spec.Argv)}; !slices.Equal(got, want) {
		t.Errorf("notices = %q, want %q", got, want)
	}
}

// A fork says how its folder was made, then what runs in it; a relaunch says
// the folder was reused.
func TestAForkNamesHowItsFolderWasMade(t *testing.T) {
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	s.Home = t.TempDir()
	from := t.TempDir()
	target := filepath.Join(s.Home, ".agentrq", "forks", forkID)

	sess, err := s.Start(t.Context(), "work", forkRequest(t, 1, from))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	spec, _ := st.last()
	want := []string{
		"copying " + quoteArg(from) + " to " + quoteArg(target) + ", which is not in a git repository",
		CommandLine(target, spec.Argv),
	}
	if got := sess.Notices(); !slices.Equal(got, want) {
		t.Errorf("first launch notices = %q, want %q", got, want)
	}

	if err := s.Kill(1); err != nil {
		t.Fatal(err)
	}
	<-sess.Ended()
	if err := s.Forget(1); err != nil {
		t.Fatal(err)
	}
	again, err := s.Start(t.Context(), "work", forkRequest(t, 1, from))
	if err != nil {
		t.Fatalf("relaunch: %v", err)
	}
	spec, _ = st.last()
	// Its first launch wrote the entry the relaunch keeps, which says so in
	// between.
	got := again.Notices()
	if len(got) < 2 || got[0] != "reusing this fork's folder "+quoteArg(target) || got[len(got)-1] != CommandLine(target, spec.Argv) {
		t.Errorf("relaunch notices = %q, want the reused folder first and the command last", got)
	}
}
