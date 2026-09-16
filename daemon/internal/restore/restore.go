// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package restore remembers what to start again after the daemon replaces
// itself.
//
// **What is restored is intent, not state.** The sessions that come back are
// new processes with new pseudo-terminals: same kind, same workspace, same
// arguments. The scrollback is gone, whatever the agent was part-way through
// is gone, and anything half-typed is gone. "Spin the same sessions back up"
// reads as continuity and is not one, which is why every restored session is
// marked as restored — in the UI and in the record — so nobody is left
// wondering why their terminal is empty.
package restore

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Version is the file format.
const Version = 1

// MaxAge is how long an intent is worth acting on.
//
// An update takes seconds. A file older than this is from a restart that never
// finished, or from a machine that was off for a week — and starting somebody's
// agents from a week-old note is a surprise, not a restoration.
const MaxAge = 15 * time.Minute

// Session is one thing to start again.
//
// It carries what the daemon needs to reproduce the *request*, and
// deliberately not the MCP URL: that contains a scoped, short-lived
// credential, and writing it to disk to survive a restart would turn a
// deliberate expiry into a file. The backend re-issues one when it is asked to
// start the session again.
type Session struct {
	ID         uint64 `json:"id"`
	Profile    string `json:"profile"`
	Kind       string `json:"kind"`
	Dir        string `json:"dir"`
	Workspace  string `json:"workspace,omitempty"`
	ServerName string `json:"serverName,omitempty"`
	Model      string `json:"model,omitempty"`
	Agent      string `json:"agent,omitempty"`
	Cols       uint16 `json:"cols,omitempty"`
	Rows       uint16 `json:"rows,omitempty"`
}

// File is the note left for the next start.
type File struct {
	Version int       `json:"version"`
	Reason  string    `json:"reason"`
	WroteAt time.Time `json:"wroteAt"`
	// FromVersion is what was running when this was written, for the log line
	// that explains why a machine's sessions all restarted at once.
	FromVersion string    `json:"fromVersion,omitempty"`
	Sessions    []Session `json:"sessions"`
}

// ErrStale means the note was found but is too old to act on.
var ErrStale = errors.New("restore: the note is too old to act on")

// Path is where the note lives.
func Path(dir string) string { return filepath.Join(dir, "restore.json") }

// Write records what to start again.
//
// Written to disk before anything is killed, and that ordering is the whole
// point: the process holding this in memory is the process about to be
// replaced. Temp file then rename, so a crash midway leaves no half-written
// note that the next start would misread.
func Write(dir string, f File) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("restore: create %s: %w", dir, err)
	}
	f.Version = Version
	if f.WroteAt.IsZero() {
		f.WroteAt = time.Now()
	}

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("restore: encode: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(dir, ".restore-*")
	if err != nil {
		return fmt.Errorf("restore: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restore: chmod temp: %w", err)
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("restore: write temp: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("restore: close temp: %w", err)
	}
	// Flushed to the directory as well, so the rename survives a power cut on
	// the machine this is meant to protect against a *restart* on.
	if err := os.Rename(tmpName, Path(dir)); err != nil {
		return fmt.Errorf("restore: replace note: %w", err)
	}
	return nil
}

// Take reads the note and removes it.
//
// Removed whether or not it is acted on, and that is deliberate: a note left
// behind is a note that starts somebody's agents again on every subsequent
// start, forever. Restoring is best effort and happens once.
func Take(dir string, now time.Time) (File, error) {
	path := Path(dir)
	b, err := os.ReadFile(path)
	if err != nil {
		// A missing note is the ordinary case — it is what an ordinary start
		// looks like — and is not an error.
		if errors.Is(err, os.ErrNotExist) {
			return File{}, nil
		}
		return File{}, fmt.Errorf("restore: read %s: %w", path, err)
	}
	_ = os.Remove(path)

	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("restore: unreadable note: %w", err)
	}
	if f.Version != Version {
		return File{}, fmt.Errorf("restore: note is version %d, this daemon writes %d", f.Version, Version)
	}
	if now.Sub(f.WroteAt) > MaxAge {
		return f, fmt.Errorf("%w: written %s", ErrStale, f.WroteAt.Format(time.RFC3339))
	}
	return f, nil
}

// Clear removes a note without reading it, for an update that was abandoned
// before anything was killed.
func Clear(dir string) {
	_ = os.Remove(Path(dir))
}
