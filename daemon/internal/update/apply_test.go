// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// releaseServer answers the manifest URL with a manifest and the artefact URL
// with a binary.
type releaseServer struct {
	manifest string
	artifact string
	requests []string
}

func (s *releaseServer) Do(r *http.Request) (*http.Response, error) {
	s.requests = append(s.requests, r.URL.String())
	body := s.artifact
	if strings.HasSuffix(r.URL.Path, ".json") {
		body = s.manifest
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(strings.NewReader(body)),
	}, nil
}

// stagedRelease builds a signed manifest for a stub binary that reports the
// given version, and the server that serves both.
func stagedRelease(t *testing.T, version, reports string) (*releaseServer, string) {
	t.Helper()
	dir := t.TempDir()

	var body string
	if runtime.GOOS == "windows" {
		body = "@echo off\r\necho agentrqd " + reports + "\r\n"
	} else {
		body = "#!/bin/sh\necho 'agentrqd " + reports + "'\n"
	}
	h := sha256.Sum256([]byte(body))

	key := goosArch()
	m := signedManifest(t, Manifest{
		Version:   version,
		Artifacts: map[string]Artifact{key: {URL: "https://releases.example/agentrqd", SHA256: hex.EncodeToString(h[:])}},
	})
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	// The "current" binary, so Prepare has something to stage beside — and
	// with the stub's own extension, because the staged file inherits it and
	// Windows will not run a batch file that is not called .bat. A real
	// release replaces agentrqd.exe with agentrqd.exe; this replaces a script
	// with a script.
	current := filepath.Join(dir, "agentrqd"+stubSuffix())
	if err := os.WriteFile(current, []byte("v-old"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &releaseServer{manifest: string(b), artifact: body}, current
}

func goosArch() string { return runtime.GOOS + "/" + runtime.GOARCH }

// stubSuffix is the extension a runnable stub needs on this platform.
func stubSuffix() string {
	if runtime.GOOS == "windows" {
		return ".bat"
	}
	return ""
}

func TestPrepareVerifiesDownloadsAndTests(t *testing.T) {
	srv, current := stagedRelease(t, "0.7.1", "0.7.1")

	plan, err := Prepare(context.Background(), srv,
		"https://releases.example/agentrqd.json", "0.7.0", runtime.GOOS, runtime.GOARCH, current)
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	if plan.Version != "0.7.1" {
		t.Errorf("version = %q", plan.Version)
	}
	if filepath.Dir(plan.Staged) != filepath.Dir(current) {
		t.Errorf("staged at %q, want beside %q", plan.Staged, current)
	}

	// Nothing has been replaced. A plan is everything reversible and none of
	// the destructive work.
	if b, _ := os.ReadFile(current); string(b) != "v-old" {
		t.Errorf("the current binary is %q; Prepare replaced something", b)
	}
}

func TestAbandonLeavesTheMachineAsItWas(t *testing.T) {
	srv, current := stagedRelease(t, "0.7.1", "0.7.1")
	plan, err := Prepare(context.Background(), srv,
		"https://releases.example/agentrqd.json", "0.7.0", runtime.GOOS, runtime.GOARCH, current)
	if err != nil {
		t.Fatal(err)
	}

	plan.Abandon()
	if _, err := os.Stat(plan.Staged); !os.IsNotExist(err) {
		t.Error("an abandoned plan left its binary on disk")
	}
	// And abandoning twice is not a problem.
	plan.Abandon()
}

// A binary that does not run here is not a binary to leave lying next to the
// one that does.
func TestAFailedSelfTestStagesNothing(t *testing.T) {
	srv, current := stagedRelease(t, "0.7.1", "0.6.4")

	_, err := Prepare(context.Background(), srv,
		"https://releases.example/agentrqd.json", "0.7.0", runtime.GOOS, runtime.GOARCH, current)
	if !errors.Is(err, ErrSelfTestFailed) {
		t.Fatalf("error = %v, want ErrSelfTestFailed", err)
	}

	entries, err := os.ReadDir(filepath.Dir(current))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("a refused update left %d files behind, want only the current binary", len(entries))
	}
}

func TestPrepareDoesNothingWhenThereIsNothingNewer(t *testing.T) {
	srv, current := stagedRelease(t, "0.7.1", "0.7.1")

	_, err := Prepare(context.Background(), srv,
		"https://releases.example/agentrqd.json", "0.7.1", runtime.GOOS, runtime.GOARCH, current)
	if !errors.Is(err, ErrAlreadyCurrent) {
		t.Fatalf("error = %v, want ErrAlreadyCurrent", err)
	}
	// One request: the manifest. Nothing was downloaded.
	if len(srv.requests) != 1 {
		t.Errorf("requests = %v", srv.requests)
	}
}

// A build with no release key stops here, before anything is downloaded.
func TestPrepareRefusesWithoutAReleaseKey(t *testing.T) {
	srv, current := stagedRelease(t, "0.7.1", "0.7.1")
	ReleaseKey = ""

	_, err := Prepare(context.Background(), srv,
		"https://releases.example/agentrqd.json", "0.7.0", runtime.GOOS, runtime.GOARCH, current)
	if !errors.Is(err, ErrNoReleaseKey) {
		t.Fatalf("error = %v, want ErrNoReleaseKey", err)
	}
	if len(srv.requests) != 1 {
		t.Errorf("a keyless build still downloaded something: %v", srv.requests)
	}
}

func TestPrepareReportsAnUnreachableFeed(t *testing.T) {
	_, current := stagedRelease(t, "0.7.1", "0.7.1")
	if _, err := Prepare(context.Background(), &serving{status: 503},
		"https://releases.example/agentrqd.json", "0.7.0", runtime.GOOS, runtime.GOARCH, current); err == nil {
		t.Error("an unreachable release feed was treated as an update")
	}
}

// Both restart paths exist, and which one is used is read from the environment
// the supervisor itself sets rather than from a setting somebody has to
// remember to change.
func TestDetectMode(t *testing.T) {
	env := func(pairs map[string]string) func(string) string {
		return func(k string) string { return pairs[k] }
	}

	for name, tc := range map[string]struct {
		vars map[string]string
		goos string
		want Mode
	}{
		"under systemd":                {map[string]string{"INVOCATION_ID": "abc"}, "linux", ModeSupervisor},
		"under systemd, notify socket": {map[string]string{"NOTIFY_SOCKET": "/run/x"}, "linux", ModeSupervisor},
		"under launchd":                {map[string]string{"XPC_SERVICE_NAME": "com.agentrq.daemon"}, "darwin", ModeSupervisor},
		"started by hand":              {map[string]string{}, "linux", ModeReexec},
		"on windows":                   {map[string]string{}, "windows", ModeReexec},
		// launchd's variable means nothing on Linux, and an uncertain answer
		// must be the one that leaves a daemon running.
		"launchd's variable elsewhere": {map[string]string{"XPC_SERVICE_NAME": "x"}, "linux", ModeReexec},
		"an empty launchd job name":    {map[string]string{"XPC_SERVICE_NAME": "  "}, "darwin", ModeReexec},
	} {
		t.Run(name, func(t *testing.T) {
			if got := DetectMode(env(tc.vars), tc.goos); got != tc.want {
				t.Errorf("DetectMode = %q, want %q", got, tc.want)
			}
		})
	}
}
