// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package supervisor

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/agentrq/agentrq/daemon/wire"
)

// Profiles on one machine can belong to different accounts, even different
// servers. Each server sees and drives only its own profile's agents.

func twoProfiles(t *testing.T) (*Supervisor, *recordingStarter) {
	t.Helper()
	st := &recordingStarter{}
	s := New(st.start, 0, 0)
	for _, c := range []struct {
		profile string
		id      uint64
	}{{"work", 1}, {"home", 2}, {"work", 3}} {
		if _, err := s.Start(t.Context(), c.profile, gatewayRequest(t, c.id)); err != nil {
			t.Fatalf("start %d: %v", c.id, err)
		}
	}
	return s, st
}

// A profile names only its own sessions, running or coming back.
func TestRunningNamesOnlyTheProfilesOwn(t *testing.T) {
	s, _ := twoProfiles(t)
	s.Expect("home", 8)
	s.Expect("work", 9)

	if got := s.Running("work"); !slices.Equal(got, []uint64{1, 3, 9}) {
		t.Errorf(`Running("work") = %v, want [1 3 9]`, got)
	}
	if got := s.Running("home"); !slices.Equal(got, []uint64{2, 8}) {
		t.Errorf(`Running("home") = %v, want [2 8]`, got)
	}
	if got := s.Running("elsewhere"); len(got) != 0 {
		t.Errorf(`Running("elsewhere") = %v, want nothing`, got)
	}
}

// A handover keeps each session under its own profile.
func TestAHandoverKeepsEachSessionsProfile(t *testing.T) {
	s, _ := twoProfiles(t)
	s.HandOver(t.Context(), 5*time.Second)

	if got := s.Running("work"); !slices.Equal(got, []uint64{1, 3}) {
		t.Errorf(`Running("work") = %v after the handover, want [1 3]`, got)
	}
	if got := s.Running("home"); !slices.Equal(got, []uint64{2}) {
		t.Errorf(`Running("home") = %v after the handover, want [2]`, got)
	}
	// The id another profile is waiting to restore cannot be taken.
	if _, err := s.Start(t.Context(), "work", gatewayRequest(t, 2)); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("work took home's returning session: %v", err)
	}
	if _, err := s.Start(t.Context(), "home", gatewayRequest(t, 2)); err != nil {
		t.Errorf("home could not restore its own session: %v", err)
	}
}

func TestOwns(t *testing.T) {
	s, _ := twoProfiles(t)
	for _, c := range []struct {
		profile string
		id      uint64
		want    bool
	}{{"work", 1, true}, {"home", 1, false}, {"home", 2, true}, {"work", 99, false}} {
		if got := s.Owns(c.profile, c.id); got != c.want {
			t.Errorf("Owns(%q, %d) = %v, want %v", c.profile, c.id, got, c.want)
		}
	}
}

// Typing into another profile's agent would be driving somebody else's.
func TestInputForAnotherProfilesSessionIsRefused(t *testing.T) {
	s, st := twoProfiles(t)
	home := st.nth(1)

	err := s.HandleFrame("work", inputFrame(t, 2, []byte("rm -rf ~\r")))
	if !errors.Is(err, ErrNoSuchSession) {
		t.Errorf("HandleFrame = %v, want ErrNoSuchSession", err)
	}
	if got := home.writes(); len(got) != 0 {
		t.Errorf("home's terminal received %q from work's server", got)
	}
}

// A kill for another profile's session leaves it running, and is answered as
// for a session that is gone.
func TestAKillForAnotherProfilesSessionLeavesItRunning(t *testing.T) {
	s, _ := twoProfiles(t)
	rep := &recordingReporter{}

	if err := s.Handle(t.Context(), "work", control(t, wire.OpKillSession, wire.KillSession{SessionID: 2}), rep); err != nil {
		t.Fatal(err)
	}
	if got := s.Running("home"); !slices.Equal(got, []uint64{2}) {
		t.Errorf(`Running("home") = %v, want home's session still running`, got)
	}
	if states := rep.all(); len(states) != 1 || states[0].State != string(StateKilled) {
		t.Errorf("states = %+v, want one killed", states)
	}

	// Its own is killed.
	if err := s.Handle(t.Context(), "home", control(t, wire.OpKillSession, wire.KillSession{SessionID: 2}), rep); err != nil {
		t.Fatal(err)
	}
	if got := s.Running("home"); len(got) != 0 {
		t.Errorf(`Running("home") = %v after its own kill, want nothing`, got)
	}
}

// A kill of the profile's own session that fails says so.
func TestAFailedKillOfTheProfilesOwnIsReturned(t *testing.T) {
	s, st := twoProfiles(t)
	home := st.nth(1)
	home.mu.Lock()
	home.closeErr = errors.New("the terminal would not close")
	home.mu.Unlock()

	err := s.Handle(t.Context(), "home", control(t, wire.OpKillSession, wire.KillSession{SessionID: 2}), &recordingReporter{})
	if err == nil {
		t.Error("a kill that failed was reported as done")
	}
}
