# Example extensions

Three working extensions, smallest to largest. They exist to be read — every
file is commented as an explanation rather than as a demo — and they are what
[`docs/EXTENSIONS.md`](../../docs/EXTENSIONS.md) teaches from.

They were also written **before the catalogue opened**, on purpose. The manifest,
the capability model and the grant screen freeze the moment somebody publishes
against them, and building real extensions is the only way to find out they are
wrong while changing them is still free. Two things did turn out wrong, and both
were fixed in the design rather than worked around here:

- The host handed extensions no way to reach MCP at all — `ctx.mcp` did not
  exist, because every piece had been built and tested separately and nothing
  had yet needed the seam between them.
- Reconciling a schedule through the extension's own grant would have meant
  every extension wanting a nightly task had to ask for the ability to delete
  any task on the account. The host does it with its own credential now, bounded
  by what it created and by the workspace the grant reaches.
- A task on the board is a Vue reactive proxy, and `contextBridge` refuses a
  Proxy — so an extension's menu row was registered correctly and never
  appeared, with the rejection swallowed into an empty list. Fixed at the
  boundary, and `npm run verify:extensions` now drives it in a real window.

Two more turned up once these were actually used:

- `standup` asked for `listTasks` on the workspace server, which has never had
  one and never will. A Go test now checks every example manifest against the
  servers' real registrations.
- `task-stats` counted messages and always said one, because the board's task
  list carries only the last message per task. The count is gone rather than
  patched: the real number needs a permission that example deliberately does not
  ask for, and `standup` — which does ask — counts it properly. The pair is now
  the clearest statement in the repo of what a permission buys.

## The three

| | Asks for | What it shows |
|---|---|---|
| [`task-stats`](task-stats/) | **nothing** | The smallest extension there is: one task menu item, no permissions, no network, no server calls. Its install screen shows no permission list at all — which is the path easiest to get wrong, because an absent list reads as safety |
| [`standup`](standup/) | `getWorkspace`, `loadMemory`, `getTask` | A sidebar page, a task menu item, <kbd>x</kbd> <kbd>s</kbd> to open it, one config field, and brokered workspace calls that answer with a refusal rather than throwing |
| [`digest`](digest/) | the **supervisor** | The full case: a page, a header action, a conditional task menu item, a secret in the keychain, a declared host, and standing work reconciled onto the server so it runs while the app is closed |

## Running them

They are not published to their own repositories, so the `artifact.sha256` in
each manifest is **all zeros** — a placeholder, and obviously one. Install them
as linked local folders instead, which is how you would develop your own:

```
Extensions → Install from folder → examples/extensions/standup
```

Linked means recorded rather than copied, so editing the folder edits the
installed extension.

`task-stats` and `standup` both work. Right-click any task for `task-stats`;
`standup` adds a **Standup** row to the sidebar, a <kbd>x</kbd> <kbd>s</kbd>
shortcut, and its own task menu item.

The sidebar row is the one to read closely. A page has no workspace in its
route, and AgentRQ will not guess one among several — so with more than one
workspace on the account it says *"Open a workspace to see where it stands."*
rather than making a call that would come back refused. The shortcut and the
task menu item always have a workspace, and show the real thing. `digest` needs the **supervisor**, which reaches every workspace on the account.
The first time it is installed, the Extensions screen offers an **Authorise**
button and a window opens on your own server's sign-in page; after that its
pages and its schedule work. Until then its calls come back refused with a
sentence saying exactly that, which is the failure it was written to handle.

## Their tests

In [`desktop/test/examples/`](../../desktop/test/examples/), because that is
where the runner lives. One file per example, plus `integration.test.js`, which
drives all three through the real installer, broker, host and reconciler with
only the two MCP servers stubbed — the pieces were built in separate tasks
against separate tests, and every seam between them is a place where two correct
halves disagree.
