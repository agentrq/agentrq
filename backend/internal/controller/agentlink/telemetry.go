// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// TelemetryReport is one piece of an agent's turn that is not its answer: its
// reasoning, its plan, or what the turn is costing.
//
// Text is a rendered, ready-to-show form of the same thing Data holds
// structurally, so a client that does not understand a given kind still has
// something to show.
type TelemetryReport struct {
	TaskID    string
	SessionID string
	Kind      string
	Text      string
	Data      map[string]any
}

// Removed reports whether the agent withdrew its plan.
func (p TelemetryReport) Removed() bool {
	gone, _ := p.Data["removed"].(bool)
	return gone
}
