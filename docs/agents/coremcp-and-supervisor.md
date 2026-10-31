# CoreMCP, and the supervisor workspace

> `backend/internal/handler/coremcp/` — the account-wide MCP server, distinct
> from the per-workspace one in `backend/internal/controller/mcp/` (that one
> has its own note, [mcp-and-tasks.md](mcp-and-tasks.md)).

Every account gets a "supervisor" workspace auto-created on signup
(`internal/controller/crud/user.go`, `FindOrCreateUser`). Its `.mcp.json`
carries a *second* server entry, named `agentrq`, pointing at coremcp
(`/mcp`, no workspace id) alongside its own normal per-workspace one — wired in
`frontend/src/composables/useWorkspaceSettings.js` (`buildSupervisorMcpUrl`,
`buildMcpServers`). That is the only wiring a coremcp addition needs; nothing
per-workspace has to change for it to be reachable.

**Adding or renaming a *tool*** here means updating `SUPERVISOR_TOOLS`
(`desktop/src/main/extensions/servers.js`), which
`desktop/test/extensions/servers.test.js` regex-compares against every non-test
`.go` file in this package, so a mismatch fails `npm test`. The Claude and
Cursor plugin docs (`plugins/{claude,cursor}/agentrq/README.md` and its `SKILL.md`) and the
Gemini extension's `plugins/gemini/README.md` need a table row too —
`plugin_docs_test.go` checks those. So does `setupTools` (`setup.go`), the
list a browser opening `/mcp` sees; `TestSetupPage_ListsEveryTool` checks it.
The setup tab's permissions snippet allows this server as `mcp__agentrq__*`,
so it needs nothing. A new *action* on a tool goes in `SUPERVISOR_ACTIONS`
beside it, and in `newerActions` (`action_tools_test.go`) with its telemetry name.

**Adding a *resource* or *prompt* trips none of the above.** Those parity
tests match only `mcp.AddTool(s.server, &mcp.Tool{Name: "..."` — an
`AddResource`/`AddPrompt` call is invisible to them. Cover a new one with its
own test (list it, read/get it) the way `resources_test.go`/`prompts_test.go`
do; nothing else needs to be told it exists.

**The `agentrq` skill this server serves (`skills/`, MCP Skills extension) is
a copy of the plugins' SKILL.md**: edit the plugin one and copy it over, or
`plugin_docs_test.go` fails. Setting `Capabilities` drops the SDK's default
`logging`, so `skillsCapabilities` keeps it.

**A new tool, resource or prompt here needs a telemetry line too** — see
[telemetry.md](telemetry.md)'s MCP section; `emitTelemetry` is on
`WorkspaceServer` in `server.go`, first line of the handler, same as the
per-workspace server.

**This server is stateless** (`StreamableHTTPOptions.Stateless = true`), which
is how it serves protocol revision 2026-07-28 — the SDK offers that revision
only on a stateless transport. Older revisions are served exactly as before;
what changes is that GET and DELETE now answer 405. It pushes only on a
`subscriptions/listen`, on that POST's own stream. The **opposite** rule holds for the
per-workspace server, which must stay stateful — see
[mcp-and-tasks.md](mcp-and-tasks.md). `protocol_version_test.go` in both
packages fails if either is flipped.

**`launchAgent` and `stopAgent` run `agentlaunch.Launcher`, the launch the web
form runs**, wired in `app.go` like the fork merge. Never give them a launch of
their own: the gates (fork capability, a second agent, a missing folder) would
then hold on one path and not the other.

**Don't put install/enrol prose in a resource.** The agentrqd install steps
already live in three places kept in sync by hand — see
[machines-and-daemon.md](machines-and-daemon.md), "Installation is answered in
three places, and they must agree". A coremcp resource that needs them should
template the live values it actually has (this server's own `baseURL`, for an
enrol command) and point at the docs URL for the rest, rather than adding a
fourth copy of the same steps.

**An open page hears of a change made here only through its CRUD event**, which
`app.go`'s forwarder turns into a stream event (`stream_events.go`). A REST
handler that calls `bus.Publish` itself leaves the same change made by the
supervisor invisible until a reload — which is how forks and task deletes were.

**`awaitingReply` is one rule written twice**: `replyState` in Go for the
fields, `awaitingReplyClause` in SQL for the filter, so paging holds. Change
both, and the shared cases in both tests, or list and filter disagree.

**A task listen hears only what the app's CRUD forwarder tells it**
(`notifyTaskListeners` → `TaskListeners`), so a change that publishes no CRUD
event never reaches the supervisor, and the in-process pubsub means a listen
on one instance misses another instance's changes. A listen is a POST that
stays open: `App.Shutdown` closes the listeners before the drain, or every
listen holds it until the deadline. **Authorization is decided once, when the
listen opens**: `subscriptionsMiddleware` keeps only URIs of workspaces the
caller owns, and the listen ends when its token expires. The SDK fans a notice
out by URI alone, so never let a URI past it unchecked.

**An agent's state is read from its terminal, not its MCP connection.** A
usage-limit pause keeps the connection open and calls nothing, so only the
screen agentrqd keeps shows it (`stream/activity.go`, sent with each
heartbeat). `agentConnected` is filled per handler here, as in REST; a
handler that skips it reads false for an agent at work.
