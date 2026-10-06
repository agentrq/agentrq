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

// registerSkillRoutes serves the account's skills under /skills, and the same
// skills as one workspace sees them under /workspaces/:id/skills, where the
// switch is that workspace's own. Writing single files is left to the MCP
// tools; people import, delete and turn skills on and off.
func (h *handler) registerSkillRoutes(workspaces fiber.Router) {
	r := h.router.Group("/skills")
	r.Get("", h.searchSkills())
	r.Post("/import", h.importSkills())
	r.Get("/:name", h.getSkill())
	r.Patch("/:name", h.setSkillEnabled())
	r.Delete("/:name", h.deleteSkill())
	r.Get("/:name/files/*", h.getSkillFile())

	workspaces.Get("/:id/skills", h.searchSkills())
	workspaces.Get("/:id/skills/:name", h.getSkill())
	workspaces.Patch("/:id/skills/:name", h.setSkillEnabled())
	workspaces.Get("/:id/skills/:name/files/*", h.getSkillFile())
}

// skillStatus is the status a skill refusal is answered with. Its message is
// written for the caller and is passed on; any other error is not.
var skillStatus = map[crud.SkillErrorKind]int{
	crud.SkillInvalid:  http.StatusUnprocessableEntity,
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

// setSkillEnabled turns a skill on or off for agents: in every workspace
// under /skills, in one under /workspaces/:id/skills.
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
