// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/wasm-hooks/sdk"
)

const secret = "s3cret"

func delivery(kind, body string, branches ...string) sdk.In {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	in := sdk.In{
		Inputs: map[string]json.RawMessage{"webhookSecret": json.RawMessage(`"` + secret + `"`)},
		Request: sdk.Request{Body: []byte(body), Headers: map[string][]string{
			"x-github-event":      {kind},
			"x-github-delivery":   {"d-1"},
			"x-hub-signature-256": {"sha256=" + hex.EncodeToString(mac.Sum(nil))},
		}},
	}
	if branches != nil {
		in.Inputs["branches"], _ = json.Marshal(branches)
	}
	return in
}

const workflowRun = `{"action":"completed","repository":{"full_name":"agentrq/agentrq"},"workflow_run":{"name":"test","head_branch":"%s","head_sha":"5c9201cf0000","conclusion":"%s","html_url":"https://github.com/agentrq/agentrq/actions/runs/1","head_commit":{"message":"fix: it\nbody","author":{"name":"Ada"}}}}`

// one is the single event of an accepted request, which the manifest must
// declare.
func one(t *testing.T, res sdk.Result) sdk.Out {
	t.Helper()
	if !res.Accept || len(res.Events) != 1 {
		t.Fatalf("result = %+v, want one event", res)
	}
	declared := false
	for _, e := range manifest.Events {
		declared = declared || e.Name == res.Events[0].Name
	}
	if !declared {
		t.Errorf("%q is not in the manifest", res.Events[0].Name)
	}
	return res.Events[0]
}

func none(t *testing.T, res sdk.Result) {
	t.Helper()
	if !res.Accept || len(res.Events) != 0 {
		t.Errorf("result = %+v, want accepted with no event", res)
	}
}

func TestTheSignatureIsChecked(t *testing.T) {
	in := delivery("ping", `{}`)
	in.Request.Body = []byte(`{"changed":true}`)
	if res := handle(in); res.Accept {
		t.Error("a body that does not match its signature was accepted")
	}
	in = delivery("ping", `{}`)
	in.Request.Headers["x-hub-signature-256"] = []string{"sha1=abc"}
	if res := handle(in); res.Accept {
		t.Error("a signature without sha256= was accepted")
	}
	in = delivery("ping", `{}`)
	in.Inputs["webhookSecret"] = json.RawMessage(`"other"`)
	if res := handle(in); res.Accept {
		t.Error("another secret was accepted")
	}
	if res := handle(delivery("ping", `not json`)); res.Accept {
		t.Error("a body that is not JSON was accepted")
	}
	none(t, handle(delivery("ping", `{"zen":"Keep it simple."}`)))
}

func TestWorkflowRuns(t *testing.T) {
	e := one(t, handle(delivery("workflow_run", fmt.Sprintf(workflowRun, "main", "failure"))))
	want := "Workflow `test` failed on `main` @ 5c9201c in agentrq/agentrq\nRun: https://github.com/agentrq/agentrq/actions/runs/1\nCommit: fix: it (Ada)"
	if e.Name != "ci.failed" || e.Payload != want || e.DedupKey != "d-1" || len(e.FAQ) != 4 {
		t.Errorf("event = %+v", e)
	}
	if e := one(t, handle(delivery("workflow_run", fmt.Sprintf(workflowRun, "main", "timed_out")))); e.Name != "ci.failed" || !strings.Contains(e.Payload.(string), "timed out") {
		t.Errorf("timed out = %+v", e)
	}
	if e := one(t, handle(delivery("workflow_run", fmt.Sprintf(workflowRun, "main", "success")))); e.Name != "ci.succeeded" {
		t.Errorf("success = %+v", e)
	}
	none(t, handle(delivery("workflow_run", fmt.Sprintf(workflowRun, "main", "cancelled"))))
	none(t, handle(delivery("workflow_run", strings.Replace(fmt.Sprintf(workflowRun, "main", "failure"), "completed", "requested", 1))))
}

