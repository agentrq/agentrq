// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package guard holds the checks the daemon makes about itself before it will
// do anything.
package guard

import (
	"errors"
	"fmt"
)

// ErrPrivileged is returned when the daemon is running with more privilege
// than it should ever need.
var ErrPrivileged = errors.New("guard: refusing to run with administrative privilege")

// CheckPrivilege reports whether it is acceptable to run.
//
// The daemon spawns agent processes and accepts keystrokes from a remote
// control panel, so whatever privilege it holds is privilege that a compromised
// account holds too. Running it as root would take a bounded problem — "someone
// can run commands as this user" — and make it unbounded.
//
// Refusing is a deliberate inconvenience. An agent that genuinely needs
// elevation should get it explicitly, for that command, in a way somebody chose
// — not inherit it from a long-lived daemon that happened to be started with
// sudo because that made an installation error go away.
//
// The platform and the uid are parameters rather than read here, so every
// branch can be tested on every machine. Being root is the one condition a test
// cannot arrange for itself, and a rule about privilege that is only exercised
// on one operating system is a rule with an untested half.
func CheckPrivilege(goos string, uid int, isWindowsAdmin bool) error {
	if goos == "windows" {
		if isWindowsAdmin {
			return fmt.Errorf("%w: started as Administrator", ErrPrivileged)
		}
		return nil
	}
	if uid == 0 {
		return fmt.Errorf("%w: started as root (uid 0)", ErrPrivileged)
	}
	return nil
}

// PrivilegeAdvice is what to tell someone who hit the refusal.
//
// An error that only says no leaves people reaching for the nearest way around
// it, which here is usually worse than what they were trying to do.
const PrivilegeAdvice = `Run agentrqd as the user whose work it should do — the one whose
checkouts, toolchains and credentials the agents need. If it was
installed as a system service, set User= in the unit file rather
than removing this check.`
