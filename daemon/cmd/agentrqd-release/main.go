// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

// Command agentrqd-release produces the signed manifest the daemon verifies
// before it replaces itself.
//
// This is the other half of the rule in internal/update: a daemon refuses to
// install anything that is not signed by the release key, so the release has
// to actually sign it. A release process that publishes binaries and no
// manifest produces a daemon that correctly refuses to update — which is the
// safe failure, and still a broken release.
//
//	agentrqd-release keygen
//	agentrqd-release manifest --version 0.7.1 --base-url https://… dist/
//
// The private key is read from AGENTRQD_RELEASE_KEY and never from a flag: an
// argument is visible in `ps` to every user on the build machine, and a CI log
// that echoes its own command line would print it.
package main

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// KeyEnv is where the signing key comes from.
const KeyEnv = "AGENTRQD_RELEASE_KEY"

// PubKeyEnv is the public half — the one built into the binaries.
const PubKeyEnv = "AGENTRQD_RELEASE_PUBKEY"

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "agentrqd-release: %v\n", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Print(usage)
		return nil
	}
	switch args[0] {
	case "keygen":
		return keygen(os.Stdout)
	case "manifest":
		return manifestCmd(args[1:], os.Stdout)
	case "verify":
		return verifyCmd(args[1:], os.Stdout)
	case "-h", "--help", "help":
		fmt.Print(usage)
		return nil
	default:
		return fmt.Errorf("unknown command %q\n\n%s", args[0], usage)
	}
}

const usage = `agentrqd-release — sign a daemon release

  agentrqd-release keygen
  agentrqd-release manifest --version <x.y.z> --base-url <url> <dist dir>
  agentrqd-release verify   --version <x.y.z> <dist dir>

The private key is read from ` + KeyEnv + `. Never pass it as an argument:
that is visible in ps to every user on the machine.
`

// keygen prints a new key pair.
//
// The public half goes into the build (-ldflags -X …update.ReleaseKey=…) and
// the private half into the release job's secrets. They are printed labelled
// and separately so nobody pastes the wrong one into a build.
func keygen(out io.Writer) error {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "public  (build into the daemon):  %s\n", hex.EncodeToString(pub))
	fmt.Fprintf(out, "private (%s):    %s\n", KeyEnv, hex.EncodeToString(priv))
	fmt.Fprintln(out, "\nThe private key is not recoverable. Store it before you lose this output.")
	fmt.Fprintln(out, "Changing it means every daemon built with the old public key can no")
	fmt.Fprintln(out, "longer update itself and has to be replaced by hand.")
	return nil
}

// artifactPattern is how a released binary is named, and how its platform is
// read back out of the filename.
//
// Matching the name rather than being told the platform: the filename is what
// is published, and deriving the mapping from it makes a mismatch between the
// manifest and the file impossible rather than merely unlikely.
var artifactPattern = regexp.MustCompile(`^agentrqd_([a-z0-9]+)_([a-z0-9]+)(\.exe)?$`)

type artifact struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size,omitempty"`
}

type signedPart struct {
	Version   string              `json:"version"`
	Artifacts map[string]artifact `json:"artifacts"`
}

type manifest struct {
	Version   string              `json:"version"`
	Artifacts map[string]artifact `json:"artifacts"`
	Signature string              `json:"signature"`
}

func manifestCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	version := fs.String("version", "", "the release version, x.y.z")
	baseURL := fs.String("base-url", "", "where the binaries will be published, https")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("give exactly one directory of built binaries")
	}
	dir := fs.Arg(0)

	if *version == "" {
		return errors.New("--version is required")
	}
	if !strings.HasPrefix(*baseURL, "https://") {
		// The checksum is what protects the artefact, but an update fetched in
		// the clear tells every network in between which version a machine is
		// about to run — and the daemon refuses a plain-http URL anyway, so a
		// manifest with one would be a release nobody can install.
		return fmt.Errorf("--base-url must be https, got %q", *baseURL)
	}

	keyHex := strings.TrimSpace(os.Getenv(KeyEnv))
	if keyHex == "" {
		return fmt.Errorf("%s is not set; run `agentrqd-release keygen` and store the private key", KeyEnv)
	}
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		return fmt.Errorf("%s is not an ed25519 private key", KeyEnv)
	}

	artifacts, err := collect(dir, strings.TrimSuffix(*baseURL, "/"))
	if err != nil {
		return err
	}
	if len(artifacts) == 0 {
		return fmt.Errorf("no binaries named agentrqd_<os>_<arch> in %s", dir)
	}

	part := signedPart{Version: *version, Artifacts: artifacts}
	body, err := json.Marshal(part)
	if err != nil {
		return err
	}
	m := manifest{
		Version:   part.Version,
		Artifacts: part.Artifacts,
		Signature: hex.EncodeToString(ed25519.Sign(ed25519.PrivateKey(key), body)),
	}

	encoded, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	fmt.Fprintln(out, string(encoded))

	// To stderr, so it does not end up in the manifest when the output is
	// redirected — and said at all because a release missing a platform is
	// silent otherwise until somebody on it tries to update.
	platforms := make([]string, 0, len(artifacts))
	for p := range artifacts {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	fmt.Fprintf(os.Stderr, "signed %s for %d platforms: %s\n", *version, len(platforms), strings.Join(platforms, ", "))
	return nil
}

