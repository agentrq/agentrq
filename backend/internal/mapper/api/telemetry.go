package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// FromHTTPRequestToRecordTelemetryRequestEntity parses a client telemetry
// report, returning nil for anything it cannot fully resolve.
//
// The action name is looked up in the allowlist here rather than passed
// through as a string, so an unrecognised name is rejected at the edge and the
// rest of the stack only ever handles actions the server already knows.
func FromHTTPRequestToRecordTelemetryRequestEntity(c *fiber.Ctx) *entity.RecordTelemetryRequest {
	var payload view.RecordTelemetryRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}

	action, ok := entity.ClientReportableAction(payload.Action)
	if !ok {
		return nil
	}

	workspaceID := monoflake.IDFromBase62(payload.WorkspaceID).Int64()
	if workspaceID == 0 {
		return nil
	}

	return &entity.RecordTelemetryRequest{
		Action:      action,
		WorkspaceID: workspaceID,
	}
}
