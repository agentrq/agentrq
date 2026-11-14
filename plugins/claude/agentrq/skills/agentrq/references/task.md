# The task tool

Every action but `listAll` and `changes` takes `workspaceId`. Following tasks up (`changes`, `list`, `listAll`, `messages`) is in `task-follow-up.md`.

- `create` with `title`, `body`, `assignee` (`human`/`agent`), optional `cronSchedule` and `parentId`; the supervisor is its `createdByWorkspaceId`. `clearContext` sends the agent `/clear` first.
- `get`: the whole task, too big to poll with; tool calls with `includeToolCalls: true`; `messagesSince` (RFC3339) keeps newer ones.
- `reply` posts to a task; an agent ignores replies on a task it finished. `followUp` (`text`) asks it for more instead of a new task: the task reopens and the agent reads the follow-up first (now, if still `ongoing`). Not on a human's task.
- `respond` answers a permission request: `action` `allow`, `allow_all`, `reject`, or `text` (no decision).
- `updateStatus` (`notstarted`, `ongoing`, `blocked`, `completed`, `rejected`; `cron` is the server's), `updateAssignee`, `updateSortingOrder`, `updateAllowAll` and `updateSchedule` (a scheduled task's title, body or cron).
- `create`, `reply`, `respond` and the updates answer only the task's `id`, `workspaceId`, `title`, `status`, `assignee`, `updatedAt` (`reply` and `followUp` add `messageId`); `verbose: true`: the whole task.
- `delete` removes a task and all it holds, for good; `rejected` stops a schedule, keeping its history.
- `getAttachment`: its public link, base64 with `format=base64`, or a text file's text: `format=textContent`.
