// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/mustafaturan/monoflake"
)

// FromHookEntityToHookView renders a hook.
func FromHookEntityToHookView(e entity.Hook) view.Hook {
	v := view.Hook{
		ID: monoflake.ID(e.ID).String(), BuiltIn: e.BuiltIn, Name: e.Name, Version: e.Version,
		Description: e.Description, SHA256: e.SHA256, SizeBytes: e.SizeBytes, ABIVersion: e.ABIVersion,
		Inputs: make([]view.HookInput, len(e.Inputs)), Events: make([]view.HookEvent, len(e.Events)),
		Enabled: e.Enabled, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
	for i, in := range e.Inputs {
		v.Inputs[i] = view.HookInput(in)
	}
	for i, ev := range e.Events {
		v.Events[i] = view.HookEvent(ev)
	}
	return v
}

// FromHookEntityToHTTPResponse renders one hook as a response body.
func FromHookEntityToHTTPResponse(e entity.Hook) []byte {
	b, _ := json.Marshal(FromHookEntityToHookView(e))
	return b
}

// FromHookEntitiesToHTTPResponse renders a list of hooks as a response body;
// none is an empty list, not null.
func FromHookEntitiesToHTTPResponse(es []entity.Hook) []byte {
	out := view.ListHooksResponse{Hooks: make([]view.Hook, len(es))}
	for i, e := range es {
		out.Hooks[i] = FromHookEntityToHookView(e)
	}
	b, _ := json.Marshal(out)
	return b
}
