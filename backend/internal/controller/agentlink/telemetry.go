// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// TelemetryReport is the payload of an agent telemetry notification.
//
// Text is a rendered, ready-to-show form of the same thing Data holds
// structurally, so a client that does not understand a given kind still has
// something to show.
type TelemetryReport struct {
	TaskID    string         `json:"task_id"`
	SessionID string         `json:"session_id"`
	Kind      string         `json:"kind"`
	Text      string         `json:"text"`
	Data      map[string]any `json:"data"`
}

// PlanID is the plan a payload refers to, or "" if it names none.
func (p TelemetryReport) PlanID() string {
	id, _ := p.Data["planId"].(string)
	return id
}

// Removed reports whether the agent withdrew the plan this payload names.
func (p TelemetryReport) Removed() bool {
	gone, _ := p.Data["removed"].(bool)
	return gone
}
