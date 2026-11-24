// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/wasm-hooks/sdk"
)

const secret = "It's a Secret to Everybody"

// webhook is a Jira admin webhook's delivery, signed.
func webhook(body string) sdk.In {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return sdk.In{
		Inputs: map[string]json.RawMessage{"webhookSecret": json.RawMessage(`"It's a Secret to Everybody"`)},
		Request: sdk.Request{Body: []byte(body), Headers: map[string][]string{
			"x-hub-signature":                {"sha256=" + hex.EncodeToString(mac.Sum(nil))},
			"x-atlassian-webhook-identifier": {"w-1"},
		}},
	}
}

// automation is an Automation rule's Send web request, with the bearer token.
func automation(body string) sdk.In {
	return sdk.In{
		Inputs:  map[string]json.RawMessage{"webhookSecret": json.RawMessage(`"It's a Secret to Everybody"`)},
		Request: sdk.Request{Body: []byte(body), Headers: map[string][]string{"authorization": {"Bearer " + secret}}},
	}
}

func with(in sdk.In, input string, values ...string) sdk.In {
	in.Inputs[input], _ = json.Marshal(values)
	return in
}

const issueJSON = `{"self":"https://acme.atlassian.net/rest/api/2/issue/10001","key":"ABC-7","fields":{"summary":"Login fails","description":"Steps:\n1. open","issuetype":{"name":"Bug"},"priority":{"name":"High"},"status":{"name":"To Do"},"project":{"key":"ABC","name":"Alpha"},"reporter":{"accountId":"r-1","displayName":"Rae"},"assignee":{"accountId":"a-1","displayName":"Ada"},"labels":["agentrq","web"]}}`

const actor = `"user":{"accountId":"u-1","displayName":"Uma"}`

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

func rejected(t *testing.T, res sdk.Result, why string) {
	t.Helper()
	if res.Accept || res.Reason == "" {
		t.Errorf("%s was accepted: %+v", why, res)
	}
}

// Atlassian's own example: this secret and body sign to this value.
func TestAtlassiansSignatureExample(t *testing.T) {
	if !sdk.ValidHMACSHA256(secret, []byte("Hello World!"), "a4771c39fbe90f317c7824e83ddef3caae9cb3d976c214ace1f2937e133263c9") {
		t.Fatal("the documented signature does not verify")
	}
}

func TestTheSecretIsChecked(t *testing.T) {
	body := `{"webhookEvent":"jira:issue_created","issue":` + issueJSON + `}`

	in := webhook(body)
	in.Request.Body = []byte(strings.Replace(body, "Login", "Logout", 1))
	rejected(t, handle(in), "a body that does not match its signature")

	in = webhook(body)
	in.Request.Headers["x-hub-signature"] = []string{"sha1=abc"}
	rejected(t, handle(in), "a signature without sha256=")

	// A signature is checked even when a token comes with it.
	in = webhook(body)
	in.Request.Headers["x-hub-signature"] = []string{"sha256=00"}
	in.Request.Headers["authorization"] = []string{"Bearer " + secret}
	rejected(t, handle(in), "a wrong signature with the right token")

	in = webhook(body)
	in.Inputs["webhookSecret"] = json.RawMessage(`"other"`)
	rejected(t, handle(in), "another secret")

	in = automation(body)
	in.Request.Headers["authorization"] = []string{"Bearer nope"}
	rejected(t, handle(in), "a wrong token")

	in = automation(body)
	in.Request.Headers["authorization"] = []string{secret}
	rejected(t, handle(in), "a token without Bearer")

	in = automation(body)
	delete(in.Request.Headers, "authorization")
	rejected(t, handle(in), "a request with neither")

	in = automation(body)
	in.Inputs["webhookSecret"] = json.RawMessage(`""`)
	in.Request.Headers["authorization"] = []string{"Bearer "}
	rejected(t, handle(in), "an empty secret")

	rejected(t, handle(webhook(`not json`)), "a body that is not JSON")
	one(t, handle(webhook(body)))
	one(t, handle(automation(body)))
}

func TestIssueCreated(t *testing.T) {
	e := one(t, handle(webhook(`{"webhookEvent":"jira:issue_created",`+actor+`,"issue":`+issueJSON+`}`)))
	want := "Jira issue ABC-7 created by Uma: Login fails\n" +
		"https://acme.atlassian.net/browse/ABC-7\n" +
		"Type: Bug · Priority: High · Status: To Do · Reporter: Rae · Assignee: Ada · Labels: agentrq, web\n\n" +
		"Description:\nSteps:\n1. open"
	if e.Name != "jira.issue.created" || e.Payload != want || e.DedupKey != "w-1" {
		t.Errorf("event = %+v\nwant payload %q", e, want)
	}
	wantFAQ := []sdk.FAQ{
		{Q: "Which Jira issue?", A: "ABC-7"},
		{Q: "Where is it?", A: "https://acme.atlassian.net/browse/ABC-7"},
		{Q: "Which Jira project?", A: "ABC"},
	}
	if len(e.FAQ) != len(wantFAQ) {
		t.Fatalf("faq = %+v", e.FAQ)
	}
	for i := range wantFAQ {
		if e.FAQ[i] != wantFAQ[i] {
			t.Errorf("faq[%d] = %+v, want %+v", i, e.FAQ[i], wantFAQ[i])
		}
	}
}

