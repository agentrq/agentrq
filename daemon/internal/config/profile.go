// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package config holds the daemon's on-disk state: which accounts it is
// enrolled with, and where each one lives.
//
// The rules here are deliberately the same ones the desktop app already
// settled, in desktop/src/main/profiles.js. Its defining sentence carries over
// unchanged:
//
//	A profile is an account. [...] Each profile carries its own server URL [...]
//	because an account exists on one server: "which account" and "which server"
//	are the same question.
//
// Splitting those two would let a daemon hold credentials for one account and
// point them at a different server, where they mean nothing. So a profile is
// (server URL, machine id, token) and the three travel together.
//
// Everything in this file is pure. The parts that touch a keychain or a socket
// live elsewhere, so these rules can be tested without either.
package config

import (
	"fmt"
	"regexp"
	"strings"
)

// DefaultLabel is shown for a profile that has no name of its own.
const DefaultLabel = "Default"

// MaxLabel is the longest label kept. Anything more is a paste accident rather
// than a name.
const MaxLabel = 40

// idPattern is deliberately narrow because an id becomes a directory name under
// the config directory — the same reasoning, and the same expression, as the
// desktop app's isValidProfileId.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// Profile is one account this daemon is enrolled with.
//
// The token is not here. It lives in the OS keychain where there is one, and
// the config file holds only the reference — a config file that is a credential
// is a config file nobody can safely copy, back up or paste into a bug report.
type Profile struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	ServerURL string `json:"serverUrl"`
	MachineID string `json:"machineId,omitempty"`
	// Insecure records that this profile was enrolled against a plain-HTTP
	// server with --insecure. Stored so that every later start can say so:
	// a warning printed once, at enrolment, is not a safeguard.
	Insecure bool `json:"insecure,omitempty"`
}

// ValidID reports whether id is usable as a profile id and a directory name.
func ValidID(id string) bool { return idPattern.MatchString(id) }

// CleanLabel normalises a label, falling back when there is nothing usable.
func CleanLabel(label string) string {
	trimmed := strings.Join(strings.Fields(label), " ")
	if trimmed == "" {
		return DefaultLabel
	}
	if len(trimmed) > MaxLabel {
		// Cut on a rune boundary: slicing bytes would split a multi-byte
		// character and leave an invalid string in a config file.
		runes := []rune(trimmed)
		if len(runes) > MaxLabel {
			return string(runes[:MaxLabel])
		}
	}
	return trimmed
}

// File is the whole of the daemon's persisted configuration.
type File struct {
	Version  int       `json:"version"`
	Profiles []Profile `json:"profiles"`
}

// Version is the current shape of the config file.
const Version = 1

// Errors from operations on a config file.
var (
	ErrNoProfile   = fmt.Errorf("config: no such profile")
	ErrDuplicateID = fmt.Errorf("config: profile already exists")
	ErrBadID       = fmt.Errorf("config: invalid profile id")
)

// Find returns the profile with the given id.
func (f File) Find(id string) (Profile, error) {
	for _, p := range f.Profiles {
		if p.ID == id {
			return p, nil
		}
	}
	return Profile{}, fmt.Errorf("%w: %q", ErrNoProfile, id)
}

// Add appends a profile, refusing a duplicate or an unusable id.
func (f File) Add(p Profile) (File, error) {
	if !ValidID(p.ID) {
		return f, fmt.Errorf("%w: %q", ErrBadID, p.ID)
	}
	if _, err := f.Find(p.ID); err == nil {
		return f, fmt.Errorf("%w: %q", ErrDuplicateID, p.ID)
	}
	p.Label = CleanLabel(p.Label)
	out := File{Version: Version, Profiles: append(append([]Profile(nil), f.Profiles...), p)}
	return out, nil
}

// Replace swaps an existing profile for an updated one, matched on id.
//
// Used by re-enrolment, which must be idempotent: running `agentrqd enroll`
// again on an already-enrolled machine refreshes its token rather than leaving
// a second, half-working profile behind.
func (f File) Replace(p Profile) (File, error) {
	for i, existing := range f.Profiles {
		if existing.ID != p.ID {
			continue
		}
		p.Label = CleanLabel(p.Label)
		out := File{Version: Version, Profiles: append([]Profile(nil), f.Profiles...)}
		out.Profiles[i] = p
		return out, nil
	}
	return f, fmt.Errorf("%w: %q", ErrNoProfile, p.ID)
}

// Remove drops a profile.
func (f File) Remove(id string) (File, error) {
	for i, p := range f.Profiles {
		if p.ID != id {
			continue
		}
		out := File{Version: Version, Profiles: append([]Profile(nil), f.Profiles[:i]...)}
		out.Profiles = append(out.Profiles, f.Profiles[i+1:]...)
		return out, nil
	}
	return f, fmt.Errorf("%w: %q", ErrNoProfile, id)
}

// Migrate brings a stored config forward from any shape, including none.
//
// A daemon enrolled before profiles existed had a single server and a single
// machine id at the top level. That becomes one profile called "default",
// exactly as the desktop app's migrateProfiles does for a pre-profiles config —
// so an upgrade is not a re-enrolment.
func Migrate(raw File, legacy LegacyFields) File {
	out := File{Version: Version, Profiles: nil}
	for _, p := range raw.Profiles {
		if !ValidID(p.ID) {
			// A profile whose id cannot be a directory name cannot be used.
			// Dropping it is better than a config that fails to load at all.
			continue
		}
		p.Label = CleanLabel(p.Label)
		out.Profiles = append(out.Profiles, p)
	}
	if len(out.Profiles) > 0 {
		return out
	}
	if legacy.ServerURL != "" {
		out.Profiles = []Profile{{
			ID:        "default",
			Label:     DefaultLabel,
			ServerURL: legacy.ServerURL,
			MachineID: legacy.MachineID,
		}}
	}
	return out
}

// LegacyFields are the top-level values a pre-profiles config carried.
type LegacyFields struct {
	ServerURL string `json:"serverUrl,omitempty"`
	MachineID string `json:"machineId,omitempty"`
}
