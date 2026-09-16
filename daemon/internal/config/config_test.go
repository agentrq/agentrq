// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package config

import (
	"errors"
	"strings"
	"testing"
)

func TestValidID(t *testing.T) {
	// The charset is narrow because an id becomes a directory name. These are
	// the desktop app's rules, deliberately unchanged.
	valid := []string{
		"a", "0", "default", "work", "build-box-2",
		"trailing-",             // permitted: harmless as a directory name
		strings.Repeat("a", 64), // the length limit, exactly
	}
	for _, id := range valid {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	invalid := []string{
		"", "-leading", "Work", "has space", "has_underscore", "has/slash",
		"..", ".", // path traversal, and not a name
		strings.Repeat("a", 65), // one past the limit
		"work\n", "nul\x00",
	}
	for _, id := range invalid {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestCleanLabel(t *testing.T) {
	tests := []struct{ in, want string }{
		{"", DefaultLabel},
		{"   ", DefaultLabel},
		{"Work", "Work"},
		{"  spaced   out  ", "spaced out"},
		{"line\nbreak", "line break"},
		{strings.Repeat("x", MaxLabel+10), strings.Repeat("x", MaxLabel)},
	}
	for _, tc := range tests {
		if got := CleanLabel(tc.in); got != tc.want {
			t.Errorf("CleanLabel(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// Slicing bytes would split a multi-byte character and leave an invalid string
// in a config file.
func TestCleanLabelCutsOnRuneBoundaries(t *testing.T) {
	got := CleanLabel(strings.Repeat("é", MaxLabel+10))
	if n := len([]rune(got)); n != MaxLabel {
		t.Errorf("label has %d runes, want %d", n, MaxLabel)
	}
	for _, r := range got {
		if r != 'é' {
			t.Fatalf("label was cut mid-character: %q", got)
		}
	}
}

func TestAddFindRemove(t *testing.T) {
	var f File
	f, err := f.Add(Profile{ID: "work", ServerURL: "https://a"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	f, err = f.Add(Profile{ID: "personal", ServerURL: "https://b"})
	if err != nil {
		t.Fatalf("Add second: %v", err)
	}

	got, err := f.Find("work")
	if err != nil {
		t.Fatalf("Find: %v", err)
	}
	if got.Label != DefaultLabel {
		t.Errorf("label = %q, want the fallback", got.Label)
	}

	if _, err := f.Add(Profile{ID: "work", ServerURL: "https://c"}); !errors.Is(err, ErrDuplicateID) {
		t.Errorf("duplicate Add error = %v, want ErrDuplicateID", err)
	}
	if _, err := f.Add(Profile{ID: "Bad Id", ServerURL: "https://c"}); !errors.Is(err, ErrBadID) {
		t.Errorf("bad id Add error = %v, want ErrBadID", err)
	}

	f, err = f.Remove("work")
	if err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := f.Find("work"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("Find after Remove error = %v, want ErrNoProfile", err)
	}
	if _, err := f.Remove("work"); !errors.Is(err, ErrNoProfile) {
		t.Errorf("Remove twice error = %v, want ErrNoProfile", err)
	}
	if len(f.Profiles) != 1 {
		t.Errorf("profiles remaining = %d, want 1", len(f.Profiles))
	}
}

// Re-enrolment has to be idempotent: running enroll again on an already
// enrolled machine refreshes it rather than leaving a second, half-working
// profile behind.
func TestReplaceIsHowReEnrolmentStaysIdempotent(t *testing.T) {
	var f File
	f, _ = f.Add(Profile{ID: "work", ServerURL: "https://a", MachineID: "m1"})

	f, err := f.Replace(Profile{ID: "work", ServerURL: "https://a", MachineID: "m2"})
	if err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if len(f.Profiles) != 1 {
		t.Fatalf("Replace added a profile: %d", len(f.Profiles))
	}
	got, _ := f.Find("work")
	if got.MachineID != "m2" {
		t.Errorf("machineId = %q, want m2", got.MachineID)
	}

	if _, err := f.Replace(Profile{ID: "nope"}); !errors.Is(err, ErrNoProfile) {
		t.Errorf("Replace unknown error = %v, want ErrNoProfile", err)
	}
}

// An upgrade must not be a re-enrolment.
func TestMigrateTurnsAPreProfilesConfigIntoOne(t *testing.T) {
	out := Migrate(File{}, LegacyFields{ServerURL: "https://app.agentrq.com", MachineID: "m1"})
	if len(out.Profiles) != 1 {
		t.Fatalf("profiles = %d, want 1", len(out.Profiles))
	}
	p := out.Profiles[0]
	if p.ID != "default" || p.ServerURL != "https://app.agentrq.com" || p.MachineID != "m1" {
		t.Errorf("migrated profile = %+v", p)
	}
	if out.Version != Version {
		t.Errorf("version = %d, want %d", out.Version, Version)
	}
}

func TestMigrateLeavesRealProfilesAlone(t *testing.T) {
	in := File{Profiles: []Profile{{ID: "work", ServerURL: "https://a"}}}
	out := Migrate(in, LegacyFields{ServerURL: "https://legacy"})
	if len(out.Profiles) != 1 || out.Profiles[0].ID != "work" {
		t.Errorf("migrate clobbered existing profiles: %+v", out.Profiles)
	}
}

// A config that fails to load at all is worse than one missing an entry that
// could never have worked.
func TestMigrateDropsUnusableIDs(t *testing.T) {
	in := File{Profiles: []Profile{{ID: "ok"}, {ID: "../escape"}, {ID: "Has Space"}}}
	out := Migrate(in, LegacyFields{})
	if len(out.Profiles) != 1 || out.Profiles[0].ID != "ok" {
		t.Errorf("profiles = %+v, want only the usable one", out.Profiles)
	}
}

func TestMigrateOfNothingIsEmptyNotAnError(t *testing.T) {
	out := Migrate(File{}, LegacyFields{})
	if len(out.Profiles) != 0 {
		t.Errorf("profiles = %+v, want none", out.Profiles)
	}
}

func TestResolveServerRequiresTLSForRemoteHosts(t *testing.T) {
	_, err := ResolveServer("http://example.com", false)
	if !errors.Is(err, ErrInsecureHTTP) {
		t.Fatalf("error = %v, want ErrInsecureHTTP", err)
	}
	// The message has to say what to do about it, not just refuse.
	if !strings.Contains(err.Error(), "--insecure") {
		t.Errorf("error does not mention the way forward: %v", err)
	}
}

// There is no network to intercept on loopback, and demanding a certificate
// there would only push people towards a blanket skip-verification flag.
func TestResolveServerAllowsPlainHTTPToLoopback(t *testing.T) {
	for _, host := range []string{
		"http://localhost:3000", "http://127.0.0.1:3000",
		"http://[::1]:3000", "http://LOCALHOST:3000", "http://127.2.3.4:8080",
	} {
		got, err := ResolveServer(host, false)
		if err != nil {
			t.Errorf("ResolveServer(%q) error = %v, want nil", host, err)
			continue
		}
		if !got.Loopback {
			t.Errorf("ResolveServer(%q).Loopback = false", host)
		}
		// Loopback over http is the documented normal case, so it must not
		// make the daemon shout on every start.
		if got.Insecure {
			t.Errorf("ResolveServer(%q).Insecure = true, want false", host)
		}
	}
}

func TestResolveServerRecordsAWaiver(t *testing.T) {
	got, err := ResolveServer("http://example.com", true)
	if err != nil {
		t.Fatalf("ResolveServer: %v", err)
	}
	if !got.Insecure {
		t.Error("an accepted plain-HTTP remote must be recorded as insecure")
	}
	if got.Loopback {
		t.Error("example.com is not loopback")
	}
}

func TestResolveServerNormalises(t *testing.T) {
	tests := []struct{ in, want string }{
		{"app.agentrq.com", "https://app.agentrq.com"},              // bare host defaults to https
		{"  https://app.agentrq.com/  ", "https://app.agentrq.com"}, // trailing slash dropped
		{"https://app.agentrq.com/some/path", "https://app.agentrq.com"},
		{"https://app.agentrq.com:8443", "https://app.agentrq.com:8443"},
	}
	for _, tc := range tests {
		got, err := ResolveServer(tc.in, false)
		if err != nil {
			t.Errorf("ResolveServer(%q) error = %v", tc.in, err)
			continue
		}
		if got.URL != tc.want {
			t.Errorf("ResolveServer(%q).URL = %q, want %q", tc.in, got.URL, tc.want)
		}
	}
}

func TestResolveServerRejectsNonsense(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want error
	}{
		{"empty", "", ErrNoServer},
		{"whitespace", "   ", ErrNoServer},
		{"wrong scheme", "ftp://example.com", ErrBadServer},
		{"no host", "https://", ErrBadServer},
		{"control character", "https://exa\x00mple.com", ErrBadServer},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := ResolveServer(tc.in, false); !errors.Is(err, tc.want) {
				t.Errorf("ResolveServer(%q) error = %v, want %v", tc.in, err, tc.want)
			}
		})
	}
}

// --insecure permits plain HTTP. It must never be read as "and also stop
// checking certificates" — those are different problems and only one of them
// is ever deliberate.
func TestInsecureDoesNotApplyToHTTPS(t *testing.T) {
	got, err := ResolveServer("https://example.com", true)
	if err != nil {
		t.Fatalf("ResolveServer: %v", err)
	}
	if got.Insecure {
		t.Error("an https URL must never be marked insecure, whatever the flag says")
	}
}

// A warning printed once at enrolment is not a safeguard.
func TestInsecureWarningIsForEveryStart(t *testing.T) {
	p := Profile{ID: "local", ServerURL: "http://box.internal", Insecure: true}
	msg := InsecureWarning(p)
	if !strings.Contains(msg, "local") || !strings.Contains(msg, "box.internal") {
		t.Errorf("warning does not identify the profile or server: %q", msg)
	}
	// It has to say what is at stake, not just that something is insecure.
	if !strings.Contains(msg, "token") {
		t.Errorf("warning does not say what is exposed: %q", msg)
	}
	if InsecureWarning(Profile{ID: "fine"}) != "" {
		t.Error("a secure profile must produce no warning")
	}
}
