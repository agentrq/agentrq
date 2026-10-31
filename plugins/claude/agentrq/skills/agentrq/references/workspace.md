# The workspace tool

The account's workspaces, and their forks.

- `list` — List all workspaces (optionally include archived): id, name, fork parent, working directory and `agent`, what its agent is doing. `includeDetails: true` returns every field
- `create` — Create a new workspace with name, description, notification settings, and self-learning notes
- `get` — Get workspace details by ID, with `agent`: `state` (`running`, `idle`, `paused` at a usage limit until `pausedUntil`, or `offline`), `machineId`, `model`, `taskId` (its latest ongoing task) and `lastActivityAt` (when its terminal last printed)
- `update` — Update workspace name, description, notification settings, or self-learning notes
- `stats` — Get workspace statistics for a given time range (`7d` or `30d`)
- `fork` — Fork a workspace: its own queue and agent, sharing the parent's settings, memory and skills. Move tasks into it for a second agent to work them
- `mergeFork` — Merge a fork back once its tasks are completed or rejected: its agent is stopped and every task moves to the parent. `deleteFolder` also deletes its folder on the machine, keeping its git branch

## Forking for parallel work

To have a second agent work some of a workspace's tasks, `workspace.fork` it, create those tasks in the fork (its own `workspaceId`) and launch an agent there. When every task in the fork is completed or rejected, `workspace.mergeFork` brings them back to the parent.
