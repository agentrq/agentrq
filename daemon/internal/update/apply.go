// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
)

// ErrAlreadyCurrent means there is nothing newer to install.
var ErrAlreadyCurrent = errors.New("update: already running the latest release")

// Plan is a verified, downloaded, tested candidate.
//
// Holding one means every reversible step has already succeeded. Nothing has
// been replaced and nothing has been killed: a plan that is thrown away leaves
// the machine exactly as it was.
type Plan struct {
	Version string
	// Staged is the new binary, beside the one it will replace, verified
	// against the release's checksum and proven to run here.
	Staged string
}

// Prepare does everything that can still be undone.
//
// Ordered so that each step that could refuse refuses before the next one
// costs anything: read the release, check it is actually newer, verify the
// signature, download, check the hash, run it. Only then is there a plan.
//
// This is deliberately the whole of the dangerous work, and none of the
// destructive work. What remains after this returns is a rename.
func Prepare(ctx context.Context, client Fetcher, manifestURL, currentVersion, goos, goarch, binaryPath string) (Plan, error) {
	m, err := FetchManifest(ctx, client, manifestURL)
	if err != nil {
		return Plan{}, err
	}

	// Checked before the signature is verified only because it is cheaper and
	// cannot be dangerous: the answer here is either "there is nothing to do"
	// or "carry on and verify".
	if !Newer(currentVersion, m.Version) {
		return Plan{}, fmt.Errorf("%w: running %s, release is %s", ErrAlreadyCurrent, currentVersion, m.Version)
	}

	// Verification happens inside ArtifactFor, so there is no path to an
	// artefact that skips it.
	a, err := m.ArtifactFor(goos, goarch)
	if err != nil {
		return Plan{}, err
	}

	staged, err := Download(ctx, client, a, filepath.Dir(binaryPath))
	if err != nil {
		return Plan{}, err
	}

	if err := SelfTest(ctx, staged, m.Version); err != nil {
		// Removed, not kept: a binary that does not run here is not a binary
		// to leave lying next to the one that does.
		_ = removeQuietly(staged)
		return Plan{}, err
	}
	return Plan{Version: m.Version, Staged: staged}, nil
}

// Abandon throws a plan away, leaving the machine as it was.
func (p Plan) Abandon() {
	if p.Staged != "" {
		_ = removeQuietly(p.Staged)
	}
}