func TestBranchesFilter(t *testing.T) {
	failed := func(branch string, globs ...string) sdk.Result {
		return handle(delivery("workflow_run", fmt.Sprintf(workflowRun, branch, "failure"), globs...))
	}
	one(t, failed("release/1.2", "main", "release/*"))
	none(t, failed("feature/x", "main", "release/*"))
	one(t, failed("anything"))
}

func TestPullRequests(t *testing.T) {
	const pr = `{"action":"%s","repository":{"full_name":"o/r"},"pull_request":{"number":7,"title":"Add hooks","html_url":"https://github.com/o/r/pull/7","user":{"login":"ada"},"base":{"ref":"main"},"head":{"ref":"feat/hooks"}}%s}`
	e := one(t, handle(delivery("pull_request", fmt.Sprintf(pr, "opened", ""))))
	if want := "Pull request #7 opened by ada in o/r: Add hooks\n`feat/hooks` → `main`\nhttps://github.com/o/r/pull/7"; e.Name != "pr.opened" || e.Payload != want {
		t.Errorf("opened = %+v", e)
	}
	e = one(t, handle(delivery("pull_request", fmt.Sprintf(pr, "review_requested", `,"requested_reviewer":{"login":"bob"}`))))
	if e.Name != "pr.review_requested" || !strings.Contains(e.Payload.(string), "review requested from bob") {
		t.Errorf("review requested = %+v", e)
	}
	e = one(t, handle(delivery("pull_request", fmt.Sprintf(pr, "review_requested", ""))))
	if !strings.Contains(e.Payload.(string), "#7 review requested in") {
		t.Errorf("a team review = %+v", e)
	}
	none(t, handle(delivery("pull_request", fmt.Sprintf(pr, "closed", ""))))
	none(t, handle(delivery("pull_request", fmt.Sprintf(pr, "opened", ""), "release/*")))
}

func TestIssuesAndReleases(t *testing.T) {
	e := one(t, handle(delivery("issues", `{"action":"opened","repository":{"full_name":"o/r"},"issue":{"number":3,"title":"Bug","html_url":"https://github.com/o/r/issues/3","user":{"login":"ada"}}}`)))
	if e.Name != "issue.opened" || e.Payload != "Issue #3 opened by ada in o/r: Bug\nhttps://github.com/o/r/issues/3" {
		t.Errorf("issue = %+v", e)
	}
	e = one(t, handle(delivery("release", `{"action":"published","repository":{"full_name":"o/r"},"release":{"tag_name":"v1.0.0","name":"One","html_url":"https://github.com/o/r/releases/tag/v1.0.0"}}`)))
	if e.Name != "release.published" || e.Payload != "Release v1.0.0 (One) published in o/r\nhttps://github.com/o/r/releases/tag/v1.0.0" {
		t.Errorf("release = %+v", e)
	}
	none(t, handle(delivery("issues", `{"action":"closed","issue":{"number":3}}`)))
}

func TestPushes(t *testing.T) {
	const push = `{"ref":"%s","compare":"https://github.com/o/r/compare/a...b","repository":{"full_name":"o/r"},"pusher":{"name":"ada"},"commits":%s}`
	e := one(t, handle(delivery("push", fmt.Sprintf(push, "refs/heads/main", `[{"id":"aaaaaaaaaa","message":"one\nmore"},{"id":"bbb","message":"two"}]`))))
	want := "ada pushed 2 commit(s) to `main` in o/r\nhttps://github.com/o/r/compare/a...b\n- aaaaaaa one\n- bbb two"
	if e.Name != "push" || e.Payload != want {
		t.Errorf("push = %q", e.Payload)
	}
	none(t, handle(delivery("push", fmt.Sprintf(push, "refs/tags/v1", `[{"id":"a","message":"m"}]`))))
	none(t, handle(delivery("push", fmt.Sprintf(push, "refs/heads/main", `[]`))))
	none(t, handle(delivery("push", fmt.Sprintf(push, "refs/heads/main", `[{"id":"a","message":"m"}]`), "release/*")))
}
