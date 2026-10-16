// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import (
	"encoding/json"
	"regexp"
	"strings"
)

// mcpServerName is the name the workspace's own MCP server is handed to the
// agent under: the same name agentrqd writes into .mcp.json, so an agent that
// also reads that file sees one server, not two.
const mcpServerName = "agentrq-workspace"

var (
	legacyAgentrqTool = regexp.MustCompile(`agentrq-[a-zA-Z0-9]{11}`)
	agentrqServer     = regexp.MustCompile(`(?i)^agentrq(?:[.\-_][A-Za-z0-9.\-_]*)?$`)
	attributedCall    = regexp.MustCompile(`(?i)^\s*([A-Za-z0-9_]+)\s*\(\s*([^()\s]+)\s+MCP\b`)
	// RE2 has no back-references, so each separator of the qualified form has
	// its own pattern.
	qualifiedCalls = []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*mcp__(.+?)__([A-Za-z0-9_]+)\s*$`),
		regexp.MustCompile(`(?i)^\s*mcp\.(.+?)\.([A-Za-z0-9_]+)\s*$`),
		regexp.MustCompile(`(?i)^\s*mcp-(.+?)-([A-Za-z0-9_]+)\s*$`),
	}
	bareTitle     = regexp.MustCompile(`^[A-Za-z0-9._-]+$`)
	nameSeparator = regexp.MustCompile(`__|[._-]`)
	replyTool     = regexp.MustCompile(`(^|[._-])reply$`)
	taskTool      = regexp.MustCompile(`(^|[._-])task$`)
)

// workspaceTool reports which of the workspace's own tools a call is, if it
// is one. Both halves must check out: a server named like AgentRQ's, and a
// tool the workspace actually offers. The name alone never grants anything.
func workspaceTool(title string, offered map[string]struct{}, names ...string) (tool string, ok bool) {
	candidates := append([]string{title}, names...)
	for _, s := range candidates {
		if tool, ok := namedWorkspaceTool(s, offered); ok {
			return tool, true
		}
	}
	for _, s := range candidates {
		if legacyAgentrqTool.MatchString(s) {
			return firstToken(s), true
		}
	}
	return "", false
}

func namedWorkspaceTool(title string, offered map[string]struct{}) (tool string, ok bool) {
	ours := func(server, tool string) bool {
		if !strings.EqualFold(server, mcpServerName) && !agentrqServer.MatchString(server) {
			return false
		}
		_, offeredTool := offered[tool]
		return offeredTool
	}
	// A title that names its two halves outright is read as it is written.
	named := func(server, tool string) (string, bool) {
		if ours(server, tool) {
			return tool, true
		}
		return "", false
	}
	if m := attributedCall.FindStringSubmatch(title); m != nil {
		return named(m[2], m[1])
	}
	for _, re := range qualifiedCalls {
		if m := re.FindStringSubmatch(title); m != nil {
			return named(m[1], m[2])
		}
	}
	trimmed := strings.TrimSpace(title)
	if bareTitle.MatchString(trimmed) {
		for _, loc := range nameSeparator.FindAllStringIndex(trimmed, -1) {
			server, tool := trimmed[:loc[0]], trimmed[loc[1]:]
			if server != "" && tool != "" && ours(server, tool) {
				return tool, true
			}
		}
	}
	return "", false
}

// firstToken is the title up to its first space or bracket: how the gateway
// names a call it recognised only by the legacy pattern.
func firstToken(title string) string {
	if i := strings.IndexAny(title, " ("); i >= 0 {
		return title[:i]
	}
	return title
}

// agentReply reads the task and text of a reply the agent sent through the
// workspace's own tool, in any of its three shapes: the old reply {chatId,
// text}, task {action: "reply", payload: {taskId, text}}, and the flat task
// {action: "reply", taskId, text}.
func agentReply(tool string, rawInput json.RawMessage) (taskID, text string, ok bool) {
	var in struct {
		ChatID  *string         `json:"chatId"`
		TaskID  *string         `json:"taskId"`
		Text    string          `json:"text"`
		Action  string          `json:"action"`
		Payload json.RawMessage `json:"payload"`
	}
	if json.Unmarshal(rawInput, &in) != nil {
		return "", "", false
	}
	switch {
	case replyTool.MatchString(tool) && in.ChatID != nil:
		return *in.ChatID, in.Text, true
	case taskTool.MatchString(tool) && in.Action == "reply":
		var nested struct {
			TaskID *string `json:"taskId"`
			Text   string  `json:"text"`
		}
		if lenient(in.Payload, &nested) {
			in.TaskID, in.Text = nested.TaskID, nested.Text
		}
		if in.TaskID != nil {
			return *in.TaskID, in.Text, true
		}
	}
	return "", "", false
}
