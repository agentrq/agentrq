# workspace

- `get`: the workspace's title and mission. Read it at the start of a task; it takes no payload.
- `publishEvent`: `name`, `payload` and optional `faq` and `taskId`; every workspace subscribed to the event gets its trigger task. A task created with an event to publish tells you so: publish it with a real payload before you complete the task.
