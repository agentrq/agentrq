# AgentRQ plugin for Claude Code

The **supervisor** plugin: it talks to the account-level MCP server, so one agent can
orchestrate work across every workspace you own.

Installed from this repository's marketplace:

```bash
/plugin marketplace add https://github.com/agentrq/agentrq
/plugin install agentrq@agentrq
```

The supervisor MCP URL is a plugin setting (`agentrq_supervisor_mcp_url`), defaulting to
`https://mcp.agentrq.com/mcp`.

For a single workspace rather than the whole account, see
[`agentrq-workspace`](../agentrq-workspace/README.md).

Human-in-the-loop task manager for agents. Connects to the AgentRQ supervisor MCP server (account-level — manage workspaces and tasks across all of them).

**Tools:**

| Tool | Description |
| --- | --- |
| `listWorkspaces` | List all workspaces for the authenticated user |
| `createWorkspace` | Create a new workspace |
| `getWorkspace` | Get a workspace by ID |
| `updateWorkspace` | Update a workspace |
| `getWorkspaceStats` | Get statistics for a workspace |
| `listTasks` | List tasks in a specific workspace |
| `listAllTasks` | List all tasks across all workspaces |
| `createTask` | Create a new task in a workspace |
| `getTask` | Get a specific task by ID |
| `respondToTask` | Answer a permission request: allow, allow_all, reject, or text |
| `replyToTask` | Post a message to a task thread |
| `updateTaskStatus` | Update a task's status |
| `updateTaskOrder` | Update a task's sort order |
| `updateTaskAssignee` | Update a task's assignee |
| `updateTaskAllowAll` | Toggle allow_all_commands for a task |
| `updateScheduledTask` | Update a scheduled/cron task |
| `getAttachment` | Get attachment data as base64 and metadata |
| `listMemories` | List a workspace's memories: name, size and when each was last changed |
| `getMemory` | Get one of a workspace's memories in full, by name |

The tables here are checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.
