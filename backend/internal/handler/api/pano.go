// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"net/http"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
)

// registerPanoRoutes serves the account's Pano under /pano. The person
// writes as workspace 0; agents write through the pano MCP tool.
func (h *handler) registerPanoRoutes() {
	r := h.router.Group("/pano")
	r.Get("", h.listPanoDocs())
	r.Post("", h.savePanoDoc())
	r.Get("/:id", h.getPanoDoc())
	r.Put("/:id", h.savePanoDoc())
	r.Delete("/:id", h.deletePanoDoc())
	r.Post("/:id/comments", h.addPanoComment())
	r.Post("/:id/attachments", h.attachPanoFiles())
}

func sendPanoError(c *fiber.Ctx, err error) error {
	e, status := mapper.FromErrorToHTTPResponse(err)
	c.Status(status)
	return c.Send(e)
}

func (h *handler) listPanoDocs() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListPanoDocs(ctx, entity.ListPanoDocsRequest{UserID: c.Locals("user_id").(string)})
		if err != nil {
			return sendPanoError(c, err)
		}
		return c.Send(mapper.FromListPanoDocsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) getPanoDoc() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetPanoDocRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetPanoDoc(ctx, *rq)
		if err != nil {
			return sendPanoError(c, err)
		}
		return c.Send(mapper.FromPanoDocEntityToHTTPResponse(rs.Doc))
	}
}

// savePanoDoc creates a doc on POST /pano and replaces one on PUT /pano/:id.
func (h *handler) savePanoDoc() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToSavePanoDocRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.SavePanoDoc(ctx, *rq)
		if err != nil {
			return sendPanoError(c, err)
		}
		if rq.ID == 0 {
			c.Status(http.StatusCreated)
		}
		return c.Send(mapper.FromPanoDocEntityToHTTPResponse(rs.Doc))
	}
}

func (h *handler) deletePanoDoc() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeletePanoDocRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeletePanoDoc(ctx, *rq); err != nil {
			return sendPanoError(c, err)
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

func (h *handler) addPanoComment() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToAddPanoCommentRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.AddPanoComment(ctx, *rq)
		if err != nil {
			return sendPanoError(c, err)
		}
		c.Status(http.StatusCreated)
		return c.Send(mapper.FromAddPanoCommentResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) attachPanoFiles() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToAttachPanoFilesRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.AttachPanoFiles(ctx, *rq)
		if err != nil {
			return sendPanoError(c, err)
		}
		c.Status(http.StatusCreated)
		return c.Send(mapper.FromAttachPanoFilesResponseEntityToHTTPResponse(rs))
	}
}
