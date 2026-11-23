# QA: end-to-end sanity checks

Playwright tests that drive the real web app against a real backend, to check
the interactions between the two after a change. Nothing is stubbed. The suite
starts its own Go server, on an empty SQLite database, and its own vite dev
server, then signs in as the root user.

## Run it

```sh
cd qa
npm ci
npx playwright install chromium   # once
npm test
```

It takes about half a minute once Go has built the server; the first build is
slower. Go and Node must be on `PATH`. The frontend's dependencies must be
installed (`cd frontend && npm ci`).

The servers are started on ports **3911** (backend) and **5912** (frontend),
never 3000/5173, so a dev server you are already running is left alone. If
something is already listening on those ports, it is reused instead, except
under `CI`.

On Postgres 18, with docker: `./postgres.sh tests/teams.spec.js` builds the
server and runs the specs named, or all of them, in a container beside a
Postgres one, on a fresh database each run.

## What it covers

| Spec | Checks |
| --- | --- |
| `escape.spec.js` | Escape closes the attachment preview, and the new task form even while typing in it. With the task finder open over the form, the first Escape closes only the finder. |
| `analytics.spec.js` | A workspace's stats and task latency answer for every range and aggregate, and the cards show the server's numbers. The account's Performance tab loads. |
| `forks.spec.js` | Fork a workspace from the sidebar menu. Merge is refused while a task is unfinished, both in the menu and by the server (409). Then the fork merges, and its task moves to the parent. |
| `workspaces.spec.js` | A workspace created, renamed and deleted through the API, and a fork made and merged that way (in `forks.spec.js`), change the open page's sidebar without a reload. |
| `tasks.spec.js` | Dragging a card into Done completes the task. Deleting from the task list removes the row. A delete the server refuses (the task was deleted elsewhere) is reported. |
| `admin-hooks.spec.js` | The operator adds the built-in hooks on the admin page, upgrades one, is refused a module that is another hook, turns one off and deletes one. |
| `hooks.spec.js` | A registration of the generic hook made on the Events page, routed to an event. A request to its URL with the right token and secret becomes a task, once: a resend is a duplicate. A wrong secret is taken and then rejected; no token, a wrong token or a deleted registration is 404. The delivery log shows each. |
| `teams.spec.js` | Two people: the owner creates a team and invites; the invitee accepts by the link and reads the shared workspace's tasks, live, with files from the team's route only (an SVG is a download). Clone, team memory from two tabs, unsharing, removal. The invitation cap answers 429. |
| `teams-roles.spec.js` | Someone not in the team gets 404 from every team route and sees nothing of it. Settings offer owners, admins and members only what their role allows, and the server refuses the rest (403). Leaving unshares the leaver's workspaces; deleting sends everyone back to Me. A fork is seen through its parent and never shared alone; an archived workspace leaves the team. A teammate's skill is added to your own, once. |
| `team-skills.spec.js` | Two people in one team workspace: an agent's new skill there is the team's; the other's agent reads it and improves it on the version it read, and she reads that on the team's Skills page. A workspace opted out of it from its Skills tab no longer sees it; one of the owner's own skills is given to the team; with auto opt-in off, what the agent saw stays and a later skill is not on; a member deletes the first for everyone; she leaves and keeps the rest, on in her workspace. |
| `team-pano.spec.js` | The owner shares one Pano doc with a team from its page; a teammate finds it on the team's Pano from the team sidebar and reads it, and its picture, on the team's route, never the owner's other docs or shares. Switched off, the doc and its file answer 404. |
| `launch-shortcuts.spec.js` | The installed app's icon shortcuts: `?shortcut=find-task` opens the task finder and `?shortcut=new-task` asks which workspace, and both drop the query. |
| `app-badge.spec.js` | The installed app's icon badge counts the tasks waiting on the person, live: one more when a task is blocked, back again when it completes. |
| `mcp-sessions.spec.js` | A workspace's MCP session id is a signed token holding only the handshake, and opens nothing without the bearer token. The session keeps working after the backend restarts: calls, its push stream, and a task pushed to it. Forged, foreign and deleted ids are 404. The server speaks 2025-11-25 and 2025-06-18, and refuses 2026-07-28 with the versions to fall back to. |
| `skills.spec.js` | The skills tab lists the account's skills, with the workspace ticked for an import. An import the server refuses shows the server's own reason. The sidebar's Skills link opens the account's Skills page. |

Every test also fails if the page made an API call that failed, or logged a
console error, other than the failures it causes on purpose.

## Settings

| Variable | Default | Meaning |
| --- | --- | --- |
| `QA_API_PORT` | `3911` | The backend's port. |
| `QA_WEB_PORT` | `5912` | The frontend's port. |
| `QA_ROOT_TOKEN` | `agentrq-qa` | The root login token. Set it to your server's token when reusing a server you started yourself. |
| `QA_DATA_DIR` | a new temp folder | Where the backend keeps its database and files. |
| `QA_SERVER_LOGS` | unset | Set it to show the servers' logs. |
| `QA_POSTGRES_HOST` | unset | Runs the server on the Postgres at this host instead of SQLite (user, password `postgres`, database `agentrq`). Set by `postgres.sh`. |
| `QA_BACKEND_CMD` | builds `backend/cmd/server` | The command that starts the server, such as a prebuilt binary. Set by `postgres.sh`. `lib/backend.mjs` runs it, so a test can restart it. |
| `QA_BROWSER_LIBRARY_PATH` | unset | A folder of shared libraries for Chromium on a host without its system dependencies. It is given to the browser only. |

## Writing a test

- Import `test` and `expect` from `lib/fixtures.js`. The page starts signed in,
  and `workspace` is one workspace shared by the whole run.
- The server lets one user create two workspaces and ten tasks a minute. So
  seed through the helpers in `lib/app.js`, which wait out a 429, and reuse
  `workspace` rather than creating new ones.
- Wait for the request a click makes with `nextResponse`, and assert on its
  status as well as on the page.
- Give seeded rows `unique()` names: a reused server keeps its database.
- End with `traffic.expectClean()`.
