// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import "testing"

// The caps are flags with the constants as their defaults, so the numbers a
// machine runs with are a property of that machine rather than of the build.
func TestSessionCaps(t *testing.T) {
	t.Run("takes what was asked for", func(t *testing.T) {
		profile, machine, err := sessionCaps(4, 9)
		if err != nil {
			t.Fatalf("sessionCaps: %v", err)
		}
		if profile != 4 || machine != 9 {
			t.Errorf("caps = %d, %d; want 4, 9", profile, machine)
		}
	})

	// Zero is what the supervisor already reads as "no cap", and it is a real
	// answer for a machine somebody owns outright.
	t.Run("zero means no limit", func(t *testing.T) {
		profile, machine, err := sessionCaps(0, 0)
		if err != nil {
			t.Fatalf("sessionCaps: %v", err)
		}
		if profile != 0 || machine != 0 {
			t.Errorf("caps = %d, %d; want 0, 0", profile, machine)
		}
	})

	// A negative is not an answer to anything, and the supervisor's own test
	// for a cap is `> 0` — so it would quietly read as "unlimited", which is
	// the opposite of what somebody typing -1 is reaching for.
	t.Run("a negative is refused rather than read as unlimited", func(t *testing.T) {
		if _, _, err := sessionCaps(-1, 8); err == nil {
			t.Error("a negative per-profile cap was accepted")
		}
		if _, _, err := sessionCaps(8, -1); err == nil {
			t.Error("a negative machine cap was accepted")
		}
	})
}

// The defaults are what somebody gets without saying anything, and they are
// the numbers the docs and the usage text promise.
func TestDefaultCaps(t *testing.T) {
	if sessionsPerProfile != 8 || sessionsPerMachine != 16 {
		t.Errorf("defaults = %d, %d; want 8, 16", sessionsPerProfile, sessionsPerMachine)
	}
}
