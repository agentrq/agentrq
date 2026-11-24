# memory

The workspace's memory: notes that outlive a task, shared by every agent working here, now and later.

- `load`: a memory by `name`; with none, `memory.md`, the index of everything remembered. Start every task with it, then load the `memory://<name>` entries it links that look relevant.
- `save`: `name` and `content`; replaces the memory whole, there is no append. Changing one needs the `version` `load` showed; if refused, load again, merge and retry.
- `delete`: removes one.

Save what would save the next agent the same detour, and link it from the index. Keep entries short: one fact or rule each.
