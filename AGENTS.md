# AgentRQ Codebase Notes

## Project layout

- `backend/` — Go backend (Fiber HTTP, GORM, MCP server)
- `frontend/` — Vue3 frontend

## Running tests

```bash
cd backend && go test ./internal/...
```

Mock packages are **generated** (gitignored). Run `make mocks` before testing if they are missing. `mockgen` lives at `~/go/bin/mockgen`.

## MCP server (`backend/internal/controller/mcp/`)

- `server.go` — all tool handlers (`handleCreateTask`, `handleReply`, etc.) and the `WorkspaceServer` struct
- Cron validation: `validateCronGranularity` enforces hourly-minimum granularity. Minute field must be a single fixed integer (0-59); wildcards/steps/ranges/comma-lists are rejected.
- Creating a task with `cron_schedule` sets `status="cron"` on the model.

## CRUD task controller (`backend/internal/controller/crud/task.go`)

- Cron validation also lives here for the REST API path (same rules).
- `isValidTaskStatus` — valid statuses: `notstarted`, `ongoing`, `completed`, `rejected`, `cron`, `blocked`.

## Events (experimental)

Named signals that let one workspace trigger tasks in another.

- **DB models**: `events` and `event_triggers` tables (monoflake IDs).
- **REST API** (all under `/api/v1/events`): CRUD for events + triggers, plus `GET /events/:id/tasks` to list tasks spawned by an event. These routes are handled by Fiber — do NOT add them to the stdlib `mux` in `app.go` (that mux is only for SSE and pub-stats routes).
- **MCP tool**: `publishEvent` — agents call this to fire an event by name with a payload and optional FAQ.
- **Task `eventId` field** (base62): set at task creation (REST or MCP) to link a task to an event. Publishing is **agent-driven**: the createTask notification sent to the agent via the MCP channel appends `[On completion: call publishEvent("name", "...")]` so the agent publishes the linked event with a meaningful payload before completing. (There is no automatic publish on completion; completing a task does not by itself fire the event.)
- **Consumer**: `backend/internal/controller/event/` subscribes to `PubSubTopicEvents` (ID 3) and fans out to all `EventTrigger` rows, creating tasks via `renderTemplate` substituting `{{EVENT_PAYLOAD}}` and `{{EVENT_FAQ}}` **in the body only** — the title is always static text.
- **`EventTrigger.emitEventId`**: optional field that chains events — when the trigger's spawned task completes it publishes this second event. The consumer appends the same `publishEvent` instruction to the task body. Triggered tasks always start as `notstarted` (no cron scheduling).
- **Frontend**: `/events` list + `/events/:id` detail (triggers CRUD + resulting tasks, 10 shown with load-more). Both the task-creation form and the trigger-creation form have an optional "Emit event on completion" selector.

## Commit convention

Include `Task: <taskID>` in the commit body for traceability.

## Coding Standards

- **API Naming**: All JSON fields in API requests and responses MUST use `camelCase` (e.g., `workspaceId`, `createdAt`). Never use `snake_case` in the API surface.
- **Backend Layers**: Follow view-entity-model separation; only `view` structs define the API schema. Avoid using repository directly from handlers; use controller methods instead.
