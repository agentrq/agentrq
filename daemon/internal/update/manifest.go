// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Package update replaces the daemon's own binary.
//
// This is the most dangerous code in the daemon, and it is written to be read
// by somebody who is suspicious of it. Two rules are not negotiable and are
// enforced here rather than by the caller remembering:
//
//  1. Nothing is swapped that has not been verified. An update channel that
//     does not verify is a remote-code-execution path that bypasses every
//     other boundary in the design, and it is the one place where getting it
//     wrong cannot be recovered from afterwards.
//  2. A build with no release key refuses to self-update at all, and says so.
//     A verification step that silently passes when it has nothing to verify
//     against is worse than no verification, because it looks like one.
package update

import (
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ReleaseKey is the ed25519 public key that signs release manifests, hex
// encoded, set at build time with -ldflags "-X …update.ReleaseKey=…".
//
// Empty by default, and that default is deliberate: a build nobody has given a
// key to cannot self-update, which is the correct behaviour for every build
// that is not an official release.
var ReleaseKey = ""

// Errors from reading a manifest.
var (
	ErrNoReleaseKey     = errors.New("update: this build has no release key and cannot update itself")
	ErrBadKey           = errors.New("update: the release key is not a usable ed25519 public key")
	ErrUnsigned         = errors.New("update: the release manifest is not signed")
	ErrBadSignature     = errors.New("update: the release manifest's signature does not verify")
	ErrNoArtifact       = errors.New("update: the release has nothing for this platform")
	ErrNoChecksum       = errors.New("update: the release names no checksum for this platform")
	ErrInsecureURL      = errors.New("update: a release must be fetched over https")
	ErrBadVersion       = errors.New("update: unusable version string")
	ErrChecksumMismatch = errors.New("update: the downloaded file is not the one the release names")
)

// Artifact is one platform's build.
type Artifact struct {
	URL string `json:"url"`
	// SHA256 is hex encoded and always required. An artefact without one is
	// refused rather than downloaded and hoped about.
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

// Manifest is a release.
//
// Signed as a whole rather than per artefact: the thing that must not be
// tampered with is the mapping from "the current version" to "this exact
// file", and signing the artefacts individually would leave the version and
// the platform table unprotected.
type Manifest struct {
	Version string `json:"version"`
	// Artifacts is keyed "goos/goarch".
	Artifacts map[string]Artifact `json:"artifacts"`
	// Signature is over the canonical JSON of everything above, hex encoded.
	Signature string `json:"signature"`
}

// versionPattern is what a version may look like.
//
// Checked because the version reaches a file path and a log line, and because
// a "version" containing a path separator is not a version.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// signedBytes is exactly what the signature covers.
//
// Rebuilt rather than taken from the response body, so that whitespace, key
// order or a field a newer publisher added cannot change what was verified
// away from what is then used.
func (m Manifest) signedBytes() ([]byte, error) {
	type signed struct {
		Version   string              `json:"version"`
		Artifacts map[string]Artifact `json:"artifacts"`
	}
	return json.Marshal(signed{Version: m.Version, Artifacts: m.Artifacts})
}

// Verify checks the manifest's signature against the compiled-in release key.
func (m Manifest) Verify() error {
	if ReleaseKey == "" {
		return ErrNoReleaseKey
	}
	key, err := hex.DecodeString(ReleaseKey)
	if err != nil || len(key) != ed25519.PublicKeySize {
		return ErrBadKey
	}
	if m.Signature == "" {
		return ErrUnsigned
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil {
		return ErrBadSignature
	}
	body, err := m.signedBytes()
	if err != nil {
		return err
	}
	if !ed25519.Verify(ed25519.PublicKey(key), body, sig) {
		return ErrBadSignature
	}
	return nil
}

// ArtifactFor returns the build for a platform, once the manifest is verified.
//
// Verification happens here rather than being left to the caller: every path
// to an artefact goes through this function, so there is no way to obtain one
// without having checked the signature first.
func (m Manifest) ArtifactFor(goos, goarch string) (Artifact, error) {
	if err := m.Verify(); err != nil {
		return Artifact{}, err
	}
	if !versionPattern.MatchString(m.Version) {
		return Artifact{}, fmt.Errorf("%w: %q", ErrBadVersion, m.Version)
	}

	a, ok := m.Artifacts[goos+"/"+goarch]
	if !ok {
		return Artifact{}, fmt.Errorf("%w: %s/%s", ErrNoArtifact, goos, goarch)
	}
	if err := checkChecksum(a.SHA256); err != nil {
		return Artifact{}, err
	}
	if err := checkURL(a.URL); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

func checkChecksum(sum string) error {
	if sum == "" {
		return ErrNoChecksum
	}
	b, err := hex.DecodeString(sum)
	if err != nil || len(b) != 32 {
		return fmt.Errorf("%w: %q is not a sha256", ErrNoChecksum, sum)
	}
	return nil
}

// checkURL refuses anything but https.
//
// The signature already makes a tampered manifest detectable, so this is not
// what protects the artefact — the checksum is. It is refused anyway, because
// an update fetched in the clear tells every network between here and there
// exactly which version this machine is about to run.
func checkURL(raw string) error {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInsecureURL, err)
	}
	if u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("%w: %q", ErrInsecureURL, raw)
	}
	return nil
}

// Newer reports whether the manifest offers something later than what is
// running.
//
// Compared field by field rather than as strings: "0.10.0" is later than
// "0.9.0" and sorts before it. A version that cannot be parsed is not newer,
// which is the safe answer — the alternative is a daemon that updates itself
// because it could not read its own version.
func Newer(current, offered string) bool {
	c, okC := parseVersion(current)
	o, okO := parseVersion(offered)
	if !okC || !okO {
		return false
	}
	for i := 0; i < 3; i++ {
		if o[i] != c[i] {
			return o[i] > c[i]
		}
	}
	return false
}

func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	if !versionPattern.MatchString(v) {
		return out, false
	}
	// The pre-release suffix is ignored for ordering. Comparing them properly
	// is semver's whole appendix, and this only has to answer "is there
	// something newer", where treating 1.2.3-rc1 as 1.2.3 is close enough and
	// never wrong in the dangerous direction.
	base := strings.SplitN(v, "-", 2)[0]
	parts := strings.Split(base, ".")
	for i := 0; i < 3; i++ {
		n := 0
		for _, r := range parts[i] {
			n = n*10 + int(r-'0')
		}
		out[i] = n
	}
	return out, true
}
