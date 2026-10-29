# The Pano

- **The tool takes `action` and `payload`** (see
  [mcp-and-tasks.md](mcp-and-tasks.md)): each action's payload is its own
  struct in `pano.go`, which is also what `help` prints.
- **A file is stored only if the markdown links it.** `save` refuses an
  unlinked file and drops one the markdown stops linking; `attach` is the only
  way in without a link. Relaxing this leaves blobs nothing points to.
- **Every write moves `version`, and an update must send the one it read.**
  The check is a compare-and-swap in `SavePanoDoc`. A task's status change is
  not a write: `SyncPanoTask` stores only `checklist_done`. `edit` reads, applies its ops
  and goes through the same save, so it cannot skip the checks; the version is
  checked before the ops, or a stale doc answers "not found" instead.
- **The markdown is in the database, the files in storage.** A file's blob is
  written before its row and purged if the row fails, as with skills; the
  other order leaves a row whose file cannot be read. Lists `Omit("markdown")`.
- **The checklist count is stored at save** (`panoChecklist`), and the board's
  `togglePanoCheckbox` must find the same items: both skip fenced code.
- **An assigned item ends `→ [@name](/workspaces/<id>/tasks/<id> "status")`**;
  `assignPanoItem` and `panoAssignee` must agree on it. Its status and tick are
  the task's, laid over the markdown on read, list, edit and save
  (`panoOverlay`), so the stored text can be stale, and its box takes a click only where one sticks: a manual item, or a completed task's. The
  count sync uses `UpdateColumn`: stamped as a save, it would reorder the board.
  An item whose task is not among the doc's (deleted, or never the account's)
  loses its assignee there; a task delete recounts its docs, which it unlinks.
- **A manual untick is `pano_doc_tasks.unticked`, never the markdown.** A save
  sets it only when the item's link already said completed (its writer saw
  it), and `SyncPanoTask` clears it on any other status; reading it from the
  stored box would hold a stale untick through a reopen. `"<status> manual"`
  keeps the writer's box; the board's `ASSIGNEE` must accept the same title.
- **A doc's tasks are rows in `pano_doc_tasks`, never markdown**: from an
  assigned item, a `#`-tagged chat (`panoDocId`), or an agent's write while it
  has exactly one ongoing task, since a guess between two would mislink. The
  write's workspace is the agent's own, not the fork's parent. Every query on
  it is checked for full scans by `pano_doc_tasks_plan_test.go`.
- **A fork merge moves the fork's links, and rewrites its items** to name and
  link the parent (`movePanoAssignees`): the overlay matches an item by its
  path, so one left on the deleted fork loses its assignee.
- **Pings are spaced a second apart**: the server makes one task a second per
  account, so a comment mentioning two workspaces at once would 429 the second.
- **`@workspace #task` in a comment posts on that task, and makes none**; a
  plain `@workspace` makes one. The server reads the task within the URL's
  workspace, so naming another workspace's task is refused, not misdelivered.
- **There is no agent search, on purpose**: agents pass doc ids. Only the board
  filters, in the browser.
- **Workspace 0 is the person.** Authors and commenters are a workspace id (the
  parent, for a fork) or 0; the board puts 0's docs first, and only they get the
  to-task button.
- **Expired docs are hidden by every query**, not only swept: the hourly sweep
  (plus up to 2 minutes of jitter) can be that late.
- **Color is a uint8 in the database and a name everywhere else.** Append new
  colors; never reorder them.
- **The workspace tool's description ends with the agentrq-ws hint; CoreMCP's
  does not**, because the CLI reads a workspace's `.mcp.json`.
- **The doc page must not reload on its own delete**: the `pano.changed` it
  causes would GET a deleted doc and 404.
- **The chat box's task options are the New Task form's**, one
  `useTaskOptions` and one `TaskOptionsTools` row for both: a control added to
  the form reaches the Pano without a second copy. The row takes no window
  breakpoint, only the nearest `@container`.
- **A task opened from a doc goes beside it on a large screen**, on
  `/pano/:id/chat/:workspaceId/:taskId`, whose `remountKey` is the doc's: keying
  by path would rebuild the doc, and its scroll, on every task opened. The
  doc's param is `docId`, never `id`: the task view reads `id` as a workspace
  and would GET the doc's id as one.
