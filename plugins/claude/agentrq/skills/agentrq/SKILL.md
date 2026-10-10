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
- **Workspace Memory**: What a workspace's agents have learned while working lives in its memory, indexed by `MEMORY.md`. Read it with `listMemories` and `getMemory` before assigning work there.

## Available Tools

### Workspace Management
| Tool | Description |
|------|-------------|
| `listWorkspaces` | List all workspaces (optionally include archived) |
| `createWorkspace` | Create a new workspace with name, description, notification settings, and self-learning notes |
| `getWorkspace` | Get workspace details by ID |
| `updateWorkspace` | Update workspace name, description, notification settings, or self-learning notes |
| `getWorkspaceStats` | Get workspace statistics for a given time range (`7d` or `30d`) |
| `forkWorkspace` | Fork a workspace: its own queue and agent, sharing the parent's settings, memory and skills. Move tasks into it for a second agent to work them |
| `mergeFork` | Merge a fork back once its tasks are completed or rejected: its agent is stopped and every task moves to the parent |

### Task Management
| Tool | Description |
|------|-------------|
| `listTasks` | List tasks in a specific workspace (filterable by status, creator; supports pagination) |
| `listAllTasks` | List tasks across all workspaces (same filters as `listTasks`) |
| `createTask` | Create a task with title, body, assignee (`human`/`agent`), optional cron schedule, and optional parent task |
| `getTask` | Get full task details including messages |
| `replyToTask` | Post a message to a task's thread |
| `respondToTask` | Answer a permission request: `allow`, `allow_all`, `reject`, or `text` to reply without deciding |
| `updateTaskStatus` | Update status: `notstarted`, `ongoing`, `blocked`, `completed`, `rejected`. (`cron` is set by the server for scheduled tasks.) |
| `updateTaskAssignee` | Reassign a task to `agent` or `human` |
| `updateTaskOrder` | Update a task's sort order |
| `updateTaskAllowAll` | Toggle `allow_all_commands` for a task |
| `updateScheduledTask` | Update a scheduled/cron task's title, body, or cron expression |
| `deleteTask` | Delete a task outright, with its messages and attachments. Irreversible — to stop a scheduled task without losing its history, set its status to `rejected` instead |

### Events and Triggers
An event is a named signal a workspace publishes; a trigger is a standing instruction to create a task somewhere when it fires. Together they are how one workspace's finished work starts another's.

| Tool | Description |
|------|-------------|
| `listEvents` | List the events defined for this account |
| `createEvent` | Define a named signal (`^[a-z][a-z0-9_]{0,128}$`) and what a publisher should put in its payload |
| `getEvent` | Get an event by ID |
| `updateEvent` | Revise an event's payload guidelines. Its name is fixed once created |
| `deleteEvent` | Delete an event. Its triggers stop firing |
| `createEventTrigger` | When this event fires, create a task in this workspace. `{{EVENT_PAYLOAD}}` and `{{EVENT_FAQ}}` are substituted in the body only; `emitEventId` chains a second event to the task's completion |
| `listEventTriggers` | List the triggers attached to an event |
| `getEventTrigger` | Get an event trigger by ID |
| `updateEventTrigger` | Rewrite a trigger. Every field is written as given, so send the ones to keep as well |
| `deleteEventTrigger` | Delete a trigger, leaving its event in place |
| `listEventTasks` | List the tasks an event has spawned, to see whether a wired-up system is running |

### Workflows
A workflow is the graph those pieces add up to: a start event, and steps that react to it and to each other.

| Tool | Description |
|------|-------------|
| `listWorkflows` | List the workflows defined for this account |
| `createWorkflow` | Create an empty workflow around a start event |
| `getWorkflow` | Get a workflow by ID |
| `updateWorkflow` | Revise name, description, start event or canvas layout. Only the fields sent are changed |
| `deleteWorkflow` | Delete a workflow and its steps. The events it named are left alone |
| `createWorkflowStep` | Add a step: the event it reacts to, the workspace and task it creates, and the event it emits on completion |
| `listWorkflowSteps` | List a workflow's steps |
| `deleteWorkflowStep` | Remove one step, leaving the rest of the graph in place |
| `listWorkflowTasks` | List the tasks a workflow has spawned |
| `getWorkflowText` | Read the whole graph as an indented document. Steps naming a deleted event or workspace are left out so it always parses |
| `replaceWorkflowFromText` | Replace the entire graph with a document. Names are resolved before anything is written, so an unknown one leaves the workflow untouched |
| `createEnrolmentCode` | Mint a one-time code for enrolling a new machine with agentrqd. Shown once and expires shortly — hand it to the human right away |
| `listMachines` | List the account's machines: enabled, online, agentrqd version and how many agents each runs |
| `launchAgent` | Start a workspace's agent (`claude-code` or `acp-gateway`) on a machine from `listMachines`. Refused while the workspace already has one, has no working directory, or the machine is off, offline or too old |
| `stopAgent` | Stop the agent a workspace is running on a machine, ending what it is in the middle of |

