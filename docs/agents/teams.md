# Teams (`controller/team`, `repository/base/team.go`)

- **A team, invitation or member the caller cannot see answers 404, never 403 or 409.** A 403 tells a stranger the id exists; so would a version conflict, which is why `changeTeam` reads the membership before it reports one.
- **Every member is equal: there are no roles.** Who may do what is decided outside AgentRQ, so no check here may grow one back. The last member leaving deletes the team.
- **Every team change starts with `changeTeam`** (`UPDATE teams SET version = version + 1 WHERE id = ? AND version = ?`), the version the client loaded. A mismatch is 409 `{conflict, version}`. The UPDATE also holds the row (Postgres's row lock, SQLite's write lock) until the transaction ends, so the membership and caps read after it see every change committed before. Accept and decline name no version (the invitee never loaded one) and use `bumpTeam`, which locks the same way.
- **Membership a write depends on is read in the repository, after the lock, never only in the controller.** `whileLocked` in `team_rules_test.go` proves each check sees a change committed while it waited.
- **Every change writes its `team_management_activities` row in the change's own transaction**, so a refused or rolled-back change leaves none. Rows outlive the team on purpose: they are the record an outside permission system audits.
- **An invitation is accepted only while its inviter is still in the team.** Checked at accept, so a removed inviter's invitations die without being revoked.
- **The daily cap counts every invitation created in the last 24 hours, revoked and declined ones included.** Otherwise revoking would reset it.
- **The per-account cap locks the inviter's `users` row too (`lockUser`, after `changeTeam`).** The team lock alone lets one account's parallel invites to two teams both pass it on Postgres.
- **Only the SHA-256 of an invitation token is stored.** The link carries the token in its `#fragment`, so it never reaches a server log. It is shown once, when no mail was sent.
- **Read access goes through `CanReadWorkspaceInTeam`, never through an owner check.** It sees a fork through its parent's share and hides archived workspaces. The existing owner checks must stay as they are.
- **Only a parent workspace is shared; a fork follows its parent's share** and shows under its parent's name.
- **A team workspace is every shared workspace of one name** (`lower(trim(name))`, matched in Go by `teamview.WorkspaceKey`, not in SQL: SQLite's `LOWER` folds ASCII only). There is no label to show a share under.

## Team mode (`controller/teamview`, `handler/api/team_view.go`)

- **A read of one row passes `CanReadWorkspaceInTeam` on the row's own workspace; a read across the team takes its workspaces from `ListTeamWorkspaces` and nothing else.** Never select by a workspace id the client sent. `TeamWorkspaceInfo` after the check filters nothing on purpose: a second filter would hide a broken check from the mutation tests.
- **After the check, read as the owner through the owner's own crud method** (`GetTask`, `GetMemory`, `GetAttachment`…). That is how a fork, an archive or storage behaves as it does for the owner. The `System*` crud methods check nothing: call them only with ids a check allowed.
- **A teammate never sees the owner's Pano.** `GetTask` drops `panoDocs`.
- **A team has no memory of its own.** In a workspace shared with a team, the workspace MCP's `memory.md` is an index built at each load (`app/workspace_memory.go`), and `<workspaceId>/<name>` reads a member's memory, read-only, after `CanReadWorkspaceInTeam`. Never store the index: it must follow every share and removal.
- **A file's link in team mode is the team's route** (`/api/v1/teams/:teamId/tasks/:taskId/attachments/:id`), in every response and event. The public `/storage` link has no session, so a teammate would keep the file after the share ends.
- **Team task events go on the account stream as `team.<type>` with `teamId`.** The plain type would land in the Me pages, which list and cache the account's own tasks. Readers are queried at every event, never cached, so an unshare, a removal or an archive stops them at once. They are the readers of the task's current workspace: an event for a task that has moved since goes to no team.
