// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// distWith writes fake built binaries and returns the directory.
func distWith(t *testing.T, names ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, n := range names {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("binary "+n), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func withKey(t *testing.T) ed25519.PublicKey {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(KeyEnv, hex.EncodeToString(priv))
	return pub
}

// The whole point of this command: produce something the daemon will accept.
func TestTheManifestVerifiesWithTheMatchingPublicKey(t *testing.T) {
	pub := withKey(t)
	dir := distWith(t,
		"agentrqd_linux_amd64", "agentrqd_linux_arm64",
		"agentrqd_darwin_amd64", "agentrqd_darwin_arm64",
		"agentrqd_windows_amd64.exe", "agentrqd_windows_arm64.exe",
	)

	var out bytes.Buffer
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://releases.example/v0.7.1", dir}, &out); err != nil {
		t.Fatalf("manifest: %v", err)
	}

	var m manifest
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	// All six targets, because a release missing one is silent until somebody
	// on that platform tries to update.
	if len(m.Artifacts) != 6 {
		t.Fatalf("artifacts = %d, want 6: %+v", len(m.Artifacts), m.Artifacts)
	}
	for _, want := range []string{"linux/amd64", "linux/arm64", "darwin/amd64", "darwin/arm64", "windows/amd64", "windows/arm64"} {
		a, ok := m.Artifacts[want]
		if !ok {
			t.Errorf("no artifact for %s", want)
			continue
		}
		if len(a.SHA256) != 64 || a.Size == 0 {
			t.Errorf("%s = %+v", want, a)
		}
		if !strings.HasPrefix(a.URL, "https://releases.example/v0.7.1/agentrqd_") {
			t.Errorf("%s url = %q", want, a.URL)
		}
	}

	// Verified exactly as the daemon verifies it: over the canonical bytes of
	// version and artifacts, rebuilt rather than taken from the response.
	body, err := json.Marshal(signedPart{Version: m.Version, Artifacts: m.Artifacts})
	if err != nil {
		t.Fatal(err)
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(pub, body, sig) {
		t.Error("the manifest does not verify with the key that signed it")
	}
}

// The hash is of the file that will be published, so a changed binary is a
// changed manifest.
func TestTheChecksumFollowsTheFile(t *testing.T) {
	withKey(t)
	dir := distWith(t, "agentrqd_linux_arm64")

	first := signOnce(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "agentrqd_linux_arm64"), []byte("a different binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	second := signOnce(t, dir)

	if first.Artifacts["linux/arm64"].SHA256 == second.Artifacts["linux/arm64"].SHA256 {
		t.Error("changing the binary did not change its checksum")
	}
}

func signOnce(t *testing.T, dir string) manifest {
	t.Helper()
	var out bytes.Buffer
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://releases.example/v", dir}, &out); err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

// A release process that publishes binaries and no manifest produces a daemon
// that correctly refuses to update — the safe failure, and still broken. So
// the absence of a key is an error rather than an unsigned manifest.
func TestSigningWithoutAKeyIsRefused(t *testing.T) {
	t.Setenv(KeyEnv, "")
	dir := distWith(t, "agentrqd_linux_arm64")

	err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://x.example/v", dir}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), KeyEnv) {
		t.Errorf("error = %v, want one naming %s", err, KeyEnv)
	}
}

func TestSigningWithSomethingThatIsNotAKey(t *testing.T) {
	t.Setenv(KeyEnv, "not hex")
	dir := distWith(t, "agentrqd_linux_arm64")
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://x.example/v", dir}, &bytes.Buffer{}); err == nil {
		t.Error("nonsense was accepted as a signing key")
	}
}

// A manifest with a plain-http URL is a release nobody can install: the daemon
// refuses it. Better to fail here, where somebody is watching.
func TestABaseURLMustBeHTTPS(t *testing.T) {
	withKey(t)
	dir := distWith(t, "agentrqd_linux_arm64")
	for _, base := range []string{"http://releases.example/v", "releases.example/v", ""} {
		if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", base, dir}, &bytes.Buffer{}); err == nil {
			t.Errorf("base url %q was accepted", base)
		}
	}
}

func TestManifestArgumentRefusals(t *testing.T) {
	withKey(t)
	dir := distWith(t, "agentrqd_linux_arm64")

	if err := manifestCmd([]string{"--base-url", "https://x.example/v", dir}, &bytes.Buffer{}); err == nil {
		t.Error("a release with no version was signed")
	}
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://x.example/v"}, &bytes.Buffer{}); err == nil {
		t.Error("a run with no directory was accepted")
	}
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://x.example/v", dir, dir}, &bytes.Buffer{}); err == nil {
		t.Error("two directories were accepted")
	}
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://x.example/v", filepath.Join(dir, "nope")}, &bytes.Buffer{}); err == nil {
		t.Error("a directory that is not there was accepted")
	}
}

// A directory of things that are not releases is an error, not an empty
// manifest: an empty manifest is a release every daemon quietly declines.
func TestADirectoryWithNoBinariesIsAnError(t *testing.T) {
	withKey(t)
	dir := distWith(t, "checksums.txt", "agentrqd.json", "README.md", "agentrqd_linux")
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://x.example/v", dir}, &bytes.Buffer{}); err == nil {
		t.Error("a directory with no released binaries produced a manifest")
	}
}

