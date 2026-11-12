// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"encoding/json"
	"strings"

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

// FromHTTPRequestToHookIncomingRequestEntity reads a request to a hook URL:
// its token out of the query, which the hook never sees, and its headers
// lowercased, without its cookies. nil when the id is not one.
func FromHTTPRequestToHookIncomingRequestEntity(c *fiber.Ctx) *entity.HookIncoming {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	in := &entity.HookIncoming{
		RegistrationID: id, Peer: c.Context().RemoteIP().String(), Method: c.Method(), Path: c.Path(),
		Query: map[string][]string{}, Headers: map[string][]string{}, Body: bytes.Clone(c.Body()),
	}
	c.Context().QueryArgs().VisitAll(func(k, v []byte) {
		if key := string(k); key == entity.HookTokenParam {
			in.Token = string(v)
		} else {
			in.Query[key] = append(in.Query[key], string(v))
		}
	})
	c.Request().Header.VisitAll(func(k, v []byte) {
		if key := strings.ToLower(string(k)); key != "cookie" {
			in.Headers[key] = append(in.Headers[key], string(v))
		}
	})
	return in
}

// FromHookDeliveryIDToHTTPResponse answers a queued request.
func FromHookDeliveryIDToHTTPResponse(id int64) []byte {
	b, _ := json.Marshal(view.HookReceivedResponse{DeliveryID: monoflake.ID(id).String()})
	return b
}

// FromHookDeliveryEntitiesToHTTPResponse renders a page of a registration's
// log; none is an empty list.
func FromHookDeliveryEntitiesToHTTPResponse(es []entity.HookDelivery) []byte {
	out := view.ListHookDeliveriesResponse{Deliveries: make([]view.HookDelivery, len(es))}
	for i, e := range es {
		names := e.EventNames
		if names == nil {
			names = []string{}
		}
		out.Deliveries[i] = view.HookDelivery{
			ID: monoflake.ID(e.ID).String(), RegistrationID: monoflake.ID(e.RegistrationID).String(), ReceivedAt: e.ReceivedAt,
			Status: e.Status, Attempts: e.Attempts, EventNames: names, Error: e.Error, DurationMs: e.DurationMs, Replayable: e.Replayable,
		}
	}
	b, _ := json.Marshal(out)
	return b
}

// FromAvailableHookEntitiesToHTTPResponse renders the hooks an account may
// register; none is an empty list.
func FromAvailableHookEntitiesToHTTPResponse(es []entity.Hook, allowUserHooks bool) []byte {
	out := view.AvailableHooksResponse{Hooks: make([]view.Hook, len(es)), AllowUserHooks: allowUserHooks}
	for i, e := range es {
		out.Hooks[i] = FromHookEntityToHookView(e)
	}
	b, _ := json.Marshal(out)
	return b
}
