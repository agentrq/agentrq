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

| Tool | Description |
| --- | --- |
| `listWorkspaces` | List all workspaces for the authenticated user |
| `createWorkspace` | Create a new workspace |
| `forkWorkspace` | Fork a workspace, for a second agent to work some of its tasks |
| `mergeFork` | Merge a fork back: stop its agent and move its tasks to the parent |
| `getWorkspace` | Get a workspace by ID |
| `updateWorkspace` | Update a workspace |
| `getWorkspaceStats` | Get statistics for a workspace |
| `task` | Read and manage every workspace's tasks with two inputs, `action` and `payload`: `list`, `listAll`, `create`, `get`, `reply`, `respond` (answer a permission request: allow, allow_all, reject, or text), `updateStatus`, `updateSortingOrder`, `updateAssignee`, `updateAllowAll`, `updateSchedule`, `delete` (with its messages and attachments; cannot be undone) and `getAttachment`. `help` lists the actions, and with `topic` prints one action's payload |
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
| `pano` | Read and write the Pano, the account's shared markdown docs, with `action` and `payload`: `save`, `read`, `comment`, `attach`, `delete` or `help`. Writes are recorded as the supervisor workspace |

**Resources:** `agentrq://guides/new-workspace` and `agentrq://guides/agentrqd-setup`.

**Prompts:** `new-workspace`, `setup-agentrqd` and `workspace-status`.

The tables here are checked against the server in CI — see
`backend/internal/handler/coremcp/plugin_docs_test.go`. A tool added to the server
without a row here fails the build.
