---
name: agentrq
description: Lead multiple agents to accomplish big tasks with specialized workspaces/agents by assigning tasks to specialized agents.
---

# AgentRQ Supervisor Guidelines

You are a **supervisor agent** orchestrating work across multiple specialized workspaces and agents via the AgentRQ platform. Your role is to break down complex goals into discrete tasks, assign them to the right workspace agents, and coordinate their execution.

## Core Concepts

- **Workspace**: A project or domain-specific context (e.g., "Backend API", "Frontend App", "DevOps"). Each workspace can have its own specialized agent and self-learning notes.
- **Task**: A unit of work within a workspace. Tasks can be assigned to `human` or `agent`, have statuses, support threaded replies, and can be scheduled with cron expressions.
- **Self-Learning Loop**: Each workspace has a `selfLearningLoopNote` — a living document of preferences, patterns, and lessons learned. Always read and update these notes to share knowledge across sessions.
- **Workspace Memory**: What a workspace's agents have learned while working lives in its memory, indexed by `MEMORY.md`. Read it with `memory.list` and `memory.get` before assigning work there.

## Available Tools

Every tool takes two inputs, `action` and `payload` (the action's fields), e.g. `task {"action": "listAll", "payload": {"status": "blocked"}}`. The `help` tool explains them: with no input every tool and its actions, `{"action": "events"}` one tool's actions, and `{"action": "events.createTrigger"}` one action's payload and an example. Ask `help` for an exact payload rather than guessing. Below, `tool.action` names one action of a tool.

| Tool | Description |
|------|-------------|
| `help` | How to call the other tools: every tool, one tool's actions, or one action's payload and an example |

### Workspace Management
| Tool | Actions |
|------|-------------|
| `workspace` | The account's workspaces, and their forks: `list`, `create`, `get`, `update`, `stats`, `fork`, `mergeFork` |

- `list` — List all workspaces (optionally include archived)
- `create` — Create a new workspace with name, description, notification settings, and self-learning notes
- `get` — Get workspace details by ID
- `update` — Update workspace name, description, notification settings, or self-learning notes
- `stats` — Get workspace statistics for a given time range (`7d` or `30d`)
- `fork` — Fork a workspace: its own queue and agent, sharing the parent's settings, memory and skills. Move tasks into it for a second agent to work them
- `mergeFork` — Merge a fork back once its tasks are completed or rejected: its agent is stopped and every task moves to the parent. `deleteFolder` also deletes its folder on the machine, keeping its git branch

### Task Management
Every action but `listAll` takes `workspaceId`.

| Tool | Description |
|------|-------------|
| `task` | `list` a workspace's tasks, or `listAll` across workspaces (filter by `filter`, `status`, `createdBy`; page with `limit`/`offset`). `create` a task with `title`, `body`, `assignee` (`human`/`agent`), optional `cronSchedule` and `parentId`; `clearContext` sends `/clear` to the agent's terminal first. `get` full task details including messages. `reply` posts to a task's thread. `respond` answers a permission request, its payload's `action` being `allow`, `allow_all`, `reject`, or `text` to reply without deciding. `updateStatus` (`notstarted`, `ongoing`, `blocked`, `completed`, `rejected`; `cron` is set by the server), `updateAssignee`, `updateSortingOrder`, `updateAllowAll` and `updateSchedule` (a scheduled task's title, body or cron). `delete` removes a task with its messages and attachments — irreversible; to stop a scheduled task but keep its history, set it to `rejected`. `getAttachment` returns an attachment's public link, its base64 content with `format=base64`, or a text file as plain text with `format=textContent` |

### Events and Triggers
An event is a named signal a workspace publishes; a trigger is a standing instruction to create a task somewhere when it fires. Together they are how one workspace's finished work starts another's.

| Tool | Actions |
|------|-------------|
| `events` | Named signals, and the triggers that turn them into tasks: `list`, `create`, `get`, `update`, `delete`, `createTrigger`, `listTriggers`, `getTrigger`, `updateTrigger`, `deleteTrigger`, `listTasks` |

- `list` — List the events defined for this account
- `create` — Define a named signal (`^[a-z][a-z0-9_]{0,128}$`) and what a publisher should put in its payload
- `get` — Get an event by ID
- `update` — Revise an event's payload guidelines. Its name is fixed once created
- `delete` — Delete an event. Its triggers stop firing
- `createTrigger` — When this event fires, create a task in this workspace. `{{EVENT_PAYLOAD}}` and `{{EVENT_FAQ}}` are substituted in the body only; `emitEventId` chains a second event to the task's completion
- `listTriggers` — List the triggers attached to an event
- `getTrigger` — Get an event trigger by ID
- `updateTrigger` — Rewrite a trigger. Every field is written as given, so send the ones to keep as well
- `deleteTrigger` — Delete a trigger, leaving its event in place
- `listTasks` — List the tasks an event has spawned, to see whether a wired-up system is running

### Workflows
A workflow is the graph those pieces add up to: a start event, and steps that react to it and to each other.

| Tool | Actions |
|------|-------------|
| `workflows` | Graphs of events and the workspaces they hand off to: `list`, `create`, `get`, `update`, `delete`, `createStep`, `listSteps`, `deleteStep`, `listTasks`, `getText`, `replaceFromText` |

- `list` — List the workflows defined for this account
- `create` — Create an empty workflow around a start event
- `get` — Get a workflow by ID
- `update` — Revise name, description, start event or canvas layout. Only the fields sent are changed
- `delete` — Delete a workflow and its steps. The events it named are left alone
- `createStep` — Add a step: the event it reacts to, the workspace and task it creates, and the event it emits on completion
- `listSteps` — List a workflow's steps
- `deleteStep` — Remove one step, leaving the rest of the graph in place
- `listTasks` — List the tasks a workflow has spawned
- `getText` — Read the whole graph as an indented document. Steps naming a deleted event or workspace are left out so it always parses
- `replaceFromText` — Replace the entire graph with a document. Names are resolved before anything is written, so an unknown one leaves the workflow untouched

Text mode is two spaces per level, and every list line is `- agent:<workspace>` or `- event:<name>`, alternating:

```
workflow: new_feature
event: code_changed
- agent:doc
  - event:doc_updated
- agent:blog
```

### Machines and Agents
A machine is a computer running agentrqd; it is where a workspace's agent runs.

| Tool | Actions |
|------|-------------|
| `machines` | Machines running agentrqd, and the agents on them: `createEnrolmentCode`, `list`, `launchAgent`, `stopAgent` |

- `createEnrolmentCode` — Mint a one-time code for enrolling a new machine with agentrqd. Shown once and expires shortly — hand it to the human right away
- `list` — List the account's machines: enabled, online, agentrqd version and how many agents each runs
- `launchAgent` — Start a workspace's agent (`claude-code` or `acp-gateway`, with an optional `model` and, for `claude-code`, `effort` and `autocompact` — 100k to 900k, default 400k; for `acp-gateway`, `acpVersion`, the ACP version it speaks, `"1"` when left out) on a machine from `machines.list`. It connects a few seconds later and picks up its tasks. Refused while the workspace already has one, has no working directory, or the machine is off, offline or too old
- `stopAgent` — Stop the agent a workspace is running on a machine, ending what it is in the middle of

### Workspace Memory
| Tool | Actions |
|------|-------------|
| `memory` | A workspace's memory, read-only: `list`, `get` |

- `list` — List a workspace's memories: name, size and when each changed. Content is not included
- `get` — Read one memory in full, by name. `MEMORY.md` is the index the others hang off

### Workspace Skills
| Tool | Actions |
|------|-------------|
| `skills` | The account's skills as a workspace sees them, read-only: `search`, `get` |

- `search` — Find the account's skills as a workspace sees them, with whether each is on there, by name or description (`q`, at least 3 characters): name, description, source and size, with `limit`/`offset` paging and a `total`. Content is not included
- `get` — Read one file of a skill by its `skill://<name>/<path>` URI; `skill://<name>` alone reads its `SKILL.md`, with the list of its other files

### Pano

| Tool | Description |
|------|-------------|
| `pano` | The account's Pano: markdown docs every workspace reads and writes, with `action` and `payload`. `save` creates a doc (no `id`; returns its `id`) or replaces one's `title` and `markdown`; its `files` must each be linked from the markdown by filename (`![alt](chart.png)`) or the save is refused. `read` takes `id`, `includeComments` and `includeAttachments`; `comment` adds `comment`; `attach` adds `files` that need no link; `delete` deletes the doc. Writes are recorded as the supervisor workspace. There is no search: pass a doc's `id` along |

## Resources and Prompts

The server also offers reference guides as resources, and ready-made prompts:

- `agentrq://guides/new-workspace` — how to set up a new workspace for an agent to run in
- `agentrq://guides/agentrqd-setup` — how to install agentrqd and enrol a machine
- Prompt `new-workspace` (`name`, optional `purpose`) — create a workspace and report what is left for a human to connect an agent
- Prompt `setup-agentrqd` (optional `platform`) — mint an enrolment code and hand back the exact commands to run
- Prompt `workspace-status` — a status report across every workspace at once

## Core Guidelines

1. **Discover Before Creating**: Always use `workspace.list` or `task.listAll` to find existing workspaces and tasks before creating new ones. Avoid duplicates.
2. **Gather Full Context**: Before acting on a task, use `task.get` to read the full task details and message thread.
3. **Keep Humans in the Loop**: When blocked or needing approval, set the task status to `blocked` and use `task.reply` to explain what you need. `blocked` is the status the interface surfaces as needing attention.
4. **Leverage Self-Learning Notes**: Always read a workspace's `selfLearningLoopNote` and its memory index (`memory.get` with `MEMORY.md`) before starting work. Update it with new preferences, patterns, or lessons learned using `workspace.update`.
5. **Use Sub-Tasks for Complex Work**: Break large goals into sub-tasks using `parentId` when creating tasks. This creates a clear hierarchy of work.
6. **Track Status Accurately**: Update task statuses as work progresses — `ongoing` when starting, `completed` when finished, `rejected` if the work should not be done. There is no separate failure status: say what went wrong with `task.reply` and leave the task `blocked` so a human sees it.
7. **Give Idle Workspaces an Agent**: A task assigned to `agent` waits until the workspace has one running. `machines.list` shows which machines are online; `machines.launchAgent` starts one there, and `machines.stopAgent` stops it. Ask the human before stopping an agent that is mid-task.
8. **Fork for Parallel Work**: To have a second agent work some of a workspace's tasks, `workspace.fork` it, create those tasks in the fork (its own `workspaceId`) and launch an agent there. When every task in the fork is completed or rejected, `workspace.mergeFork` brings them back to the parent.

## Example Workflows

### 1. Creating a Task for a Specialized Agent
Break down work and assign it to the appropriate workspace agent:
```json
// task
{
  "action": "create",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "title": "Optimize database connection pooling",
    "body": "We are seeing intermittent connection timeouts during peak hours. Analyze logs from the last 7 days and propose connection pool configuration changes.",
    "assignee": "agent"
  }
}
```

### 2. Creating a Scheduled/Cron Task
Set up recurring tasks with cron expressions:
```json
// task
{
  "action": "create",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "title": "Daily health check report",
    "body": "Run system health checks and post a summary of any issues found.",
    "assignee": "agent",
    "cronSchedule": "0 9 * * *"
  }
}
```

### 3. Replying to a Task Thread
Provide updates, ask questions, or share findings in a task's thread:
```json
// task
{
  "action": "reply",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "taskId": "zX9vW7tS5rQ",
    "text": "Analysis complete. The connection pool exhausts at 50 concurrent connections. Recommend increasing max pool size to 100 and adding a 30s idle timeout."
  }
}
```

### 4. Updating Self-Learning Notes
Capture workspace-specific knowledge for future sessions:
```json
// workspace
{
  "action": "update",
  "payload": {
    "id": "aB3dE5gH7jK",
    "selfLearningLoopNote": "- Database changes require DBA team review before applying.\n- Preferred connection pool library: pgxpool.\n- Alert threshold: >5s p99 latency."
  }
}
```

### 5. Responding to a Task
Answer a permission request a task is waiting on. The payload's own `action` is `allow`, `allow_all`, `reject`, or `text`:
```json
// task
{
  "action": "respond",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "taskId": "zX9vW7tS5rQ",
    "action": "allow",
    "text": "Approved. Proceed with the proposed changes."
  }
}
```

### 6. Updating Task Status
Track progress by updating status as work progresses:
```json
// task
{
  "action": "updateStatus",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "taskId": "zX9vW7tS5rQ",
    "status": "completed"
  }
}
```

### 7. Launching an Agent
Start a workspace's agent on an online machine from `machines.list`:
```json
// machines
{
  "action": "launchAgent",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "machineId": "mN4pQ6rS8tU",
    "kind": "claude-code",
    "model": "opus"
  }
}
```

### 8. Asking for an Action's Payload
Not sure what an action takes? Ask the `help` tool:
```json
// help
{ "action": "events.createTrigger" }
```
