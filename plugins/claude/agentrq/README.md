# AgentRQ plugin for Claude Code

The **supervisor** plugin: it talks to the account-level MCP server, so one agent can
orchestrate work across every workspace you own.

Installed from this repository's marketplace:

```bash
/plugin marketplace add https://github.com/agentrq/agentrq
/plugin install agentrq@agentrq
```

The plugin connects to `https://mcp.agentrq.com/mcp` and asks nothing at install; Claude Code
opens the AgentRQ sign-in the first time a tool is used. For a self-hosted server, install
[`agentrq-custom-supervisor`](../agentrq-custom-supervisor/README.md) instead.

For a single workspace rather than the whole account, see
[`agentrq-workspace`](../agentrq-workspace/README.md).

## Permissions

A plugin cannot pre-approve its own tools, so Claude Code asks before each call until you
allow them. One wildcard in `.claude/settings.local.json` covers every tool the server
offers, including any added later:

```json
{
  "permissions": {
    "allow": ["mcp__plugin_agentrq_agentrq__*"]
  }
}
```

Human-in-the-loop task manager for agents. Connects to the AgentRQ supervisor MCP server (account-level — manage workspaces and tasks across all of them).

**Tools:**

Every tool takes two inputs, `action` and `payload` (the action's fields), e.g.
`task {"action": "create", "payload": {"workspaceId": "<id>", "title": "Ship it"}}`. Ask the
`help` tool for an action's exact payload and an example.

| Tool | Actions |
| --- | --- |
| `workspace` | The account's workspaces: `list`, `create`, `get` (with its `mcpUrl`), `update` (only the fields sent change), `stats` (over 7 or 30 days), `fork` (its own queue and agent, sharing the parent's settings, memory and skills) and `mergeFork` (back into the parent, once its tasks are done) |
| `task` | The tasks of every workspace: `list`, `listAll`, `create`, `get`, `reply`, `respond` (answer a permission request: allow, allow_all, reject, or text), `updateStatus`, `updateSortingOrder`, `updateAssignee`, `updateAllowAll`, `updateSchedule`, `delete` (with its messages and attachments; cannot be undone) and `getAttachment` |
| `memory` | A workspace's memory, read-only: `list` (name, size, last change) and `get` (one in full; `MEMORY.md` is the index) |
| `skills` | The account's skills as a workspace sees them, read-only: `search` and `get` (one file by `skill://` URI) |
| `events` | Named signals and the triggers that turn them into tasks: `list`, `create`, `get`, `update`, `delete`, `listTasks`, `listTriggers`, `createTrigger`, `getTrigger`, `updateTrigger` and `deleteTrigger` |
| `workflows` | Graphs of events and the workspaces they hand off to: `list`, `create`, `get`, `getText`, `update`, `delete`, `replaceFromText`, `listSteps`, `createStep`, `deleteStep` and `listTasks` |
| `machines` | Machines running agentrqd: `list`, `createEnrolmentCode` (one-time, expires shortly), `launchAgent` and `stopAgent` |
| `pano` | The account's shared markdown docs: `save`, `read`, `comment`, `attach` and `delete`. Writes are recorded as the supervisor workspace |
| `help` | How to call the other tools: every tool, one tool's actions (`{"action": "task"}`), or one action's payload and an example (`{"action": "task.create"}`) |

**Resources:** `agentrq://guides/new-workspace` and `agentrq://guides/agentrqd-setup`.

**Prompts:** `new-workspace`, `setup-agentrqd` and `workspace-status`.

The tables here are checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.

[Privacy Policy](https://agentrq.com/privacy)

