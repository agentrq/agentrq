// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	"bytes"
	"compress/gzip"
	"net/http"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v3"
)

// registerAccountRoutes serves what holds in every workspace of an account:
// its settings, its own presets, and the download of all its data. The preset handlers are the workspace
// ones; with no :id in the path they work on the account's.
func (h *handler) registerAccountRoutes() {
	r := h.router.Group("/account")
	r.Get("/settings", h.getAccountSettings())
	r.Put("/settings", h.updateAccountSettings())
	r.Get("/presets", h.listWorkspacePresets())
	r.Post("/presets", h.createWorkspacePreset())
	r.Put("/presets/:name", h.updateWorkspacePreset())
	r.Delete("/presets/:name", h.deleteWorkspacePreset())
	r.Get("/export", h.exportAccountData())
}

// exportAccountData answers everything the account owns as a gzipped JSON
// file to save, named for the day it was taken. The file itself is gzip, not
// the transfer: with Content-Encoding the browser would unpack it on the way.
func (h *handler) exportAccountData() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		email, _ := c.Locals("user_email").(string)
		name, _ := c.Locals("user_name").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ExportAccountData(ctx, entity.ExportAccountDataRequest{UserID: c.Locals("user_id").(string), Email: email, Name: name})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		var file bytes.Buffer
		zw := gzip.NewWriter(&file)
		_, _ = zw.Write(mapper.FromAccountExportResponseEntityToHTTPResponse(rs))
		_ = zw.Close() // into memory, which cannot fail
		c.Set(_headerContentType, "application/gzip")
		c.Set(fiber.HeaderContentDisposition, contentDisposition("attachment", "agentrq-export-"+rs.ExportedAt.Format("2006-01-02")+".json.gz"))
		c.Set(fiber.HeaderCacheControl, "no-store")
		return c.Send(file.Bytes())
	}
}

func (h *handler) getAccountSettings() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetAccountSettings(ctx, entity.GetAccountSettingsRequest{UserID: c.Locals("user_id").(string)})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromAccountSettingsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateAccountSettings() fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateAccountSettingsRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateAccountSettings(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromAccountSettingsResponseEntityToHTTPResponse(rs))
	}
}
