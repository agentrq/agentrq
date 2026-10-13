# Events (experimental)

> Read before changing events, event triggers, or the tasks they spawn.

Named signals that let one workspace trigger tasks in another.

- **DB models**: `events` and `event_triggers` tables (monoflake IDs).
- **REST API** (all under `/api/v1/events`): CRUD for events + triggers, plus `GET /events/:id/tasks` to list tasks spawned by an event. These routes are handled by Fiber — do NOT add them to the stdlib `mux` in `app.go` (that mux is only for SSE and pub-stats routes).
- **MCP tool**: `workspace` action `publishEvent` — agents call this to fire an event by name with a payload and optional FAQ.
- **Task `eventId` field** (base62): set at task creation (REST or MCP) to link a task to an event. Publishing is **agent-driven**: the task's notification sent to the agent via the MCP channel appends `[On completion, before marking this task completed: call workspace {action: "publishEvent", payload: {name: …, taskId: …, payload: …, faq: […]}}]` (`service/eventinstruction`) so the agent publishes the linked event with a meaningful payload before completing. (There is no automatic publish on completion; completing a task does not by itself fire the event.)
- **Consumer**: `backend/internal/controller/event/` subscribes to `PubSubTopicEvents` (ID 3) and fans out to all `EventTrigger` rows, creating tasks via `renderTemplate` substituting `{{EVENT_PAYLOAD}}` and `{{EVENT_FAQ}}` **in the body only** — the title is always static text.
- **`EventTrigger.emitEventId`**: optional field that chains events — when the trigger's spawned task completes it publishes this second event. The consumer appends the same `publishEvent` instruction to the task body. Triggered tasks always start as `notstarted` (no cron scheduling).
- **Frontend**: `/events` list + `/events/:id` detail (triggers CRUD + resulting tasks, 10 shown with load-more). Both the task-creation form and the trigger-creation form have an optional "Emit event on completion" selector.

- **A trigger or workflow step dies with its workspace**: deleting *or archiving* a workspace deletes the triggers and steps that target it (unarchiving does not restore them), and the consumer's startup sweep removes any left behind. The consumer also skips an archived target, since `SystemGetWorkspace` returns archived workspaces and a task created in one is seen by nobody.
