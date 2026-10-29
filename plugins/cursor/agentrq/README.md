# AgentRQ plugin for Cursor

The **supervisor** plugin: it talks to the account-level MCP server, so one agent can
orchestrate work across every workspace you own.

Install it from the Cursor Marketplace: open **Customize** in the sidebar, find **AgentRQ**
and select **Install**. Teams can also import this repository under **Dashboard → Plugins &
MCPs → Add Marketplace → Import from Repo**; the marketplace manifest is
[`.cursor-plugin/marketplace.json`](../../../.cursor-plugin/marketplace.json) at the
repository root, and its `source` points back here.

The plugin connects to `https://mcp.agentrq.com/mcp` and asks nothing at install; Cursor
opens the AgentRQ sign-in the first time the server is used. Its server shows up as
`plugin-agentrq-agentrq`.

For a single workspace rather than the whole account, see
[`agentrq-workspace`](../agentrq-workspace/README.md). For a self-hosted server, add its
supervisor MCP URL to `~/.cursor/mcp.json` yourself instead:

```json
{
  "mcpServers": {
    "agentrq": { "url": "https://agentrq.example.com/mcp" }
  }
}
```

To try a change locally, copy this folder to `~/.cursor/plugins/local/agentrq` and run
**Developer: Reload Window**.

Human-in-the-loop task manager for agents. Connects to the AgentRQ supervisor MCP server (account-level — manage workspaces and tasks across all of them).

**Tools:**

Every tool takes two inputs, `action` and `payload` (the action's fields), e.g.
`task {"action": "create", "payload": {"workspaceId": "<id>", "title": "Ship it"}}`. Ask the
`help` tool for an action's exact payload and an example.

| Tool | Actions |
| --- | --- |
| `workspace` | The account's workspaces: `list` (short unless `includeDetails`), `create`, `get` (with its `mcpUrl`), each with what its agent is doing (running, idle, paused at a usage limit, or offline), `update` (only the fields sent change), `stats` (over 7 or 30 days), `fork` (its own queue and agent, sharing the parent's settings, memory and skills) and `mergeFork` (back into the parent, once its tasks are done) |
| `task` | The tasks of every workspace: `list`, `listAll` (each task says whether it is `awaitingReply`, which also filters), `create`, `get` (tool calls only with `includeToolCalls`; `messagesSince` keeps the newer messages), `messages` (just the conversation, oldest first, with the task's status), `reply`, `respond` (answer a permission request: allow, allow_all, reject, or text), `updateStatus`, `updateSortingOrder`, `updateAssignee`, `updateAllowAll`, `updateSchedule`, `delete` (with its messages and attachments; cannot be undone) and `getAttachment` |
| `memory` | A workspace's memory, read-only: `list` (name, size, last change) and `get` (one in full; `MEMORY.md` is the index) |
| `skills` | The account's skills as a workspace sees them, read-only: `search` and `get` (one file by `skill://` URI) |
| `events` | Named signals and the triggers that turn them into tasks: `list`, `create`, `get`, `update`, `delete`, `listTasks`, `listTriggers`, `createTrigger`, `getTrigger`, `updateTrigger` and `deleteTrigger` |
| `workflows` | Graphs of events and the workspaces they hand off to: `list`, `create`, `get`, `getText`, `update`, `delete`, `replaceFromText`, `listSteps`, `createStep`, `deleteStep` and `listTasks` |
| `machines` | Machines running agentrqd: `list` (with each agent's workspace and state), `createEnrolmentCode` (one-time, expires shortly), `launchAgent` and `stopAgent` |
| `pano` | The account's shared markdown docs: `save`, `read`, `comment`, `attach` and `delete`. Writes are recorded as the supervisor workspace |
| `help` | How to call the other tools: every tool, one tool's actions (`{"action": "task"}`), or one action's payload and an example (`{"action": "task.create"}`) |

**Resources:** `agentrq://guides/new-workspace` and `agentrq://guides/agentrqd-setup`.

**Prompts:** `new-workspace`, `setup-agentrqd` and `workspace-status`.

The tables here are checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.

[Privacy Policy](https://agentrq.com/privacy)

