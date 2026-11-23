// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package api

import (
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v3"
)

// registerTeamSkillRoutes serves team workspaces' skills: as one of the
// account's workspaces reaches them, where its owner opts in and out and
// gives a skill to the team, and on the team's page, where any member reads
// and writes them. Every check is the controller's; a team or workspace the
// caller is not in answers 404.
func (h *handler) registerTeamSkillRoutes(workspaces fiber.Router) {
	workspaces.Get("/:id/team-skills", h.listWorkspaceTeamSkills())
	workspaces.Put("/:id/team-skills/:skillId", h.setWorkspaceTeamSkill())
	workspaces.Post("/:id/skills/:name/give", h.giveSkillToTeam())

	r := h.router.Group("/teams/:teamId/team-workspaces/:teamWorkspaceId/skills")
	r.Get("", h.listTeamWorkspaceSkills())
	r.Post("", h.createTeamWorkspaceSkill())
	r.Post("/import", h.importTeamWorkspaceSkills())
	r.Get("/:skillId", h.getTeamWorkspaceSkill())
	r.Delete("/:skillId", h.deleteTeamWorkspaceSkill())
	r.Get("/:skillId/files/*", h.getTeamWorkspaceSkillFile())
	r.Put("/:skillId/files/*", h.saveTeamWorkspaceSkillFile())
	r.Delete("/:skillId/files/*", h.deleteTeamWorkspaceSkillFile())
}

// teamSkillRoute answers with fn's body, 400 when the request does not
// parse (fn's body and error both nil), and 204 for no body.
func teamSkillRoute(fn func(c fiber.Ctx) ([]byte, bool, error)) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		b, ok, err := fn(c)
		switch {
		case !ok:
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		case err != nil:
			return teamError(c, err)
		case b == nil:
			return c.SendStatus(fiber.StatusNoContent)
		}
		return c.Send(b)
	}
}

func (h *handler) listWorkspaceTeamSkills() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToWorkspaceTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListWorkspaceTeamSkills(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromWorkspaceTeamSkillsResponseEntityToHTTPResponse(rs), true, nil
	})
}

func (h *handler) setWorkspaceTeamSkill() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToSetWorkspaceTeamSkillRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.SetWorkspaceTeamSkill(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillEntityToHTTPResponse(rs), true, nil
	})
}

func (h *handler) giveSkillToTeam() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToGiveSkillToTeamRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GiveSkillToTeam(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillEntityToHTTPResponse(rs), true, nil
	})
}

func (h *handler) listTeamWorkspaceSkills() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListTeamSkills(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillEntitiesToHTTPResponse(rs), true, nil
	})
}

func (h *handler) getTeamWorkspaceSkill() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetTeamSkill(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillEntityToHTTPResponse(rs), true, nil
	})
}

func (h *handler) getTeamWorkspaceSkillFile() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetTeamSkillFile(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillFileResponseEntityToHTTPResponse(rs), true, nil
	})
}

// createTeamWorkspaceSkill and saveTeamWorkspaceSkillFile are one write: with
// no skill in the path, a SKILL.md creates one.
func (h *handler) createTeamWorkspaceSkill() fiber.Handler {
	return h.saveTeamWorkspaceSkillFile()
}

func (h *handler) saveTeamWorkspaceSkillFile() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToSaveTeamSkillFileRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.SaveTeamSkillFile(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillFileResponseEntityToHTTPResponse(rs), true, nil
	})
}

func (h *handler) deleteTeamWorkspaceSkillFile() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.DeleteTeamSkillFile(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromTeamSkillEntityToHTTPResponse(rs), true, nil
	})
}

func (h *handler) deleteTeamWorkspaceSkill() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, true, h.crud.DeleteTeamSkill(ctx, *rq)
	})
}

func (h *handler) importTeamWorkspaceSkills() fiber.Handler {
	return teamSkillRoute(func(c fiber.Ctx) ([]byte, bool, error) {
		rq := mapper.FromHTTPRequestToImportTeamSkillsRequestEntity(c)
		if rq == nil {
			return nil, false, nil
		}
		rq.UserID = sessionUser(c)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ImportTeamSkills(ctx, *rq)
		if err != nil {
			return nil, true, err
		}
		return mapper.FromImportTeamSkillsResponseEntityToHTTPResponse(rs), true, nil
	})
}
