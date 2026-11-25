// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package hookrunner

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"maps"
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
// checks, and the GitHub and Jira ones must turn a genuine delivery into an
// event.
func TestBuiltInHooksRunOnTheServer(t *testing.T) {
	s := newService(t)
	s.maxPayloadSize = 65536
	// Each array input shows its own example, never another hook's (#1422).
	wantPlaceholders := map[string]map[string]string{
		"generic": {},
		"github":  {"branches": "main, release/*"},
		"jira":    {"assignees": "5b10ac8d82e05b22cc7d4ef5", "actors": "5b10ac8d82e05b22cc7d4ef5", "projects": "ABC, OPS", "labels": "bug, backend"},
	}
	for name, want := range wantPlaceholders {
		m, _, err := s.Inspect(context.Background(), buildHook(t, name))
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		got := map[string]string{}
		for _, in := range m.Inputs {
			if in.Placeholder != "" {
				got[in.Name] = in.Placeholder
			}
		}
		if !maps.Equal(got, want) {
			t.Errorf("the %s hook's inputs have placeholders %v, want %v", name, got, want)
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

// A Jira Automation rule cannot sign, so it sends the secret as a bearer
// token.
func TestBuiltInJiraHookRunsOnTheServer(t *testing.T) {
	s := newService(t)
	s.maxPayloadSize = 65536
	wasm := buildHook(t, "jira")
	m, sum, err := s.Inspect(context.Background(), wasm)
	if err != nil {
		t.Fatal(err)
	}
	handle := func(auth string) (Result, error) {
		return s.Handle(context.Background(),
			Module{SHA256: sum, Load: func(context.Context) ([]byte, error) { return wasm, nil }}, m,
			HandleInput{
				RegistrationID: "0ls4JilchmO",
				Request: Request{Method: "POST", Headers: map[string][]string{"authorization": {auth}},
					Body: []byte(`{"user":{"accountId":"u-1"},"issue":{"self":"https://acme.atlassian.net/rest/api/2/issue/1","key":"ABC-7","fields":{"summary":"Login fails","project":{"key":"ABC"},"assignee":{"accountId":"a-1"}}}}`)},
				Inputs: map[string]any{"webhookSecret": "s3cret", "assignees": []string{"a-1"}, "actors": []string{"u-1"}, "projects": []string{"ABC"}},
				Now:    time.Now(),
			})
	}
	res, err := handle("Bearer s3cret")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Accept || len(res.Events) != 1 || res.Events[0].Name != "jira.issue.sent" ||
		res.Events[0].Payload != "Jira issue ABC-7 sent: Login fails\nhttps://acme.atlassian.net/browse/ABC-7" {
		t.Errorf("result = %+v", res)
	}
	if res, err := handle("Bearer nope"); err != nil || res.Accept {
		t.Errorf("a wrong token = %+v, %v", res, err)
	}
}
