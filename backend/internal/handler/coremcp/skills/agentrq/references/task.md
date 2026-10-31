# The task tool

Every action but `listAll` takes `workspaceId`.

- `list` a workspace's tasks, or `listAll` across workspaces: filter by `filter`, text in the title or body, `status`, `createdBy`, `createdByWorkspaceId` (the workspace that delegated it) and `awaitingReply: true`; page with `limit`/`offset`.
- A listed task's `awaitingReply` is true while a question or permission request is pending, or its agent's last message ends with "?" unanswered; `lastMessageAt`/`lastMessageBy` say who spoke last. An agent often waits with its task `ongoing`: check `awaitingReply`.
- `create` with `title`, `body`, `assignee` (`human`/`agent`), optional `cronSchedule` and `parentId`; the supervisor workspace is recorded as its `createdByWorkspaceId`. `clearContext` sends `/clear` to the agent's terminal first.
- `get`: the whole task with its messages; tool calls only with `includeToolCalls: true`; `messagesSince` (RFC3339) keeps the messages after it.
- `messages`: just the conversation, oldest first, with the task's `status` and `updatedAt`: each message's `id`, `createdAt`, `sender`, `text` and attachment names. `since` is the `createdAt` of the last one you read; `limit` (default 20, at most 100) keeps the newest. Follow up on a delegated task with `messages`, not `get`.
- `reply` posts to a task's thread. `respond` answers a permission request, its payload's `action` being `allow`, `allow_all`, `reject`, or `text` to reply without deciding.
- `updateStatus` (`notstarted`, `ongoing`, `blocked`, `completed`, `rejected`; `cron` is set by the server), `updateAssignee`, `updateSortingOrder`, `updateAllowAll` and `updateSchedule` (a scheduled task's title, body or cron).
- `delete` removes a task with its messages and attachments, irreversibly; to stop a scheduled task but keep its history, set it `rejected`.
- `getAttachment`: its public link, base64 with `format=base64`, or a text file's text with `format=textContent`.
