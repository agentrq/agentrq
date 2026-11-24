# memory

The workspace's memory: notes that outlive a task, shared by every agent working here, now and later. Save what would save the next agent the same detour, and keep each entry to one fact or rule.

- `load`: a memory by `name`; with none, `memory.md`, the index of everything remembered. Start every task with it, then load the `memory://<name>` entries it links that look relevant.
Examples:
```json
{ "action": "load" }
{ "action": "load", "payload": { "name": "deploys.md" } }
```

- `save`: `name` and `content`; replaces the memory whole, there is no append. Changing one needs the `version` `load` showed; if refused, load again, merge and retry. A new memory is found only through the index, so save `memory.md` too with a `memory://<name>` link to it.
Examples:
```json
{ "action": "save", "payload": { "name": "deploys.md", "content": "# How we ship\n\nTag main, then ...", "version": 3 } }
{ "action": "save", "payload": { "name": "memory.md", "content": "# Memory\n\n- [How we ship](memory://deploys.md): tag main first", "version": 7 } }
```

- `delete`: removes one. The index is not changed for you: save `memory.md` without its link too, or it points at nothing. With no name it deletes `memory.md` itself, and every link with it.
Examples:
```json
{ "action": "delete", "payload": { "name": "deploys.md" } }
{ "action": "save", "payload": { "name": "memory.md", "content": "# Memory\n", "version": 8 } }
```
