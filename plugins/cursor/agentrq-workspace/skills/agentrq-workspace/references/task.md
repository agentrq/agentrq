# task

Tasks, and the only way to reach the human. `taskId` is a task's `chat_id`.

- `get`: a task; `includeConversation` adds its messages (`cursor`, `limit` page them). With no `taskId`, takes your next not started task.
Examples:
```json
{ "action": "get", "payload": { "taskId": "0kGogHmaqvZ", "includeConversation": true, "limit": 10 } }
```

- `updateStatus`: `ongoing` to start, `completed` when done, `blocked` to need the human; also `rejected`, `notstarted`.
Examples:
```json
{ "action": "updateStatus", "payload": { "taskId": "0kGogHmaqvZ", "status": "ongoing" } }
```

- `reply`: `text`, optional `attachments` and `ui`, a card: [a2ui.md](a2ui.md).
Examples:
```json
{ "action": "reply", "payload": { "taskId": "0kGogHmaqvZ", "text": "Tests pass; opening a PR." } }
```
```sh
npx -y @agentrq/agentrq-ws@latest reply 0kGogHmaqvZ "Screenshot attached." --attach shot.png
```

- `elicit`: asks the human, waits: a form (`requestedSchema`), link (`url`) or card ([a2ui-buttons.md](a2ui-buttons.md)).
Examples:
```json
{ "action": "elicit", "payload": { "taskId": "0kGogHmaqvZ", "message": "Which branch?", "mode": "form", "requestedSchema": { "type": "object", "properties": { "branch": { "type": "string" } } } } }
```

- `create`: `title`, `body`, `assignee` (`human` or `agent`); optional `cronSchedule`, `attachments`, `eventId` (published on completion).
Examples:
```json
{ "action": "create", "payload": { "title": "Review the release notes", "body": "Before Friday.", "assignee": "human" } }
```

- `getAttachment`: its public link, or its content (`format`: `base64`, `textContent`).
Examples:
```json
{ "action": "getAttachment", "payload": { "taskId": "0kGogHmaqvZ", "attachmentId": "att_1" } }
```
```sh
npx -y @agentrq/agentrq-ws@latest attachment get att_1 --task 0kGogHmaqvZ --out ./downloads
```

**Upload and download files with the `npx` lines**, not `data` or `format` `base64`: they move a file through disk, so it never bloats your context.