// The private key is never printed alongside the public one without saying
// which is which: pasting the wrong one into a build is an unrecoverable
// mistake made silently.
func TestKeygenLabelsBothHalves(t *testing.T) {
	var out bytes.Buffer
	if err := keygen(&out); err != nil {
		t.Fatal(err)
	}
	s := out.String()
	for _, want := range []string{"public", "private", KeyEnv, "not recoverable"} {
		if !strings.Contains(s, want) {
			t.Errorf("keygen output does not mention %q:\n%s", want, s)
		}
	}
}

func TestRunDispatches(t *testing.T) {
	if err := run(nil); err != nil {
		t.Errorf("no arguments: %v", err)
	}
	if err := run([]string{"--help"}); err != nil {
		t.Errorf("--help: %v", err)
	}
	if err := run([]string{"keygen"}); err != nil {
		t.Errorf("keygen: %v", err)
	}
	if err := run([]string{"nonsense"}); err == nil {
		t.Error("an unknown command was accepted")
	}
	if err := run([]string{"verify"}); err == nil {
		t.Error("verify ran with no arguments at all")
	}
}

// signRelease writes a dist directory, signs it, and installs both halves of
// the key in the environment the way the release job does.
func signRelease(t *testing.T, names ...string) string {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(KeyEnv, hex.EncodeToString(priv))
	t.Setenv(PubKeyEnv, hex.EncodeToString(pub))

	dir := distWith(t, names...)
	var out bytes.Buffer
	if err := manifestCmd([]string{"--version", "0.7.1", "--base-url", "https://releases.example/v0.7.1", dir}, &out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agentrqd.json"), out.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestVerifyAcceptsAGoodRelease(t *testing.T) {
	dir := signRelease(t, "agentrqd_linux_arm64", "agentrqd_windows_amd64.exe")
	var out bytes.Buffer
	if err := verifyCmd([]string{"--version", "0.7.1", dir}, &out); err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !strings.Contains(out.String(), "linux/arm64") {
		t.Errorf("verify said %q", out.String())
	}
}

// The failure this command exists to catch: a manifest signed with a key that
// does not match the one compiled into the binaries. Every daemon refuses it,
// and the failure is silent until somebody presses update months later.
func TestVerifyCatchesAManifestSignedWithTheWrongKey(t *testing.T) {
	dir := signRelease(t, "agentrqd_linux_arm64")

	other, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(PubKeyEnv, hex.EncodeToString(other))

	err = verifyCmd([]string{"--version", "0.7.1", dir}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), "no daemon will install") {
		t.Errorf("error = %v, want one saying the release is uninstallable", err)
	}
}

// A manifest that verifies but points at a file nobody uploaded is the same
// broken release wearing a valid signature.
func TestVerifyCatchesAMissingOrChangedBinary(t *testing.T) {
	t.Run("the file is gone", func(t *testing.T) {
		dir := signRelease(t, "agentrqd_linux_arm64")
		if err := os.Remove(filepath.Join(dir, "agentrqd_linux_arm64")); err != nil {
			t.Fatal(err)
		}
		if err := verifyCmd([]string{"--version", "0.7.1", dir}, &bytes.Buffer{}); err == nil {
			t.Error("a manifest naming a file that is not there verified")
		}
	})

	t.Run("the file changed after signing", func(t *testing.T) {
		dir := signRelease(t, "agentrqd_linux_arm64")
		if err := os.WriteFile(filepath.Join(dir, "agentrqd_linux_arm64"), []byte("swapped"), 0o755); err != nil {
			t.Fatal(err)
		}
		err := verifyCmd([]string{"--version", "0.7.1", dir}, &bytes.Buffer{})
		if err == nil || !strings.Contains(err.Error(), "checksum") {
			t.Errorf("error = %v, want one about the checksum", err)
		}
	})
}

// A release job that built one version and signed another is a release nobody
// asked for.
func TestVerifyCatchesTheWrongVersion(t *testing.T) {
	dir := signRelease(t, "agentrqd_linux_arm64")
	if err := verifyCmd([]string{"--version", "0.8.0", dir}, &bytes.Buffer{}); err == nil {
		t.Error("a manifest for a different version verified")
	}
}

func TestVerifyArgumentRefusals(t *testing.T) {
	dir := signRelease(t, "agentrqd_linux_arm64")

	t.Setenv(PubKeyEnv, "")
	if err := verifyCmd([]string{dir}, &bytes.Buffer{}); err == nil {
		t.Error("verify ran with no public key to verify against")
	}
	t.Setenv(PubKeyEnv, "not hex")
	if err := verifyCmd([]string{dir}, &bytes.Buffer{}); err == nil {
		t.Error("nonsense was accepted as a public key")
	}
	t.Setenv(PubKeyEnv, hex.EncodeToString(make([]byte, ed25519.PublicKeySize)))
	if err := verifyCmd([]string{}, &bytes.Buffer{}); err == nil {
		t.Error("verify ran with no directory")
	}
	if err := verifyCmd([]string{t.TempDir()}, &bytes.Buffer{}); err == nil {
		t.Error("a directory with no manifest verified")
	}

	bad := t.TempDir()
	if err := os.WriteFile(filepath.Join(bad, "agentrqd.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCmd([]string{bad}, &bytes.Buffer{}); err == nil {
		t.Error("an unreadable manifest verified")
	}
}

func TestVerifyCatchesASignatureThatIsNotHex(t *testing.T) {
	dir := signRelease(t, "agentrqd_linux_arm64")
	path := filepath.Join(dir, "agentrqd.json")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	m.Signature = "not hex"
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := verifyCmd([]string{dir}, &bytes.Buffer{}); err == nil {
		t.Error("a manifest with a nonsense signature verified")
	}
}
