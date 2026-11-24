# Examples

One task from start to finish, across the tools. Each call is the tool's `action` and `payload`; each tool's file has more.

1. Claim the task (`task`), read the mission (`workspace`), what the workspace remembers (`memory`) and the account's playbooks (`skills`):
```json
{ "action": "updateStatus", "payload": { "taskId": "0kGogHmaqvZ", "status": "ongoing" } }
{ "action": "get" }
{ "action": "load" }
{ "action": "search", "payload": { "q": "release" } }
```

2. Follow a matching playbook (`skills`):
```json
{ "action": "load", "payload": { "uri": "skill://release-notes" } }
```

3. Report progress as you go (`task`):
```json
{ "action": "reply", "payload": { "taskId": "0kGogHmaqvZ", "text": "Found the bug: the nil check in handler.go:42 is missing. Fixing now." } }
```

4. Hand a document to other workspaces (`pano`), passing on its `id`:
```json
{ "action": "save", "payload": { "title": "Release plan", "markdown": "# Release plan\n\n- [ ] Tag v0.9.20" } }
```

5. Keep the lesson for the next agent (`memory`), with the `version` load showed:
```json
{ "action": "save", "payload": { "name": "deploys.md", "content": "# How we ship\n\nTag main, then ...", "version": 3 } }
```

6. Finish (`task`): the summary first, then the status.
```json
{ "action": "reply", "payload": { "taskId": "0kGogHmaqvZ", "text": "Done: fixed the nil check in handler.go:42 and added a test; all tests pass." } }
{ "action": "updateStatus", "payload": { "taskId": "0kGogHmaqvZ", "status": "completed" } }
```
