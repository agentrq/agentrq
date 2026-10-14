// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"encoding/json"
	"regexp"
	"strings"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
)

// optionText reads a config value id, which the schema types as a string but
// some agents send as a number.
func optionText(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	return string(raw)
}

// isModelOption is the gateway's guess at whether a select that does not say
// it is the model is the model anyway.
func isModelOption(o configOption) bool {
	if o.Type != "select" {
		return false
	}
	switch o.Category {
	case "model":
		return true
	case "model_config", "thought_level":
		return false
	}
	id, name := strings.ToLower(o.ID), strings.ToLower(o.Name)
	for _, word := range []string{"effort", "thinking", "reasoning"} {
		if strings.Contains(id, word) || strings.Contains(name, word) {
			return false
		}
	}
	return strings.Contains(id, "model") || strings.Contains(name, "model") || id == "llm"
}

// modelsFrom finds the agent's model list among its config options: a select
// with category "model" or id "model" first, else the first that looks like
// one. ok is false when there is none.
func modelsFrom(opts []configOption) (configID, current string, models []mcpctrl.AgentModel, ok bool) {
	var pick *configOption
	var choices []selectOption
	for i := range opts {
		o := &opts[i]
		if o.Type == "select" && (o.Category == "model" || o.ID == "model") {
			if choices = o.selectOptions(); len(choices) > 0 {
				pick = o
				break
			}
		}
	}
	if pick == nil {
		for i := range opts {
			if isModelOption(opts[i]) {
				if choices = opts[i].selectOptions(); len(choices) > 0 {
					pick = &opts[i]
					break
				}
			}
		}
	}
	if pick == nil {
		return "", "", nil, false
	}
	current = optionText(pick.CurrentValue)
	for _, so := range choices {
		id := optionText(so.Value)
		name := so.Name
		if name == "" {
			name = id
		}
		models = append(models, mcpctrl.AgentModel{ID: id, Name: name, Description: so.Description, Current: id == current, Group: so.Group})
	}
	return pick.ID, current, models, true
}

// resolveModel finds a requested model by id, else by display name.
func resolveModel(models []mcpctrl.AgentModel, requested string) (string, bool) {
	for _, m := range models {
		if m.ID == requested {
			return m.ID, true
		}
	}
	for _, m := range models {
		if strings.EqualFold(m.Name, requested) {
			return m.ID, true
		}
	}
	return "", false
}

// withCurrent is models with id marked as the current one.
func withCurrent(models []mcpctrl.AgentModel, id string) []mcpctrl.AgentModel {
	out := make([]mcpctrl.AgentModel, len(models))
	for i, m := range models {
		m.Current = m.ID == id
		out[i] = m
	}
	return out
}

var (
	autoApprovingMode = regexp.MustCompile(`(?i)auto|full[\s_-]?access|danger|bypass|yolo|never|always`)
	humanApprovalMode = regexp.MustCompile(`(?i)ask|approval|approve|manual|prompt|review|read[\s_-]?only`)
)

func describeMode(m sessionMode) string {
	var meta struct {
		Kind string `json:"kind"`
	}
	lenient(m.Meta, &meta)
	return m.ID + " " + m.Name + " " + meta.Kind
}

// humanMode picks the mode that routes every tool-call approval to the human,
// or "" when the agent offers none. In a mode that approves on the user's
// behalf the agent never asks, so its tool calls never reach AgentRQ.
func humanMode(modes *sessionModeState) string {
	if modes == nil {
		return ""
	}
	var candidates []sessionMode
	for _, m := range modes.AvailableModes {
		if !autoApprovingMode.MatchString(describeMode(m)) {
			candidates = append(candidates, m)
		}
	}
	for _, m := range candidates {
		if humanApprovalMode.MatchString(describeMode(m)) {
			return m.ID
		}
	}
	if len(candidates) > 0 {
		return candidates[0].ID
	}
	return ""
}

// maxModeReenforcements is how often one session is put back into a mode that
// asks before AgentRQ stops arguing with an agent that keeps leaving it.
const maxModeReenforcements = 3

// modeAsks reports whether the mode an agent moved a session into still asks
// the human. A mode the agent never advertised cannot be vouched for, so it
// counts as one that approves on the human's behalf.
func modeAsks(modes *sessionModeState, id string) bool {
	for _, m := range modes.AvailableModes {
		if m.ID == id {
			return !autoApprovingMode.MatchString(describeMode(m))
		}
	}
	return false
}