func TestIssueUpdatedSaysWhatChanged(t *testing.T) {
	e := one(t, handle(webhook(`{"webhookEvent":"jira:issue_updated",`+actor+`,"issue":`+issueJSON+
		`,"changelog":{"items":[{"field":"status","fromString":"To Do","toString":"In Progress"},{"field":"labels","fromString":"","toString":"agentrq"}]}}`)))
	if e.Name != "jira.issue.updated" ||
		!strings.HasPrefix(e.Payload.(string), "Jira issue ABC-7 updated by Uma: Login fails\n") ||
		!strings.Contains(e.Payload.(string), "\n\nChanged:\n- status: To Do → In Progress\n- labels: (none) → agentrq\n\nDescription:") {
		t.Errorf("event = %+v", e)
	}

	var items []string
	for range maxChanges + 3 {
		items = append(items, `{"field":"x","fromString":"a","toString":"b"}`)
	}
	e = one(t, handle(webhook(`{"webhookEvent":"jira:issue_updated","issue":`+issueJSON+`,"changelog":{"items":[`+strings.Join(items, ",")+`]}}`)))
	if p := e.Payload.(string); strings.Count(p, "\n- x: a → b") != maxChanges || !strings.Contains(p, "\n- and 3 more") ||
		!strings.HasPrefix(p, "Jira issue ABC-7 updated: ") {
		t.Errorf("long changelog = %q", p)
	}
}

func TestCommentCreated(t *testing.T) {
	body := `{"webhookEvent":"comment_created","comment":{"body":{"type":"doc","version":1,"content":[{"type":"paragraph","content":[{"type":"text","text":"Can an agent take this?"}]}]},"author":{"accountId":"c-1","displayName":"Cy"}},"issue":` + issueJSON + `}`
	e := one(t, handle(webhook(body)))
	want := "Comment on Jira issue ABC-7 by Cy: Login fails\nhttps://acme.atlassian.net/browse/ABC-7\n" +
		"Type: Bug · Priority: High · Status: To Do · Reporter: Rae · Assignee: Ada · Labels: agentrq, web\n\n" +
		"Comment:\nCan an agent take this?"
	if e.Name != "jira.comment.created" || e.Payload != want {
		t.Errorf("event = %+v\nwant payload %q", e, want)
	}
	e = one(t, handle(webhook(`{"webhookEvent":"comment_created","comment":{"body":""},"issue":{"key":"ABC-8","fields":{"summary":"S"}}}`)))
	if e.Payload != "Comment on Jira issue ABC-8: S" {
		t.Errorf("an empty comment = %q", e.Payload)
	}
}

func TestEventsOfNoInterest(t *testing.T) {
	none(t, handle(webhook(`{"webhookEvent":"jira:issue_deleted","issue":`+issueJSON+`}`)))
	none(t, handle(webhook(`{"webhookEvent":"comment_updated","issue":`+issueJSON+`}`)))
	none(t, handle(webhook(`{"webhookEvent":"jira:issue_created"}`)))
	none(t, handle(webhook(`{"webhookEvent":"jira:issue_created","issue":{"fields":{}}}`)))
	none(t, handle(automation(`{}`)))
	none(t, handle(automation(`{"key":"ABC-7"}`)))
	none(t, handle(automation(`{"issue":{"fields":{"summary":"no key"}}}`)))
}

func TestAutomationSendsAnIssue(t *testing.T) {
	// "Work item data (Jira format)" wraps the issue, with who ran the rule.
	e := one(t, handle(automation(`{"timestamp":1,`+actor+`,"issue":`+issueJSON+`}`)))
	if e.Name != "jira.issue.sent" || !strings.HasPrefix(e.Payload.(string), "Jira issue ABC-7 sent by Uma: Login fails\nhttps://acme.atlassian.net/browse/ABC-7\n") ||
		e.DedupKey != "" {
		t.Errorf("Jira format = %+v", e)
	}
	// "Work item data (Automation format)" is the issue itself.
	e = one(t, handle(automation(issueJSON)))
	if e.Name != "jira.issue.sent" || !strings.HasPrefix(e.Payload.(string), "Jira issue ABC-7 sent: Login fails\n") ||
		!strings.HasSuffix(e.Payload.(string), "Description:\nSteps:\n1. open") {
		t.Errorf("Automation format = %+v", e)
	}
}