// collect hashes every released binary in a directory.
func collect(dir, baseURL string) (map[string]artifact, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	out := map[string]artifact{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		m := artifactPattern.FindStringSubmatch(e.Name())
		if m == nil {
			continue
		}
		goos, goarch := m[1], m[2]

		path := filepath.Join(dir, e.Name())
		sum, size, err := hashFile(path)
		if err != nil {
			return nil, err
		}
		out[goos+"/"+goarch] = artifact{
			URL:    baseURL + "/" + e.Name(),
			SHA256: sum,
			Size:   size,
		}
	}
	return out, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer func() { _ = f.Close() }()

	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

// verifyCmd checks a release the way a daemon will.
//
// Proving the release is *installable*, not merely that it built. A manifest
// signed with a key that does not match the one compiled into the binaries is
// a release every daemon correctly refuses — and the failure is silent until
// somebody, months later, presses update and nothing happens. Finding that out
// in the release job costs a minute.
func verifyCmd(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	version := fs.String("version", "", "the version that should be in the manifest")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("give exactly one directory containing agentrqd.json")
	}
	dir := fs.Arg(0)

	pubHex := strings.TrimSpace(os.Getenv(PubKeyEnv))
	if pubHex == "" {
		return fmt.Errorf("%s is not set; it must be the key built into these binaries", PubKeyEnv)
	}
	pub, err := hex.DecodeString(pubHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("%s is not an ed25519 public key", PubKeyEnv)
	}

	b, err := os.ReadFile(filepath.Join(dir, "agentrqd.json"))
	if err != nil {
		return fmt.Errorf("no manifest to verify: %w", err)
	}
	var m manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return fmt.Errorf("the manifest is not readable: %w", err)
	}

	if *version != "" && m.Version != *version {
		return fmt.Errorf("the manifest says %q, this release is %q", m.Version, *version)
	}

	// Rebuilt from the parsed fields rather than taken from the file, exactly
	// as the daemon does it — so this checks what will actually be verified
	// rather than what happens to be on disk.
	body, err := json.Marshal(signedPart{Version: m.Version, Artifacts: m.Artifacts})
	if err != nil {
		return err
	}
	sig, err := hex.DecodeString(m.Signature)
	if err != nil {
		return errors.New("the manifest's signature is not hex")
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), body, sig) {
		return errors.New("the manifest does not verify against the key built into these binaries — no daemon will install this release")
	}

	// And every file it names is actually here, with the hash it claims. A
	// manifest that verifies but points at a file that was never uploaded is
	// the same broken release wearing a valid signature.
	for platform, a := range m.Artifacts {
		name := path.Base(a.URL)
		sum, size, err := hashFile(filepath.Join(dir, name))
		if err != nil {
			return fmt.Errorf("%s: the manifest names %s, which is not here: %w", platform, name, err)
		}
		if sum != a.SHA256 {
			return fmt.Errorf("%s: %s does not match the checksum in the manifest", platform, name)
		}
		if a.Size != 0 && a.Size != size {
			return fmt.Errorf("%s: %s is %d bytes, the manifest says %d", platform, name, size, a.Size)
		}
	}

	platforms := make([]string, 0, len(m.Artifacts))
	for p := range m.Artifacts {
		platforms = append(platforms, p)
	}
	sort.Strings(platforms)
	fmt.Fprintf(out, "%s verifies against the shipped key, %d platforms: %s\n",
		m.Version, len(platforms), strings.Join(platforms, ", "))
	return nil
}
