# Telemetry

> Read before adding a telemetry action — it is four places, or it reads as zero.

Most actions are emitted by the backend right after it does the work, which
makes them self-evidently true. A few happen entirely in the browser and are
*reported* by it instead — the local-AI features, and interface usage
(shortcuts, search, copies, the trajectory view).

- The allowlist in `entity.ClientReportableAction` is the security boundary for
  `POST /api/v1/telemetry`: only names in it may be reported, and the controller
  additionally checks the caller owns the workspace.
- Adding one means four places, or it reads as zero: the `Action` constant and
  its `String()`, the allowlist, the `model.ActionID*` constant (**append only**
  — the value is stored), and the mapping in `controller/telemetry`.
- `frontend/src/composables/useUiTelemetry.js` resolves the workspace from the
  route and **drops the report when there is none**, rather than guessing one.
  Interface counts are therefore actions-with-a-workspace-in-context.
- **Not every action has a workspace, and the machine ones deliberately do
  not.** A machine belongs to an account and runs agents for many workspaces at
  once, so `machine_add`, `machine_remove`, `machine_disable`, `machine_enable`
  and `machine_enrol_code_create` store workspace `0` — they will never appear
  in a *workspace's* stats, only in an account's. The session and terminal
  actions do carry one, because a session knows which workspace it is working
  in. Do not "fix" a zero there by attributing it to whichever workspace
  happens to be open.
- All of the machine actions are **backend-emitted and none is in
  `ClientReportableAction`**, which `telemetry_test.go` enforces by name: the
  server observes every enrolment, delete, session and terminal attach itself,
  so a browser claiming one could invent machines and agent runs that never
  happened.
- The route is rate limited per user. It was raised to 60/minute when interface
  usage was added; ordinary use passes the old ceiling of 10 easily, and being
  short there loses reports silently and starves the local-AI metrics that share
  the bucket.
- **MCP tool/resource/prompt calls are a separate, backend-only path** —
  `mcp.Action`/`MCPEvent` on `PubSubTopicMCP`, not the CRUD path above — shared
  by *both* the per-workspace MCP server and CoreMCP (the account-wide
  supervisor server). A tool call is `ActionMCPToolCall`; a resource read or
  prompt get is `ActionMCPMethodCall`, so a supervisor pulling a guide resource
  doesn't inflate the same count as it calling a tool. Neither server
  distinguishes *itself* in the stored row — a `getTask` call looks the same
  whether it came from the workspace server or CoreMCP — but *which* tool,
  resource or prompt it was is in `Telemetry.SubActionID`
  (`model.SubActionIDMCP*`, its own append-only enumeration, meaningful only
  alongside `ActionIDMCPToolCall`/`ActionIDMCPMethodCall`). Adding a tool,
  resource or prompt to either server means adding its name to
  `subActionIDByToolName` in `controller/telemetry/telemetry.go` — skip it and
  `sub_action_test.go` fails, because it lists what the live servers actually
  registered rather than trusting this doc. CoreMCP calls that aren't scoped to
  one workspace (`listWorkspaces`, `createEnrolmentCode`, defining an
  event/workflow) store workspace `0`, same convention as the machine actions
  above.
- **Skills used from the interface** (`skill_import`/`_view`/`_search`/`_enable`/`_disable`) share
  controller methods with the MCP skill tools, so MCP callers mark their ctx
  `OriginMCP` and the controller skips it; drop the mark and every agent read
  counts twice.
- **Rollups live in three separate tables** (`internal/service/telemetryaggregator`):
  hourly sums the raw `telemetries` table, daily sums hourly, monthly sums
  daily — never the raw table twice. Monthly re-runs **every day**, not once
  at month end, so the current month's row stays current; its upsert key is
  therefore the day it ran, not the month, or the daily-shaped key would
  collide with daily's own claim row. It sums **the month yesterday was in**:
  taking today's month sums nothing on the 1st and loses every month's last
  day. A `telemetry_aggregations` row is claimed (`ON CONFLICT DO NOTHING`)
  before each run so two backend instances polling the same schedule don't
  double-count a period — skip the claim and a duplicate run doubles every
  number downstream of it silently.
- **Task latency** (`service/latencyaggregator`, `service/tasklatency`) rolls up
  the same way, from `task_latencies` — one row per closed task, written in
  `recordTaskStateTransition` so no path can close a task without it. A read
  takes rollups up to the newest claim and the task rows after it; reading only
  the rollups loses today, and reading both past the claim counts a task twice.
  Older tasks are backfilled once, in a goroutine at boot, behind the claim
  `latency_backfill`/`v1`. Keep the claim even when the run fails, or every
  restart redoes it; `backend/sql/backfill` is the manual fallback.
