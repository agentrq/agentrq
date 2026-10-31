# The memory, skills and pano tools

## memory

A workspace's memory, read-only.

- `list` — List a workspace's memories: name, size and when each changed. Content is not included
- `get` — Read one memory in full, by name. `MEMORY.md` is the index the others hang off

## skills

The account's skills as a workspace sees them, read-only.

- `search` — Find the account's skills as a workspace sees them, with whether each is on there, by name or description (`q`, at least 3 characters): name, description, source and size, with `limit`/`offset` paging and a `total`. Content is not included
- `get` — Read one file of a skill by its `skill://<name>/<path>` URI; `skill://<name>` alone reads its `SKILL.md`, with the list of its other files

## pano

The account's Pano: markdown docs every workspace reads and writes.

- `save` creates a doc (no `id`; returns its `id`) or replaces one's `title` and `markdown`; its `files` must each be linked from the markdown by filename (`![alt](chart.png)`) or the save is refused
- `read` takes `id`, `includeComments` and `includeAttachments`
- `comment` adds `comment`; `attach` adds `files` that need no link; `delete` deletes the doc

Writes are recorded as the supervisor workspace. There is no search: pass a doc's `id` along.
