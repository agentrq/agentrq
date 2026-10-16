// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package agentlink

import "strings"

// Model is one model the connected agent offers.
type Model struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Current     bool   `json:"current,omitempty"`
	Group       string `json:"group,omitempty"`
}

// ModelsReport is the payload of a models notification.
//
// snake_case, unlike the REST surface: this is the wire format the gateways
// already send (acp-gateway's `sendModelsToWorkspace`), and renaming the fields
// here would break every gateway in the field to satisfy a convention that
// governs the HTTP API rather than this channel.
type ModelsReport struct {
	TaskID       string  `json:"task_id"`
	SessionID    string  `json:"session_id"`
	ConfigID     string  `json:"config_id"`
	CurrentModel string  `json:"current_model"`
	Models       []Model `json:"models"`
	// CanSet is the gateway saying it will act on a set_model notification.
	//
	// Absent means no, which is what makes this safe to add: every gateway
	// already in the field reports its models and none of them can be told to
	// change one, so reading silence as "yes" would offer a picker that does
	// nothing on every deployment that exists today.
	//
	// It has to come from the gateway rather than from a list of client names
	// here, because the question is about the gateway's *version*, and the
	// handshake name — "acp-gateway" either way — cannot tell two versions
	// apart. stopCapableClients can be a name list precisely because stopping
	// has been there since that client's first release.
	CanSet bool `json:"can_set,omitempty"`
}

// ModelsSnapshot is what one connected session last reported.
//
// ConfigID is carried through because selecting a model means writing back to
// the same session config option the agent advertised it under; a snapshot
// without it describes models nobody can switch to. SessionID is the *agent's*
// session — see the note on keying in HandleAgentModels — and is kept for the
// same reason: a future "use this model" has to name it.
type ModelsSnapshot struct {
	SessionID    string
	ConfigID     string
	CurrentModel string
	Models       []Model
	// CanSet reports whether the session that sent this will act on a
	// set_model notification. See the field on ModelsReport.
	CanSet bool
}

// Selectable reports whether a choice can actually be written back.
//
// The one definition of that rule, because it is asked in three places — the
// predicate the interface reads, the guard on the notification, and the flag on
// the workspace payload — and three copies of "CanSet && ConfigID != \"\"" would
// eventually disagree about whether a gateway that forgot its config option
// counts.
//
// ConfigID is half of it because a selection is a write to that session config
// option: a snapshot without one describes models nobody can switch to, however
// willing the gateway says it is.
func (s ModelsSnapshot) Selectable() bool {
	return s.CanSet && s.ConfigID != ""
}

// CurrentModelName is the display name of the model the session is running:
// the name its list gives the current ID, or the ID itself, or "".
func (s ModelsSnapshot) CurrentModelName() string {
	if s.CurrentModel == "" {
		return ""
	}
	for _, m := range s.Models {
		if m.ID == s.CurrentModel && strings.TrimSpace(m.Name) != "" {
			return strings.TrimSpace(m.Name)
		}
	}
	return s.CurrentModel
}

// CurrentModelID is the model the payload says is selected.
//
// The top-level field is preferred, falling back to whichever entry carries the
// agent's own `current` flag: gateways send one or the other, and a picker with
// nothing selected is worse than either.
func (p ModelsReport) CurrentModelID() string {
	if p.CurrentModel != "" {
		return p.CurrentModel
	}
	for _, m := range p.Models {
		if m.Current {
			return m.ID
		}
	}
	return ""
}
