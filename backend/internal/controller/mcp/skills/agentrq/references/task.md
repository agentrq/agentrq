# task

This workspace's tasks, and the only way to reach the human. `taskId` is the `chat_id` a task arrives with.

- `get`: a task by `taskId`; `includeConversation` adds its messages (`cursor`, `limit` page them). With no `taskId`, takes the next not started task assigned to you.
- `updateStatus`: `taskId` and `status`: `ongoing` when you start, `completed` when you finish, `blocked` when you need the human, also `rejected` and `notstarted`.
- `reply`: `taskId` and `text`, with optional `attachments` (base64 `data`, or plain `textContent` for a text file). The human sees only these.
- `elicit`: `taskId` and `message`; asks the human and waits for the answer, as a form (`requestedSchema`) or a link to confirm (`url`).
- `create`: a task with `title`, `body` and `assignee` (`human` or `agent`); optional `cronSchedule` repeats it, `attachments` add files, `eventId` publishes that event when it completes, `clearContext` sends `/clear` to the agent first.
- `getAttachment`: `attachmentId` and `taskId`; its public link, or its content with `format` `base64` or `textContent`.

From a shell, `npx -y @agentrq/agentrq-ws@latest reply <taskId> "text" --attach a.png` uploads files from disk.

## Examples

```json
{ "action": "updateStatus", "payload": { "taskId": "0kGogHmaqvZ", "status": "ongoing" } }
{ "action": "get", "payload": { "taskId": "0kGogHmaqvZ", "includeConversation": true, "limit": 10 } }
{ "action": "reply", "payload": { "taskId": "0kGogHmaqvZ", "text": "Tests pass; opening the pull request." } }
{ "action": "elicit", "payload": { "taskId": "0kGogHmaqvZ", "message": "Which branch should I release from?", "mode": "form", "requestedSchema": { "type": "object", "properties": { "branch": { "type": "string", "enum": ["main", "release"] } }, "required": ["branch"] } } }
{ "action": "create", "payload": { "title": "Review the release notes", "body": "Check the v0.9.20 notes before Friday.", "assignee": "human" } }
```
