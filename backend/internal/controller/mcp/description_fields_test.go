// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"regexp"
	"strings"
	"testing"
)

// actionsFields is every JSON field name an action of the list takes.
func actionsFields(actions []ToolAction) map[string]bool {
	fields := map[string]bool{}
	for _, a := range actions {
		if a.payload == nil {
			continue
		}
		for _, f := range payloadFields(a.payload) {
			fields[f.name] = true
		}
	}
	return fields
}

// An agent without the plugin's skill guesses a payload's fields from the
// tool's description alone, so a field the description names in backticks
// must be one the tool takes: a renamed field would otherwise send agents to
// the old name, which the strict payload refuses.
func TestToolDescriptionsNameRealPayloadFields(t *testing.T) {
	ps := &WorkspaceServer{}
	actions := map[string][]ToolAction{
		"task":      ps.taskActions(),
		"memory":    ps.memoryActions(),
		"skills":    ps.skillsActions(),
		"workspace": ps.workspaceActions(),
		"siteTools": ps.siteToolsActions(),
		"pano":      PanoActions(nil, "", 0),
	}
	named := regexp.MustCompile("`([A-Za-z]+)`")
	descriptions := map[string]string{}
	for _, tool := range toolsOverTheWire(t) {
		name, _ := tool["name"].(string)
		d, _ := tool["description"].(string)
		descriptions[name] = d
		list, ok := actions[name]
		if !ok {
			continue
		}
		fields := actionsFields(list)
		for _, m := range named.FindAllStringSubmatch(d, -1) {
			if !fields[m[1]] {
				t.Errorf("tool %q's description names `%s`, which none of its actions takes", name, m[1])
			}
		}
	}

	// The guesses that failed, in task 0mVt2O9Gf5B: "message" for reply and
	// "query" for skills search.
	for _, want := range []struct{ tool, text string }{
		{"task", "reply: a message (`text`)"},
		{"skills", "search (`q`)"},
		{"siteTools", "search (`q`)"},
	} {
		if !strings.Contains(descriptions[want.tool], want.text) {
			t.Errorf("tool %q's description does not say %q:\n%s", want.tool, want.text, descriptions[want.tool])
		}
	}
}
