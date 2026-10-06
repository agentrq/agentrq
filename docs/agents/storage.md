# Storage and public links

> `service/storage`, the public file route (`handler/api/file.go`, `crud.GetPublicFile`) and the key layout. Read before changing how a file is kept, keyed or served.

- **A public link is the only credential for a file, and it is predictable by design** — the human's call (task 0jNpPFmXlMP). Keys are plain monoflake ids: hard to guess without the workspace and task ids, but one link gives away its siblings (same millisecond, sequence +1). Don't add a random suffix without asking.
- **The `/storage/...` route is unauthenticated.** It matches the raw, undecoded path against one exact regex before routing, the controller matches the key again, and `LoadRaw` reads through `os.Root`. Loosen none of the three: the storage dir's root holds the SQLite database. Never serve that directory with `app.Static` or a proxy alias.
- **Only a type a browser cannot run is served inline** (`storage.SafeContentType`); HTML, SVG and the rest download, locally and in S3.
- **Attachments are keyed `w-<ws>/<task>/<id>`** under `<storage.dir>/artifacts/` (the storage name for attachments); older ones sit flat in `<storage.dir>`, found by `storage.WithFallback`, and cleanup sweeps both. Anything saved or deleted takes the workspace and task ids, or it lands where no load will look.
- **A fork keeps no files, memory, skill switches or site shares of its own**: all of them key under its parent (`model.Workspace.ContentID`), so a task moved in or merged back never moves a blob and its public link holds. Key by the task's `WorkspaceID` and a fork's files land where the parent cannot find them after the merge.
