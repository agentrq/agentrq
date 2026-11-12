// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// registerHookRoutes adds an account's hooks and its registrations of them.
func (h *handler) registerHookRoutes() {
	r := h.router.Group("/hooks")
	r.Get("", h.listAvailableHooks())
	r.Post("", h.createOwnHook())
	r.Put("/:id", h.upgradeOwnHook())
	r.Delete("/:id", h.deleteOwnHook())
	r.Get("/registrations", h.listHookRegistrations())
	r.Post("/registrations", h.createHookRegistration())
	r.Get("/registrations/:id", h.getHookRegistration())
	r.Patch("/registrations/:id", h.updateHookRegistration())
	r.Delete("/registrations/:id", h.deleteHookRegistration())
	r.Post("/registrations/:id/rotateToken", h.rotateHookToken())
	r.Get("/registrations/:id/deliveries", h.listHookDeliveries())
	r.Post("/registrations/:id/deliveries/:deliveryId/replay", h.replayHookDelivery())
}

// registerPublicHookRoutes adds the URL outside services send requests to.
// It takes no session: the registration's token, in the query, is the
// credential, and a wrong one answers as an unknown id does.
func (h *handler) registerPublicHookRoutes() {
	h.router.Post("/hooks/in/:id", h.receiveHookRequest())
}

func (h *handler) receiveHookRequest() fiber.Handler {
	return func(c *fiber.Ctx) error {
		in := mapper.FromHTTPRequestToHookIncomingRequestEntity(c)
		if in == nil {
			return hookError(c, base.ErrNotFound)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		id, err := h.hooks.Receive(ctx, *in)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Status(fiber.StatusAccepted).Send(mapper.FromHookDeliveryIDToHTTPResponse(id))
	}
}

func (h *handler) listHookDeliveries() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		limit := c.QueryInt("limit", defaultDeliveryPage)
		if id == 0 || limit < 1 || limit > maxDeliveryPage {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		var before int64
		if s := c.Query("before"); s != "" {
			if before = monoflake.IDFromBase62(s).Int64(); before == 0 {
				return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
			}
		}
		ctx, cancel := newContext(c)
		defer cancel()
		ds, err := h.hooks.ListDeliveries(ctx, sessionUser(c), id, before, limit)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromHookDeliveryEntitiesToHTTPResponse(ds))
	}
}

func (h *handler) replayHookDelivery() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		deliveryID := monoflake.IDFromBase62(c.Params("deliveryId")).Int64()
		if id == 0 || deliveryID == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.hooks.ReplayDelivery(ctx, sessionUser(c), id, deliveryID); err != nil {
			return hookError(c, err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

const (
	defaultDeliveryPage = 50
	maxDeliveryPage     = 200
)

func sessionUser(c *fiber.Ctx) int64 {
	return monoflake.IDFromBase62(c.Locals("user_id").(string)).Int64()
}

func (h *handler) listAvailableHooks() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := newContext(c)
		defer cancel()
		hooks, err := h.hooks.ListAvailable(ctx, sessionUser(c))
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromAvailableHookEntitiesToHTTPResponse(hooks, h.hooks.AllowsUserHooks()))
	}
}

// createOwnHook adds an account's own hook from its module, sent as the
// admin upload is: application/wasm, gzipped when large.
func (h *handler) createOwnHook() fiber.Handler {
	return func(c *fiber.Ctx) error {
		wasm, ok := hookModule(c)
		if !ok {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		hook, err := h.hooks.CreateOwn(ctx, sessionUser(c), wasm)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Status(fiber.StatusCreated).Send(mapper.FromHookEntityToHTTPResponse(hook))
	}
}

func (h *handler) upgradeOwnHook() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		wasm, ok := hookModule(c)
		if id == 0 || !ok {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		hook, err := h.hooks.UpgradeOwn(ctx, sessionUser(c), id, wasm)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromHookEntityToHTTPResponse(hook))
	}
}

func (h *handler) deleteOwnHook() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		if id == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.hooks.DeleteOwn(ctx, sessionUser(c), id); err != nil {
			return hookError(c, err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

func (h *handler) listHookRegistrations() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := newContext(c)
		defer cancel()
		regs, err := h.hooks.ListRegistrations(ctx, sessionUser(c))
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromHookRegistrationEntitiesToHTTPResponse(regs))
	}
}

func (h *handler) createHookRegistration() fiber.Handler {
	return func(c *fiber.Ctx) error {
		rq := mapper.FromHTTPRequestToHookRegistrationRequestEntity(c)
		if rq == nil || rq.HookID == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		reg, err := h.hooks.CreateRegistration(ctx, sessionUser(c), *rq)
		return sendRegistration(c, fiber.StatusCreated, reg, err)
	}
}

func (h *handler) getHookRegistration() fiber.Handler {
	return h.registrationByID(func(c *fiber.Ctx, id int64) (entity.HookRegistration, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return h.hooks.GetRegistration(ctx, sessionUser(c), id)
	})
}

func (h *handler) updateHookRegistration() fiber.Handler {
	return h.registrationByID(func(c *fiber.Ctx, id int64) (entity.HookRegistration, error) {
		rq := mapper.FromHTTPRequestToHookRegistrationRequestEntity(c)
		if rq == nil || rq.HookID != 0 {
			// A registration keeps its hook; another hook is a new one.
			return entity.HookRegistration{}, errBadRequest
		}
		ctx, cancel := newContext(c)
		defer cancel()
		return h.hooks.UpdateRegistration(ctx, sessionUser(c), id, *rq)
	})
}

func (h *handler) rotateHookToken() fiber.Handler {
	return h.registrationByID(func(c *fiber.Ctx, id int64) (entity.HookRegistration, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return h.hooks.RotateToken(ctx, sessionUser(c), id)
	})
}

func (h *handler) deleteHookRegistration() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		if id == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.hooks.DeleteRegistration(ctx, sessionUser(c), id); err != nil {
			return hookError(c, err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// errBadRequest is a request whose body or id is not one; answered 400.
var errBadRequest = fiber.ErrBadRequest

// registrationByID answers with the registration fn returns for the id in the
// path.
func (h *handler) registrationByID(fn func(c *fiber.Ctx, id int64) (entity.HookRegistration, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		if id == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		reg, err := fn(c, id)
		return sendRegistration(c, fiber.StatusOK, reg, err)
	}
}

func sendRegistration(c *fiber.Ctx, status int, reg entity.HookRegistration, err error) error {
	if err == errBadRequest {
		return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
	}
	if err != nil {
		return hookError(c, err)
	}
	c.Set(_headerContentType, _mimeJSON)
	return c.Status(status).Send(mapper.FromHookRegistrationEntityToHTTPResponse(reg))
}
