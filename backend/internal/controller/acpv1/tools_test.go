// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import "testing"

func TestWorkspaceTool(t *testing.T) {
	offered := map[string]struct{}{"reply": {}, "saveMemory": {}, "updateTaskStatus": {}, "task": {}}
	cases := []struct {
		name, title string
		names       []string
		tool        string
		ok          bool
	}{
		{"a call attributed to the workspace server is ours", "saveMemory (agentrq-workspace MCP Server)", nil, "saveMemory", true},
		{"a double-underscore qualified call is ours", "mcp__agentrq-workspace__reply", nil, "reply", true},
		{"a dotted qualified call is ours", "mcp.agentrq-workspace.updateTaskStatus", nil, "updateTaskStatus", true},
		{"a dashed qualified call is ours", "mcp-agentrq-task", nil, "task", true},
		{"a bare server_tool title is split both ways and checked", "agentrq-workspace_updateTaskStatus", nil, "updateTaskStatus", true},
		{"a human-readable title with a programmatic workspace tool name is ours", "Replying to task", []string{"mcp__agentrq-workspace__task"}, "task", true},
		{"a structured workspace tool name wins over a legacy match in the title", "Replying in agentrq-AbCdEfGhIjK", []string{"mcp__agentrq-workspace__task"}, "task", true},
		{"a tool the workspace does not offer is not ours", "mcp__agentrq-evil__rm_rf", nil, "", false},
		{"a server not named like AgentRQ's is not ours", "mcp__notagentrq__reply", nil, "", false},
		{"an attributed call to another server is not ours", "reply (filesystem MCP Server)", nil, "", false},
		{"a plain tool name is not ours", "write_file", nil, "", false},
		{"a path is not a call", "cat /home/me/agentrq-workspace/reply", nil, "", false},
		{"the legacy pattern is kept as generous as the gateway", "reply agentrq-AbCdEfGhIjK", nil, "reply", true},
		{"the legacy pattern with no space names the whole title", "agentrq-AbCdEfGhIjK", nil, "agentrq-AbCdEfGhIjK", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool, ok := workspaceTool(c.title, offered, c.names...)
			if tool != c.tool || ok != c.ok {
				t.Errorf("workspaceTool(%q, %v) was (%q, %v), want (%q, %v)", c.title, c.names, tool, ok, c.tool, c.ok)
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
