// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package machine

import (
	"strings"
	"testing"
	"time"
)

func TestNewEnrolmentCodeShape(t *testing.T) {
	seen := map[string]bool{}
	for i := 0; i < 200; i++ {
		code, err := NewEnrolmentCode()
		if err != nil {
			t.Fatalf("NewEnrolmentCode: %v", err)
		}
		if len(code) != CodeLength+1 || code[CodeLength/2] != '-' {
			t.Fatalf("code %q is not the shape a person expects to type", code)
		}
		for _, r := range strings.ReplaceAll(code, "-", "") {
			if !strings.ContainsRune(codeAlphabet, r) {
				t.Fatalf("code %q contains %q, which is not in the alphabet", code, r)
			}
		}
		if seen[code] {
			t.Fatalf("NewEnrolmentCode repeated %q within 200 draws", code)
		}
		seen[code] = true
	}
}

// Excluding the confusable characters is the point of the alphabet. A code
// containing O and 0, or 1 and l, generates a support conversation that starts
// "it says my code is wrong".
func TestCodeAlphabetOmitsConfusableCharacters(t *testing.T) {
	for _, r := range "01OILU" {
		if strings.ContainsRune(codeAlphabet, r) {
			t.Errorf("alphabet contains the confusable character %q", r)
		}
	}
}

// People paste codes with spaces, type them lowercase, and leave the hyphen
// out. All three should work.
func TestNormalizeCodeAcceptsHowPeopleActuallyType(t *testing.T) {
	want := "ABCD-2345"
	for _, typed := range []string{
		"ABCD-2345", "abcd-2345", "  ABCD-2345  ", "ABCD2345", "abcd 2345",
		"AB CD-23 45", "\tabcd-2345\n",
	} {
		if got := NormalizeCode(typed); got != want {
			t.Errorf("NormalizeCode(%q) = %q, want %q", typed, got, want)
		}
	}
}

// Padding or truncating a wrong-length code would make it silently become a
// different code.
func TestNormalizeCodeLeavesWrongLengthsAlone(t *testing.T) {
	for _, typed := range []string{"", "ABC", "ABCD-23456789"} {
		got := NormalizeCode(typed)
		if len(got) == CodeLength+1 && strings.Contains(got, "-") {
			t.Errorf("NormalizeCode(%q) = %q — a wrong-length code was made to look valid", typed, got)
		}
	}
}

func TestMachineTokenIsLongRandomAndHashed(t *testing.T) {
	token, hash, err := NewMachineToken()
	if err != nil {
		t.Fatalf("NewMachineToken: %v", err)
	}
	// 32 bytes, base64url without padding.
	if len(token) < 40 {
		t.Errorf("token is only %d characters: %q", len(token), token)
	}
	if strings.ContainsAny(token, "+/=") {
		t.Errorf("token is not URL-safe: %q", token)
	}
	if hash == token {
		t.Fatal("the stored hash is the token itself")
	}
	if len(hash) != 64 {
		t.Errorf("hash is %d characters, want 64 hex", len(hash))
	}
	if !SecretMatches(hash, token) {
		t.Error("the token does not match its own hash")
	}

	other, _, err := NewMachineToken()
	if err != nil {
		t.Fatal(err)
	}
	if other == token {
		t.Fatal("two tokens came out identical")
	}
	if SecretMatches(hash, other) {
		t.Error("a different token matched the hash")
	}
}

func TestSecretMatchesRefusesNonsense(t *testing.T) {
	_, hash, err := NewMachineToken()
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name              string
		stored, presented string
	}{
		{"empty stored", "", "anything"},
		{"empty presented", hash, ""},
		{"both empty", "", ""},
		{"stored is not hex", "zzzz", "anything"},
		{"wrong secret", hash, "not-the-token"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if SecretMatches(tc.stored, tc.presented) {
				t.Error("SecretMatches returned true")
			}
		})
	}
}

// A stored flag says "online" forever when a daemon is killed — which is
// exactly the moment the answer matters.
func TestIsOnlineIsDerivedFromTheLastHeartbeat(t *testing.T) {
	now := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name     string
		lastSeen time.Time
		want     bool
	}{
		{"just now", now, true},
		{"within the threshold", now.Add(-OnlineThreshold + time.Second), true},
		{"exactly at the threshold", now.Add(-OnlineThreshold), true},
		{"past the threshold", now.Add(-OnlineThreshold - time.Second), false},
		{"long ago", now.Add(-24 * time.Hour), false},
		{"never connected", time.Time{}, false},
		// Clock skew: the machine plainly just spoke to us, so online is the
		// safe reading.
		{"from the future", now.Add(time.Minute), true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsOnline(tc.lastSeen, now, OnlineThreshold); got != tc.want {
				t.Errorf("IsOnline(%v) = %v, want %v", tc.lastSeen, got, tc.want)
			}
		})
	}
}

// Comfortably more than the heartbeat interval, so one dropped beat on a busy
// machine does not flap the UI between online and offline.
func TestOnlineThresholdLeavesRoomForADroppedBeat(t *testing.T) {
	const maxHeartbeatInterval = 30 * time.Second
	if OnlineThreshold < 2*maxHeartbeatInterval {
		t.Errorf("OnlineThreshold %v is under two heartbeat intervals — the UI will flap", OnlineThreshold)
	}
}

func TestCodeTTLIsShort(t *testing.T) {
	// This is the one moment a credential crosses a human's hands. A code left
	// in scrollback should be useless by the time anyone finds it.
	if CodeTTL > 15*time.Minute {
		t.Errorf("CodeTTL = %v, which is long enough to be worth stealing", CodeTTL)
	}
}
