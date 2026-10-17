// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package acpv1

import "testing"

func TestWorkspaceTool(t *testing.T) {
	offered := map[string]struct{}{"reply": {}, "saveMemory": {}, "updateTaskStatus": {}, "task": {}}
	servers := []string{"agentrq-workspace", "agentrq-AbCdEfGhIjK"}
	cases := []struct {
		name, title, callName, tool string
		ok                          bool
	}{
		{"a call attributed to the workspace server is ours", "saveMemory (agentrq-workspace MCP Server)", "", "saveMemory", true},
		{"a double-underscore qualified call is ours", "mcp__agentrq-workspace__reply", "", "reply", true},
		{"a dotted qualified call is ours", "mcp.agentrq-workspace.updateTaskStatus", "", "updateTaskStatus", true},
		{"a dashed qualified call is ours", "mcp-agentrq-workspace-task", "", "task", true},
		{"a bare server_tool title is split both ways and checked", "agentrq-workspace_updateTaskStatus", "", "updateTaskStatus", true},
		{"a human-readable title with a programmatic workspace tool name is ours", "Replying to task", "mcp__agentrq-workspace__task", "task", true},
		{"a structured workspace tool name wins over an agentrq id in the title", "Replying in agentrq-AbCdEfGhIjK", "mcp__agentrq-workspace__task", "task", true},
		{"a bare offered tool name with an attributed workspace title is ours", "task (agentrq-workspace MCP Server)", "task", "task", true},
		{"a non-workspace tool name cannot spoof a workspace tool through its title", "mcp__agentrq-workspace__task", "bash", "", false},
		{"an attributed call with trailing text is read by server and tool", "task (agentrq-workspace MCP Server): reply to task 42", "", "task", true},
		{"a structured name naming AgentRQ in an unknown form falls back to the title", "task (agentrq-workspace MCP Server)", "mcp_agentrq-workspace_task", "task", true},
		{"a slashed structured name falls back to the title", "task (agentrq-workspace MCP Server)", "agentrq-workspace/task", "task", true},
		{"a structured name naming another server is final", "task (agentrq-workspace MCP Server)", "mcp__filesystem__task", "", false},
		{"a structured name for a tool not offered is not ours", "Run command", "mcp__agentrq-workspace__rm_rf", "", false},
		{"a structured name mentioning agentrq is never matched loosely", "Run command", "cat /tmp/agentrq-abcdefghijk/x", "", false},
		{"a lookalike server offering a workspace tool's name is not ours", "mcp__agentrq-evil__task", "", "", false},
		{"a lookalike structured name is not ours", "Run command", "mcp__agentrq-evil__task", "", false},
		{"a server differing only in case is not ours", "mcp__AgentRQ-Workspace__task", "", "", false},
		{"the workspace's own agentrq-<id> server is ours", "mcp__agentrq-AbCdEfGhIjK__task", "", "task", true},
		{"the workspace's own agentrq-<id> server is ours when attributed", "task (agentrq-AbCdEfGhIjK MCP Server)", "", "task", true},
		{"the workspace's own agentrq-<id> structured name is ours", "Replying to task", "mcp__agentrq-AbCdEfGhIjK__task", "task", true},
		{"another workspace's agentrq-<id> server is not ours", "mcp__agentrq-ZyXwVuTsRqP__task", "", "", false},
		{"another workspace's agentrq-<id> structured name is final", "mcp__agentrq-workspace__task", "mcp__agentrq-ZyXwVuTsRqP__task", "", false},
		{"a bare agentrq server is not ours", "reply (agentrq MCP tool)", "", "", false},
		{"a tool and an agentrq id is not ours", "reply agentrq-AbCdEfGhIjK", "", "", false},
		{"a tool and a bracketed agentrq id is not ours", "reply (agentrq-AbCdEfGhIjK)", "", "", false},
		{"a shell command starting with an offered tool's name is not ours", "reply agentrq-AbCdEfGhIjK; rm -rf ~", "", "", false},
		{"a SQL injection payload with an agentrq id embedded is not ours", "sqlite3 \"'; DROP TABLE tasks; --\" agentrq-AbCdEfGhIjK", "", "", false},
		{"a tool the workspace does not offer is not ours", "mcp__agentrq-workspace__rm_rf", "", "", false},
		{"an attributed call to another server is not ours", "reply (filesystem MCP Server)", "", "", false},
		{"a server not named like AgentRQ's is not ours", "mcp__notagentrq__reply", "", "", false},
		{"a plain tool name is not ours", "write_file", "", "", false},
		{"a path is not a call", "cat /home/me/agentrq-workspace/reply", "", "", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			tool, ok := workspaceTool(c.title, c.callName, servers, offered)
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
