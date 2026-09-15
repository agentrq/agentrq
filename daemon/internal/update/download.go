// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// MaxArtifact bounds a download.
//
// The daemon is a few tens of megabytes; this is generous enough never to
// matter and small enough that a redirect to something enormous fills a log
// line rather than a disk.
const MaxArtifact = 256 << 20

// Fetcher is the HTTP client, narrowed so a test needs no server.
type Fetcher interface {
	Do(*http.Request) (*http.Response, error)
}

// FetchManifest reads a release manifest.
//
// It is not verified here. Verification happens where the artefact is taken
// out of it, so there is no way to hold a verified-looking manifest that was
// never checked.
func FetchManifest(ctx context.Context, client Fetcher, manifestURL string) (Manifest, error) {
	if err := checkURL(manifestURL); err != nil {
		return Manifest{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, manifestURL, nil)
	if err != nil {
		return Manifest{}, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return Manifest{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return Manifest{}, fmt.Errorf("update: release feed answered %s", resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Manifest{}, err
	}
	var m Manifest
	if err := jsonUnmarshal(body, &m); err != nil {
		return Manifest{}, fmt.Errorf("update: unreadable release feed: %w", err)
	}
	return m, nil
}

// Download writes an artefact beside the binary it will replace and returns
// its path.
//
// The hash is computed while writing and checked before the file is made
// executable. A file that does not match is removed rather than left on disk:
// a rejected update should leave nothing behind that a later mistake could
// pick up and run.
func Download(ctx context.Context, client Fetcher, a Artifact, dir string) (string, error) {
	if err := checkChecksum(a.SHA256); err != nil {
		return "", err
	}
	if err := checkURL(a.URL); err != nil {
		return "", err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("update: download answered %s", resp.Status)
	}

	// Written beside the target, because the swap is a rename and a rename
	// only works within one filesystem. A temp directory elsewhere would work
	// on a developer's laptop and fail on a machine with /tmp on tmpfs.
	tmp, err := os.CreateTemp(dir, ".agentrqd-update-*")
	if err != nil {
		return "", err
	}
	tmpName := tmp.Name()
	cleanup := func() {
		_ = tmp.Close()
		_ = os.Remove(tmpName)
	}

	sum := sha256.New()
	if _, err := io.Copy(io.MultiWriter(tmp, sum), io.LimitReader(resp.Body, MaxArtifact)); err != nil {
		cleanup()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}

	got := hex.EncodeToString(sum.Sum(nil))
	if got != a.SHA256 {
		_ = os.Remove(tmpName)
		return "", fmt.Errorf("%w: got %s, release names %s", ErrChecksumMismatch, got, a.SHA256)
	}

	// Executable only after it has been verified. A file that is executable
	// before it has been checked is a file something could run before it was.
	if err := os.Chmod(tmpName, 0o755); err != nil {
		_ = os.Remove(tmpName)
		return "", err
	}
	return filepath.Clean(tmpName), nil
}
