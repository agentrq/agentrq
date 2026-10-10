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

| Tool | Description |
| --- | --- |
| `listWorkspaces` | List all workspaces for the authenticated user |
| `createWorkspace` | Create a new workspace |
| `forkWorkspace` | Fork a workspace, for a second agent to work some of its tasks |
| `mergeFork` | Merge a fork back: stop its agent and move its tasks to the parent |
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
| `deleteTask` | Delete a task, with its messages and attachments |
| `getAttachment` | Get an attachment of a task: its name, type and public link (`format=url`, the default), or its content as base64 (`format=base64`) |
| `listMemories` | List a workspace's memories: name, size and when each was last changed |
| `getMemory` | Get one of a workspace's memories in full, by name |
| `searchSkills` | Find the account's skills as a workspace sees them, with whether each is on there, by name or description, with paging; content is not included |
| `getSkill` | Get one file of a workspace's skill in full, by its `skill://<name>/<path>` URI |
| `listEvents` | List the events defined for this account |
| `createEvent` | Define a named signal workspaces can publish |
| `getEvent` | Get an event by ID |
| `updateEvent` | Revise an event's payload guidelines |
| `deleteEvent` | Delete an event; its triggers stop firing |
| `createEventTrigger` | React to an event by creating a task in a workspace |
| `listEventTriggers` | List everything that happens when an event fires |
| `getEventTrigger` | Get an event trigger by ID |
| `updateEventTrigger` | Rewrite an event trigger; every field is written as given |
| `deleteEventTrigger` | Delete an event trigger, leaving its event in place |
| `listEventTasks` | List the tasks an event has spawned |
| `listWorkflows` | List the workflows defined for this account |
| `createWorkflow` | Create an empty workflow around a start event |
| `getWorkflow` | Get a workflow by ID |
| `updateWorkflow` | Revise a workflow; only the fields sent are changed |
| `deleteWorkflow` | Delete a workflow and its steps |
| `createWorkflowStep` | Add a step: an event, the task it creates, and the event it emits |
| `listWorkflowSteps` | List a workflow's steps |
| `deleteWorkflowStep` | Remove one step, leaving the rest of the graph |
| `listWorkflowTasks` | List the tasks a workflow has spawned |
| `getWorkflowText` | Read a workflow's graph as the indented document text mode edits |
| `replaceWorkflowFromText` | Replace a workflow's entire graph with a document |
| `createEnrolmentCode` | Mint a one-time code for enrolling a new machine with agentrqd |
| `listMachines` | List the account's machines, and whether each is online |
| `launchAgent` | Start a workspace's agent on one of the account's machines |
| `stopAgent` | Stop the agent a workspace is running on a machine |

**Resources:** `agentrq://guides/new-workspace` and `agentrq://guides/agentrqd-setup`.

**Prompts:** `new-workspace`, `setup-agentrqd` and `workspace-status`.

This table is checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.
