// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"

	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/hookrunner"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// registerAdminHookRoutes adds the root user's built-in hooks. A module is
// the request body, as application/wasm; see hookModule.
func (h *handler) registerAdminHookRoutes(r fiber.Router) {
	r.Get("/hooks", h.listBuiltInHooks())
	r.Post("/hooks", h.createBuiltInHook())
	r.Put("/hooks/:id", h.upgradeBuiltInHook())
	r.Patch("/hooks/:id", h.setBuiltInHookEnabled())
	r.Delete("/hooks/:id", h.deleteBuiltInHook())
}

func (h *handler) listBuiltInHooks() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ctx, cancel := newContext(c)
		defer cancel()
		hooks, err := h.hooks.ListBuiltIns(ctx)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromHookEntitiesToHTTPResponse(hooks))
	}
}

func (h *handler) createBuiltInHook() fiber.Handler {
	return func(c *fiber.Ctx) error {
		wasm, ok := hookModule(c)
		if !ok {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		hook, err := h.hooks.CreateBuiltIn(ctx, wasm)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Status(fiber.StatusCreated).Send(mapper.FromHookEntityToHTTPResponse(hook))
	}
}

func (h *handler) upgradeBuiltInHook() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		wasm, ok := hookModule(c)
		if id == 0 || !ok {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		hook, err := h.hooks.UpgradeBuiltIn(ctx, id, wasm)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromHookEntityToHTTPResponse(hook))
	}
}

func (h *handler) setBuiltInHookEnabled() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		var req view.SetHookEnabledRequest
		if id == 0 || c.BodyParser(&req) != nil || req.Enabled == nil {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		hook, err := h.hooks.SetBuiltInEnabled(ctx, id, *req.Enabled)
		if err != nil {
			return hookError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(mapper.FromHookEntityToHTTPResponse(hook))
	}
}

func (h *handler) deleteBuiltInHook() fiber.Handler {
	return func(c *fiber.Ctx) error {
		id := monoflake.IDFromBase62(c.Params("id")).Int64()
		if id == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.hooks.DeleteBuiltIn(ctx, id); err != nil {
			return hookError(c, err)
		}
		return c.SendStatus(fiber.StatusNoContent)
	}
}

// hookModule reads the module a request carries as its body. A Go hook is
// over the server's body limit, so it may come gzipped (Content-Encoding:
// gzip), and is unpacked here with a cap rather than by Fiber with none. One
// over hookrunner.MaxModuleSize is cut one byte past it, and the runner
// refuses it for its size.
func hookModule(c *fiber.Ctx) ([]byte, bool) {
	body := c.Request().Body()
	switch strings.ToLower(strings.TrimSpace(c.Get(fiber.HeaderContentEncoding))) {
	case "":
	case "gzip":
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, false
		}
		body, err = io.ReadAll(io.LimitReader(zr, hookrunner.MaxModuleSize+1))
		if err != nil {
			return nil, false
		}
	default:
		return nil, false
	}
	return body, len(body) > 0
}

func hookError(c *fiber.Ctx, err error) error {
	b, status := mapper.FromErrorToHTTPResponse(err)
	if status >= fiber.StatusInternalServerError {
		zlog.Error().Err(err).Msg("admin hooks")
	}
	c.Set(_headerContentType, _mimeJSON)
	return c.Status(status).Send(b)
}
