# Teams (`controller/team`, `repository/base/team.go`)

- **A team, invitation or member the caller cannot see answers 404, never 403.** A 403 tells a stranger the id exists. 403 is only for a member short of the role.
- **Every write that counts or checks owners starts with `lockTeam`** (`UPDATE teams SET id = id`). It takes Postgres's row lock and SQLite's write lock before the read. Without it, parallel invites pass the daily cap, and two demotions leave a team with no owner. Both have tests run in parallel.
- **The daily cap counts every invitation created in the last 24 hours, revoked and declined ones included.** Otherwise revoking would reset it.
- **Only the SHA-256 of an invitation token is stored.** The link carries the token in its `#fragment`, so it never reaches a server log. It is shown once, when no mail was sent.
- **Read access goes through `CanReadWorkspaceInTeam`, never through an owner check.** It sees a fork through its parent's share and hides archived workspaces. The existing owner checks must stay as they are.
