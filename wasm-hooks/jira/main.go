// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

// Command jira is the built-in hook for Jira Cloud. It takes a Jira admin
// webhook, signed in X-Hub-Signature, and a Jira Automation "Send web
// request", which cannot sign and sends the secret as a bearer token. It
// writes events an agent can act on, not Jira's payloads as they come.
package main

import (
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/agentrq/agentrq/wasm-hooks/sdk"
)

var manifest = sdk.Manifest{
	Name:        "jira",
	Version:     "1.0.0",
	Description: "Jira Cloud issues and comments, from an admin webhook (checks X-Hub-Signature) or an Automation rule's Send web request (checks a bearer token).",
	Inputs: []sdk.Input{
		{Name: "webhookSecret", Label: "Secret (the webhook's secret, or the rule's Authorization: Bearer token)", Type: "string", Secret: true, Required: true},
		{Name: "assignees", Label: "Assignees: Jira account ids; the issue must be assigned to one", Type: "array", Items: "string", Required: true},
		{Name: "actors", Label: "Actors: Jira account ids; one must have run the rule, changed the issue or commented", Type: "array", Items: "string", Required: true},
		{Name: "projects", Label: "Project keys, such as ABC (empty is every project)", Type: "array", Items: "string"},
		{Name: "labels", Label: "Labels; the issue must have one (empty is any issue)", Type: "array", Items: "string"},
	},
	Events: []sdk.Event{
		{Name: "jira.issue.sent", Description: "An Automation rule sent an issue, such as from a manual \"Send to AgentRQ\" action"},
		{Name: "jira.issue.created", Description: "An issue was created"},
		{Name: "jira.issue.updated", Description: "An issue was updated; the payload says what changed"},
		{Name: "jira.comment.created", Description: "A comment was added to an issue"},
	},
}

// Most bytes of an issue's description and of a comment an event carries.
// The server's payload limit is 64 KiB unless its operator lowers it.
const (
	maxDescription = 8 << 10
	maxComment     = 4 << 10
	maxValue       = 200
	maxChanges     = 20
)

func main() { sdk.Main(manifest, handle) }

type person struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
}

type named struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

type issue struct {
	Self   string `json:"self"`
	Key    string `json:"key"`
	Fields struct {
		Summary     string          `json:"summary"`
		Description json.RawMessage `json:"description"`
		IssueType   *named          `json:"issuetype"`
		Priority    *named          `json:"priority"`
		Status      *named          `json:"status"`
		Project     *named          `json:"project"`
		Reporter    *person         `json:"reporter"`
		Assignee    *person         `json:"assignee"`
		Labels      []string        `json:"labels"`
	} `json:"fields"`
}

type payload struct {
	WebhookEvent string  `json:"webhookEvent"`
	User         *person `json:"user"`
	Issue        *issue  `json:"issue"`
	Comment      *struct {
		Body   json.RawMessage `json:"body"`
		Author *person         `json:"author"`
	} `json:"comment"`
	Changelog *struct {
		Items []struct {
			Field      string `json:"field"`
			FromString string `json:"fromString"`
			ToString   string `json:"toString"`
		} `json:"items"`
	} `json:"changelog"`
}

func handle(in sdk.In) sdk.Result {
	if reason := authentic(in); reason != "" {
		return sdk.Reject(reason)
	}
	// The server takes an empty list as set; without them every issue Jira
	// sends would pass.
	for _, input := range []string{"assignees", "actors"} {
		if len(slices.DeleteFunc(in.Strings(input), func(id string) bool { return id == "" })) == 0 {
			return sdk.Reject("the registration's " + input + " is empty: give at least one Jira account id")
		}
	}
	var p payload
	if err := json.Unmarshal(in.Request.Body, &p); err != nil {
		return sdk.Reject("the body is not JSON")
	}
	name := eventName(p)
	if name == "" {
		return sdk.Accept()
	}
	if !wanted(in, p) {
		return sdk.Accept()
	}
	ev := event(name, p)
	if id := in.Request.Header("X-Atlassian-Webhook-Identifier"); len(id) <= 255 {
		ev.DedupKey = id
	}
	return sdk.Accept(ev)
}

// authentic says why a request is not genuine, or "". A Jira webhook signs
// its body; an Automation rule cannot, so it sends the secret itself.
func authentic(in sdk.In) string {
	secret := in.String("webhookSecret")
	if secret == "" {
		return "the registration has no secret"
	}
	if sig := in.Request.Header("X-Hub-Signature"); sig != "" {
		hex, ok := strings.CutPrefix(sig, "sha256=")
		if !ok || !sdk.ValidHMACSHA256(secret, in.Request.Body, hex) {
			return "X-Hub-Signature does not match the webhook secret"
		}
		return ""
	}
	if !sdk.Equal("Bearer "+secret, in.Request.Header("Authorization")) {
		return "neither an X-Hub-Signature nor an Authorization: Bearer header with the secret"
	}
	return ""
}

// webhookEvents maps the webhook events the hook takes to its own.
var webhookEvents = map[string]string{
	"jira:issue_created": "jira.issue.created",
	"jira:issue_updated": "jira.issue.updated",
	"comment_created":    "jira.comment.created",
}

// eventName is the event a body stands for, or "" for one of no interest.
func eventName(p payload) string {
	hasIssue := p.Issue != nil && p.Issue.Key != ""
	switch p.WebhookEvent {
	case "jira:issue_created", "jira:issue_updated", "comment_created":
		if hasIssue {
			return webhookEvents[p.WebhookEvent]
		}
	case "":
		// An Automation rule's "Work item data (Jira format)". Not its
		// "(Automation format)", the bare issue: it names nobody who acted.
		if hasIssue {
			return "jira.issue.sent"
		}
	}
	return ""
}