Text mode is two spaces per level, and every list line is `- agent:<workspace>` or `- event:<name>`, alternating:

```
workflow: new_feature
event: code_changed
- agent:doc
  - event:doc_updated
- agent:blog
```

### Attachments
| Tool | Description |
|------|-------------|
| `getAttachment` | Get an attachment by workspace, task and attachment ID: its public link by default, or its base64 content with `format=base64` |

### Workspace Memory
| Tool | Description |
|------|-------------|
| `listMemories` | List a workspace's memories: name, size and when each changed. Content is not included |
| `getMemory` | Read one memory in full, by name. `MEMORY.md` is the index the others hang off |

### Workspace Skills
| Tool | Description |
|------|-------------|
| `searchSkills` | Find the account's skills as a workspace sees them, with whether each is on there, by name or description (`q`, at least 3 characters): name, description, source and size, with `limit`/`offset` paging and a `total`. Content is not included |
| `getSkill` | Read one file of a skill by its `skill://<name>/<path>` URI; `skill://<name>` alone reads its `SKILL.md`, with the list of its other files |

## Resources and Prompts

The server also offers reference guides as resources, and ready-made prompts:

- `agentrq://guides/new-workspace` — how to set up a new workspace for an agent to run in
- `agentrq://guides/agentrqd-setup` — how to install agentrqd and enrol a machine
- Prompt `new-workspace` (`name`, optional `purpose`) — create a workspace and report what is left for a human to connect an agent
- Prompt `setup-agentrqd` (optional `platform`) — mint an enrolment code and hand back the exact commands to run
- Prompt `workspace-status` — a status report across every workspace at once

## Core Guidelines

1. **Discover Before Creating**: Always use `listWorkspaces` or `listAllTasks` to find existing workspaces and tasks before creating new ones. Avoid duplicates.
2. **Gather Full Context**: Before acting on a task, use `getTask` to read the full task details and message thread.
3. **Keep Humans in the Loop**: When blocked or needing approval, set the task status to `blocked` and use `replyToTask` to explain what you need. `blocked` is the status the interface surfaces as needing attention.
4. **Leverage Self-Learning Notes**: Always read a workspace's `selfLearningLoopNote` and its memory index (`getMemory` with `MEMORY.md`) before starting work. Update it with new preferences, patterns, or lessons learned using `updateWorkspace`.
5. **Use Sub-Tasks for Complex Work**: Break large goals into sub-tasks using `parentId` when creating tasks. This creates a clear hierarchy of work.
6. **Track Status Accurately**: Update task statuses as work progresses — `ongoing` when starting, `completed` when finished, `rejected` if the work should not be done. There is no separate failure status: say what went wrong with `replyToTask` and leave the task `blocked` so a human sees it.

## Example Workflows

### 1. Creating a Task for a Specialized Agent
Break down work and assign it to the appropriate workspace agent:
```json
{
  "workspaceId": "aB3dE5gH7jK",
  "title": "Optimize database connection pooling",
  "body": "We are seeing intermittent connection timeouts during peak hours. Analyze logs from the last 7 days and propose connection pool configuration changes.",
  "assignee": "agent"
}
```

### 2. Creating a Scheduled/Cron Task
Set up recurring tasks with cron expressions:
```json
{
  "workspaceId": "aB3dE5gH7jK",
  "title": "Daily health check report",
  "body": "Run system health checks and post a summary of any issues found.",
  "assignee": "agent",
  "cronSchedule": "0 9 * * *"
}
```

### 3. Replying to a Task Thread
Provide updates, ask questions, or share findings in a task's thread:
```json
{
  "workspaceId": "aB3dE5gH7jK",
  "taskId": "zX9vW7tS5rQ",
  "text": "Analysis complete. The connection pool exhausts at 50 concurrent connections. Recommend increasing max pool size to 100 and adding a 30s idle timeout."
}
```

### 4. Updating Self-Learning Notes
Capture workspace-specific knowledge for future sessions:
```json
{
  "id": "aB3dE5gH7jK",
  "selfLearningLoopNote": "- Database changes require DBA team review before applying.\n- Preferred connection pool library: pgxpool.\n- Alert threshold: >5s p99 latency."
}
```

### 5. Responding to a Task
Answer a permission request a task is waiting on. `action` is `allow`, `allow_all`, `reject`, or `text`:
```json
{
  "workspaceId": "aB3dE5gH7jK",
  "taskId": "zX9vW7tS5rQ",
  "action": "allow",
  "text": "Approved. Proceed with the proposed changes."
}
```

### 6. Updating Task Status
Track progress by updating status as work progresses:
```json
{
  "workspaceId": "aB3dE5gH7jK",
  "taskId": "zX9vW7tS5rQ",
  "status": "completed"
}
```
