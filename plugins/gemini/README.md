# AgentRQ extension for Gemini CLI

The **supervisor** extension: it connects [Gemini CLI](https://github.com/google-gemini/gemini-cli)
to AgentRQ's account-level MCP server, so one agent can orchestrate work across every
workspace you own.

```bash
gemini extensions install https://github.com/agentrq/agentrq
```

The server is `https://mcp.agentrq.com/mcp`. It answers an unauthenticated request with
a 401, Gemini CLI discovers its OAuth endpoints from that, and you sign in with your
AgentRQ account; `/mcp auth agentrq` starts the sign-in by hand. To use a self-hosted
AgentRQ, define a server named `agentrq` in `~/.gemini/settings.json`; a server defined
there takes precedence over the extension's:

```json
{
  "mcpServers": {
    "agentrq": { "type": "http", "url": "https://agentrq.example.com/mcp" }
  }
}
```

> Previously this lived in a separate `agentrq-gemini-extension` repository. If you
> installed it from there, uninstall it (`gemini extensions uninstall agentrq`) and
> install it again from the URL above.

## Where the files are

Gemini CLI installs an extension from a git URL, and reads its manifest from the
repository root. So the manifest is [`gemini-extension.json`](../../gemini-extension.json)
at the root of this repository, and its `contextFileName` points back here, at
[`GEMINI.md`](GEMINI.md), the instructions loaded into every session.

Installing from GitHub prefers the latest release's source archive to a clone, so the
extension that users get, and its updates, follow AgentRQ's release tags.

Gemini CLI also loads `skills/`, `agents/`, `commands/`, `hooks/` and `policies/` from
an extension's root, which is this repository's root. Adding a folder with one of those
names there adds it to the extension.

## Permissions

Gemini CLI asks before each tool call. To allow every tool the server offers, including
any added later, add a policy file such as `~/.gemini/policies/agentrq.toml`:

```toml
[[rule]]
mcpName = "agentrq"
decision = "allow"
priority = 100
```

That also allows the deletes, which cannot be undone. Add a rule deciding `ask_user`
with a `toolName` and a higher priority for any tool you want to keep asking about.

## Tools

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

This table is checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.
