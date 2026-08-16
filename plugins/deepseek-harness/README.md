# @agentrq/dsh-plugin-agentrq

AgentRQ task manager for [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness).

Create, manage, and automatically receive [AgentRQ](https://agentrq.com) tasks without leaving the harness. The bundle ships two rows: the workspace's tools bridged to the model, and the harness-side behavior a tool bridge cannot provide on its own — a supervised workspace session that opens a dedicated dsh session for each task AgentRQ pushes, and the AgentRQ working agreement as a system-prompt section.

## Install

**Requires pnpm.** `dsh plugin` is a thin forwarder to `pnpm` for every profile — installing any plugin, this one included, fails with `pnpm not found on PATH` unless pnpm is already installed (`npm install -g pnpm`, `corepack enable pnpm`, or `brew install pnpm`). This is a DeepSeek Harness CLI requirement, not something this plugin can opt out of.

**One profile per workspace.** A profile serves one AgentRQ workspace and carries its own endpoint, so name it after the workspace rather than using `default` — that is what makes [several workspaces](#multiple-workspaces) work. Your workspace's **Settings → Setup → DeepSeek Harness** page prints every command and config block below already filled in.

```sh
npx @deepseek-ai/dsh plugin --profile agentrq-<workspace> add @agentrq/dsh-plugin-agentrq
```

Then pin this workspace's endpoint in the profile's own patch layer, `~/.dsh/profiles/agentrq-<workspace>/cordis.patch.yml`. Copy the URL from the Settings page — it already carries the `?token=` credential that authenticates a headless client:

```yaml
- id: agentrq
  name: '@agentrq/dsh-plugin-agentrq'
  config:
    url: "https://<workspace>.mcp.agentrq.com/mcp?token=<token>"
```

One row, one URL: the plugin mounts `@deepseek-ai/dsh-mcp-client` itself as a child fiber, so the endpoint is configured in exactly one place and the bridge shares this row's lifetime — disposal and HMR take it along.

```sh
npx @deepseek-ai/dsh --profile agentrq-<workspace> --dump-config   # shows the bundle layer and your override
npx @deepseek-ai/dsh --profile agentrq-<workspace>
```

The profile's patch is applied after every bundle layer, so those two rows win. dsh watches both `cordis.patch.yml` layers and reapplies valid edits transactionally, so changing the URL takes effect without a restart.

### Using dsh web (browser UI)

`dsh web` is an alias for `--profile web` — a fixed, dsh-shipped profile that is a different profile from any `agentrq-<workspace>` profile made above, and the two do not share bundles. Installing into one does not make the plugin visible in the other. To use this plugin from the harness's browser UI, target `web` directly:

```sh
npx @deepseek-ai/dsh plugin --profile web add @agentrq/dsh-plugin-agentrq
```

Then either pin the endpoint in `~/.dsh/profiles/web/cordis.patch.yml` (same block as above) or export `AGENTRQ_WORKSPACE_MCP_URL` before starting:

```sh
export AGENTRQ_WORKSPACE_MCP_URL='https://<workspace>.mcp.agentrq.com/mcp?token=<token>'
npx @deepseek-ai/dsh web
```

Because [a single profile cannot serve two workspaces](#multiple-workspaces), only one AgentRQ workspace can be wired into the browser UI this way — use the `--profile agentrq-<workspace>` flow above for several at once.

### Configuring without a file edit

The bundle's own patch defaults both rows to `!!js process.env.AGENTRQ_WORKSPACE_MCP_URL`, so a container or CI job can export the endpoint instead of writing a profile patch:

```sh
export AGENTRQ_WORKSPACE_MCP_URL='https://<workspace>.mcp.agentrq.com/mcp?token=<token>'
npx @deepseek-ai/dsh --profile agentrq-<workspace>
```

Prefer the profile patch for an interactive install: an environment variable is process-global, so with one profile per workspace you have to remember the right `export` before each start, and the wrong one connects the wrong workspace without complaint. Supply neither and the row fails to load with the `url` field named — it is a required field, not a silent default.

Installing from a git checkout instead of the registry fetches sources rather than built artifacts, so pnpm must be allowed to run this package's `prepare` build. Add the allowance to your profile's `pnpm-workspace.yaml` and re-run the `add`:

```yaml
allowBuilds:
  '@agentrq/dsh-plugin-agentrq': true
```

That allowance is permission to execute this package's code on your machine at install time. Pin a commit (`github:agentrq/agentrq#<sha>`) if you take that route. Publishing to npm or shipping a `pnpm pack` tarball avoids the allowance entirely.

## What the model gets

Seven AgentRQ tools, bridged by `@deepseek-ai/dsh-mcp-client` under the `agentrq` namespace:

| Tool | Purpose |
|---|---|
| `mcp__agentrq__getTask` | Fetch a task, or dequeue the next one assigned to this agent |
| `mcp__agentrq__createTask` | Assign work to the human or to another agent |
| `mcp__agentrq__updateTaskStatus` | Move a task to `ongoing`, `completed`, `blocked`, … |
| `mcp__agentrq__reply` | Send a message into a task thread — the only thing the remote human sees |
| `mcp__agentrq__getWorkspace` | Read the workspace title and mission |
| `mcp__agentrq__downloadAttachment` | Fetch an attachment's content |
| `mcp__agentrq__publishEvent` | Fire a named event so subscriber workspaces spawn their trigger tasks |

Plus one tool this package owns:

| Tool | Purpose |
|---|---|
| `agentrq_autopull` | `status`, `pause`, `resume`, or `pull_now` for this session's AgentRQ delivery |

`pull_now` returns the dequeued task as its own tool result rather than queuing a turn, because a tool body runs mid-turn by definition.

## How work arrives

**The plugin does not poll.** AgentRQ already decides when there is work and pushes it over `notifications/claude/channel`:

- creating a task assigned to the agent pushes it immediately (`backend/internal/handler/api/task.go`), provided nothing else is ongoing;
- `WorkspaceServer.StartPoller` re-pushes the next unclaimed task — or a status check for the ongoing one — every 60 seconds.

The plugin's own workspace session subscribes to that channel, exactly as [`acp-gateway`](https://github.com/agentrq/agentrq-acp-gateway) does for Gemini and other ACP agents. Polling the queue from the client would duplicate the server's own ticker and deliver every task twice.

Each push is forwarded **as written**. A new task, the periodic reminder, a status check, and a human's reply all arrive on the same channel; the plugin adds a framing line naming the `chat_id` and the tools to answer with, then hands over the content. It does not try to classify what kind of push it is, because that would only add a way to be wrong. The content is JSON-escaped into the framing, so pushed content cannot forge a framing field.

**Each task gets its own dedicated dsh session.** The plugin's own workspace connection is not tied to any chat you open — the first push for a given task (its `chat_id`) opens a fresh, dedicated agent via `ctx.agents.create()` and hands it the framed push; every later push for that same task (the reminder, a status check, a human's reply) is routed into that same session, not into whatever chat you happen to have open. This is what keeps one task's history from bleeding into another's. Delivery route within a task's session depends on its agent's state: `inject()` while a turn is running, so it lands at the next step boundary, and `followup()` while idle, since nothing else would wake it. Neither interrupts a turn in flight.

A session created with no provider/model fails every turn outright (prompt assembly has no value for `{{model}}`), and one created with the wrong `cwd` leaves the agent unable to find its own codebase, groups as "Ungrouped" instead of under your project in a capable UI, and can fall back to naming the session after that wrong directory (prompt assembly also has no value for `{{cwd}}` with none set at all). So every dedicated session gets an explicit `cwd` and `agentOptions`: `cwd`/`provider`/`model` config when set, otherwise both come from the *reference agent* — whichever live agent in the same process most recently started a turn (tracked from `agent/status` since the manager started), falling back to the longest-lived agent still around when nothing has been active yet. **`cwd` deliberately does not fall back to `process.cwd()`** except as an absolute last resort (no reference agent at all): under `dsh web`, the harness process's own cwd is the dsh *profile* directory (e.g. `~/.dsh/profiles/web`), not the project a human actually works in, and defaulting to it left a real dedicated session stuck for dozens of steps trying to locate a codebase that was never there. There is no way to ask the harness for its own configured default model/cwd from a plugin, only to read them off an agent that already has them, so the very first task pushed before this process has ever seen any activity still fails/misbehaves unless `model` (and, if the process cwd is wrong for this deployment, `cwd`) is configured explicitly.

`SendChannelNotification` puts the task id in `meta.chat_id` on every push, so the id never has to be recovered from the content, and it doubles as the key the manager uses to find a task's session again.

**A newly opened session is titled and grouped, not left for a UI to guess at.** A session's display name is a durable `session/title` log event (`@deepseek-ai/dsh-session-title`) — never inferred from a message's content, and even that package's own deterministic fallback only considers genuinely human-sourced messages, which this plugin's pushes never are. So the manager calls `sessionTitle.rename(session, title)` directly: the task's own title when the push already carries it (the startup catch-up path), otherwise fetched via `getTask` — one extra round trip, only the first time a task's session opens, skipped silently (with a warning) if the lookup fails or the task has no title. Grouping works the same way: a session is never folded into a workspace just because its `cwd` happens to match one (`@deepseek-ai/dsh-workspace`'s own docs: "later cwd-only sessions remain Ungrouped") — the manager resolves or creates the workspace owning the session's `cwd` and calls `Workspace.attachSession` explicitly. Both `sessionTitle` and `workspaceRegistry` are optional host services (`ctx.get(name)`, never a direct `ctx.sessionTitle`/`ctx.workspaceRegistry` read — that assumes an `inject`-declared, guaranteed-present service, which these deliberately aren't); a deployment without session persistence mounts neither, and the session still works, just untitled and ungrouped.

**A task's session survives a harness restart.** The manager's live task→session map is only ever in memory, but a session's own id already encodes its task id (`agentrq-task-<taskId>-<random>`), so nothing separate needs to persist it: at startup the manager lists every session `@deepseek-ai/dsh-session-persistence` already knows about (headers only, not full logs), and the first push for a task whose session survived resumes it (`ctx.agents.resume`) instead of opening a duplicate. A resume that fails (a corrupted log, say) falls back to opening fresh rather than blocking delivery. The random suffix matters here, not just for looks: an incrementing in-process counter resets across a restart, and the moment the same task got pushed twice across one, the persistence layer rejected the reused id outright as already belonging to a different session.

**Repeats are dropped.** The workspace re-pushes an unclaimed task verbatim every minute; the manager remembers recent `(task, content)` pairs, so a task's session is handed it once and not woken every sixty seconds for work it already has. A genuinely new message on the same task still gets through.

**A task's session closes itself once the task is done.** An idle agent does not by itself mean the task is finished — it may simply have asked the human a question and be waiting on a reply, which must land in the same session. So on every idle transition the manager reads the task's own status back through `getTask`; only `completed` or `rejected` closes the dedicated session. `blocked` and anything else keep it open so the next push — including the human's reply — still has somewhere to go.

**Staying connected is the load-bearing part.** No workspace session, no pushes — so a closed transport or an unrecoverable transport error triggers a reconnect with exponential backoff (`reconnect.initialDelayMs` doubling to `reconnect.maxDelayMs`), on top of the SDK's own SSE resumption. Because the server re-pushes on its own schedule, a recovered connection catches up on the next tick without any client-side replay. A task's own dedicated session is unaffected by a workspace reconnect — it is a separate, harness-native agent, not part of the workspace transport.

`catchUpOnStart` dequeues one task when the workspace connection opens, so work that predates the connection does not wait for the server's next tick. A failed startup check costs latency, not work.

`agentrq_autopull pause` stops the manager from opening or routing into task sessions; already-open ones stay live until their task closes them, and the workspace connection itself stays open.

## Multiple workspaces

AgentRQ users normally have several workspaces, each with its own queue, mission, and agent identity. **Run one profile per workspace.**

Install once per profile, and let each profile's `cordis.patch.yml` carry its own endpoint:

```sh
npx @deepseek-ai/dsh plugin --profile agentrq-acme add @agentrq/dsh-plugin-agentrq
npx @deepseek-ai/dsh plugin --profile agentrq-beta add @agentrq/dsh-plugin-agentrq
# …then pin acme's URL in ~/.dsh/profiles/agentrq-acme/cordis.patch.yml
#     and beta's URL in ~/.dsh/profiles/agentrq-beta/cordis.patch.yml

npx @deepseek-ai/dsh --profile agentrq-acme    # terminal 1
npx @deepseek-ai/dsh --profile agentrq-beta    # terminal 2
```

Because the endpoint lives in the profile rather than the environment, switching workspaces is switching profiles — nothing to re-export, and no way to start one workspace's profile pointed at another's queue.

Each profile gets its own process, sessions, working directory, and workspace connection, which matches how AgentRQ already models a workspace: one workspace, one agent, one mission. It also matches the usual case where workspaces track different repositories.

Two consequences worth knowing:

- **A single profile cannot serve two workspaces.** Mounting the bundle twice in one profile registers the `agentrq:protocol` prompt section and the `agentrq_autopull` tool twice in the same layer, and both registrations throw on a duplicate name. Namespacing them per instance is deferred until someone needs it.
- **No cross-workspace view.** AgentRQ's CoreMCP supervisor (`https://mcp.agentrq.com/mcp`) does expose `listWorkspaces` and `listAllTasks`, so a deployment that wants "what is outstanding everywhere" can mount it as an extra `@deepseek-ai/dsh-mcp-client` row. It sends no channel notifications, so it complements per-workspace delivery rather than replacing it.

`serverName` is safe to change: the guidance section and every framing derive their tool names from it, so the namespace the model sees and the namespace the prose describes cannot drift apart.

## Config

| Key | Default | Meaning |
|---|---|---|
| `url` | — (required) | Workspace MCP endpoint, including its `?token=` credential |
| `token` | `''` | Bearer token, for deployments that prefer an `Authorization` header over `?token=` |
| `mountBridge` | `true` | Mount the `@deepseek-ai/dsh-mcp-client` child that gives the model AgentRQ's tools |
| `serverName` | `agentrq` | Namespace for the bridged tools; the guidance section and framings follow it |
| `deliverPushes` | `true` | Open and route into a dedicated session for each task the workspace pushes |
| `catchUpOnStart` | `true` | Dequeue one task when the workspace connection opens |
| `reconnect.initialDelayMs` | `1000` | Delay before the first reconnect attempt |
| `reconnect.maxDelayMs` | `900000` | Ceiling for the reconnect backoff |
| `guidance` | `true` | Contribute the AgentRQ working-agreement system-prompt section |
| `requestTimeoutMs` | `30000` | Timeout for one AgentRQ tool call |
| `provider` | `''` | Provider route for each task's dedicated session. Empty copies the most recently active agent's provider |
| `model` | `''` | Model id for each task's dedicated session. Empty copies the most recently active agent's model |
| `cwd` | `''` | Working directory for each task's dedicated session. Empty copies the reference agent's cwd (see above) — **not** the dsh process's own cwd, which under `dsh web` is the profile directory, not your project |

Set any of these in the same profile patch. A patch replaces a row's whole `config` rather than merging into it, but every key except `url` has a schema default, so a row only restates what it changes:

```yaml
- id: agentrq
  name: '@agentrq/dsh-plugin-agentrq'
  config:
    url: "https://<workspace>.mcp.agentrq.com/mcp?token=<token>"
    catchUpOnStart: false
    reconnect:
      initialDelayMs: 2000
      maxDelayMs: 60000
```

## The prompt section

AgentRQ's MCP server ships its collaboration rules as server `Instructions`, and the harness does not surface an MCP server's instructions to the model. Without them the model has the tools but not the contract — that the human is remote, sees only what `reply` sends, and needs the task claimed before work starts. This package contributes those rules as the `agentrq:protocol` section in the tool-guidance band (order 150), so behavior in dsh matches behavior in the Claude Code and Gemini extensions. Turn it off with `guidance: false` when a deployment states the same protocol in its own persona.

## Development

```sh
npm install --legacy-peer-deps   # harness packages declare peers pnpm resolves from the profile
npm run typecheck
npm test
npm run build
```

`make plugin-deepseek` from the repository root runs all four.

## Releasing

`.github/workflows/plugin-deepseek-harness.yml` typechecks, tests, and builds this package on every pull request that touches `plugins/deepseek-harness/**`, and publishes it to npm when such a change lands on `main`.

**Bumping `version` in `package.json` is what releases.** npm refuses to republish an existing version, so the workflow checks first and skips the publish when the current version is already on the registry — an ordinary fix that touches this path does not need a version bump to merge.

Authentication is [npm trusted publishing](https://docs.npmjs.com/trusted-publishers): the package names `agentrq/agentrq` and this workflow file as its trusted publisher, the job requests an OIDC token with `id-token: write`, and npm exchanges it for a short-lived publish credential. There is no `NPM_TOKEN` secret to store, rotate, or leak, and npm attaches build provenance automatically.

Two things that break it, both non-obvious:

- **Renaming the workflow file.** The trusted-publisher record names `plugin-deepseek-harness.yml` exactly; a rename must be made on npm's side too or every publish is rejected.
- **npm older than 11.5.1.** `setup-node` with Node 22 installs npm 10.x, which has no OIDC support and silently falls back to looking for a token. The workflow upgrades npm explicitly for this reason — do not remove that step.

## Known limitations and deferred work

- **Two pushes racing for the same brand-new task can still open two sessions** — `deliverPush` doesn't await the previous call for a different chat, so if two pushes for a task neither has seen before arrive close enough together, both can find no live *or* persisted session and both create one. Existing pushes for an already-open task don't have this problem (they queue into the resolved session, live or resumed).
- **One workspace per profile** — each row carries one `url`, and mounting the bundle twice in one profile collides on the prompt-section and tool names. [Several workspaces means several profiles](#multiple-workspaces).
- **The endpoint is configured, not discovered** — there is no in-harness command to switch workspaces; the profile's `cordis.patch.yml` (watched, so no restart needed) or `AGENTRQ_WORKSPACE_MCP_URL` is the switch. A `ctx.settings` namespace would give a schema-driven editor with `role('secret')` redaction for the token, but its document is `$DSH_HOME`-global by default and so does not carry per-profile values without extra plumbing.
- **The bridge is mounted, not injectable** — the plugin mounts one `@deepseek-ai/dsh-mcp-client` child with the settings it derives from its own config. A deployment that needs the bridge's other knobs sets `mountBridge: false` and mounts its own row, and is then responsible for keeping `serverName` aligned.
- **No in-harness command lists or switches to a task's session** — it's titled and grouped correctly now (a capable UI like `dsh web` shows it in the sidebar under its project, named after the task), but there's still no dedicated "show me AgentRQ's sessions" affordance; you find one the way you'd find any other session there.
- **The very first task session can still fail if nothing else is configured** — `provider`/`model` default to copying the most recently active agent in the process, falling back to the longest-lived one if none has been active yet; if a task is pushed before any session has ever existed at all (e.g. right at process startup with `catchUpOnStart`), there is nothing to copy and the session opens with no model, which fails every turn. Set `model` (and `provider`, if it's not the deployment's default route) explicitly to avoid depending on load order.
- **Terminal-status check costs a round trip per idle transition** — the manager cannot tell "done" from "idle mid-task, waiting on a reply" without asking the workspace, so every `agent/status: idle` for a task's session calls `getTask` once. Cheap in practice (one call per turn boundary, not per second), but not free.
- **Session-lifetime repeat memory** — the delivered-set is process-local and bounded, so a restarted harness may be handed a task it saw before if that task is still unclaimed. Claiming a task with `updateTaskStatus` is what stops the workspace re-pushing it.
- **Auth is the URL's credential** — the plugin does not run the AgentRQ OAuth authorization-code flow; it uses the long-lived token from Workspace Settings, as a bearer header or a `?token=` query parameter.
- **Attachments travel through the model** — the plugin's own session only receives pushes and dequeues on request; `downloadAttachment` remains a model-facing tool call on the bridged server.
- **Permission verdicts are not bridged** — `acp-gateway` also consumes `notifications/claude/channel/permission` to answer AgentRQ's allow/deny prompts. The harness has its own `tools/pre-execute` approval axis, and wiring the two together is deferred.

## License

[Apache-2.0](./LICENSE), matching the rest of the AgentRQ repository.
