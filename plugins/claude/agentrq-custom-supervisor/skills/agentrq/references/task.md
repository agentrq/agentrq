# The task tool

Every action but `listAll` takes `workspaceId`.

- `list` a workspace's tasks, or `listAll` across workspaces: filter by `filter` (title or body text), `status`, `createdBy`, `createdByWorkspaceId` (who delegated it) and `awaitingReply: true`; page with `limit`/`offset`.
- A listed task's `awaitingReply` is true while a question or permission request is pending, or its agent's last message is an unanswered "?"; `lastMessageAt`/`lastMessageBy` say who spoke last. An agent may wait while `ongoing`: check `awaitingReply`.
- `create` with `title`, `body`, `assignee` (`human`/`agent`), optional `cronSchedule` and `parentId`; the supervisor is its `createdByWorkspaceId`. `clearContext` sends `/clear` to the agent first.
- `get`: the whole task and messages; tool calls only with `includeToolCalls: true`; `messagesSince` (RFC3339) keeps newer messages.
- `messages`: just the conversation, oldest first, with the task's `status` and `updatedAt`: each message's `id`, `createdAt`, `sender`, `text`, attachment names. `since`: the last read `createdAt`; `limit` (default 20, at most 100) keeps the newest. Follow up on a delegated task with `messages`, not `get`.
- `reply` posts to a task's thread. `respond` answers a permission request: `action` `allow`, `allow_all`, `reject`, or `text` to reply without deciding.
- `updateStatus` (`notstarted`, `ongoing`, `blocked`, `completed`, `rejected`; `cron` is the server's), `updateAssignee`, `updateSortingOrder`, `updateAllowAll` and `updateSchedule` (a scheduled task's title, body or cron).
- `create`, `reply`, `respond` and the updates answer only the task's `id`, `workspaceId`, `title`, `status`, `assignee`, `updatedAt` (`reply` adds `messageId`); `verbose: true`: the whole task.
- `delete` removes a task, messages and attachments, irreversibly; to stop a schedule but keep its history, set it `rejected`.
- `getAttachment`: its public link, base64 with `format=base64`, or a text file's text with `format=textContent`.
