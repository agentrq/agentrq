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

## What it covers

| Spec | Checks |
| --- | --- |
| `analytics.spec.js` | A workspace's stats and task latency answer for every range and aggregate, and the cards show the server's numbers. The account's Performance tab loads. |
| `forks.spec.js` | Fork a workspace from the sidebar menu. Merge is refused while a task is unfinished, both in the menu and by the server (409). Then the fork merges, and its task moves to the parent. |
| `tasks.spec.js` | Dragging a card into Done completes the task. Deleting from the task list removes the row. A delete the server refuses (the task was deleted elsewhere) is reported. |
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
