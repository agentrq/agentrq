// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package supervisor

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrNoFolder is a workspace folder that is missing and cannot be made, or
// cannot be written to.
var ErrNoFolder = errors.New("the folder is not set for this workspace")

// expandTilde is dir with a leading ~ replaced by home, and dir as it is
// otherwise. The server cannot know this machine's home, so a workspace created
// without a folder is given ~/.agentrq/<id>.
func expandTilde(dir, home string) string {
	dir = strings.TrimSpace(dir)
	if dir == "~" {
		return home
	}
	if strings.HasPrefix(dir, "~/") || strings.HasPrefix(dir, `~\`) {
		return filepath.Join(home, filepath.FromSlash(dir[2:]))
	}
	return dir
}

// prepareDir expands a workspace folder's ~, makes the folder when it does not
// exist, and checks it can be written to, since the MCP config is written into
// it next. A relative path is left for [workspaceDir] to refuse; one holding
// ".." is refused here.
func (s *Supervisor) prepareDir(dir string) (string, error) {
	if strings.HasPrefix(strings.TrimSpace(dir), "~") {
		home, err := s.home()
		if err != nil {
			return "", err
		}
		dir = expandTilde(dir, home)
	}
	if !filepath.IsAbs(dir) {
		return dir, nil
	}
	// The path comes from the server and a folder may be made from it, so
	// "..", which could climb out of what was meant, is refused outright.
	dir = filepath.Clean(dir)
	if strings.Contains(dir, "..") {
		return "", fmt.Errorf("%w: dir=%q contains \"..\"", ErrBadParameter, dir)
	}
	if info, err := os.Stat(dir); err != nil {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return "", fmt.Errorf("%w: %s could not be made: %w", ErrNoFolder, dir, err)
		}
	} else if !info.IsDir() {
		return dir, nil // workspaceDir says it is not a folder
	}
	probe, err := os.CreateTemp(dir, ".agentrq-write-*")
	if err != nil {
		return "", fmt.Errorf("%w: %s cannot be written to: %w", ErrNoFolder, dir, err)
	}
	_ = probe.Close()
	_ = os.Remove(probe.Name())
	return dir, nil
}
