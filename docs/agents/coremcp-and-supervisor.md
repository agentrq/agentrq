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
`.go` file in this package, so a mismatch fails `npm test`. The two Claude
plugin docs (`plugins/claude/agentrq/README.md` and its `SKILL.md`) and the
Gemini extension's `plugins/gemini/README.md` need a table row too —
`plugin_docs_test.go` checks those. The setup tab's permissions
snippet allows this server as `mcp__agentrq__*`, so it needs nothing.

**Adding a *resource* or *prompt* trips none of the above.** Those parity
tests match only `mcp.AddTool(s.server, &mcp.Tool{Name: "..."` — an
`AddResource`/`AddPrompt` call is invisible to them. Cover a new one with its
own test (list it, read/get it) the way `resources_test.go`/`prompts_test.go`
do; nothing else needs to be told it exists.

**A new tool, resource or prompt here needs a telemetry line too** — see
[telemetry.md](telemetry.md)'s MCP section; `emitTelemetry` is on
`WorkspaceServer` in `server.go`, first line of the handler, same as the
per-workspace server.

**This server is stateless** (`StreamableHTTPOptions.Stateless = true`), which
is how it serves protocol revision 2026-07-28 — the SDK offers that revision
only on a stateless transport. Older revisions are served exactly as before;
what changes is that GET and DELETE now answer 405, which costs a server that
has never pushed a notification nothing. The **opposite** rule holds for the
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
