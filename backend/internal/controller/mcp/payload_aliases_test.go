// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/service/mcp"
)

// decodeAs decodes payload as the server's action tool.action would, aliases
// first, and returns the payload struct or the refusal.
func decodeAs(t *testing.T, tool, action string, payload map[string]any) (any, error) {
	t.Helper()
	ps := &WorkspaceServer{}
	lists := map[string][]ToolAction{"task": ps.taskActions(), "memory": ps.memoryActions(), "skills": ps.skillsActions()}
	for _, a := range lists[tool] {
		if a.Name != action {
			continue
		}
		if a.aliases != nil {
			if err := a.aliases(payload); err != nil {
				return nil, err
			}
		}
		out := reflect.New(a.payload)
		if err := decodePayload(payload, out.Interface()); err != nil {
			return nil, err
		}
		return out.Elem().Interface(), nil
	}
	t.Fatalf("no action %s.%s", tool, action)
	return nil, nil
}

// The field names agents really sent, from their refusals, decode as the
// fields they meant.
func TestPayloadAliases(t *testing.T) {
	for _, tc := range []struct {
		tool, action string
		payload      map[string]any
		want         any
	}{
		{"skills", "search", map[string]any{"query": "release"}, SearchSkillsParams{Q: "release"}},
		{"skills", "search", map[string]any{"text": "release"}, SearchSkillsParams{Q: "release"}},
		{"skills", "search", map[string]any{"topic": "release", "limit": 5}, SearchSkillsParams{Q: "release", Limit: 5}},
		{"skills", "search", map[string]any{"q": "release"}, SearchSkillsParams{Q: "release"}},
		{"skills", "load", map[string]any{"name": "animation-fw-screen-video"}, LoadSkillParams{URI: "skill://animation-fw-screen-video"}},
		{"skills", "load", map[string]any{"name": "skill://release-notes/SKILL.md"}, LoadSkillParams{URI: "skill://release-notes/SKILL.md"}},
		{"skills", "load", map[string]any{"uri": "skill://release-notes"}, LoadSkillParams{URI: "skill://release-notes"}},
		{"task", "reply", map[string]any{"taskId": "0kGogHmaqvZ", "message": "done"}, ReplyParams{TaskID: "0kGogHmaqvZ", Text: "done"}},
		{"task", "elicit", map[string]any{"taskId": "0kGogHmaqvZ", "text": "Which branch?", "mode": "form"}, ElicitParams{TaskID: "0kGogHmaqvZ", Message: "Which branch?", Mode: "form"}},
		{"memory", "load", map[string]any{"uri": "memory://project-handoff-video.md"}, LoadMemoryParams{Name: "project-handoff-video.md"}},
		{"memory", "load", map[string]any{"name": "memory://deploys.md"}, LoadMemoryParams{Name: "deploys.md"}},
		{"memory", "load", map[string]any{"name": "0ZzhYQG2qtl/memory"}, LoadMemoryParams{Name: "0ZzhYQG2qtl/memory.md"}},
		{"memory", "load", map[string]any{}, LoadMemoryParams{}},
		{"memory", "save", map[string]any{"name": "horizontal-scaling-map", "content": "x"}, SaveMemoryParams{Name: "horizontal-scaling-map.md", Content: "x"}},
		{"memory", "save", map[string]any{"name": "Deploys.MD", "content": "x"}, SaveMemoryParams{Name: "Deploys.MD", Content: "x"}},
		// Not a name with .md added either, so left for the handler to refuse.
		{"memory", "save", map[string]any{"name": "two  spaces", "content": "x"}, SaveMemoryParams{Name: "two  spaces", Content: "x"}},
		{"memory", "delete", map[string]any{"uri": "memory://old"}, DeleteMemoryParams{Name: "old.md"}},
		{"memory", "load", map[string]any{"uri": " MEMORY://Deploys.md "}, LoadMemoryParams{Name: "Deploys.md"}},
		{"skills", "load", map[string]any{"name": " release-notes "}, LoadSkillParams{URI: "skill://release-notes"}},
		{"skills", "load", map[string]any{"name": "Skill://release-notes"}, LoadSkillParams{URI: "Skill://release-notes"}},
	} {
		got, err := decodeAs(t, tc.tool, tc.action, tc.payload)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%s.%s %v decoded as %+v (%v), want %+v", tc.tool, tc.action, tc.payload, got, err, tc.want)
		}
	}
}

