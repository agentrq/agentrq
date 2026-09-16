// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package localstatus lets the person at the keyboard find out what is driving
// their machine.
//
// A running daemon and `agentrqd status` are different processes, so the
// running one writes what it is doing to a file and the other reads it. Not a
// socket: a file survives the daemon being killed, which is exactly the case
// where somebody wants to know what *was* running, and it needs no
// per-platform IPC to work the same on three operating systems.
//
// This exists because somebody whose machine is running agents for an account
// they no longer control must be able to see that without asking the account.
package localstatus

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// Session is one agent running on this machine.
//
// Deliberately only what a person at the keyboard needs to answer "what is
// this, and who asked for it": no tokens, no URLs, no terminal contents.
type Session struct {
	ID        uint64    `json:"id"`
	Profile   string    `json:"profile"`
	Kind      string    `json:"kind"`
	Dir       string    `json:"dir"`
	Workspace string    `json:"workspace,omitempty"`
	StartedAt time.Time `json:"startedAt"`
	// Viewers is how many browsers are attached to its terminal right now.
	// Somebody watching a terminal on this machine is the fact most worth
	// surfacing locally.
	Viewers int `json:"viewers"`
}

// File is what a running daemon reports about itself.
type File struct {
	PID       int       `json:"pid"`
	Version   string    `json:"version"`
	StartedAt time.Time `json:"startedAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Sessions  []Session `json:"sessions"`
}

// Name is the file inside the state directory.
const Name = "status.json"

// Stale is how old a report can be before it is treated as left behind.
//
// A daemon that was killed does not get to clean up, so a file alone is not
// evidence that anything is running. Generous relative to the write interval,
// so an idle daemon is never mistaken for a dead one.
const Stale = 5 * time.Minute

// Path is where the report lives.
func Path(dir string) string { return filepath.Join(dir, Name) }

// Write records what this daemon is doing.
//
// 0600, because the list of workspaces and folders on a machine is not
// something every user on it needs. Temp file and rename, so a reader never
// sees half a report.
func Write(dir string, f File) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("localstatus: create %s: %w", dir, err)
	}
	f.UpdatedAt = time.Now()
	sort.Slice(f.Sessions, func(i, j int) bool { return f.Sessions[i].ID < f.Sessions[j].ID })

	b, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("localstatus: encode: %w", err)
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(dir, ".status-*")
	if err != nil {
		return fmt.Errorf("localstatus: create temp: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, Path(dir))
}

// ErrNoDaemon means nothing is reporting.
var ErrNoDaemon = errors.New("localstatus: no daemon is reporting on this machine")

// ErrStale means the last report is old enough that the daemon is probably gone.
var ErrStale = errors.New("localstatus: the last report is old; the daemon may have been killed")

// Read returns the current report.
//
// A stale file is returned *with* an error rather than instead of one: what a
// killed daemon was last doing is exactly what somebody investigating wants to
// see, and hiding it would answer "nothing is running" to the question "what
// was running".
func Read(dir string, now time.Time) (File, error) {
	b, err := os.ReadFile(Path(dir))
	if errors.Is(err, os.ErrNotExist) {
		return File{}, ErrNoDaemon
	}
	if err != nil {
		return File{}, err
	}
	var f File
	if err := json.Unmarshal(b, &f); err != nil {
		return File{}, fmt.Errorf("localstatus: unreadable report: %w", err)
	}
	if now.Sub(f.UpdatedAt) > Stale {
		return f, ErrStale
	}
	return f, nil
}

// Clear removes the report, for a daemon shutting down cleanly.
func Clear(dir string) { _ = os.Remove(Path(dir)) }