// wanted reports whether an issue passes the registration's filters.
func wanted(in sdk.In, p payload) bool {
	is := p.Issue
	if projects := in.Strings("projects"); len(projects) > 0 {
		key := projectKey(is)
		if !slices.ContainsFunc(projects, func(p string) bool { return strings.EqualFold(p, key) }) {
			return false
		}
	}
	if labels := in.Strings("labels"); len(labels) > 0 &&
		!slices.ContainsFunc(is.Fields.Labels, func(l string) bool { return slices.Contains(labels, l) }) {
		return false
	}
	if !matches(in.Strings("assignees"), is.Fields.Assignee) {
		return false
	}
	var author *person
	if p.Comment != nil {
		author = p.Comment.Author
	}
	return matches(in.Strings("actors"), p.User, author)
}

// matches reports whether one of the people's account ids is in want. Never
// a display name: two people can share one, and anybody can change theirs.
func matches(want []string, people ...*person) bool {
	for _, u := range people {
		if u != nil && u.AccountID != "" && slices.Contains(want, u.AccountID) {
			return true
		}
	}
	return false
}

// projectKey is the issue's project key, from its fields or else its key.
func projectKey(is *issue) string {
	if is.Fields.Project != nil && is.Fields.Project.Key != "" {
		return is.Fields.Project.Key
	}
	key, _, _ := strings.Cut(is.Key, "-")
	return key
}

func event(name string, p payload) sdk.Out {
	is := p.Issue
	link := browseURL(is)
	var b strings.Builder
	switch name {
	case "jira.issue.sent":
		fmt.Fprintf(&b, "Jira issue %s sent", is.Key)
		if p.User != nil && p.User.DisplayName != "" {
			fmt.Fprintf(&b, " by %s", p.User.DisplayName)
		}
	case "jira.issue.created":
		fmt.Fprintf(&b, "Jira issue %s created", is.Key)
		if p.User != nil && p.User.DisplayName != "" {
			fmt.Fprintf(&b, " by %s", p.User.DisplayName)
		}
	case "jira.issue.updated":
		fmt.Fprintf(&b, "Jira issue %s updated", is.Key)
		if p.User != nil && p.User.DisplayName != "" {
			fmt.Fprintf(&b, " by %s", p.User.DisplayName)
		}
	case "jira.comment.created":
		fmt.Fprintf(&b, "Comment on Jira issue %s", is.Key)
		if p.Comment != nil && p.Comment.Author != nil && p.Comment.Author.DisplayName != "" {
			fmt.Fprintf(&b, " by %s", p.Comment.Author.DisplayName)
		}
	}
	fmt.Fprintf(&b, ": %s", cut(is.Fields.Summary, 512, ""))
	if link != "" {
		b.WriteString("\n" + link)
	}
	if facts := details(is); facts != "" {
		b.WriteString("\n" + facts)
	}
	if name == "jira.issue.updated" && p.Changelog != nil && len(p.Changelog.Items) > 0 {
		b.WriteString("\n\nChanged:")
		for i, c := range p.Changelog.Items {
			if i == maxChanges {
				fmt.Fprintf(&b, "\n- and %d more", len(p.Changelog.Items)-maxChanges)
				break
			}
			fmt.Fprintf(&b, "\n- %s: %s → %s", cut(c.Field, maxValue, "…"), orNone(cut(c.FromString, maxValue, "…")), orNone(cut(c.ToString, maxValue, "…")))
		}
	}
	if name == "jira.comment.created" && p.Comment != nil {
		if body := text(p.Comment.Body); body != "" {
			b.WriteString("\n\nComment:\n" + cut(body, maxComment, "\n… (cut; the rest is in Jira)"))
		}
	} else if desc := text(is.Fields.Description); desc != "" {
		b.WriteString("\n\nDescription:\n" + cut(desc, maxDescription, "\n… (cut; the rest is in Jira)"))
	}

	faq := []sdk.FAQ{{Q: "Which Jira issue?", A: is.Key}}
	if link != "" {
		faq = append(faq, sdk.FAQ{Q: "Where is it?", A: link})
	}
	if key := projectKey(is); key != "" {
		faq = append(faq, sdk.FAQ{Q: "Which Jira project?", A: key})
	}
	return sdk.Out{Name: name, Payload: b.String(), FAQ: faq}
}

// details is the issue's type, priority, status, people and labels, as one
// line.
func details(is *issue) string {
	f := is.Fields
	var parts []string
	add := func(label, value string) {
		if value != "" {
			parts = append(parts, label+": "+cut(value, maxValue, "…"))
		}
	}
	if f.IssueType != nil {
		add("Type", f.IssueType.Name)
	}
	if f.Priority != nil {
		add("Priority", f.Priority.Name)
	}
	if f.Status != nil {
		add("Status", f.Status.Name)
	}
	if f.Reporter != nil {
		add("Reporter", f.Reporter.DisplayName)
	}
	if f.Assignee != nil {
		add("Assignee", f.Assignee.DisplayName)
	}
	add("Labels", strings.Join(f.Labels, ", "))
	return strings.Join(parts, " · ")
}

// browseURL is the issue's page, on the site its REST self link names.
func browseURL(is *issue) string {
	u, err := url.Parse(is.Self)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || is.Key == "" {
		return ""
	}
	return (&url.URL{Scheme: u.Scheme, Host: u.Host, Path: "/browse/" + is.Key}).String()
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// cut shortens s to at most n bytes, on a rune boundary, adding more when
// it does.
func cut(s string, n int, more string) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + more
}
