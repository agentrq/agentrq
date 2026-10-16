// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import "testing"

func TestWorkspaceTool(t *testing.T) {
	offered := map[string]struct{}{"reply": {}, "saveMemory": {}, "updateTaskStatus": {}, "task": {}}
	cases := []struct {
		name, title, callName, tool string
		ok                          bool
	}{
		{"a call attributed to the workspace server is ours", "saveMemory (agentrq-workspace MCP Server)", "", "saveMemory", true},
		{"a double-underscore qualified call is ours", "mcp__agentrq-workspace__reply", "", "reply", true},
		{"a dotted qualified call is ours", "mcp.agentrq-workspace.updateTaskStatus", "", "updateTaskStatus", true},
		{"a dashed qualified call is ours", "mcp-agentrq-task", "", "task", true},
		{"a bare server_tool title is split both ways and checked", "agentrq-workspace_updateTaskStatus", "", "updateTaskStatus", true},
		{"a human-readable title with a programmatic workspace tool name is ours", "Replying to task", "mcp__agentrq-workspace__task", "task", true},
		{"a structured workspace tool name wins over a legacy match in the title", "Replying in agentrq-AbCdEfGhIjK", "mcp__agentrq-workspace__task", "task", true},
		{"a bare offered tool name with an attributed workspace title is ours", "task (agentrq-workspace MCP Server)", "task", "task", true},
		{"a non-workspace tool name cannot spoof a workspace tool through its title", "mcp__agentrq-workspace__task", "bash", "", false},
		{"a SQL injection payload with a legacy token embedded is not ours", "sqlite3 \"'; DROP TABLE tasks; --\" agentrq-AbCdEfGhIjK", "", "", false},
		{"a legacy match is not ours unless its first word is an offered tool", "rm agentrq-AbCdEfGhIjK", "", "", false},
		{"a legacy match reads its first word past leading space", " reply agentrq-AbCdEfGhIjK", "", "reply", true},
		{"a legacy match in brackets is ours, as the gateway reads it", "reply (agentrq-AbCdEfGhIjK)", "", "reply", true},
		{"a legacy match with trailing text is ours, as the gateway reads it", "reply agentrq-AbCdEfGhIjK: Done.", "", "reply", true},
		{"an attributed call with trailing text is read by server and tool", "task (agentrq-workspace MCP Server): reply to task 42", "", "task", true},
		{"an attributed call with another suffix is read by server and tool", "reply (agentrq MCP tool)", "", "reply", true},
		{"a structured name naming AgentRQ in an unknown form falls back to the title", "task (agentrq-workspace MCP Server)", "mcp_agentrq-workspace_task", "task", true},
		{"a slashed structured name falls back to the title", "task (agentrq-workspace MCP Server)", "agentrq-workspace/task", "task", true},
		{"a structured name naming another server is final", "task (agentrq-workspace MCP Server)", "mcp__filesystem__task", "", false},
		{"a tool the workspace does not offer is not ours", "mcp__agentrq-evil__rm_rf", "", "", false},
		{"a qualified call to a tool not offered is not ours, even with a legacy-shaped server", "mcp__agentrq-AbCdEfGhIjK__rm_rf", "", "", false},
		{"an attributed call to a tool not offered is not ours, even with a legacy-shaped server", "Bash (agentrq-AbCdEfGhIjK MCP Server)", "", "", false},
		{"a structured name is never read by the legacy pattern", "Run command", "cat /tmp/agentrq-abcdefghijk/x", "", false},
		{"a structured name for a tool not offered is not ours", "Run command", "mcp__agentrq-workspace__rm_rf", "", false},
		{"a server not named like AgentRQ's is not ours", "mcp__notagentrq__reply", "", "", false},
		{"an attributed call to another server is not ours", "reply (filesystem MCP Server)", "", "", false},
		{"a plain tool name is not ours", "write_file", "", "", false},
		{"a path is not a call", "cat /home/me/agentrq-workspace/reply", "", "", false},
		{"the legacy pattern is kept as generous as the gateway", "reply agentrq-AbCdEfGhIjK", "", "reply", true},
		{"the legacy pattern alone names no offered tool", "agentrq-AbCdEfGhIjK", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool, ok := workspaceTool(c.title, c.callName, offered)
			if tool != c.tool || ok != c.ok {
				t.Errorf("workspaceTool(%q, %q) was (%q, %v), want (%q, %v)", c.title, c.callName, tool, ok, c.tool, c.ok)
			}
		})
	}
}

func TestAgentReply(t *testing.T) {
	cases := []struct {
		name, tool, input, task, text string
		ok                            bool
	}{
		{"the old reply tool carries chatId and text", "reply", `{"chatId":"0kA","text":"Done."}`, "0kA", "Done.", true},
		{"the task tool's reply carries its payload", "task", `{"action":"reply","payload":{"taskId":"0kB","text":"Hi"}}`, "0kB", "Hi", true},
		{"the task tool's flat reply carries taskId and text", "task", `{"action":"reply","taskId":"0kC","text":"Yo"}`, "0kC", "Yo", true},
		{"a payload with no taskId is no reply", "task", `{"action":"reply","payload":{"text":"Hi"}}`, "", "", false},
		{"another action of the task tool is no reply", "task", `{"action":"get","taskId":"0kC"}`, "", "", false},
		{"a reply with no chatId is no reply", "reply", `{"text":"x"}`, "", "", false},
		{"input that is not an object is no reply", "reply", `"x"`, "", "", false},
		{"another tool is no reply", "memory", `{"chatId":"0kA","text":"x"}`, "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			task, text, ok := agentReply(c.tool, []byte(c.input))
			if task != c.task || text != c.text || ok != c.ok {
				t.Errorf("agentReply(%q, %s) was (%q, %q, %v), want (%q, %q, %v)", c.tool, c.input, task, text, ok, c.task, c.text, c.ok)
			}
		})
	}
}
