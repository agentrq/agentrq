// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

// signedManifest builds a manifest signed by a freshly generated key, and
// installs that key as the release key for the duration of the test.
func signedManifest(t *testing.T, m Manifest) Manifest {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	previous := ReleaseKey
	ReleaseKey = hex.EncodeToString(pub)
	t.Cleanup(func() { ReleaseKey = previous })

	body, err := m.signedBytes()
	if err != nil {
		t.Fatal(err)
	}
	m.Signature = hex.EncodeToString(ed25519.Sign(priv, body))
	return m
}

func sum(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

func release(t *testing.T) Manifest {
	t.Helper()
	return signedManifest(t, Manifest{
		Version: "0.7.1",
		Artifacts: map[string]Artifact{
			"linux/arm64":   {URL: "https://releases.agentrq.com/agentrqd-0.7.1-linux-arm64", SHA256: sum("a")},
			"windows/amd64": {URL: "https://releases.agentrq.com/agentrqd-0.7.1-windows-amd64.exe", SHA256: sum("b")},
		},
	})
}

func TestAVerifiedReleaseYieldsItsArtifact(t *testing.T) {
	a, err := release(t).ArtifactFor("linux", "arm64")
	if err != nil {
		t.Fatalf("ArtifactFor: %v", err)
	}
	if a.SHA256 != sum("a") {
		t.Errorf("artifact = %+v", a)
	}
}

// The single most important test in this package. A build nobody has given a
// key to cannot update itself, because a verification step that silently
// passes when it has nothing to verify against is worse than none — it looks
// like one.
func TestABuildWithNoReleaseKeyRefusesToUpdate(t *testing.T) {
	m := release(t)
	ReleaseKey = ""

	if _, err := m.ArtifactFor("linux", "arm64"); !errors.Is(err, ErrNoReleaseKey) {
		t.Fatalf("error = %v, want ErrNoReleaseKey", err)
	}
	if err := m.Verify(); !errors.Is(err, ErrNoReleaseKey) {
		t.Errorf("Verify = %v, want ErrNoReleaseKey", err)
	}
}

// Tampering with any part the signature covers must be caught, including the
// parts that are not the binary: the version and the platform table decide
// *which* file gets run.
func TestTamperingIsCaught(t *testing.T) {
	for name, tamper := range map[string]func(Manifest) Manifest{
		"a different artifact url": func(m Manifest) Manifest {
			a := m.Artifacts["linux/arm64"]
			a.URL = "https://elsewhere.example/payload"
			m.Artifacts["linux/arm64"] = a
			return m
		},
		"a different checksum": func(m Manifest) Manifest {
			a := m.Artifacts["linux/arm64"]
			a.SHA256 = sum("something else")
			m.Artifacts["linux/arm64"] = a
			return m
		},
		"a different version": func(m Manifest) Manifest {
			m.Version = "9.9.9"
			return m
		},
		"an added platform": func(m Manifest) Manifest {
			m.Artifacts["darwin/arm64"] = Artifact{URL: "https://x.example/y", SHA256: sum("c")}
			return m
		},
	} {
		t.Run(name, func(t *testing.T) {
			m := tamper(release(t))
			if _, err := m.ArtifactFor("linux", "arm64"); !errors.Is(err, ErrBadSignature) {
				t.Errorf("error = %v, want ErrBadSignature", err)
			}
		})
	}
}

func TestSignatureRefusals(t *testing.T) {
	t.Run("no signature at all", func(t *testing.T) {
		m := release(t)
		m.Signature = ""
		if err := m.Verify(); !errors.Is(err, ErrUnsigned) {
			t.Errorf("error = %v, want ErrUnsigned", err)
		}
	})

	t.Run("a signature that is not hex", func(t *testing.T) {
		m := release(t)
		m.Signature = "not hex"
		if err := m.Verify(); !errors.Is(err, ErrBadSignature) {
			t.Errorf("error = %v, want ErrBadSignature", err)
		}
	})

	t.Run("a release key that is not a key", func(t *testing.T) {
		m := release(t)
		for _, bad := range []string{"zz", hex.EncodeToString([]byte("too short"))} {
			ReleaseKey = bad
			if err := m.Verify(); !errors.Is(err, ErrBadKey) {
				t.Errorf("ReleaseKey=%q gave %v, want ErrBadKey", bad, err)
			}
		}
	})

	t.Run("a signature by somebody else's key", func(t *testing.T) {
		m := release(t)
		other, _, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		ReleaseKey = hex.EncodeToString(other)
		if err := m.Verify(); !errors.Is(err, ErrBadSignature) {
			t.Errorf("error = %v, want ErrBadSignature", err)
		}
	})
}

// An artefact without a checksum is refused rather than downloaded and hoped
// about: the checksum is the only thing standing between the manifest and
// whatever the artefact URL actually serves.
func TestAnArtifactWithoutAUsableChecksumIsRefused(t *testing.T) {
	for name, sha := range map[string]string{
		"none at all":      "",
		"not hex":          "nonsense",
		"the wrong length": hex.EncodeToString([]byte("short")),
	} {
		t.Run(name, func(t *testing.T) {
			m := signedManifest(t, Manifest{
				Version:   "0.7.1",
				Artifacts: map[string]Artifact{"linux/arm64": {URL: "https://x.example/y", SHA256: sha}},
			})
			if _, err := m.ArtifactFor("linux", "arm64"); !errors.Is(err, ErrNoChecksum) {
				t.Errorf("error = %v, want ErrNoChecksum", err)
			}
		})
	}
}

// An update fetched in the clear tells every network between here and there
// exactly which version this machine is about to run.
func TestAnArtifactMustBeFetchedOverHTTPS(t *testing.T) {
	for _, raw := range []string{"http://releases.example/x", "ftp://releases.example/x", "", "https://"} {
		m := signedManifest(t, Manifest{
			Version:   "0.7.1",
			Artifacts: map[string]Artifact{"linux/arm64": {URL: raw, SHA256: sum("a")}},
		})
		if _, err := m.ArtifactFor("linux", "arm64"); !errors.Is(err, ErrInsecureURL) {
			t.Errorf("url %q gave %v, want ErrInsecureURL", raw, err)
		}
	}
}

func TestAPlatformWithNoBuildIsSaidSo(t *testing.T) {
	if _, err := release(t).ArtifactFor("plan9", "mips"); !errors.Is(err, ErrNoArtifact) {
		t.Errorf("error = %v, want ErrNoArtifact", err)
	}
}

// A "version" with a path separator in it is not a version, and this one
// reaches a file path and a log line.
func TestAnUnusableVersionIsRefused(t *testing.T) {
	for _, v := range []string{"", "latest", "../../etc/passwd", "0.7", "v0.7.1", "0.7.1 "} {
		m := signedManifest(t, Manifest{
			Version:   v,
			Artifacts: map[string]Artifact{"linux/arm64": {URL: "https://x.example/y", SHA256: sum("a")}},
		})
		if _, err := m.ArtifactFor("linux", "arm64"); !errors.Is(err, ErrBadVersion) {
			t.Errorf("version %q gave %v, want ErrBadVersion", v, err)
		}
	}
}

func TestNewer(t *testing.T) {
	// The case that catches a naive string comparison.
	if !Newer("0.9.0", "0.10.0") {
		t.Error("0.10.0 was not seen as newer than 0.9.0")
	}
	if Newer("0.10.0", "0.9.0") {
		t.Error("0.9.0 was seen as newer than 0.10.0")
	}
	if Newer("1.2.3", "1.2.3") {
		t.Error("a version was seen as newer than itself")
	}
	if !Newer("1.2.3", "1.3.0") || !Newer("1.2.3", "2.0.0") || !Newer("1.2.3", "1.2.4") {
		t.Error("an ordinary bump was not seen as newer")
	}

	// A daemon that cannot read its own version must not conclude that it
	// should update itself. "Unknown" is answered as "not newer".
	for _, pair := range [][2]string{{"dev", "0.7.1"}, {"0.7.1", "dev"}, {"", ""}, {"0.7.1", "latest"}} {
		if Newer(pair[0], pair[1]) {
			t.Errorf("Newer(%q, %q) = true, want false", pair[0], pair[1])
		}
	}

	// A pre-release is ordered by its base version, which is close enough to
	// answer "is there something newer" and never wrong in the dangerous
	// direction.
	if !Newer("0.7.0", "0.7.1-rc1") {
		t.Error("a pre-release of a later version was not seen as newer")
	}
}
