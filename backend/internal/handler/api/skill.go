// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"errors"
	"net/http"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
)

// registerSkillRoutes serves a workspace's skills. Writing single files is
// left to the MCP tools; people import, delete and share.
func (h *handler) registerSkillRoutes(r fiber.Router) {
	r.Get("/:id/skills", h.searchSkills())
	r.Post("/:id/skills/import", h.importSkills())
	r.Get("/:id/skills/:name", h.getSkill())
	r.Patch("/:id/skills/:name", h.setSkillEnabled())
	r.Delete("/:id/skills/:name", h.deleteSkill())
	r.Get("/:id/skills/:name/files/*", h.getSkillFile())
	r.Get("/:id/skills/:name/shares", h.listSkillShares())
	r.Put("/:id/skills/:name/shares/:targetWorkspaceId", h.shareSkill())
	r.Delete("/:id/skills/:name/shares/:targetWorkspaceId", h.unshareSkill())
}

// skillStatus is the status a skill refusal is answered with. Its message is
// written for the caller and is passed on; any other error is not.
var skillStatus = map[crud.SkillErrorKind]int{
	crud.SkillInvalid:  http.StatusUnprocessableEntity,
	crud.SkillReadOnly: http.StatusForbidden,
	crud.SkillConflict: http.StatusConflict,
	crud.SkillUpstream: http.StatusBadGateway,
}

func sendSkillError(c *fiber.Ctx, err error) error {
	var se *crud.SkillError
	if errors.As(err, &se) {
		c.Status(skillStatus[se.Kind])
		return c.Send(mapper.FromMessageToHTTPResponse(se.Message, skillStatus[se.Kind]))
	}
	e, status := mapper.FromErrorToHTTPResponse(err)
	c.Status(status)
	return c.Send(e)
}

func (h *handler) searchSkills() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToSearchSkillsRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.SearchSkills(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromSearchSkillsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) getSkill() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetSkillRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetSkill(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromGetSkillResponseEntityToHTTPResponse(rs))
	}
}

// setSkillEnabled turns a skill on or off for agents.
func (h *handler) setSkillEnabled() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToSetSkillEnabledRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.SetSkillEnabled(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromSetSkillEnabledResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) getSkillFile() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetSkillFileRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetSkillFile(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromGetSkillFileResponseEntityToHTTPResponse(rs))
	}
}

// importSkills fetches skills from a public GitHub repository. What was left
// out comes back in the report with a reason, not as a failure.
func (h *handler) importSkills() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToImportSkillsRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ImportSkills(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromImportSkillsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) deleteSkill() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteSkillRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeleteSkill(ctx, *rq); err != nil {
			return sendSkillError(c, err)
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

func (h *handler) listSkillShares() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToListSkillSharesRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListSkillShares(ctx, *rq)
		if err != nil {
			return sendSkillError(c, err)
		}
		return c.Send(mapper.FromListSkillSharesResponseEntityToHTTPResponse(rs))
	}
}

// shareSkill is idempotent: sharing a skill that is already shared there is
// not an error, since the state the caller asked for already holds.
func (h *handler) shareSkill() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToShareSkillRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.ShareSkill(ctx, *rq); err != nil {
			return sendSkillError(c, err)
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

func (h *handler) unshareSkill() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToShareSkillRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.UnshareSkill(ctx, *rq); err != nil {
			return sendSkillError(c, err)
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}
