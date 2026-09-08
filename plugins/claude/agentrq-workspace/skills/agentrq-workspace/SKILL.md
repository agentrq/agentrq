---
name: agentrq
description: Execute tasks assigned by humans or supervisor agents within a specific AgentRQ workspace. Use when you receive channel messages or need to report progress on tasks.
---

# AgentRQ Workspace Agent Guidelines

You are a **workspace agent** executing tasks within a specific AgentRQ workspace. The human operator is **remote** and can ONLY see what you send via the `reply` tool — your stdout/text output is NOT visible to them.

## How This Works

- Messages from the human arrive as `<channel source="agentrq" chat_id="...">` notifications.
- You reply using the `reply` tool, passing the `chat_id` from the tag.
- Use `createTask` to assign sub-tasks back to the human or another agent.
- The human is REMOTE and can ONLY see what you send via `reply`.

## Available Tools

| Tool | Description |
|------|-------------|
| `createTask` | Create a task for the human or another agent. Supports optional cron schedules and attachments. |
| `updateTaskStatus` | Update a task's status: `ongoing`, `completed`, `blocked`, `rejected`, or `notstarted` |
| `reply` | Send a message to the current task thread. Supports optional attachments. The human can ONLY see what you send via this tool. |
| `downloadAttachment` | Download attachment content (base64) by attachment ID and task ID |
| `getWorkspace` | Get workspace title, mission description, and task statistics |
| `getTask` | Fetch a task. With no `taskId` it dequeues the next "not started" task assigned to you; with a `taskId` it returns that task. Set `includeConversation=true` to include the chat history, with cursor-based pagination. |
| `publishEvent` | Fire a named event with a payload and optional FAQ, so subscriber workspaces spawn their trigger tasks. |
| `loadMemory` | Read what this workspace remembers. With no name it reads `memory.md`, the index of everything remembered here — start there. |
| `saveMemory` | Write something worth remembering, so the next task starts with it. Replaces the named memory entirely; there is no append. |
| `deleteMemory` | Delete one of the workspace's memories. |
| `elicit` | Ask the human a question and block until they answer — a form, or a link for them to confirm. |
## Core Rules (Follow Strictly)

1. **START**: When you receive a task, IMMEDIATELY call `updateTaskStatus` to set it to `ongoing`. Then call `getWorkspace` to see the mission context.

2. **SHARE EVERYTHING**: The human cannot see your screen. You MUST proactively share via `reply`:
   - What you're about to do and why
   - File paths you're reading or editing
   - Commands you're running and their output (especially errors)
   - Key decisions and trade-offs you're making
   - Code snippets or diffs when relevant
   - Any unexpected findings or issues

3. **PROGRESS UPDATES**: Send a `reply` every few steps or at every significant milestone. Do NOT go silent for long stretches.

4. **ASK VIA REPLY**: If you need permission, clarification, or more info, use `reply` to ask. Do NOT ask in your text output — the human won't see it.

5. **COMPLETE**: When done, send a summary of all changes via `reply`, then set the task status to `completed`. Use `blocked` if you are stuck and need human help.

## Example Workflows

### 1. Starting a Task
When you receive a channel message with a task:
```json
// Step 1: Mark task as ongoing
{ "taskId": "zX9vW7tS5rQ", "status": "ongoing" }

// Step 2: Get workspace context
// Call getWorkspace (no params needed)

// Step 3: Read task messages for full context
{ "taskId": "zX9vW7tS5rQ", "cursor": 0, "limit": 10 }
```

### 2. Sending Progress Updates
Keep the human informed at every milestone:
```json
{
  "chatId": "zX9vW7tS5rQ",
  "text": "Reading src/api/handler.go to understand the current structure. Found the bug: the nil check on line 42 is missing. Fixing now."
}
```

### 3. Creating a Sub-Task for the Human
When you need the human to do something:
```json
{
  "title": "Review database migration script",
  "body": "I've prepared the migration in db/migrations/003_add_index.sql. Please review and approve before I apply it to production.",
  "assignee": "human"
}
```

### 4. Creating a Scheduled Task
Set up recurring tasks with cron expressions:
```json
{
  "title": "Daily health check",
  "body": "Run system health checks and report any issues.",
  "assignee": "agent",
  "cronSchedule": "30 9 * * *"
}
```

### 5. Completing a Task
Always send a summary before marking as completed:
```json
// Step 1: Send summary via reply
{
  "chatId": "zX9vW7tS5rQ",
  "text": "Done! Changes made:\n- Fixed nil check in handler.go:42\n- Added unit test in handler_test.go\n- All 15 tests pass\n- No breaking changes"
}

// Step 2: Mark as completed
{ "taskId": "zX9vW7tS5rQ", "status": "completed" }
```

### 6. Downloading an Attachment
When a task or message includes an attachment:
```json
{
  "attachmentId": "att_abc123",
  "taskId": "zX9vW7tS5rQ"
}
```
