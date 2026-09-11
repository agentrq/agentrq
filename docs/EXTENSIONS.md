# Extensions

Extensions add pages, actions, keyboard shortcuts and scheduled work to AgentRQ.
They are ordinary Node modules, published as GitHub repositories, and installed
from the **desktop app**.

> **Desktop only, and deliberately.** An extension is code somebody else wrote,
> running with the privileges of the process it is in. On a self-hosted AgentRQ
> server that would mean running a stranger's code on your infrastructure, next
> to your database and your other users. On the desktop it runs on the machine
> of the person who chose to install it — which is the same trust decision they
> already make installing anything else. The server never loads extension code.

---

## What an extension is

Three files, at minimum:

```
my-extension/
  agentrq-extension.json   the manifest — what it is, and what it asks for
  index.js                 the module — `name`, `inject`, `apply`
  package.json             ordinary npm metadata
```

`index.js` exports the same shape a Cordis plugin does, which is where the
pattern comes from:

```js
export const name = 'my-extension'
export const inject = ['ui', 'shortcuts']

export function apply(ctx, config) {
  ctx.ui.add({ id: 'today', surface: 'page', label: 'Today', view: () => page(ctx) })
}
```

`apply` is called once, with a context carrying exactly the registries listed in
`inject`. Everything it registers is remembered against the extension's name, so
unloading is complete by construction rather than by an author remembering to
tidy up.

### The three examples are the tutorial

They live in [`examples/extensions/`](../examples/extensions/) and go from
smallest to largest:

| | Asks for | Shows |
|---|---|---|
| [`task-stats`](../examples/extensions/task-stats/) | nothing at all | The smallest possible extension: a task menu item, no permissions, no network |
| [`standup`](../examples/extensions/standup/) | three workspace tools | A page, a task menu item, a keyboard shortcut, a config field, brokered MCP calls |
| [`digest`](../examples/extensions/digest/) | the supervisor | Three surfaces, a secret, a declared host, and standing work on a schedule |

Read the first two together: `task-stats` describes the task it was handed and
says out loud that it cannot see the conversation; `standup` asks for `getTask`
at install and can therefore count one. That difference is what the permission
model is *for*, and it is the difference between the two screens a user sees.

Read them in that order. Each one is commented as an explanation rather than as
a demo.

---

## The manifest

`agentrq-extension.json`, at the root of the repository.

```json
{
  "name": "standup",
  "displayName": "Standup",
  "version": "1.0.0",
  "description": "What moved in this workspace today.",
  "license": "MIT",
  "engines": { "agentrq": ">=0.5" },
  "mcp": { "workspace": ["listTasks", "getTask"] },
  "net": ["api.example.com"],
  "config": [{ "key": "since", "type": "number", "label": "Hours to look back" }],
  "provides": { "ui": ["page"], "shortcuts": ["s"] },
  "shortcuts": [{ "key": "s", "action": "open", "title": "Standup: today" }],
  "artifact": {
    "release": "v1.0.0",
    "asset": "standup-1.0.0.tgz",
    "sha256": "…64 hex characters…"
  }
}
```

| Field | Required | Meaning |
|---|---|---|
| `name` | yes | Lowercase words joined by single hyphens. It is an **address**, not a label: it appears in routes, registry keys and the install directory |
| `version` | yes | `1.2.0`, or `1.2` |
| `license` | yes | An SPDX identifier, spelled exactly. `UNLICENSED` is accepted and means what it says |
| `engines.agentrq` | yes | `1.4.0`, `^1.4`, `~1.4` or `>=1.4`. Anything else is refused rather than guessed at |
| `artifact` | yes | The release, the asset file name, and its SHA-256 |
| `displayName` | no | Defaults to `name` |
| `description` | no | One line, shown in the catalogue |
| `mcp.workspace` | no | Workspace-server tools this extension may call |
| `mcp.supervisor` | no | Supervisor tools this extension may call |
| `net` | no | Hosts the author says it contacts — **see below** |
| `config` | no | Settings the user fills in: `string`, `number`, `boolean` or `secret` |
| `provides` | no | What it contributes, for the catalogue card |
| `shortcuts` | no | Keys it wants, under the `x` prefix |

A repository with a bad manifest is **listed as broken with its reason** rather
than dropped, because an author needs to see why and a silently missing
extension looks like a broken catalogue to whoever followed a link to it.

### Name only tools that exist

A manifest can name anything; the servers decide what is real, and
`checkCompatibility` refuses an install with *"the workspace server does not
offer listTasks"* — on somebody's machine, at install time, which is late.

