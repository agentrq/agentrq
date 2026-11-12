// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: Apache-2.0

// Command github is the built-in hook for a GitHub repository or
// organisation webhook. It checks X-Hub-Signature-256 and writes events an
// agent can act on, not GitHub's payloads as they come.
package main

import (
	"encoding/json"
	"fmt"
	"path"
	"strings"

	"github.com/agentrq/agentrq/wasm-hooks/sdk"
)

var manifest = sdk.Manifest{
	Name:        "github",
	Version:     "1.0.0",
	Description: "CI runs, pull requests, issues, releases and pushes. Checks X-Hub-Signature-256.",
	Inputs: []sdk.Input{
		{Name: "webhookSecret", Label: "Webhook secret", Type: "string", Secret: true, Required: true},
		{Name: "branches", Label: "Branches, as globs such as release/* (empty is every branch)", Type: "array", Items: "string"},
	},
	Events: []sdk.Event{
		{Name: "ci.failed", Description: "A GitHub Actions workflow run finished with failure or timed out"},
		{Name: "ci.succeeded", Description: "A GitHub Actions workflow run finished with success"},
		{Name: "pr.opened", Description: "A pull request was opened"},
		{Name: "pr.review_requested", Description: "A review was requested on a pull request"},
		{Name: "issue.opened", Description: "An issue was opened"},
		{Name: "release.published", Description: "A release was published"},
		{Name: "push", Description: "Commits were pushed to a branch"},
	},
}

func main() { sdk.Main(manifest, handle) }

type repo struct {
	FullName string `json:"full_name"`
}

type user struct {
	Login string `json:"login"`
}

type payload struct {
	Action      string `json:"action"`
	Repository  repo   `json:"repository"`
	WorkflowRun *struct {
		Name       string `json:"name"`
		HeadBranch string `json:"head_branch"`
		HeadSHA    string `json:"head_sha"`
		Conclusion string `json:"conclusion"`
		HTMLURL    string `json:"html_url"`
		HeadCommit struct {
			Message string `json:"message"`
			Author  struct {
				Name string `json:"name"`
			} `json:"author"`
		} `json:"head_commit"`
	} `json:"workflow_run"`
	PullRequest *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		User    user   `json:"user"`
		Base    struct {
			Ref string `json:"ref"`
		} `json:"base"`
		Head struct {
			Ref string `json:"ref"`
		} `json:"head"`
	} `json:"pull_request"`
	RequestedReviewer *user `json:"requested_reviewer"`
	Issue             *struct {
		Number  int    `json:"number"`
		Title   string `json:"title"`
		HTMLURL string `json:"html_url"`
		User    user   `json:"user"`
	} `json:"issue"`
	Release *struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
		HTMLURL string `json:"html_url"`
	} `json:"release"`
	Ref     string `json:"ref"`
	Compare string `json:"compare"`
	Pusher  struct {
		Name string `json:"name"`
	} `json:"pusher"`
	Commits []struct {
		ID      string `json:"id"`
		Message string `json:"message"`
	} `json:"commits"`
}

func handle(in sdk.In) sdk.Result {
	sig, ok := strings.CutPrefix(in.Request.Header("X-Hub-Signature-256"), "sha256=")
	if !ok || !sdk.ValidHMACSHA256(in.String("webhookSecret"), in.Request.Body, sig) {
		return sdk.Reject("X-Hub-Signature-256 does not match the webhook secret")
	}
	var p payload
	if err := json.Unmarshal(in.Request.Body, &p); err != nil {
		return sdk.Reject("the body is not JSON")
	}
	branches := in.Strings("branches")
	ev := event(in.Request.Header("X-GitHub-Event"), p, branches)
	if ev == nil {
		// ping, an action of no interest, or a branch not watched.
		return sdk.Accept()
	}
	ev.DedupKey = in.Request.Header("X-GitHub-Delivery")
	return sdk.Accept(*ev)
}

