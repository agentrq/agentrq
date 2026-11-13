// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

// registerTeamRoutes adds teams, their members and invitations, the
// account's own invitations, and the sharing of a workspace with a team.
func (h *handler) registerTeamRoutes() {
	r := h.router.Group("/teams")
	r.Get("", h.listTeams())
	r.Post("", h.createTeam())
	r.Get("/:teamId", h.getTeam())
	r.Patch("/:teamId", h.renameTeam())
	r.Delete("/:teamId", h.deleteTeam())
	r.Get("/:teamId/members", h.listTeamMembers())
	r.Patch("/:teamId/members/:userId", h.setTeamMemberRole())
	r.Delete("/:teamId/members/:userId", h.removeTeamMember())
	r.Post("/:teamId/leave", h.leaveTeam())
	r.Get("/:teamId/invitations", h.listTeamInvitations())
	r.Post("/:teamId/invitations", h.createTeamInvitation())
	r.Delete("/:teamId/invitations/:invitationId", h.revokeTeamInvitation())

	i := h.router.Group("/invitations")
	i.Get("", h.listMyInvitations())
	i.Post("/preview", h.previewInvitation())
	i.Post("/accept", h.acceptInvitation())
	i.Post("/:invitationId/accept", h.acceptInvitationByID())
	i.Post("/:invitationId/decline", h.declineInvitation())

	h.router.Get("/workspaces/:id/shares", h.listWorkspaceShares())
	h.router.Put("/workspaces/:id/shares/:teamId", h.shareWorkspace())
	h.router.Delete("/workspaces/:id/shares/:teamId", h.unshareWorkspace())
}

// teamError answers a refused team request.
func teamError(c *fiber.Ctx, err error) error {
	b, status := mapper.FromErrorToHTTPResponse(err)
	if status >= fiber.StatusInternalServerError {
		zlog.Error().Err(err).Msg("teams")
	}
	c.Set(_headerContentType, _mimeJSON)
	return c.Status(status).Send(b)
}

// pathIDs reads the named base62 ids from the path; false when one is not an
// id.
func pathIDs(c *fiber.Ctx, names ...string) ([]int64, bool) {
	ids := make([]int64, len(names))
	for i, name := range names {
		if ids[i] = monoflake.IDFromBase62(c.Params(name)).Int64(); ids[i] == 0 {
			return nil, false
		}
	}
	return ids, true
}

// teamRoute answers a request on the ids in its path, with fn's body or
// error. A path id that is not one answers 400.
func teamRoute(names []string, fn func(c *fiber.Ctx, ids []int64) ([]byte, error)) fiber.Handler {
	return func(c *fiber.Ctx) error {
		ids, ok := pathIDs(c, names...)
		if !ok {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		b, err := fn(c, ids)
		if err == errBadRequest {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		if err != nil {
			return teamError(c, err)
		}
		if b == nil {
			return c.SendStatus(fiber.StatusNoContent)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Send(b)
	}
}

// body reads the request's JSON body into v; errBadRequest when it is not
// JSON.
func body(c *fiber.Ctx, v any) error {
	if !mapper.FromHTTPRequestToTeamRequestView(c.Body(), v) {
		return errBadRequest
	}
	return nil
}

var (
	teamPathNone         = []string{}
	teamPathTeam         = []string{"teamId"}
	teamPathMember       = []string{"teamId", "userId"}
	teamPathInvitation   = []string{"teamId", "invitationId"}
	teamPathMyInvitation = []string{"invitationId"}
	teamPathShare        = []string{"id", "teamId"}
	teamPathWorkspace    = []string{"id"}
)

func (h *handler) listTeams() fiber.Handler {
	return teamRoute(teamPathNone, func(c *fiber.Ctx, _ []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		teams, err := h.teams.ListTeams(ctx, sessionUser(c))
		return mapper.FromTeamEntitiesToHTTPResponse(teams), err
	})
}

func (h *handler) createTeam() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var rq view.TeamNameRequest
		if body(c, &rq) != nil {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		team, err := h.teams.CreateTeam(ctx, sessionUser(c), rq.Name)
		if err != nil {
			return teamError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Status(fiber.StatusCreated).Send(mapper.FromTeamEntityToHTTPResponse(team))
	}
}

func (h *handler) getTeam() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		team, err := h.teams.GetTeam(ctx, sessionUser(c), ids[0])
		return mapper.FromTeamEntityToHTTPResponse(team), err
	})
}

func (h *handler) renameTeam() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		var rq view.TeamNameRequest
		if err := body(c, &rq); err != nil {
			return nil, err
		}
		ctx, cancel := newContext(c)
		defer cancel()
		team, err := h.teams.RenameTeam(ctx, sessionUser(c), ids[0], rq.Name)
		return mapper.FromTeamEntityToHTTPResponse(team), err
	})
}

func (h *handler) deleteTeam() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.DeleteTeam(ctx, sessionUser(c), ids[0])
	})
}