// A field sent with its alias, or two aliases of one field, is refused:
// either may be the one the agent meant.
func TestPayloadAliases_RefuseAConflict(t *testing.T) {
	for _, tc := range []struct {
		tool, action string
		payload      map[string]any
		want         string
	}{
		{"skills", "search", map[string]any{"q": "a", "query": "b"}, "send q only: query is another name for it"},
		{"skills", "search", map[string]any{"query": "a", "text": "b"}, "send q only, not both query and text"},
		{"task", "reply", map[string]any{"taskId": "x", "text": "a", "message": "b"}, "send text only: message is another name for it"},
		{"memory", "load", map[string]any{"name": "a.md", "uri": "memory://b.md"}, "send name only: uri is another name for it"},
		{"skills", "load", map[string]any{"uri": "skill://a", "name": "b"}, "send uri only: name is another name for it"},
		// Left as no name, it would be memory.md: a delete would remove the index.
		{"memory", "delete", map[string]any{"uri": "memory://"}, "name memory:// names no memory"},
	} {
		if _, err := decodeAs(t, tc.tool, tc.action, tc.payload); err == nil || err.Error() != tc.want {
			t.Errorf("%s.%s %v: got %v, want the refusal %q", tc.tool, tc.action, tc.payload, err, tc.want)
		}
	}
}

// The call itself: aliases run on a copy of the payload, and a refusal,
// whether of a conflict or of the payload, names the fields and an example.
func TestRunAction_AliasesAndRefusals(t *testing.T) {
	var got ReplyParams
	reply := NewToolAction("reply", "Post a message.", func(_ context.Context, _ *mcp.CallToolRequest, p ReplyParams) (*mcp.CallToolResult, any, error) {
		got = p
		return textResult("ok"), nil, nil
	}).WithAliases(aliasField("text", "message")).WithExample(`{"taskId": "t", "text": "hi"}`)
	actions := []ToolAction{reply}

	payload := map[string]any{"taskId": "t", "message": "hi"}
	res, _, _ := RunAction(context.Background(), nil, "task", actions, "reply", payload)
	if res.IsError || !reflect.DeepEqual(got, ReplyParams{TaskID: "t", Text: "hi"}) {
		t.Errorf("reply with message ran with %+v (error %v)", got, res.IsError)
	}
	if _, ok := payload["message"]; !ok {
		t.Error("the aliases changed the caller's payload")
	}

	usage := `reply takes {taskId, text, attachments?}, like {"taskId": "t", "text": "hi"}`
	for want, payload := range map[string]map[string]any{
		"reply: send text only: message is another name for it; " + usage: {"taskId": "t", "text": "a", "message": "b"},
		`reply: bad payload: unknown field "body"; ` + usage:              {"taskId": "t", "body": "a"},
		"reply: payload needs text; " + usage:                             {"taskId": "t"},
	} {
		res, _, _ := RunAction(context.Background(), nil, "task", actions, "reply", payload)
		if text := res.Content[0].(*mcp.TextContent).Text; !res.IsError || text != want {
			t.Errorf("reply %v answered %q, want the error %q", payload, text, want)
		}
	}

	// An action with no example still names its fields.
	bare := NewToolAction("get", "Get one.", func(context.Context, *mcp.CallToolRequest, GetTaskParams) (*mcp.CallToolResult, any, error) {
		return textResult("ok"), nil, nil
	})
	res, _, _ = RunAction(context.Background(), nil, "task", []ToolAction{bare}, "get", map[string]any{"id": "x"})
	if text := res.Content[0].(*mcp.TextContent).Text; !strings.HasSuffix(text, "; get takes {taskId?, includeConversation?, cursor?, limit?}") {
		t.Errorf("get answered %q, want its fields and no example", text)
	}
}
