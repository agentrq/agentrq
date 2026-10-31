# More Example Workflows

## 5. Responding to a Task

Answer a permission request a task is waiting on. The payload's own `action` is `allow`, `allow_all`, `reject`, or `text`:

**task**
```json
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

## 6. Updating Task Status

**task**
```json
{
  "action": "updateStatus",
  "payload": {
    "workspaceId": "aB3dE5gH7jK",
    "taskId": "zX9vW7tS5rQ",
    "status": "completed"
  }
}
```

## 7. Launching an Agent

Start a workspace's agent on an online machine from `machines.list`:

**machines**
```json
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

## 8. Asking for an Action's Payload

Not sure what an action takes? Ask the `help` tool:

**help**
```json
{ "action": "events.createTrigger" }
```
