# Examples

Starting a task (`task`, then `workspace`, `memory`, `skills`):
```json
{ "action": "updateStatus", "payload": { "taskId": "zX9vW7tS5rQ", "status": "ongoing" } }
{ "action": "get" }
{ "action": "load" }
{ "action": "search", "payload": { "q": "release" } }
```

Reading the task with its conversation (`task`):
```json
{ "action": "get", "payload": { "taskId": "zX9vW7tS5rQ", "includeConversation": true, "limit": 10 } }
```

A progress update (`task`):
```json
{ "action": "reply", "payload": { "taskId": "zX9vW7tS5rQ", "text": "Found the bug: the nil check in handler.go:42 is missing. Fixing now." } }
```

A task for the human, and a scheduled one (`task`):
```json
{ "action": "create", "payload": { "title": "Review the migration", "body": "db/migrations/003_add_index.sql is ready; approve before I apply it.", "assignee": "human" } }
{ "action": "create", "payload": { "title": "Daily health check", "body": "Run the health checks and report issues.", "assignee": "agent", "cronSchedule": "30 9 * * *" } }
```

Finishing (`task`): summary first, then the status.
```json
{ "action": "reply", "payload": { "taskId": "zX9vW7tS5rQ", "text": "Done: fixed the nil check in handler.go:42, added a test; all tests pass." } }
{ "action": "updateStatus", "payload": { "taskId": "zX9vW7tS5rQ", "status": "completed" } }
```

An attachment (`task`):
```json
{ "action": "getAttachment", "payload": { "attachmentId": "att_abc123", "taskId": "zX9vW7tS5rQ" } }
```
