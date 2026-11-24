// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"regexp"
	"slices"
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
// tool's description alone, so the description gives each action's fields as
// `action {field, ...}`: every field it names must be one that action takes,
// and every field the action requires must be named. A field the description
// names in backticks must be one the tool takes too.
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
	// The pano tool's description predates the form.
	braced := map[string]bool{"task": true, "memory": true, "skills": true, "workspace": true, "siteTools": true}
	named := regexp.MustCompile("`([A-Za-z]+)`")
	withFields := regexp.MustCompile(`(?:^|[ .;:])([A-Za-z]+) \{([A-Za-z, ]*)\}`)
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
		described := map[string][]string{}
		for _, m := range withFields.FindAllStringSubmatch(d, -1) {
			described[m[1]] = strings.Split(m[2], ", ")
		}
		for _, a := range list {
			listed, ok := described[a.Name]
			delete(described, a.Name)
			takes := map[string]bool{}
			for _, f := range payloadFields(a.payload) {
				takes[f.name] = true
				if braced[name] && f.required && !slices.Contains(listed, f.name) {
					t.Errorf("tool %q's description does not give %s's required field %s as %s {%s, ...}", name, a.Name, f.name, a.Name, f.name)
				}
			}
			if !ok {
				continue
			}
			for _, f := range listed {
				if !takes[f] {
					t.Errorf("tool %q's description gives %s the field %s, which it does not take", name, a.Name, f)
				}
			}
		}
		for action := range described {
			t.Errorf("tool %q's description gives fields to %s, which is not one of its actions", name, action)
		}
	}

	// The guesses agents got wrong most (task 0mXbE6g6U87).
	for _, want := range []struct{ tool, text string }{
		{"task", "reply {taskId, text}"},
		{"task", "elicit {taskId, message, mode}"},
		{"skills", "search {q}"},
		{"skills", "load {uri}"},
		{"memory", "load {name}"},
		{"siteTools", "search {q}"},
	} {
		if !strings.Contains(descriptions[want.tool], want.text) {
			t.Errorf("tool %q's description does not say %q:\n%s", want.tool, want.text, descriptions[want.tool])
		}
	}
}