func TestFilters(t *testing.T) {
	created := func() sdk.In {
		return webhook(`{"webhookEvent":"jira:issue_created",` + actor + `,"issue":` + issueJSON + `}`)
	}
	one(t, handle(with(created(), "projects", "XYZ", "abc")))
	none(t, handle(with(created(), "projects", "XYZ")))
	// Without a project field, the key's prefix is the project.
	one(t, handle(with(automation(`{"key":"ABC-1","fields":{"summary":"S"}}`), "projects", "ABC")))
	none(t, handle(with(automation(`{"key":"ABC-1","fields":{"summary":"S"}}`), "projects", "AB")))

	one(t, handle(with(created(), "labels", "ops", "agentrq")))
	none(t, handle(with(created(), "labels", "AgentRQ")))
	none(t, handle(with(automation(`{"key":"ABC-1","fields":{}}`), "labels", "agentrq")))

	one(t, handle(with(created(), "accountIds", "a-1"))) // the assignee
	one(t, handle(with(created(), "accountIds", "u-1"))) // who acted
	none(t, handle(with(created(), "accountIds", "r-1")))
	comment := webhook(`{"webhookEvent":"comment_created","comment":{"body":"hi","author":{"accountId":"c-1"}},"issue":{"key":"ABC-1","fields":{}}}`)
	one(t, handle(with(comment, "accountIds", "c-1")))
	none(t, handle(with(automation(`{"key":"ABC-1","fields":{"assignee":null}}`), "accountIds", "a-1")))

	in := with(with(created(), "projects", "ABC"), "labels", "web")
	one(t, handle(with(in, "accountIds", "a-1")))
	none(t, handle(with(in, "accountIds", "z-9")))
}

func TestDedupKey(t *testing.T) {
	in := webhook(`{"webhookEvent":"jira:issue_created","issue":` + issueJSON + `}`)
	in.Request.Headers["x-atlassian-webhook-identifier"] = []string{strings.Repeat("x", 256)}
	if e := one(t, handle(in)); e.DedupKey != "" {
		t.Errorf("an over-long identifier was kept: %q", e.DedupKey)
	}
}

func TestTheLinkComesFromSelf(t *testing.T) {
	for self, want := range map[string]string{
		"https://acme.atlassian.net/rest/api/3/issue/1": "https://acme.atlassian.net/browse/ABC-7",
		"http://jira.local:8080/rest/api/2/issue/1":     "http://jira.local:8080/browse/ABC-7",
		"javascript:alert(1)":                           "",
		"/rest/api/2/issue/1":                           "",
		"":                                              "",
		"https://%zz":                                   "",
	} {
		if got := browseURL(&issue{Self: self, Key: "ABC-7"}); got != want {
			t.Errorf("browseURL(%q) = %q, want %q", self, got, want)
		}
	}
	if got := browseURL(&issue{Self: "https://acme.atlassian.net/x"}); got != "" {
		t.Errorf("no key = %q", got)
	}
	e := one(t, handle(automation(`{"key":"ABC-1","fields":{"summary":"S"}}`)))
	if e.Payload != "Jira issue ABC-1 sent: S" || len(e.FAQ) != 2 {
		t.Errorf("an issue with no link or details = %+v", e)
	}
}

func TestLongTextIsCut(t *testing.T) {
	long := strings.Repeat("é", maxDescription) // two bytes each
	body, _ := json.Marshal(map[string]any{"key": "ABC-1", "fields": map[string]any{"summary": strings.Repeat("s", 600), "description": long}})
	p := one(t, handle(automation(string(body)))).Payload.(string)
	desc := p[strings.Index(p, "Description:\n")+len("Description:\n"):]
	if !strings.HasSuffix(desc, "\n… (cut; the rest is in Jira)") || len(desc) > maxDescription+64 || !strings.Contains(p, strings.Repeat("s", 512)+"\n") ||
		strings.Contains(p, strings.Repeat("s", 513)) {
		t.Errorf("cut payload is %d bytes, description %d", len(p), len(desc))
	}
	if got := cut("aé", 2, "…"); got != "a…" {
		t.Errorf("cut on a rune = %q", got)
	}
	if got := cut("abc", 3, "…"); got != "abc" {
		t.Errorf("cut that fits = %q", got)
	}
	v := strings.Repeat("v", maxValue+1)
	e := one(t, handle(webhook(`{"webhookEvent":"jira:issue_updated","issue":{"key":"ABC-1","fields":{"status":{"name":"`+v+`"}}},"changelog":{"items":[{"field":"f","fromString":"`+v+`","toString":""}]}}`)))
	if p := e.Payload.(string); !strings.Contains(p, "Status: "+v[:maxValue]+"…") || !strings.Contains(p, "- f: "+v[:maxValue]+"… → (none)") {
		t.Errorf("long values = %q", p)
	}
}