The two surfaces are **not** the same, and the difference is deliberate rather
than an oversight to route around.

**The per-workspace server** — what an agent working a queue can do:

```
createTask  updateTaskStatus  reply  downloadAttachment  getWorkspace
getTask  publishEvent  loadMemory  saveMemory  deleteMemory  elicit
```

It answers in **prose, not JSON** — it is written for agents to read.
`getWorkspace` returns the name, the description and a count per status;
`getTask` with `includeConversation` returns the task followed by a
`Conversation:` block of JSON. `standup` parses both, and those few lines are
worth reading before you write your own.

There is **no task listing here on purpose**. An agent connected to a workspace
acts on the task it was given and reads what the workspace remembers; it does
not enumerate the board. `getWorkspace` answers with counts per status, which is
what a summary actually needs, and `getTask` takes an id you already have.

**The supervisor server** is account-wide and does list things — `listWorkspaces`,
`listTasks`, `listAllTasks`, the event and workflow tools, and so on. Reaching it
is reaching every workspace, which is why it sits at the top of the grant ladder.

`standup` was first written asking for `listTasks` on the workspace server and
would never have installed. Two tests now guard this from both sides: a Go test
checks every example manifest against the servers' actual registrations, and
`desktop/src/main/extensions/servers.js` — the lists the install screen judges
against — is checked against the same Go source, so a tool added on one side
fails the build on the other.

Those lists describe **the desktop app's idea of the servers**, not the one it is
connected to. A self-hosted backend older than the app may not have a tool listed
there; an extension needing it installs and is then refused at call time, by the
server, with the server's own message.

### Why the licence is required

An extension runs with full access to the machine. The terms it is offered under
are part of the decision to install it, not metadata about it. Free text was not
accepted because `MIT`, `mit` and `MIT License` would be three incomparable
licences; the list is fixed and case-sensitive, and a missing identifier is a
one-line addition to `desktop/src/main/extensions/manifest.js`.

### `net` is a description, not a restriction

Nothing enforces it. An extension is trusted Node code and can open any socket
it likes, and this field changes nothing about that.

It is there because it is still worth knowing — an extension that says it talks
to `hooks.slack.com` has told you something true about what it is for. The
install screen presents it as a claim by the author, kept out of the permission
list, because a list of hosts under a permission heading reads as a boundary
somebody is holding, and here nobody is.

---

## The capability model

There are exactly two things AgentRQ enforces, and it is worth being precise
about what they are and are not.

**They are not a sandbox.** That was considered and deliberately not built: the
value of this design is the ordinary Node ecosystem, and a sandbox costs exactly
that. The install screen says so in those words on every install, including —
especially — the ones with no permission list at all.

**What is enforced is everything AgentRQ owns:**

1. **Which registries an extension can reach**, and which names it may claim
   inside them.
2. **Which MCP tools it may call, and against which workspaces.**

That is a real boundary around your AgentRQ data. It is not a boundary around
your machine.

### The credential never reaches the extension

An extension is an MCP *client*, and it never holds a token. It asks:

```js
const answer = await ctx.mcp.workspace('listTasks', { workspaceId, limit: 50 })
if (!answer.ok) return somethingSensible(answer.reason)
```

The host attaches the workspace token or the supervisor session on the way out,
after checking the tool against the manifest and the workspace against the
grant. A refusal comes back as `{ ok: false, reason }` — **a value, not a
throw** — so a refused extension shows a sentence rather than becoming an
unhandled rejection somewhere in the host.

`workspaceId` says which workspace you mean. On the **workspace** surface that is
all it is: the per-workspace server has one endpoint per workspace, so the host
turns it into a URL and keeps it out of the arguments — those tools take no such
parameter and the server refuses one outright. On the **supervisor** surface it
stays, because there it genuinely is an argument.

**The supervisor is not reachable from the desktop app yet.** Its endpoint wants
a token whose audience is `coremcp`, and only the OAuth2 flow mints one. An
extension asking for account-wide tools installs, loads, registers its surfaces
and is then told so in as many words. Workspace tools work.

This matters because a workspace token and a supervisor session outlive any
single extension and reach every workspace on the account. An extension that is
compromised, or merely careless with what it logs, cannot leak a key it was
never given.

### The grant ladder

At install, the user picks one rung. What is offered depends on what the
manifest asked for **and on where they are installing from**:

