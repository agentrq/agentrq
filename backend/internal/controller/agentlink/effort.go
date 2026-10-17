// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

// Effort is one reasoning level the connected agent offers.
type Effort struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// EffortReport is what a session reports about its reasoning level: the
// agent's ACP config option with category "thought_level". An empty Efforts
// withdraws the offer, which is what a switch to a model without levels does.
type EffortReport struct {
	TaskID        string
	SessionID     string
	ConfigID      string
	CurrentEffort string
	Efforts       []Effort
	CanSet        bool
}

// EffortSnapshot is what one connected session last reported.
type EffortSnapshot struct {
	SessionID     string
	ConfigID      string
	CurrentEffort string
	Efforts       []Effort
	CanSet        bool
}

// Selectable reports whether a choice can be written back.
func (s EffortSnapshot) Selectable() bool {
	return s.CanSet && s.ConfigID != ""
}