func (h *handler) listTeamMembers() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		members, err := h.teams.ListMembers(ctx, sessionUser(c), ids[0])
		return mapper.FromTeamMemberEntitiesToHTTPResponse(members), err
	})
}

func (h *handler) setTeamMemberRole() fiber.Handler {
	return teamRoute(teamPathMember, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		var rq view.TeamMemberRoleRequest
		if err := body(c, &rq); err != nil {
			return nil, err
		}
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.SetMemberRole(ctx, sessionUser(c), ids[0], ids[1], rq.Role)
	})
}

func (h *handler) removeTeamMember() fiber.Handler {
	return teamRoute(teamPathMember, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.RemoveMember(ctx, sessionUser(c), ids[0], ids[1])
	})
}

func (h *handler) leaveTeam() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.LeaveTeam(ctx, sessionUser(c), ids[0])
	})
}

func (h *handler) listTeamInvitations() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		invs, err := h.teams.ListTeamInvitations(ctx, sessionUser(c), ids[0])
		return mapper.FromTeamInvitationEntitiesToHTTPResponse(invs), err
	})
}

// createTeamInvitation answers 201 with the invitation, whether or not the
// address has an account.
func (h *handler) createTeamInvitation() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ids, ok := pathIDs(c, teamPathTeam...)
		var rq view.CreateTeamInvitationRequest
		if !ok || body(c, &rq) != nil {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.teams.CreateInvitation(ctx, sessionUser(c), ids[0], rq.Email, rq.Role)
		if err != nil {
			return teamError(c, err)
		}
		c.Set(_headerContentType, _mimeJSON)
		return c.Status(fiber.StatusCreated).Send(mapper.FromCreateTeamInvitationEntityToHTTPResponse(rs))
	}
}

func (h *handler) revokeTeamInvitation() fiber.Handler {
	return teamRoute(teamPathInvitation, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.RevokeInvitation(ctx, sessionUser(c), ids[0], ids[1])
	})
}

func (h *handler) listMyInvitations() fiber.Handler {
	return teamRoute(teamPathNone, func(c *fiber.Ctx, _ []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		invs, err := h.teams.ListMyInvitations(ctx, sessionUser(c))
		return mapper.FromTeamInvitationEntitiesToHTTPResponse(invs), err
	})
}

func (h *handler) previewInvitation() fiber.Handler {
	return teamRoute(teamPathNone, func(c *fiber.Ctx, _ []int64) ([]byte, error) {
		var rq view.TeamInvitationTokenRequest
		if err := body(c, &rq); err != nil {
			return nil, err
		}
		ctx, cancel := newContext(c)
		defer cancel()
		p, err := h.teams.PreviewInvitation(ctx, sessionUser(c), rq.Token)
		return mapper.FromTeamInvitationPreviewEntityToHTTPResponse(p), err
	})
}

func (h *handler) acceptInvitation() fiber.Handler {
	return teamRoute(teamPathNone, func(c *fiber.Ctx, _ []int64) ([]byte, error) {
		var rq view.TeamInvitationTokenRequest
		if err := body(c, &rq); err != nil {
			return nil, err
		}
		ctx, cancel := newContext(c)
		defer cancel()
		team, err := h.teams.AcceptInvitation(ctx, sessionUser(c), rq.Token)
		return mapper.FromTeamEntityToHTTPResponse(team), err
	})
}

func (h *handler) acceptInvitationByID() fiber.Handler {
	return teamRoute(teamPathMyInvitation, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		team, err := h.teams.AcceptInvitationByID(ctx, sessionUser(c), ids[0])
		return mapper.FromTeamEntityToHTTPResponse(team), err
	})
}

func (h *handler) declineInvitation() fiber.Handler {
	return teamRoute(teamPathMyInvitation, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.DeclineInvitation(ctx, sessionUser(c), ids[0])
	})
}

func (h *handler) listWorkspaceShares() fiber.Handler {
	return teamRoute(teamPathWorkspace, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		shares, err := h.teams.ListShares(ctx, sessionUser(c), ids[0])
		return mapper.FromWorkspaceShareEntitiesToHTTPResponse(shares), err
	})
}

func (h *handler) shareWorkspace() fiber.Handler {
	return teamRoute(teamPathShare, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		var rq view.WorkspaceShareRequest
		// No body shares under the workspace's name.
		if len(c.Body()) > 0 {
			if err := body(c, &rq); err != nil {
				return nil, err
			}
		}
		ctx, cancel := newContext(c)
		defer cancel()
		s, err := h.teams.ShareWorkspace(ctx, sessionUser(c), ids[0], ids[1], rq.Group)
		return mapper.FromWorkspaceShareEntityToHTTPResponse(s), err
	})
}

func (h *handler) unshareWorkspace() fiber.Handler {
	return teamRoute(teamPathShare, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		return nil, h.teams.UnshareWorkspace(ctx, sessionUser(c), ids[0], ids[1])
	})
}
