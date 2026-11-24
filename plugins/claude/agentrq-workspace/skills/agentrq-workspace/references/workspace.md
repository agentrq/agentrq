# workspace

This workspace, and the events that start tasks in other workspaces.

- `get`: the workspace's title and mission. Read it at the start of a task; it takes no payload.
Examples:
```json
{ "action": "get" }
```

- `publishEvent`: `name`, `payload` and optional `faq` and `taskId`; every workspace subscribed to the event gets its trigger task. A task created with an event to publish tells you so: publish it with a real payload before you complete the task.
Examples:
```json
{ "action": "publishEvent", "payload": { "name": "deploy_done", "taskId": "0kGogHmaqvZ", "payload": "v0.9.20 is live on production.", "faq": [{ "q": "Which commit?", "a": "a0b8ab0" }] } }
```
