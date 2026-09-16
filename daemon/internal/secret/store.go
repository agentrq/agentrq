// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package secret stores machine tokens.
//
// A machine token authorises everything this daemon can do on its machine, so
// where it lives matters more than most of the code around it.
//
// # Why a file, and why that is not a cop-out
//
// The plan says "OS keychain where there is one, else a 0600 file with a loud
// warning". The file store is here first and the keychain is not, and that
// ordering is deliberate rather than laziness: **the obvious place to run this
// daemon is a headless build box**, and a headless Linux box has no keyring
// daemon to talk to. A keychain implementation would fall back to a file on
// exactly the machines this is aimed at, while adding a dependency that fails
// in ways that are miserable to debug over SSH.
//
// So the interface is here, the file store implements it, and a keychain-backed
// store can be added behind the same interface when there is a desktop case
// that wants one. [Store] is what the rest of the daemon talks to.
package secret

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNotFound is returned for a token that was never stored, or was deleted.
//
// Distinguished from a read failure on purpose: "this profile has no token" is
// an ordinary state that means "enrol", while "the token file is unreadable" is
// a fault that needs a person.
var ErrNotFound = errors.New("secret: no token stored")

// Store keeps one token per profile.
type Store interface {
	Get(profileID string) (string, error)
	Set(profileID, token string) error
	Delete(profileID string) error
	// Describe says where tokens are kept, for `agentrqd status` and for the
	// enrolment output. A person should never have to guess where their
	// credentials went.
	Describe() string
}

// FileStore keeps each token in its own 0600 file under a directory.
//
// One file per profile rather than one file holding all of them: a corrupt
// write, an interrupted disk, or a bad edit then costs one profile instead of
// every profile at once.
type FileStore struct {
	Dir string
}

// NewFileStore prepares a directory to hold tokens.
//
// The directory is created 0700. That is not belt-and-braces with the 0600 on
// the files: directory permissions are what stop another user *listing* which
// profiles exist, which is information even without the tokens.
func NewFileStore(dir string) (*FileStore, error) {
	if dir == "" {
		return nil, errors.New("secret: no directory given")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("secret: create %s: %w", dir, err)
	}
	return &FileStore{Dir: dir}, nil
}

// path is where a profile's token lives.
//
// The profile id is validated by the caller against a narrow charset, but this
// checks again rather than trusting it: this function turns a string into a
// filesystem path, and that is exactly the place where "someone else already
// validated it" turns into a directory traversal.
func (s *FileStore) path(profileID string) (string, error) {
	if profileID == "" {
		return "", errors.New("secret: no profile id")
	}
	if strings.ContainsAny(profileID, `/\`) || strings.Contains(profileID, "..") {
		return "", fmt.Errorf("secret: profile id %q cannot be a filename", profileID)
	}
	return filepath.Join(s.Dir, profileID+".token"), nil
}

func (s *FileStore) Get(profileID string) (string, error) {
	p, err := s.path(profileID)
	if err != nil {
		return "", err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("%w for profile %q", ErrNotFound, profileID)
	}
	if err != nil {
		return "", fmt.Errorf("secret: read token for %q: %w", profileID, err)
	}
	return strings.TrimSpace(string(b)), nil
}

// Set writes a token, replacing any previous one.
//
// Written to a temporary file and renamed, so a crash midway leaves the old
// token intact rather than a truncated one. A half-written token is worse than
// a stale one: the stale one still works.
func (s *FileStore) Set(profileID, token string) error {
	if strings.TrimSpace(token) == "" {
		return errors.New("secret: refusing to store an empty token")
	}
	p, err := s.path(profileID)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(s.Dir, ".token-*")
	if err != nil {
		return fmt.Errorf("secret: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }() // no-op once the rename succeeds

	// Chmod before writing: between create and chmod the file exists, and
	// CreateTemp's 0600 is already correct on Unix but not guaranteed
	// everywhere.
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secret: chmod temp: %w", err)
	}
	if _, err := tmp.WriteString(token); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("secret: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("secret: close temp: %w", err)
	}
	if err := os.Rename(tmpName, p); err != nil {
		return fmt.Errorf("secret: replace token for %q: %w", profileID, err)
	}
	return nil
}

// Delete removes a token. Deleting one that is not there is not an error:
// revocation should be idempotent, since the caller's goal is "there is no
// token", and that goal is already met.
func (s *FileStore) Delete(profileID string) error {
	p, err := s.path(profileID)
	if err != nil {
		return err
	}
	if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("secret: delete token for %q: %w", profileID, err)
	}
	return nil
}

func (s *FileStore) Describe() string {
	return fmt.Sprintf("files in %s (0600)", s.Dir)
}
