# Workspace forks

A fork is a second workspace made from an existing one, so that a second agent can work on some of its tasks in parallel. The fork has its own task queue and its own agent. Its agent never sees the parent's tasks, and the parent's agent never sees the fork's. When the fork's work is done, you merge it back and its tasks return to the parent.

A fork is a **workspace fork**, not a git fork. It forks the AgentRQ workspace. What happens to your files is described in [The fork's folder](#the-forks-folder) below.

## Forking a workspace

There are two ways to make a fork:

- **Fork workspace**: right-click a workspace in the sidebar, or use its **⋯** button, and choose **Fork workspace**. Give the fork a name, or keep the default, `<parent>-fork`. Fork names are kebab-case, like workspace names.
- **Spin up**: on a task row or a kanban card, choose **Spin up**, next to **Edit**. In one step it makes a fork named after the task, moves the task into it and launches an agent there. If one of those steps fails, it tells you which. It is not offered on a task an agent has already started.

Forks appear under their parent in the sidebar, with a fork mark. To give a fork more work, move tasks into it with **Move**, one or several at a time. The Move dialog lists forks under their parent.

You can fork any active workspace, with three exceptions: a fork cannot be forked again, the `supervisor` workspace cannot be forked, and neither can an archived workspace.

## What a fork shares with its parent

| | |
|---|---|
| Memory (`memory.md` and the rest) | Shared. Whatever either agent saves, both read |
| Skills turned on in the parent | Shared: the fork uses the parent's switches |
| Websites shared from the Chrome extension | Shared |
| Attachments | Shared storage, so an attachment still opens after its task moves between the two |
| Notifications, auto-allowed tools, allow-all-commands, clear-context default, self-learning note, message send delay | Inherited. Change them on the parent, and every fork follows. A fork's settings page shows them read-only |
| Name, icon, description | The fork's own |
| Tasks, queue, agent, MCP token | The fork's own |

## The fork's folder

A fork's agent runs in a folder of its own, which `agentrqd` makes the first time the fork is launched:

- If the parent's folder is inside a git repository, the fork gets a **git worktree** on a new branch, `agentrq/fork-<fork id>`.
- Otherwise the fork gets a **copy** of the parent's folder.

The folder is at `~/.agentrq/forks/<fork id>` on that machine, and the fork's settings page shows its path. The fork's terminal opens with how the folder was made (the `git worktree add` command, the copy, or that an existing folder was reused) and then the command its agent was started with. The parent must have a working directory set, because that is where the fork's folder is made from.

To run a fork, the machine needs `agentrqd` 0.9.3 or newer, the first release that makes fork folders. On an older one, the launch is refused with "update agentrqd on this machine to run a fork (it needs 0.9.3 or newer)". If the parent's repository commits its own `.mcp.json` with an AgentRQ workspace entry, a worktree would inherit that entry and point the fork's agent at the parent, so that launch is refused too, with a message naming the file.

## Merging a fork back

Right-click the fork and choose **Merge into `<parent>`**. A merge:

1. stops the fork's agent, if it is running, and waits until its machine says it has stopped;
2. moves every task in the fork to the parent, with its whole thread and attachments;
3. removes the fork.

A merge is refused while any task in the fork is unfinished, that is, in any status other than `completed` or `rejected`. The menu item is disabled and says how many tasks are unfinished. A scheduled (`cron`) task does not hold a merge up: it moves back to the parent and keeps its schedule. A merge is also refused if the fork's agent is on a machine that is offline, because the agent cannot be stopped from here.

A merge leaves the fork's files alone unless you ask otherwise. The worktree or copy stays where it is, and the merge dialog shows its path, so you can merge its branch, copy files out of it, or delete it yourself.

To have the merge delete the folder too, tick **Delete the fork's folder on the machine** in the merge dialog. `agentrqd` removes the worktree or the copy and keeps the git branch, so the fork's commits are not lost. Uncommitted changes in the folder go with it. The merge is refused, before anything moves, if a machine that ran the fork is offline or runs an `agentrqd` that cannot delete the folder yet; merge without the box ticked, or bring the machine back. A fork last launched before AgentRQ kept a record of its machines is refused too, because nothing says where its folder is; merge it without the box ticked and delete the folder by hand.

A fork cannot be deleted or archived. Merging is the only way to remove it, so its tasks are never lost with it. For the same reason, a workspace that has forks cannot be deleted or archived until they are merged.

## From an agent

The account-wide supervisor server has two tools for forks:

| Tool | What it does |
|---|---|
| `forkWorkspace` | `{workspaceId, name?}`. Makes a fork and returns it, with its `forkOfId` |
| `mergeFork` | `{workspaceId, deleteFolder?}`. Merges a fork back, stopping its agent first, and returns `{parentId, movedTasks}`. `deleteFolder` also deletes the fork's folder on its machine |

`listWorkspaces` and `getWorkspace` show a fork's `forkOfId`. A browser agent driving the web app has the same two tools over [WebMCP](WEBMCP.md).