func event(kind string, p payload, branches []string) *sdk.Out {
	repo := p.Repository.FullName
	switch {
	case kind == "workflow_run" && p.Action == "completed" && p.WorkflowRun != nil:
		r := p.WorkflowRun
		if !watched(r.HeadBranch, branches) {
			return nil
		}
		name, verb := "ci.failed", "failed"
		switch r.Conclusion {
		case "failure":
		case "timed_out":
			verb = "timed out"
		case "success":
			name, verb = "ci.succeeded", "succeeded"
		default:
			return nil
		}
		return &sdk.Out{
			Name: name,
			Payload: fmt.Sprintf("Workflow `%s` %s on `%s` @ %s in %s\nRun: %s\nCommit: %s (%s)",
				r.Name, verb, r.HeadBranch, short(r.HeadSHA), repo, r.HTMLURL, firstLine(r.HeadCommit.Message), r.HeadCommit.Author.Name),
			FAQ: []sdk.FAQ{
				{Q: "Which repository?", A: repo},
				{Q: "Which branch?", A: r.HeadBranch},
				{Q: "Which commit?", A: r.HeadSHA},
				{Q: "Where are the logs?", A: r.HTMLURL},
			},
		}
	case kind == "pull_request" && p.PullRequest != nil && (p.Action == "opened" || p.Action == "review_requested"):
		pr := p.PullRequest
		if !watched(pr.Base.Ref, branches) {
			return nil
		}
		name, what := "pr.opened", "opened by "+pr.User.Login
		if p.Action == "review_requested" {
			name, what = "pr.review_requested", "review requested"
			if p.RequestedReviewer != nil {
				what += " from " + p.RequestedReviewer.Login
			}
		}
		return &sdk.Out{
			Name: name,
			Payload: fmt.Sprintf("Pull request #%d %s in %s: %s\n`%s` → `%s`\n%s",
				pr.Number, what, repo, pr.Title, pr.Head.Ref, pr.Base.Ref, pr.HTMLURL),
			FAQ: []sdk.FAQ{{Q: "Which repository?", A: repo}, {Q: "Which pull request?", A: pr.HTMLURL}},
		}
	case kind == "issues" && p.Action == "opened" && p.Issue != nil:
		is := p.Issue
		return &sdk.Out{
			Name:    "issue.opened",
			Payload: fmt.Sprintf("Issue #%d opened by %s in %s: %s\n%s", is.Number, is.User.Login, repo, is.Title, is.HTMLURL),
			FAQ:     []sdk.FAQ{{Q: "Which repository?", A: repo}, {Q: "Which issue?", A: is.HTMLURL}},
		}
	case kind == "release" && p.Action == "published" && p.Release != nil:
		rel := p.Release
		return &sdk.Out{
			Name:    "release.published",
			Payload: fmt.Sprintf("Release %s (%s) published in %s\n%s", rel.TagName, rel.Name, repo, rel.HTMLURL),
			FAQ:     []sdk.FAQ{{Q: "Which repository?", A: repo}, {Q: "Which tag?", A: rel.TagName}},
		}
	case kind == "push":
		branch, ok := strings.CutPrefix(p.Ref, "refs/heads/")
		if !ok || len(p.Commits) == 0 || !watched(branch, branches) {
			return nil
		}
		var b strings.Builder
		fmt.Fprintf(&b, "%s pushed %d commit(s) to `%s` in %s\n%s", p.Pusher.Name, len(p.Commits), branch, repo, p.Compare)
		for _, c := range p.Commits {
			fmt.Fprintf(&b, "\n- %s %s", short(c.ID), firstLine(c.Message))
		}
		return &sdk.Out{
			Name:    "push",
			Payload: b.String(),
			FAQ:     []sdk.FAQ{{Q: "Which repository?", A: repo}, {Q: "Which branch?", A: branch}},
		}
	}
	return nil
}

// watched reports whether a branch matches one of the globs; none watches
// every branch.
func watched(branch string, globs []string) bool {
	if len(globs) == 0 {
		return true
	}
	for _, g := range globs {
		if ok, _ := path.Match(g, branch); ok {
			return true
		}
	}
	return false
}

func short(sha string) string { return sha[:min(7, len(sha))] }

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
