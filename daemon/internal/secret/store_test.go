// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package secret

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newStore(t *testing.T) *FileStore {
	t.Helper()
	s, err := NewFileStore(filepath.Join(t.TempDir(), "tokens"))
	if err != nil {
		t.Fatalf("NewFileStore: %v", err)
	}
	return s
}

func TestSetGetDelete(t *testing.T) {
	s := newStore(t)

	if _, err := s.Get("work"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get before Set error = %v, want ErrNotFound", err)
	}
	if err := s.Set("work", "tok-1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	got, err := s.Get("work")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != "tok-1" {
		t.Errorf("token = %q, want tok-1", got)
	}

	// Rotation replaces rather than appends.
	if err := s.Set("work", "tok-2"); err != nil {
		t.Fatalf("Set again: %v", err)
	}
	if got, _ := s.Get("work"); got != "tok-2" {
		t.Errorf("after rotation token = %q, want tok-2", got)
	}

	if err := s.Delete("work"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get("work"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get after Delete error = %v, want ErrNotFound", err)
	}
}

// Revocation should be idempotent: the caller's goal is "there is no token",
// and that goal is already met.
func TestDeleteMissingIsNotAnError(t *testing.T) {
	if err := newStore(t).Delete("never-existed"); err != nil {
		t.Errorf("Delete of a missing token = %v, want nil", err)
	}
}

func TestProfilesAreIsolated(t *testing.T) {
	s := newStore(t)
	if err := s.Set("work", "a"); err != nil {
		t.Fatal(err)
	}
	if err := s.Set("personal", "b"); err != nil {
		t.Fatal(err)
	}
	if err := s.Delete("work"); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Get("personal"); err != nil || got != "b" {
		t.Errorf("deleting one profile disturbed another: %q %v", got, err)
	}
}

// This function turns a string into a filesystem path, which is exactly where
// "someone else validated it" becomes a directory traversal.
func TestPathRefusesIdsThatAreNotFilenames(t *testing.T) {
	s := newStore(t)
	for _, id := range []string{"", "..", "../escape", "a/b", `a\b`, "x/../../y"} {
		if err := s.Set(id, "tok"); err == nil {
			t.Errorf("Set(%q) succeeded, want refusal", id)
		}
		if _, err := s.Get(id); err == nil {
			t.Errorf("Get(%q) succeeded, want refusal", id)
		}
		if err := s.Delete(id); err == nil {
			t.Errorf("Delete(%q) succeeded, want refusal", id)
		}
	}
	// Nothing escaped into the parent directory.
	entries, err := os.ReadDir(filepath.Dir(s.Dir))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != "tokens" {
			t.Errorf("stray entry beside the store: %q", e.Name())
		}
	}
}

func TestSetRefusesAnEmptyToken(t *testing.T) {
	s := newStore(t)
	if err := s.Set("work", "   "); err == nil {
		t.Error("Set with a blank token should fail")
	}
}

// The permissions are the point of the whole package.
func TestTokensAreNotReadableByOthers(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits do not apply; Windows ACLs are inherited from the directory")
	}
	s := newStore(t)
	if err := s.Set("work", "tok"); err != nil {
		t.Fatal(err)
	}

	info, err := os.Stat(filepath.Join(s.Dir, "work.token"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("token file mode = %o, want 600", perm)
	}

	// Directory permissions matter separately: they are what stops another
	// user listing which profiles exist, which is information on its own.
	dir, err := os.Stat(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	if perm := dir.Mode().Perm(); perm != 0o700 {
		t.Errorf("store directory mode = %o, want 700", perm)
	}
}

// A half-written token is worse than a stale one: the stale one still works.
func TestSetLeavesNoPartialFilesBehind(t *testing.T) {
	s := newStore(t)
	if err := s.Set("work", "tok"); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".token-") {
			t.Errorf("temporary file left behind: %q", e.Name())
		}
	}
	if len(entries) != 1 {
		t.Errorf("entries = %d, want exactly the one token file", len(entries))
	}
}

func TestNewFileStoreNeedsADirectory(t *testing.T) {
	if _, err := NewFileStore(""); err == nil {
		t.Error("NewFileStore(\"\") should fail")
	}
	// A path that cannot be a directory, because a file is already there.
	file := filepath.Join(t.TempDir(), "in-the-way")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewFileStore(filepath.Join(file, "tokens")); err == nil {
		t.Error("NewFileStore under a file should fail")
	}
}

// Nobody should have to guess where their credentials went.
func TestDescribeSaysWhereTokensLive(t *testing.T) {
	s := newStore(t)
	if got := s.Describe(); !strings.Contains(got, s.Dir) {
		t.Errorf("Describe() = %q, want it to name %q", got, s.Dir)
	}
}

// Interface conformance, so a keychain-backed store can be dropped in later
// without touching anything that talks to a Store.
var _ Store = (*FileStore)(nil)

func TestGetReportsARealReadFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod 000 does not deny the owner on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can read anything, so there is no failure to observe")
	}
	s := newStore(t)
	if err := s.Set("work", "tok"); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(s.Dir, "work.token")
	if err := os.Chmod(p, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(p, 0o600) })

	// "Unreadable" is a fault that needs a person, and must not be reported as
	// the ordinary "no token yet, go and enrol" state.
	_, err := s.Get("work")
	if err == nil {
		t.Fatal("expected a read failure")
	}
	if errors.Is(err, ErrNotFound) {
		t.Errorf("an unreadable token reported as ErrNotFound: %v", err)
	}
}

// A store that cannot write must say so rather than appear to succeed. The
// realistic causes are a full disk or a directory that has lost its
// permissions; a read-only directory reproduces the second portably enough.
func TestWritesReportFailureRatherThanSilentlySucceeding(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a read-only directory does not stop the owner creating files on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores the permission bits, so there is no failure to observe")
	}
	s := newStore(t)
	if err := s.Set("work", "tok"); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(s.Dir, 0o500); err != nil { // read and traverse, no write
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(s.Dir, 0o700) })

	if err := s.Set("work", "tok-2"); err == nil {
		t.Error("Set into a read-only directory should fail")
	}
	// The old token has to survive a failed write. A stale token still works;
	// a truncated one does not.
	if got, err := s.Get("work"); err != nil || got != "tok" {
		t.Errorf("a failed write damaged the stored token: %q %v", got, err)
	}
	if err := s.Delete("work"); err == nil {
		t.Error("Delete from a read-only directory should fail")
	}
}
