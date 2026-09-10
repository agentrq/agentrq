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

## The three

| | Asks for | What it shows |
|---|---|---|
| [`task-stats`](task-stats/) | **nothing** | The smallest extension there is: one task menu item, no permissions, no network, no server calls. Its install screen shows no permission list at all — which is the path easiest to get wrong, because an absent list reads as safety |
| [`standup`](standup/) | `listTasks`, `getTask` | A sidebar page, <kbd>x</kbd> <kbd>s</kbd> to open it, one config field, and brokered workspace calls that answer with a refusal rather than throwing |
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

`task-stats` works fully — right-click any task and it is there under a divider,
and clicking it opens a panel. It asks for nothing and reads only the task it is
handed. `standup` and `digest` install, load and register their surfaces, but
their MCP calls come back refused: the desktop app has no MCP transport of its
own yet, so `ctx.mcp` answers with a reason rather than a result. Their pages
render that reason, which is at least the failure they were written to handle.

## Their tests

In [`desktop/test/examples/`](../../desktop/test/examples/), because that is
where the runner lives. One file per example, plus `integration.test.js`, which
drives all three through the real installer, broker, host and reconciler with
only the two MCP servers stubbed — the pieces were built in separate tasks
against separate tests, and every seam between them is a place where two correct
halves disagree.
