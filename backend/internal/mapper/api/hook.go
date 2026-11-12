// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
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

// FromHTTPRequestToHookRegistrationRequestEntity reads a registration's
// fields from the body; nil when it is not JSON or names an id that is not
// one.
func FromHTTPRequestToHookRegistrationRequestEntity(c *fiber.Ctx) *entity.HookRegistrationRequest {
	var v view.HookRegistrationRequest
	if err := json.Unmarshal(c.Body(), &v); err != nil {
		return nil
	}
	rq := &entity.HookRegistrationRequest{Name: v.Name, Enabled: v.Enabled, Inputs: v.Inputs}
	if v.HookID != "" {
		if rq.HookID = monoflake.IDFromBase62(v.HookID).Int64(); rq.HookID == 0 {
			return nil
		}
	}
	if v.Routes != nil {
		rq.Routes = make(map[string]int64, len(v.Routes))
		for name, id := range v.Routes {
			if rq.Routes[name] = monoflake.IDFromBase62(id).Int64(); rq.Routes[name] == 0 {
				return nil
			}
		}
	}
	return rq
}

// FromHookRegistrationEntityToHookRegistrationView renders a registration.
func FromHookRegistrationEntityToHookRegistrationView(e entity.HookRegistration) view.HookRegistration {
	v := view.HookRegistration{
		ID: monoflake.ID(e.ID).String(), HookID: monoflake.ID(e.HookID).String(), HookName: e.HookName,
		HookAvailable: e.HookAvailable, Name: e.Name, Enabled: e.Enabled, Inputs: e.Inputs, SecretsSet: e.SecretsSet,
		Routes: make(map[string]string, len(e.Routes)), URL: e.URL, URLWithToken: e.URLWithToken,
		AllowAllCommandsEvents: e.AllowAllCommandsEvents, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
	}
	for name, id := range e.Routes {
		v.Routes[name] = monoflake.ID(id).String()
	}
	if v.Inputs == nil {
		v.Inputs = map[string]any{}
	}
	if v.SecretsSet == nil {
		v.SecretsSet = []string{}
	}
	if v.AllowAllCommandsEvents == nil {
		v.AllowAllCommandsEvents = []string{}
	}
	return v
}

// FromHookRegistrationEntityToHTTPResponse renders one registration.
func FromHookRegistrationEntityToHTTPResponse(e entity.HookRegistration) []byte {
	b, _ := json.Marshal(FromHookRegistrationEntityToHookRegistrationView(e))
	return b
}

// FromHookRegistrationEntitiesToHTTPResponse renders an account's
// registrations; none is an empty list.
func FromHookRegistrationEntitiesToHTTPResponse(es []entity.HookRegistration) []byte {
	out := view.ListHookRegistrationsResponse{Registrations: make([]view.HookRegistration, len(es))}
	for i, e := range es {
		out.Registrations[i] = FromHookRegistrationEntityToHookRegistrationView(e)
	}
	b, _ := json.Marshal(out)
	return b
}
