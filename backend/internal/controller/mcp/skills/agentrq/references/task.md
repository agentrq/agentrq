# task

This workspace's tasks, and the only way to reach the human. `taskId` is the `chat_id` a task arrives with.

- `get`: a task by `taskId`; `includeConversation` adds its messages (`cursor`, `limit` page them). With no `taskId`, takes the next not started task assigned to you.
Examples:
```json
{ "action": "get", "payload": { "taskId": "0kGogHmaqvZ", "includeConversation": true, "limit": 10 } }
```

- `updateStatus`: `ongoing` when you start, `completed` when you finish, `blocked` when you need the human; also `rejected`, `notstarted`.
Examples:
```json
{ "action": "updateStatus", "payload": { "taskId": "0kGogHmaqvZ", "status": "ongoing" } }
```

- `reply`: `text`, with optional `attachments` (base64 `data`, or `textContent` for a text file). The human sees only these.
Examples:
```json
{ "action": "reply", "payload": { "taskId": "0kGogHmaqvZ", "text": "Tests pass; opening the pull request." } }
```

- `elicit`: asks the human and waits: a form (`requestedSchema`) or a link to confirm (`url`).
Examples:
```json
{ "action": "elicit", "payload": { "taskId": "0kGogHmaqvZ", "message": "Which branch?", "mode": "form", "requestedSchema": { "type": "object", "properties": { "branch": { "type": "string", "enum": ["main", "release"] } } } } }
```

- `create`: a task with `title`, `body`, `assignee` (`human` or `agent`); optional `cronSchedule`, `attachments`, `eventId` (published when it completes), `clearContext` (sends `/clear` first).
Examples:
```json
{ "action": "create", "payload": { "title": "Review the release notes", "body": "Check the v0.9.20 notes before Friday.", "assignee": "human" } }
```

- `getAttachment`: its public link, or its content with `format` `base64` or `textContent`.
Examples:
```json
{ "action": "getAttachment", "payload": { "taskId": "0kGogHmaqvZ", "attachmentId": "att_1" } }
```

From a shell, `npx -y @agentrq/agentrq-ws@latest reply <taskId> "text" --attach a.png` uploads files from disk.