| Rung | Offered when | Means |
|---|---|---|
| This workspace only | any `mcp.workspace` tools, *and a workspace is in context* | The workspace they were in |
| Selected workspaces | any `mcp.workspace` tools | The ones they tick |
| All workspaces | any `mcp.supervisor` tools | The account, including workspaces created later |

Extensions are installed from the sidebar's Extensions screen, which belongs to
no workspace — so "this workspace only" is not offered there. It shipped offered,
meaning nothing, and a grant made from it carried an empty workspace list: not a
narrow grant but an inert one, refusing every call with *"not granted access to
that workspace"* for a workspace the user believed they had just allowed.

An extension that asks for nothing shows **no permission list at all** — only
the confirmation and the sentence about machine access.

There is deliberately no "supervisor, but only this workspace". `listAllTasks`
spans the platform and `createTask(workspaceId)` reaches anywhere, so the
supervisor surface *is* every workspace; that combination would be the same
grant wearing a narrower label, and the ladder makes it unrepresentable rather
than merely discouraged.

---

## What an extension can contribute

Three registries, reached through `inject`.

### `ui` — pages, actions and menu items

```js
ctx.ui.add({ id, surface, label, order, view, run, when })
```

| `surface` | Where it appears | Invoked with |
|---|---|---|
| `page` | The sidebar, at `/extensions/:name/:id` | `view(context)` returns a view spec |
| `workspace-action` | The top of a workspace | `run(context)` |
| `task-menu` | A task's right-click menu | `run(task)`, filtered by `when(task)` |

`run` and `view` are the same thing under two names — an action produces a
panel, a page produces a page — and either is accepted on any surface.

**Where each one appears.** A `page` gets a row in the sidebar under Extensions
and a route of its own at `/extensions/:name/:id`. **It may be opened with no
workspace**: a sidebar page belongs to the application, and AgentRQ passes one
only when it is unambiguous — the account's single workspace when there is
exactly one, never a guess among several. Every workspace tool needs one, so
write the page to say so when `context.workspaceId` is empty rather than making
a call that comes back "This call names no workspace." `standup` does exactly
that. A `workspace-action` and a `task-menu` item always have one. A `workspace-action` becomes a
button in the workspace header, after AgentRQ's own and behind a divider, so
installing something never shuffles a control somebody's hand already knows. A
`task-menu` item appears on right-click, also behind a divider, filtered by its
`when`.

**Your functions never leave the main process.** The renderer receives a
*description* of an entry and asks this side to run it, which is what keeps
third-party code away from a privileged origin that has a bridge to files, the
clipboard and the shell. `when(task)` is evaluated there too: the renderer sends
the task and receives the rows that apply to it.

A `run` that throws becomes a refusal with your extension's name on it, shown
where the panel would have been. A `run` that returns nothing draws no panel and
reports no error — that is the shape for an action that did something other than
display.

`order` defaults to 100, and ties break on owner then id — so what the user sees
never depends on which extension happened to load first.

**Give a task-menu item a `when`.** Without one, ten installed extensions mean
ten permanent rows on every task, and a menu that long is one nobody reads. An
entry that says nothing about when it applies is assumed to always apply, which
is right for AgentRQ's own items and rare for anybody else's.

### `shortcuts` — keys under the `x` prefix

```js
ctx.shortcuts.add({ id: 'open', key: 's', label: 'Standup: today', run })
```

Pressing <kbd>x</kbd> then <kbd>s</kbd> runs it, wherever you are in the app —
so what it opens is a panel rather than a page, since a page would mean
navigating away from whatever you were looking at.

The single bare letters are AgentRQ's — `k n w m t ?` are taken and more will
be. A blocklist would be a tiny namespace that freezes the application and still
lets extensions collide with each other; one reserved prefix fixes all three, and
`g`-then-key in Gmail and GitHub means people already know the scheme.

Conflicts are reported **at install**, naming the extension already holding the
key. A conflict discovered when somebody presses a key is a key that silently
does nothing, and no amount of looking at the screen explains it.

The application's own system-wide key grab (Cmd+Shift+N, registered in the main
process so it fires whether or not the window has focus) is not available to
extensions. That is a different order of capability from a key that works while
the app is in front of you.

### `renderers` — drawing a fenced code block

```js
ctx.renderers.add({
  id: 'mermaid',
  language: 'mermaid',
  when: (context) => appliesTo(context.workspaceId),
  run: (context) => ({ nodes: [{ type: 'diagram', format: 'mermaid', source: context.source }] }),
})
```

