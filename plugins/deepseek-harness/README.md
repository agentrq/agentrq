# @agentrq/dsh-plugin-agentrq

AgentRQ task manager for [DeepSeek Harness](https://github.com/deepseek-ai/deepseek-harness).

Create, manage, and automatically receive [AgentRQ](https://agentrq.com) tasks without leaving the harness. The bundle ships two rows: the workspace's tools bridged to the model, and the harness-side behavior a tool bridge cannot provide on its own — a supervised workspace session that delivers AgentRQ's pushes into the live agent, and the AgentRQ working agreement as a system-prompt section.

## Install

**One profile per workspace.** A profile serves one AgentRQ workspace and carries its own endpoint, so name it after the workspace rather than using `default` — that is what makes [several workspaces](#multiple-workspaces) work. Your workspace's **Settings → Setup → DeepSeek Harness** page prints every command and config block below already filled in.

```sh
dsh plugin --profile agentrq-<workspace> add @agentrq/dsh-plugin-agentrq
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
dsh --profile agentrq-<workspace> --dump-config   # shows the bundle layer and your override
dsh --profile agentrq-<workspace>
```

The profile's patch is applied after every bundle layer, so those two rows win. dsh watches both `cordis.patch.yml` layers and reapplies valid edits transactionally, so changing the URL takes effect without a restart.

### Configuring without a file edit

The bundle's own patch defaults both rows to `!!js process.env.AGENTRQ_WORKSPACE_MCP_URL`, so a container or CI job can export the endpoint instead of writing a profile patch:

```sh
export AGENTRQ_WORKSPACE_MCP_URL='https://<workspace>.mcp.agentrq.com/mcp?token=<token>'
dsh --profile agentrq-<workspace>
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

Delivery route depends on the agent's state: `inject()` while a turn is running, so it lands at the next step boundary, and `followup()` while idle, since nothing else would wake it. Neither interrupts a turn in flight.

`SendChannelNotification` puts the task id in `meta.chat_id` on every push, so the id never has to be recovered from the content.

**Repeats are dropped.** The workspace re-pushes an unclaimed task verbatim every minute; the runtime remembers recent `(task, content)` pairs, so the agent is handed it once and not woken every sixty seconds for work it already has. A genuinely new message on the same task still gets through.

**Staying connected is the load-bearing part.** No session, no pushes — so a closed transport or an unrecoverable transport error triggers a reconnect with exponential backoff (`reconnect.initialDelayMs` doubling to `reconnect.maxDelayMs`), on top of the SDK's own SSE resumption. Because the server re-pushes on its own schedule, a recovered session catches up on the next tick without any client-side replay.

`catchUpOnStart` dequeues one task when the session opens, so work that predates the connection does not wait for the server's next tick. A failed startup check costs latency, not work.

`agentrq_autopull pause` stops pushes from reaching the session; the session itself stays open.

One AgentRQ queue serves one worker, the harness Web UI creates a root agent per chat session, and pushes are broadcast to **every** connected session. Under the default `scope: single-agent`, exactly one live root agent holds the workspace session, so a second chat session does not get every task delivered a second time; a later session inherits the connection only after the owning agent is gone. Set `every-agent` when your agents work disjoint queues or you want deliberate fan-out.

## Multiple workspaces

AgentRQ users normally have several workspaces, each with its own queue, mission, and agent identity. **Run one profile per workspace.**

Install once per profile, and let each profile's `cordis.patch.yml` carry its own endpoint:

```sh
dsh plugin --profile agentrq-acme add @agentrq/dsh-plugin-agentrq
dsh plugin --profile agentrq-beta add @agentrq/dsh-plugin-agentrq
# …then pin acme's URL in ~/.dsh/profiles/agentrq-acme/cordis.patch.yml
#     and beta's URL in ~/.dsh/profiles/agentrq-beta/cordis.patch.yml

dsh --profile agentrq-acme    # terminal 1
dsh --profile agentrq-beta    # terminal 2
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
| `deliverPushes` | `true` | Deliver the workspace's tasks and messages into the live session |
| `catchUpOnStart` | `true` | Dequeue one task when the session opens |
| `scope` | `single-agent` | Whether one root agent or every root agent holds a workspace session |
| `reconnect.initialDelayMs` | `1000` | Delay before the first reconnect attempt |
| `reconnect.maxDelayMs` | `900000` | Ceiling for the reconnect backoff |
| `guidance` | `true` | Contribute the AgentRQ working-agreement system-prompt section |
| `requestTimeoutMs` | `30000` | Timeout for one AgentRQ tool call |

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

Publishing requires an `NPM_TOKEN` repository secret with publish rights to `@agentrq/dsh-plugin-agentrq`.

## Known limitations and deferred work

- **One workspace per profile** — each row carries one `url`, and mounting the bundle twice in one profile collides on the prompt-section and tool names. [Several workspaces means several profiles](#multiple-workspaces).
- **The endpoint is configured, not discovered** — there is no in-harness command to switch workspaces; the profile's `cordis.patch.yml` (watched, so no restart needed) or `AGENTRQ_WORKSPACE_MCP_URL` is the switch. A `ctx.settings` namespace would give a schema-driven editor with `role('secret')` redaction for the token, but its document is `$DSH_HOME`-global by default and so does not carry per-profile values without extra plumbing.
- **The bridge is mounted, not injectable** — the plugin mounts one `@deepseek-ai/dsh-mcp-client` child with the settings it derives from its own config. A deployment that needs the bridge's other knobs sets `mountBridge: false` and mounts its own row, and is then responsible for keeping `serverName` aligned.
- **Load-order boundary** — the plugin attaches only to root agents published after it loads; an agent that was already live when the plugin loaded gets no workspace session and no `agentrq_autopull` tool.
- **Ownership does not migrate to a live agent** — under `single-agent`, when the owning agent is disposed the connection stops until the *next* root agent is created; an already-open second session does not adopt it.
- **Session-lifetime repeat memory** — the delivered-set is process-local and bounded, so a restarted harness may be handed a task it saw before if that task is still unclaimed. Claiming a task with `updateTaskStatus` is what stops the workspace re-pushing it.
- **Auth is the URL's credential** — the plugin does not run the AgentRQ OAuth authorization-code flow; it uses the long-lived token from Workspace Settings, as a bearer header or a `?token=` query parameter.
- **Attachments travel through the model** — the plugin's own session only receives pushes and dequeues on request; `downloadAttachment` remains a model-facing tool call on the bridged server.
- **Permission verdicts are not bridged** — `acp-gateway` also consumes `notifications/claude/channel/permission` to answer AgentRQ's allow/deny prompts. The harness has its own `tools/pre-execute` approval axis, and wiring the two together is deferred.

## License

[Apache-2.0](./LICENSE), matching the rest of the AgentRQ repository.
