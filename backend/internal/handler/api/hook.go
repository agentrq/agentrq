// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// registerHookRoutes adds an account's hooks and its registrations of them.
func (h *handler) registerHookRoutes() {
	r := h.router.Group("/hooks")
	r.Get("", h.listAvailableHooks())
	r.Get("/registrations", h.listHookRegistrations())
	r.Post("/registrations", h.createHookRegistration())
	r.Get("/registrations/:id", h.getHookRegistration())
	r.Patch("/registrations/:id", h.updateHookRegistration())
	r.Delete("/registrations/:id", h.deleteHookRegistration())
	r.Post("/registrations/:id/rotateToken", h.rotateHookToken())
}

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
		return c.Send(mapper.FromHookEntitiesToHTTPResponse(hooks))
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
