// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package guard

import (
	"errors"
	"strings"
	"testing"
)

func TestCheckPrivilege(t *testing.T) {
	tests := []struct {
		name  string
		goos  string
		uid   int
		admin bool
		want  bool // want a refusal
	}{
		{"unix root", "linux", 0, false, true},
		{"macos root", "darwin", 0, false, true},
		{"unix user", "linux", 1000, false, false},
		{"unix nobody", "linux", 65534, false, false},
		{"windows admin", "windows", 0, true, true},
		{"windows user", "windows", 0, false, false},
		// The Windows flag must not leak into the Unix rule, and the uid must
		// not leak into the Windows one — on Windows every process reports
		// uid 0 through this path, which would refuse everything.
		{"unix user with admin flag", "linux", 1000, true, false},
		{"windows user with uid 0", "windows", 0, false, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := CheckPrivilege(tc.goos, tc.uid, tc.admin)
			if got := errors.Is(err, ErrPrivileged); got != tc.want {
				t.Errorf("CheckPrivilege(%q, %d, %v) refused = %v, want %v",
					tc.goos, tc.uid, tc.admin, got, tc.want)
			}
		})
	}
}

// The message has to name the condition, or the person reads "refused" and
// starts guessing.
func TestRefusalSaysWhich(t *testing.T) {
	if err := CheckPrivilege("linux", 0, false); err == nil || !strings.Contains(err.Error(), "root") {
		t.Errorf("unix refusal = %v, want it to say root", err)
	}
	if err := CheckPrivilege("windows", 0, true); err == nil || !strings.Contains(err.Error(), "Administrator") {
		t.Errorf("windows refusal = %v, want it to say Administrator", err)
	}
}

// An error that only says no leaves people reaching for the nearest way around
// it, which here is usually worse than what they were trying to do.
func TestPrivilegeAdviceOffersTheRightWayRound(t *testing.T) {
	if !strings.Contains(PrivilegeAdvice, "User=") {
		t.Error("advice should name the service-unit fix")
	}
	if strings.Contains(strings.ToLower(PrivilegeAdvice), "sudo agentrqd") {
		t.Error("advice must not suggest running it with sudo")
	}
}
