# AgentRQ Codebase Notes

## Project layout
- `backend/` — Go backend (Fiber HTTP, GORM, MCP server)
- `frontend/` — React frontend

## Running tests
```
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

## Commit convention
Include `Task: <taskID>` in the commit body for traceability.
