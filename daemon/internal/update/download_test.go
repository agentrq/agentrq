// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serving answers every request with one body and status.
type serving struct {
	status int
	body   string
	err    error
	gotURL string
}

func (s *serving) Do(r *http.Request) (*http.Response, error) {
	s.gotURL = r.URL.String()
	if s.err != nil {
		return nil, s.err
	}
	return &http.Response{
		StatusCode: s.status,
		Status:     http.StatusText(s.status),
		Body:       io.NopCloser(strings.NewReader(s.body)),
	}, nil
}

// target is the binary an update would replace, which is what Download is
// given: the staged file has to end up with the same extension.
func target(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "agentrqd"+exeSuffix())
}

func TestDownloadVerifiesBeforeItInstalls(t *testing.T) {
	path0 := target(t)
	client := &serving{status: 200, body: "a"}

	path, err := Download(context.Background(), client,
		Artifact{URL: "https://releases.example/agentrqd", SHA256: sum("a")}, path0)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "a" {
		t.Fatalf("downloaded %q, %v", b, err)
	}

	// Executable only after it has been verified: a file that is executable
	// before it has been checked is a file something could run before it was.
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o111 == 0 {
		t.Errorf("mode = %v, want executable", info.Mode())
	}
}

// The case this whole package exists for. A file whose hash does not match is
// not installed, and is not left on disk for a later mistake to pick up.
func TestAFileThatDoesNotMatchIsRefusedAndRemoved(t *testing.T) {
	path0 := target(t)
	dir := filepath.Dir(path0)
	client := &serving{status: 200, body: "something else entirely"}

	_, err := Download(context.Background(), client,
		Artifact{URL: "https://releases.example/agentrqd", SHA256: sum("a")}, path0)
	if !errors.Is(err, ErrChecksumMismatch) {
		t.Fatalf("error = %v, want ErrChecksumMismatch", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Errorf("a rejected download left %v behind", names)
	}
}

func TestDownloadRefusalsBeforeAnythingIsFetched(t *testing.T) {
	path0 := target(t)
	client := &serving{status: 200, body: "a"}

	if _, err := Download(context.Background(), client,
		Artifact{URL: "https://x.example/y"}, path0); !errors.Is(err, ErrNoChecksum) {
		t.Errorf("a checksum-less artifact was fetched anyway")
	}
	if _, err := Download(context.Background(), client,
		Artifact{URL: "http://x.example/y", SHA256: sum("a")}, path0); !errors.Is(err, ErrInsecureURL) {
		t.Errorf("a plain-http artifact was fetched anyway")
	}
	if client.gotURL != "" {
		t.Errorf("a refusal still made a request to %q", client.gotURL)
	}
}

func TestDownloadReportsAServerThatSaysNo(t *testing.T) {
	path0 := target(t)
	dir := filepath.Dir(path0)
	a := Artifact{URL: "https://releases.example/agentrqd", SHA256: sum("a")}

	if _, err := Download(context.Background(), &serving{status: 404}, a, path0); err == nil {
		t.Error("a 404 was treated as a download")
	}
	if _, err := Download(context.Background(), &serving{err: errors.New("no route")}, a, path0); err == nil {
		t.Error("a transport failure was treated as a download")
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Error("a failed download left a file behind")
	}
}

// Beside the target, because the swap is a rename and a rename only works
// within one filesystem — a temp directory elsewhere works on a laptop and
// fails on a machine with /tmp on tmpfs.
// Beside it, and with its extension. On Windows the extension is what makes
// the staged file runnable by name, and the self-test runs it before anything
// is swapped — so a staged file without one could never be installed.
func TestTheDownloadLandsBesideTheBinaryWithItsExtension(t *testing.T) {
	path0 := target(t)
	path, err := Download(context.Background(), &serving{status: 200, body: "a"},
		Artifact{URL: "https://releases.example/agentrqd", SHA256: sum("a")}, path0)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Dir(path) != filepath.Dir(path0) {
		t.Errorf("downloaded to %q, want a file beside %q", path, path0)
	}
	if filepath.Ext(path) != filepath.Ext(path0) {
		t.Errorf("staged as %q, want the extension of %q", path, path0)
	}
}

func TestFetchManifest(t *testing.T) {
	client := &serving{status: 200, body: `{"version":"0.7.1","artifacts":{},"signature":"ab"}`}
	m, err := FetchManifest(context.Background(), client, "https://releases.example/agentrqd.json")
	if err != nil {
		t.Fatalf("FetchManifest: %v", err)
	}
	if m.Version != "0.7.1" || m.Signature != "ab" {
		t.Errorf("manifest = %+v", m)
	}
}

// A field a newer publisher added must not stop an older daemon reading the
// version and the artefact it already understands.
func TestAManifestWithFieldsThisDaemonDoesNotKnowStillReads(t *testing.T) {
	client := &serving{status: 200, body: `{"version":"0.7.1","channel":"beta","artifacts":{},"signature":"ab"}`}
	m, err := FetchManifest(context.Background(), client, "https://releases.example/agentrqd.json")
	if err != nil || m.Version != "0.7.1" {
		t.Errorf("manifest = %+v, %v", m, err)
	}
}

func TestFetchManifestRefusals(t *testing.T) {
	ctx := context.Background()
	if _, err := FetchManifest(ctx, &serving{status: 200}, "http://releases.example/x.json"); !errors.Is(err, ErrInsecureURL) {
		t.Error("a plain-http release feed was fetched")
	}
	if _, err := FetchManifest(ctx, &serving{status: 500}, "https://releases.example/x.json"); err == nil {
		t.Error("a 500 was treated as a manifest")
	}
	if _, err := FetchManifest(ctx, &serving{status: 200, body: "{not json"}, "https://releases.example/x.json"); err == nil {
		t.Error("nonsense was treated as a manifest")
	}
	if _, err := FetchManifest(ctx, &serving{err: errors.New("no route")}, "https://releases.example/x.json"); err == nil {
		t.Error("a transport failure was treated as a manifest")
	}
}
