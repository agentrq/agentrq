# The task tool

Every action but `listAll` takes `workspaceId`.

- `list` a workspace's tasks, or `listAll` across workspaces: each one's ids, `title`, `status`, `assignee`, times, `cronSchedule`, `awaitingReply`, `lastMessageBy`; `includeBody: true` adds bodies. Filter by `filter` (title, body), `status`, `createdBy`, `createdByWorkspaceId`, `awaitingReply`; page with `limit`/`offset`.
- `awaitingReply`: a question or permission request is pending, or the agent's last message is an open "?". An agent may wait while `ongoing`: check it.
- `create` with `title`, `body`, `assignee` (`human`/`agent`), optional `cronSchedule` and `parentId`; the supervisor is its `createdByWorkspaceId`. `clearContext` sends the agent `/clear` first.
- `get`: the whole task, too big to poll with; tool calls with `includeToolCalls: true`; `messagesSince` (RFC3339) keeps newer ones.
- `messages`, to poll: the conversation, oldest first, with `status`, `updatedAt`, `total`, `hasMore`; each message's `id`, `createdAt`, `sender`, `text`, file names. `limit` (default 5, at most 100) keeps the newest. `since`/`before` (message id or RFC3339) keep later/earlier ones: the last `id` read, or the oldest to page back. `maxChars` cuts each text.
- `reply` posts to a task. `respond` answers a permission request: `action` `allow`, `allow_all`, `reject`, or `text` (no decision).
- `updateStatus` (`notstarted`, `ongoing`, `blocked`, `completed`, `rejected`; `cron` is the server's), `updateAssignee`, `updateSortingOrder`, `updateAllowAll` and `updateSchedule` (a scheduled task's title, body or cron).
- `create`, `reply`, `respond` and the updates answer only the task's `id`, `workspaceId`, `title`, `status`, `assignee`, `updatedAt` (`reply` adds `messageId`); `verbose: true`: the whole task.
- `delete` removes a task and all it holds, for good; `rejected` stops a schedule, keeping its history.
- `getAttachment`: its public link, base64 with `format=base64`, or a text file's text: `format=textContent`.