When a task body or a message contains a ```` ```mermaid ```` fence, whoever
claimed that language is asked what to do with it, and answers with a view spec
like any other surface. AgentRQ draws the answer and puts a **Text** / **Diagram**
toggle beside it, so the source is always one click away.

**A language belongs to one extension.** Two renderers both claiming `mermaid`
would be resolved by whichever loaded first, which is no answer at all — so the
second claim is refused at install, naming the first.

**Answering nothing is a legitimate answer**, and it leaves the fence as the
code block it was. That is the right response to a diagram that is too large, or
malformed, or asking for something the renderer will not do: the text is what
somebody needs in order to fix it, and replacing a readable block with a
complaint about it helps nobody.

`when(context)` is given the workspace the message is in, so *"draw diagrams in
this workspace and not that one"* is the extension's own answer rather than a
setting AgentRQ keeps on its behalf.

### `schedules` — work that runs when the app does not

```js
ctx.schedules.add({
  id: 'daily',
  value: { kind: 'task', workspaceId, title: 'Daily digest', body, cron: '0 9 * * *' },
})
```

This is what makes desktop-only a design rather than a compromise. Your
extension is not running at three in the morning — it does not need to be. It
*declares* a cron task or an event trigger, and the host reconciles that against
what is already on the server: creating what is missing, revising what has
drifted, and removing what you stopped declaring.

Two kinds:

```js
{ kind: 'task',    workspaceId, title, body, assignee, cron }
{ kind: 'trigger', workspaceId, title, body, assignee, event, emitEvent, cron }
```

Cron granularity is **hourly at most** — the minute field must be a single fixed
number. `0 9 * * *` is fine; `*/5 * * * *` is refused.

Reconciliation means reinstalling leaves no duplicates and uninstalling leaves
nothing behind. The host makes these calls with its own credential rather than
through your grant, because otherwise every extension that wanted a nightly task
would have to ask for the ability to delete any task on the account. What bounds
it instead: it only ever touches what it created, and a schedule naming a
workspace your grant does not reach is refused.

---

## Drawing a page

Extensions **describe**; AgentRQ renders. A page is a JSON spec in a closed
vocabulary, drawn with the application's own components. There is no
third-party markup in the renderer and no iframe — which is what keeps extension
UI looking like the rest of the product instead of approximating it, and keeps a
privileged origin free of anybody else's code.

```js
{
  title: 'Standup',
  nodes: [
    { type: 'text', tone: 'muted', value: '3 tasks moved today.' },
    { type: 'group', label: 'Waiting on somebody', children: [
      { type: 'row', label: 'Ship the release', value: 'Asked on Tuesday' },
    ] },
  ],
}
```

| Node | Fields |
|---|---|
| `text` | `value`, `tone` |
| `heading` | `value` |
| `rows` | `items` |
| `row` | `label`, `value`, `href` |
| `badge` | `value`, `tone` |
| `button` | `label`, `action`, `tone` |
| `link` | `label`, `href` |
| `empty` | `value` |
| `group` | `label`, `children` |
| `diagram` | `format`, `source`, `label` |

`tone` is one of `default`, `muted`, `positive`, `warning`, `critical`.

Limits: 200 nodes, 5 levels deep, 2000 characters per string. Longer strings are
truncated; the other two are refused.

**A `diagram` carries source, not markup.** `format` is `mermaid`; `source` is
the diagram's text, which AgentRQ draws. It is the one node whose content is not
clamped — half a diagram is a syntax error rather than a shorter diagram — so a
source longer than 20,000 characters is refused instead. There is deliberately
no node that carries HTML or SVG: an extension that could hand back markup would
be putting it on a privileged origin, which is what this whole vocabulary
exists to prevent.

**A node type nobody recognises is rejected, not skipped.** Skipping would draw a
page quietly missing whatever you thought you had written — worse for you than
being told, and worse for the user than seeing nothing.

Everything you send is treated as untrusted text. Not because extensions are
assumed hostile — they run as trusted code and could do far worse than inject
markup — but because text arriving from an extension often did not originate
there: an issue title, a commit message, a webhook payload. Only `http` and
`https` hrefs stay clickable, for the same reason a `file:` URL in a message body
is not followable.

---

## What the task you are handed actually contains

A `task-menu` entry is called with the task from the board, and **that task is a
summary**. The list endpoint sends the last message per task and nothing else, so
`task.messages` has one element however long the thread is. Counting it reports
one, always — which is what `task-stats` did before it was fixed.

The rule: **describe what you were handed, not what you assume is behind it.** If
you need the thread, ask for `getTask` in your manifest and call it with
`includeConversation: true`, then read `total` from the answer rather than the
length of `messages`, which is a page of at most five by default. `standup` does
exactly this, and the two examples side by side are the clearest statement of
what a permission buys.

That window also **starts at the oldest message**, so a page at `cursor: 0` is
the *first* of the thread and not the last. `standup` reads `total` from one call
and then asks for `cursor: total - 1` when there is more than one message —
which is two round trips, and the alternative is quietly showing the wrong
message under a heading that says otherwise.

## Configuration and secrets

```json
"config": [
  { "key": "since", "type": "number", "label": "Hours to look back" },
  { "key": "apiKey", "type": "secret", "label": "API key" }
]
```

Values arrive as `ctx.config` and as the second argument to `apply`.

A `secret` is stored encrypted through the OS keychain, never shown back to the
user or to the catalogue, and — importantly — **refused rather than written in
the clear** if the machine has no secure storage available. Storing it anyway
would be the worst of both worlds: the user believes it is protected, and it is
sitting in a JSON file.

Your extension receives the decrypted value. Do the only correct thing with it:
send it, keep no copy, and never log it — not the value, not a prefix of it, and
not a URL it is embedded in. `digest` shows the shape, including the failure
paths that deliberately say the status code and nothing else.

---

## Publishing one

1. Add the **`agentrq-extension`** topic to the GitHub repository. That is the
   whole of discovery — the desktop app searches for it.
2. Put `agentrq-extension.json` at the root.
3. Cut a release with the built asset attached, and put its SHA-256 in
   `artifact.sha256`.

The digest is what makes an install honest: a git tag can be moved under one that
is already in place, and a hash cannot.

### Installing without publishing

**Extensions → Install from folder**, then pick the folder holding
`agentrq-extension.json`. The folder is **linked**, not copied: editing it edits
the installed extension, which is the only way developing one is tolerable. The
row says `linked folder` because it can change underneath the app, unlike
anything else that gets installed.

If the extension asks for MCP tools you are asked what it may reach before
anything is written, and any `config` fields are on the same screen. An extension
that asks for nothing installs without that step — a permission screen with
nothing on it is how people learn to click past the one that matters.

Try it with the examples:

```
Extensions → Install from folder → examples/extensions/task-stats
```

The installer also supports **a release**, verified against the digest, and **a
git URL** cloned at a commit for a private repository — those are how a published
extension arrives.

### Managing what is installed

Each installed row says what it actually contributed (`1 view, 1 shortcut`) or
why it did not. **Disabled** and **not running** are different states and the row
distinguishes them: an enabled extension that threw on load is not running, and
the row says how many times it failed.

**Disable** stops it and takes its standing work down with it — a disabled
extension must not still be creating tasks at three in the morning.
**Uninstall** does that and then forgets its settings, its grant and its files. A
linked folder is unlinked, never deleted; it is your working copy.

---

## Testing yours

The examples' tests are in `desktop/test/examples/` and are worth copying as a
pattern:

- **Pure functions for anything that shapes output.** `statsFor(task)`,
  `buildPage(tasks)` and `summarise(workspaces, tasks)` take data and return a
  description, so they test without a server anywhere near them.
- **Run every view you can produce through `normaliseView`.** The renderer
  refuses a node type it does not know, and a page it will not draw is otherwise
  a bug nobody finds until somebody installs your extension.
- **Test the refusals.** `{ ok: false, reason }` is a normal answer, not an edge
  case: it is what a user who declined a permission, or narrowed a grant later,
  will actually see.

---

## Where things live

| | |
|---|---|
| Manifest parsing and compatibility | `desktop/src/main/extensions/manifest.js` |
| Discovery through the GitHub topic | `desktop/src/main/extensions/discovery.js` |
| Installing, updating, uninstalling | `desktop/src/main/extensions/install.js` |
| Loading, and unloading completely | `desktop/src/main/extensions/host.js` |
| The registries | `desktop/src/main/extensions/registry.js` |
| The MCP boundary | `desktop/src/main/extensions/broker.js` |
| Config and secrets | `desktop/src/main/extensions/config.js` |
| Schedule reconciliation | `desktop/src/main/extensions/schedules.js` |
| The grant screen's rules | `frontend/src/composables/useExtensionGrant.js` |
| The view vocabulary | `frontend/src/composables/useExtensionView.js` |
