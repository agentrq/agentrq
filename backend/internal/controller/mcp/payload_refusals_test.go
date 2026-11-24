// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"maps"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// workspaceAction is the server's action tool.action, and the tool's actions.
func workspaceAction(t *testing.T, tool, action string) (ToolAction, []ToolAction) {
	t.Helper()
	ps := &WorkspaceServer{}
	lists := map[string][]ToolAction{"task": ps.taskActions(), "memory": ps.memoryActions(), "skills": ps.skillsActions()}
	for _, a := range lists[tool] {
		if a.Name == action {
			return a, lists[tool]
		}
	}
	t.Fatalf("no action %s.%s", tool, action)
	return ToolAction{}, nil
}

// skills.search takes query for q, its one alias, and leaves the caller's
// payload as it was.
func TestSkillsSearch_TakesQueryForQ(t *testing.T) {
	a, _ := workspaceAction(t, "skills", "search")
	for _, payload := range []map[string]any{
		{"query": "release", "limit": 5},
		{"q": "release", "limit": 5},
	} {
		sent := maps.Clone(payload)
		p, err := a.unalias(payload)
		var got SearchSkillsParams
		if err == nil {
			err = decodePayload(p, &got)
		}
		if err != nil || got != (SearchSkillsParams{Q: "release", Limit: 5}) {
			t.Errorf("search %v decoded as %+v (%v)", payload, got, err)
		}
		if !maps.Equal(payload, sent) {
			t.Errorf("search changed the caller's payload to %v", payload)
		}
	}
}

// The mistakes agents really made, from their refusals: each is refused, and
// the refusal names the action's fields and an example, so one retry gets it
// right. Only skills.search's query is taken, and sent with q it is refused.
func TestBadPayloads_AreRefusedWithFieldsAndExample(t *testing.T) {
	for _, tc := range []struct {
		tool, action string
		payload      map[string]any
		want         string
	}{
		{"skills", "search", map[string]any{"q": "a", "query": "b"}, `search: send q only: query is another name for it; search takes {q?, limit?, offset?}, like {"q": "release"}`},
		{"skills", "search", map[string]any{"text": "release"}, `search: bad payload: unknown field "text"; search takes {q?, limit?, offset?}, like {"q": "release"}`},
		{"skills", "search", map[string]any{"topic": "release"}, `search: bad payload: unknown field "topic"; search takes {q?, `},
		{"task", "reply", map[string]any{"taskId": "0kGogHmaqvZ", "message": "done"}, `reply: bad payload: unknown field "message"; reply takes {taskId, text, attachments?}, like {"taskId": "0kGogHmaqvZ", "text": "Tests pass; opening the pull request."}`},
		{"task", "elicit", map[string]any{"taskId": "0kGogHmaqvZ", "text": "Which branch?"}, `elicit: bad payload: unknown field "text"; elicit takes {taskId, message, mode, requestedSchema?, url?, timeoutSeconds?}, like {"taskId": "0kGogHmaqvZ", "message": "Which branch should I release from?", "mode": "form"`},
		{"task", "elicit", map[string]any{"taskId": "0kGogHmaqvZ", "message": "Which?", "mode": "form", "options": []any{"a", "b"}}, `elicit: bad payload: unknown field "options"; elicit takes {taskId, message, mode, `},
		{"memory", "load", map[string]any{"uri": "memory://project-handoff-video.md"}, `load: bad payload: unknown field "uri"; load takes {name?}, like {"name": "deploys.md"}`},
		{"memory", "delete", map[string]any{"uri": "memory://old.md"}, `delete: bad payload: unknown field "uri"; delete takes {name?}, like {"name": "deploys.md"}`},
		{"skills", "load", map[string]any{"name": "animation-fw-screen-video"}, `load: bad payload: unknown field "name"; load takes {uri}, like {"uri": "skill://release-notes"}`},
	} {
		_, actions := workspaceAction(t, tc.tool, tc.action)
		res, _, err := RunAction(context.Background(), nil, tc.tool, actions, tc.action, tc.payload)
		if err != nil {
			t.Fatal(err)
		}
		if text := res.Content[0].(*mcp.TextContent).Text; !res.IsError || !strings.HasPrefix(text, tc.want) {
			t.Errorf("%s.%s %v answered %q, want an error starting %q", tc.tool, tc.action, tc.payload, text, tc.want)
		}
	}
}

// A memory name without its .md is refused, not repaired, and the refusal
// gives the name with it as the example, keeping a workspace it names.
func TestMemoryName_WithoutMDIsRefusedWithTheName(t *testing.T) {
	for name, want := range map[string]string{
		"horizontal-scaling-map":             `memory name "horizontal-scaling-map" is not usable; names are lowercase words joined by single hyphens and ending in .md, like "horizontal-scaling-map.md"`,
		"0ZzhYQG2qtl/horizontal-scaling-map": `memory name "0ZzhYQG2qtl/horizontal-scaling-map" is not usable; names are lowercase words joined by single hyphens and ending in .md, like "0ZzhYQG2qtl/horizontal-scaling-map.md"`,
		"0ZzhYQG2qtl/two  spaces":            `memory name "two  spaces" is not usable; names are lowercase words joined by single hyphens and ending in .md, like "release-notes.md"`,
		"two  spaces":                        `memory name "two  spaces" is not usable; names are lowercase words joined by single hyphens and ending in .md, like "release-notes.md"`,
	} {
		if _, err := resolveMemoryName(name); err == nil || err.Error() != want {
			t.Errorf("resolveMemoryName(%q) = %v, want the refusal %q", name, err, want)
		}
	}
}

// The call itself: a refusal names the fields and the example, and an action
// with no example still names its fields.
func TestRunAction_RefusalsNameFields(t *testing.T) {
	reply := NewToolAction("reply", "Post a message.", func(context.Context, *mcp.CallToolRequest, ReplyParams) (*mcp.CallToolResult, any, error) {
		return textResult("ok"), nil, nil
	}).WithExample(`{"taskId": "t", "text": "hi"}`)
	usage := `reply takes {taskId, text, attachments?}, like {"taskId": "t", "text": "hi"}`
	for want, payload := range map[string]map[string]any{
		`reply: bad payload: unknown field "body"; ` + usage: {"taskId": "t", "body": "a"},
		"reply: payload needs text; " + usage:                {"taskId": "t"},
	} {
		res, _, _ := RunAction(context.Background(), nil, "task", []ToolAction{reply}, "reply", payload)
		if text := res.Content[0].(*mcp.TextContent).Text; !res.IsError || text != want {
			t.Errorf("reply %v answered %q, want the error %q", payload, text, want)
		}
	}

	bare := NewToolAction("get", "Get one.", func(context.Context, *mcp.CallToolRequest, GetTaskParams) (*mcp.CallToolResult, any, error) {
		return textResult("ok"), nil, nil
	})
	res, _, _ := RunAction(context.Background(), nil, "task", []ToolAction{bare}, "get", map[string]any{"id": "x"})
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.HasSuffix(text, "; get takes {taskId?, includeConversation?, cursor?, limit?}") {
		t.Errorf("get answered %q, want its fields and no example", text)
	}
}
