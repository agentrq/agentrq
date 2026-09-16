// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"fmt"
	"os"
	"path/filepath"
)

// Suffixes used around a swap.
//
// `.old` is the retained previous binary, which is what makes a rollback
// possible and is also what the Windows path depends on: a running .exe
// cannot be deleted, but it can be renamed out of the way.
const (
	OldSuffix = ".old"
)

// Swap replaces the binary at path with the one at staged.
//
// Two platforms, two mechanisms, and the difference is not cosmetic:
//
//   - **Unix** can rename over a running binary. The running process keeps its
//     inode and carries on with the old code until it re-executes, which is
//     exactly the behaviour wanted here.
//   - **Windows** cannot delete or overwrite a running .exe, but it *can*
//     rename one. So the running binary is moved aside first and the new one
//     put in its place. The moved-aside file cannot be deleted until this
//     process exits, which is why cleaning it up is the *next* start's job.
//
// Either way the old binary is retained rather than discarded, because an
// update that cannot be rolled back is an update that takes a machine offline
// with no way in.
func Swap(path, staged string) error {
	path = filepath.Clean(path)
	staged = filepath.Clean(staged)
	old := path + OldSuffix

	// A leftover from a previous update, which on Windows is normal: the file
	// could not be removed while the process that was replaced still held it.
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("update: cannot clear %s: %w", old, err)
	}

	// The running binary goes aside rather than away. On Unix this is what
	// gives a rollback something to roll back to; on Windows it is also the
	// only way to free the name at all.
	if err := os.Rename(path, old); err != nil {
		return fmt.Errorf("update: cannot move the current binary aside: %w", err)
	}

	if err := os.Rename(staged, path); err != nil {
		// Put it back. A failure here would otherwise leave the machine with
		// no binary at that path at all, which is the one outcome worse than
		// not updating.
		if restoreErr := os.Rename(old, path); restoreErr != nil {
			return fmt.Errorf("update: cannot install the new binary (%w) and cannot restore the old one (%v)", err, restoreErr)
		}
		return fmt.Errorf("update: cannot install the new binary: %w", err)
	}
	return nil
}

// Rollback puts the retained binary back.
//
// Called when the new one does not come up. The new binary is moved aside for
// inspection rather than deleted — something that failed to start is the one
// thing somebody will want to look at.
func Rollback(path string) error {
	path = filepath.Clean(path)
	old := path + OldSuffix

	if _, err := os.Stat(old); err != nil {
		return fmt.Errorf("update: nothing to roll back to: %w", err)
	}
	failed := path + ".failed"
	_ = os.Remove(failed)
	if err := os.Rename(path, failed); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("update: cannot move the failed binary aside: %w", err)
	}
	if err := os.Rename(old, path); err != nil {
		return fmt.Errorf("update: cannot restore the previous binary: %w", err)
	}
	return nil
}

// The retained binary is deliberately *not* cleaned up at startup.
//
// It would be convenient, and it would delete the only thing a rollback can
// roll back to, at exactly the moment the new build has proved least. It is
// cleared instead at the start of the *next* [Swap] — by which time the build
// that replaced it has been running long enough for somebody to have noticed
// if it did not work. That also happens to be when Windows will finally let go
// of it: the process holding the old file is long gone.
