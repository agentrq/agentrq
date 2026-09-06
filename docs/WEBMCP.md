# WebMCP: driving AgentRQ from a browser agent

AgentRQ offers its own interface to an AI agent running in your browser. If your
browser supports [WebMCP](https://github.com/webmachinelearning/webmcp), an
agent you talk to there can list your workspaces, open a task, reply in it,
change a status, build a workflow — anything you can do by clicking, it can do
by calling a tool.

Nothing to install and nothing to configure. Open AgentRQ, sign in, and the
tools are there.

---

## What it is

WebMCP is a browser API that lets a *page* hand the agent a set of typed tools.
It is the same idea as an MCP server, with one difference that matters: the
tools run inside the page you already have open, as you, in your session. The
agent never needs a token, never sees your credentials, and can do exactly what
your account can do — no more.

That last point is worth being concrete about. A tool call is the same HTTP
request the interface itself makes, carrying the same cookie. If you cannot
delete a workspace, neither can the agent.

## What you can ask for

Every capability of the interface, grouped roughly as the app is:

| Area | Examples |
|---|---|
| **Where you are** | `getCurrentPage`, `navigate` |
| **Workspaces** | list, read, create, update, archive, unarchive, delete, stats, Slack channel, connection token |
| **Tasks** | list, read, create, reply, respond, status, assignee, order, move, stop, delete, permission verdicts, elicitation answers, scheduled-task template, counts |
| **Events** | list, read, create, update, delete, triggers (list/create/update/delete), tasks an event spawned |
| **Workflows** | list, read, create, update, delete, steps, tasks, and the whole workflow as editable text |

`getCurrentPage` is the one to know about. It tells the agent which page you are
looking at and the IDs in the URL, which is what turns "reply to this task" into
a real call. Without it the agent would have to ask you for IDs you should never
have to read.

Two things in the interface are deliberately **not** tools:

- **Attachments.** Their URLs are rendered by the page and fetched by the
  browser with your session; there is nothing an agent can usefully do with one.
- **Telemetry.** The app records a little about its own use. That is not an
  action you take, so it is not something to offer.

## Reading the annotations

Every tool says what kind of thing it is, so an agent can decide what to do
without asking you and what to check first:

- **`readOnlyHint`** — reads nothing but data. `listWorkspaces`, `getTask`,
  `getCurrentPage` and the rest of the read side.
- **`destructiveHint`** — removes something that does not come back:
  `deleteWorkspace`, `deleteTask`, `deleteEvent`, `deleteEventTrigger`,
  `deleteWorkflow`, `deleteWorkflowStep`, and `replaceWorkflowFromText`, which
  drops anything not in the text you give it.

A good agent will confirm a destructive call with you first. AgentRQ marks them
so it can.

## Which browsers

WebMCP is new and support is uneven. AgentRQ looks for it in both places the
specification has used — `document.modelContext` (current) and
`navigator.modelContext` (deprecated in Chromium 150) — and if neither is there,
does nothing at all. The app is unchanged; there are simply no tools.

The API also requires a secure context, so it will not appear on a plain `http://`
page. Serve AgentRQ over HTTPS, or use `http://localhost`, which browsers treat
as secure.

This works the same way in the desktop app, which runs the same interface.

## Signing out

The tools are registered when you sign in and withdrawn the moment you sign out.
Because they act as you, leaving them registered would leave an agent holding a
session that has ended — so it does not happen, and there is a test that fails
if it ever starts.

---

## For contributors

Three files, and the split between them is deliberate:

- `frontend/src/webmcp/modelContext.js` — the browser seam. Finds the API,
  registers tools one at a time so a refusal costs one tool rather than the
  page, and reports why nothing registered. No Vue.
- `frontend/src/webmcp/tools.js` — the catalogue. Pure: it takes the API client
  and a router and returns descriptors.
- `frontend/src/composables/useWebMCP.js` — the wiring, and the only part that
  knows about Vue.

**The invariant to keep:** the catalogue mirrors `frontend/src/api.js`. That
module is the interface's capability surface, so mirroring it is what makes
"anything you can do in the UI" checkable. `frontend/test/webmcpTools.test.js`
enforces it — it drives every tool against a recording stub and asserts that
every exported API function was reached. Add a function to `api.js` without
adding a tool and that test fails, which is the intent.

Verify the whole path in a real browser with:

```bash
cd desktop && npm run verify:webmcp
```

It needs no backend: it installs a stub `document.modelContext` exactly as a
supporting browser would, answers the API itself so each tool call is
observable, and checks registration, invocation, navigation, annotations and
withdrawal on sign-out.
