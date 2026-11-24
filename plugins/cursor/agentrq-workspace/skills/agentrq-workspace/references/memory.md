# memory

The workspace's memory: notes that outlive a task, shared by every agent working here, now and later. Save what would save the next agent the same detour, link it from the index, and keep each entry to one fact or rule.

- `load`: a memory by `name`; with none, `memory.md`, the index of everything remembered. Start every task with it, then load the `memory://<name>` entries it links that look relevant.
Examples:
```json
{ "action": "load" }
{ "action": "load", "payload": { "name": "deploys.md" } }
```

- `save`: `name` and `content`; replaces the memory whole, there is no append. Changing one needs the `version` `load` showed; if refused, load again, merge and retry.
Examples:
```json
{ "action": "save", "payload": { "name": "deploys.md", "content": "# How we ship\n\nTag main, then ...", "version": 3 } }
```

- `delete`: removes one.
Examples:
```json
{ "action": "delete", "payload": { "name": "deploys.md" } }
```
