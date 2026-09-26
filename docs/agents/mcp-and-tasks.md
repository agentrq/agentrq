# The MCP server, and tasks

> The workspace MCP server (`backend/internal/controller/mcp/`) and the task rules it shares with the REST API. Read before changing a tool, cron validation, or task status.

- `server.go` — all tool handlers (`handleCreateTask`, `handleReply`, etc.) and the `WorkspaceServer` struct
- Cron validation: `validateCronGranularity` enforces hourly-minimum granularity. Minute field must be a single fixed integer (0-59); wildcards/steps/ranges/comma-lists are rejected.
- A schedule may open with `CRON_TZ=<zone> `; **without one it means UTC**. Read it with
  `schedule.Parse` and `schedule.Fields` — robfig alone uses the host's zone, and panics on a bare prefix.
- Creating a task with `cron_schedule` sets `status="cron"` on the model.

## A tool on the server is a tool in four other places

The `mcp.AddTool(mcpSrv, …)` block in `server.go` is the source of truth for
what a workspace offers — 15 tools as of 2026-09-23. Every other list of them
in this repository is a copy, and the copies are what go stale.

**`cli/agentrq-ws` is the one that is easiest to forget**, because it is a
separate npm package and nothing in the backend mentions it. It is a
command-line client over exactly these tools — 18 commands for the 15 — so
**adding, renaming or removing a tool on the server is a change to the CLI
too**: `COMMANDS` in `cli/agentrq-ws/src/commands.js`, the command table in its
`README.md`, and the sentence there that spells the tool count out in words. A
renamed tool is worse than a missing one, because the verb stays in the help
output and fails at the server instead of at the parser.

Skipping it is survivable and that is the trap: the `call` escape hatch means a
tool added today is *reachable* today (`agentrq-ws call <tool> --args '{…}'`).
But reachable is not covered — `call` asks the person to write the JSON
envelope the CLI exists to spare them.

**The test there is not the safety net it looks like.**
`cli/agentrq-ws/test/commands.test.js` has a case called "the CLI covers every
tool the workspace server offers", and its list of tool names is a literal *in
the test file*; it never reads the Go source. Add a tool to the server and it
stays green. It catches only the reverse — a CLI verb that stops calling a tool
it used to.

The one test that really compares the two is `plugin_docs_test.go`: it fails
when `plugins/claude/agentrq-workspace/README.md` or its `SKILL.md` lacks a row
for a tool. The setup tab's `.claude/settings.local.json` snippet needs nothing —
it allows the server with one `mcp__<server>__*` wildcard.

The remaining copies are documentation and fail silently: `README.md` (the
"Available MCP Tools" list), `README.zh-CN.md` (the
same list, translated), and `plugins/deepseek-harness/README.md`, whose table
and hard-coded count are **stale at seven today** — worth knowing because the
plugin filters nothing, so every server tool reaches the model whatever that
table says.

## This server is stateful, so it cannot serve 2026-07-28

Never set `StreamableHTTPOptions.Stateless = true` here to "add" protocol
revision 2026-07-28: stateless 405s the GET/SSE stream, and that stream is how
`SendChannelNotification` and `SendPermissionVerdict` reach a connected agent.
We advertise `2025-11-25` downwards and refuse the newer revision with a
JSON-RPC `-32022` naming those versions, so clients renegotiate down.

coremcp is stateless and does serve 2026-07-28 — it pushes nothing, so it has
no stream to lose. The two servers differ on purpose; see
[coremcp-and-supervisor.md](coremcp-and-supervisor.md).

## Annotations, and why `Name:` stays first

Every tool on both servers declares annotations, built by
`internal/service/mcphint` so the hints cannot disagree tool to tool. A client
decides from them whether to run something without asking, so `readOnlyHint` on
a tool that writes is worse than no hint at all.

**Keep `Name:` the first field of every `&mcp.Tool{…}` literal.** Three tests
regex the Go source for it — the frontend allow-list parity test and two plugin
doc checks — and all three stop seeing a tool if anything precedes its name.

## The server instructions must stay under 2048 characters

Claude Code keeps only the first 2048 characters of a server's instructions. The
rest is cut without warning, and the skills rule used to fall past that point.
The instruction test enforces the cap.

## Workspace memory

`loadMemory` / `saveMemory` (`memory.go`) give agents notes that outlive a task.

- Stored in `memories`, keyed per **(workspace owner, workspace, name)** — it is
  the *workspace's* memory, so every agent working there shares it. Keying it to
  the connecting client would make it per-agent memory wearing a workspace's
  name.
- `memory.md` is the default for both tools and is meant to stay an index of the
  other named memories, linking them as `memory://<name>`. The server
  instructions tell connecting agents to load it first.
- **Names are canonicalised at the tool boundary**: trimmed, lowercased, and
  then required to match `^[a-z0-9]+(-[a-z0-9]+)*\.md$`. So `MEMORY.md`,
  `Memory.md` and `memory.md` are one memory rather than three. Folding here
  rather than in a query is deliberate — `=` is case-sensitive by default on
  both SQLite and Postgres and a `NOCASE` collation does not port between them,
  so one stored spelling is what makes the unique key behave the same on either.
  The strict shape is also what lets `memory://<name>` be parsed as a URL at
  all: a name with a space in it is not one.
- **Limits are enforced at the tool boundary, not in the repository**: 16 KiB of
  UTF-8 per memory and 32 characters per name, both refused rather than
  truncated — an agent told its memory is too large can split it, while one
  silently cut in half cannot know to.
- A `loadMemory` miss is **not** an error: every agent's first call on a fresh
  workspace misses, and answering with an error teaches agents to stop asking.

## CRUD task controller (`backend/internal/controller/crud/task.go`)

- Cron validation also lives here for the REST API path (same rules).
- `isValidTaskStatus` — valid statuses: `notstarted`, `ongoing`, `completed`, `rejected`, `cron`, `blocked`.

## `StartPoller` is not the only thing that pushes a task's first message

`createTask` and `updateTaskAssignee` in `handler/api/task.go` push a new or
reassigned task immediately when nothing else is running, rather than waiting
for `StartPoller`'s next tick. Both must call `ClearContextForTask` before
`SendChannelNotification`, exactly like the poller does — clear after, and the
clear wipes the task text the agent was just handed.

**A pending task must never be able to hide the ones behind it.** The handler
holds a push back only when the agent is already running its full
`maxConcurrency` — never because another task is merely queued — and the poller
moves on to a different pending task each tick instead of always offering the
oldest. With both rules reversed, one task the agent ignored blocked every task
created after it, permanently and silently: the handler skipped them and the
poller re-offered the stuck one forever, which acp-gateway then dropped as a
repeat.

**Ask the database the question you actually have.** Both push paths used to
list the workspace unfiltered to look at one or two statuses, which fetches a
hundred whole tasks — bodies, responses, a JSON unmarshal of every attachment —
and, worse, takes the hundred *most recent*, so an older ongoing or pending
task falls outside the window and is simply not seen.

**The push repeats and the clear does not.** A pending task is offered again on
every tick until the agent moves it to `ongoing`, because a push is the only
thing that starts an idle agent (a gateway asks for work itself only when a
task finishes or its concurrency limit moves) and a push made while nothing was
attached reaches nobody, silently. Recording the push instead — which is what
`MarkTaskPushed` used to do — turned a task created at that moment into one
that never arrived at all. The `/clear` is the half that must not repeat, and
`clearContextFor` remembers it per task, only once it has actually gone out.

