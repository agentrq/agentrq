// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// SelfTestTimeout bounds the check.
//
// Running `version` is a few milliseconds of work. Anything that takes longer
// than this is not slow, it is stuck, and a stuck candidate must not hold up
// the decision not to install it.
const SelfTestTimeout = 20 * time.Second

// ErrSelfTestFailed means the downloaded binary does not run here.
var ErrSelfTestFailed = errors.New("update: the new binary does not run on this machine")

// SelfTest runs the staged binary before anything is replaced.
//
// This is what makes "never auto-update on a failed start" enforceable rather
// than aspirational. Once the swap has happened and this process has gone,
// nothing here can observe the new binary failing — so the check has to happen
// while there is still something to check *with*, and while not installing it
// is still an option.
//
// It catches what actually goes wrong: an artefact built for another
// architecture, a truncated download that matched no checksum because it was
// never checked, a binary needing a libc this machine does not have. It does
// not catch a daemon that starts and then misbehaves, and nothing at this
// layer could.
func SelfTest(ctx context.Context, path, wantVersion string) error {
	ctx, cancel := context.WithTimeout(ctx, SelfTestTimeout)
	defer cancel()

	out, err := exec.CommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %v", ErrSelfTestFailed, err)
	}
	// The version it reports must be the version that was asked for. A binary
	// that runs but is not the release being installed means the manifest and
	// the artefact disagree, which is exactly the case where carrying on is
	// worst.
	if !strings.Contains(string(out), wantVersion) {
		return fmt.Errorf("%w: it reports %q, not %s", ErrSelfTestFailed, strings.TrimSpace(string(out)), wantVersion)
	}
	return nil
}
