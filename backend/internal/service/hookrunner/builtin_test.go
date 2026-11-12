// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package hookrunner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// buildHook builds a built-in hook from wasm-hooks/ for wasip1.
func buildHook(t *testing.T, name string) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), name+".wasm")
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags=-s -w", "-o", out, "./"+name)
	cmd.Dir = "../../../../wasm-hooks"
	cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build %s: %v\n%s", name, err, b)
	}
	wasm, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return wasm
}

// The built-in hooks are what the server runs, so each must pass its own
// checks, and the GitHub one must turn a signed delivery into an event.
func TestBuiltInHooksRunOnTheServer(t *testing.T) {
	s := newService(t)
	s.maxPayloadSize = 65536
	for _, name := range []string{"generic", "github"} {
		if _, _, err := s.Inspect(context.Background(), buildHook(t, name)); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}

	wasm := buildHook(t, "github")
	m, sum, err := s.Inspect(context.Background(), wasm)
	if err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"action":"completed","repository":{"full_name":"o/r"},"workflow_run":{"name":"test","head_branch":"main","head_sha":"5c9201cf","conclusion":"failure","html_url":"https://github.com/o/r/actions/runs/1"}}`)
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(body)
	res, err := s.Handle(context.Background(),
		Module{SHA256: sum, Load: func(context.Context) ([]byte, error) { return wasm, nil }}, m,
		HandleInput{
			RegistrationID: "0ls4JilchmO",
			Request: Request{Method: "POST", Body: body, Headers: map[string][]string{
				"x-github-event":      {"workflow_run"},
				"x-github-delivery":   {"d-1"},
				"x-hub-signature-256": {"sha256=" + hex.EncodeToString(mac.Sum(nil))},
			}},
			Inputs: map[string]any{"webhookSecret": "s3cret", "branches": []string{"main"}},
			Now:    time.Now(),
		})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Accept || len(res.Events) != 1 || res.Events[0].Name != "ci.failed" || res.Events[0].DedupKey != "d-1" ||
		!strings.HasPrefix(res.Events[0].Payload, "Workflow `test` failed on `main`") {
		t.Errorf("result = %+v", res)
	}
}
