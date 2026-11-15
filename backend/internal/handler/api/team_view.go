// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"strconv"
	"strings"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// registerTeamViewRoutes adds team mode: a team's shared work, read-only,
// and the two writes it allows. Each answers 404 to anyone not in the
// team, and for anything in a workspace the team no longer reads.
func (h *handler) registerTeamViewRoutes() {
	r := h.router.Group("/teams/:teamId")
	r.Get("/workspaces", h.listTeamWorkspaces())
	r.Get("/tasks", h.listTeamTasks())
	r.Get("/analytics", h.getTeamStats())
	r.Get("/tasks/:taskId", h.getTeamTask())
	r.Get("/tasks/:taskId/attachments/:attachmentId", h.getTeamAttachment())
	r.Post("/tasks/:taskId/clone", h.cloneTeamTask())
	r.Get("/pano", h.listTeamPanoDocs())
	r.Get("/pano/:docId", h.getTeamPanoDoc())
	r.Get("/pano/:docId/files/:fileId", h.getTeamPanoFile())
	r.Get("/skills", h.listTeamSkills())
	r.Get("/skills/:name", h.getTeamSkill())
	r.Post("/skills/:name/add", h.addTeamSkill())
	h.router.Get("/workspaces/:id/teammate-memories", h.listTeammateMemories())
	h.router.Put("/workspaces/:id/teammate-memories/:userId", h.setTeammateMemory())
}

var (
	teamPathTask     = []string{"teamId", "taskId"}
	teamPathPanoDoc  = []string{"teamId", "docId"}
	teamPathPanoFile = []string{"teamId", "docId", "fileId"}
)

func (h *handler) listTeamWorkspaces() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		ws, err := h.teams.ListTeamWorkspaces(ctx, sessionUser(c), ids[0])
		return mapper.FromTeamWorkspaceEntitiesToHTTPResponse(ws), err
	})
}

// teamTasksPage is the page a team's task list asks for: 10 tasks unless it
// says, and never more than 50, as for the account's own list.
func teamTasksPage(c *fiber.Ctx) (limit, offset int) {
	limit, _ = strconv.Atoi(c.Query("limit"))
	if limit <= 0 {
		limit = 10
	}
	offset, _ = strconv.Atoi(c.Query("offset"))
	return min(limit, 50), max(offset, 0)
}

func (h *handler) listTeamTasks() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		rq := entity.ListTeamTasksRequest{UserID: sessionUser(c), TeamID: ids[0], Workspace: c.Query("workspace")}
		if s := c.Query("status"); s != "" {
			rq.Status = strings.Split(s, ",")
		}
		if m := c.Query("member"); m != "" {
			if rq.Member = monoflake.IDFromBase62(m).Int64(); rq.Member == 0 {
				return nil, errBadRequest
			}
		}
		rq.Limit, rq.Offset = teamTasksPage(c)
		ctx, cancel := newContext(c)
		defer cancel()
		tasks, err := h.teamView.ListTasks(ctx, rq)
		return mapper.FromTeamTaskEntitiesToHTTPResponse(tasks, ids[0]), err
	})
}

// getTeamStats is a workspace's analytics, in its shape, summed over the
// team's workspaces or over the team workspace ?workspace= names.
func (h *handler) getTeamStats() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		from, _ := strconv.ParseInt(c.Query("from"), 10, 64)
		to, _ := strconv.ParseInt(c.Query("to"), 10, 64)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.teamView.GetStats(ctx, entity.GetTeamStatsRequest{
			UserID: sessionUser(c), TeamID: ids[0], Workspace: c.Query("workspace"), Range: c.Query("range", "7d"), From: from, To: to,
		})
		if err != nil {
			return nil, err
		}
		return mapper.FromTeamStatsEntityToHTTPResponse(rs), nil
	})
}

func (h *handler) getTeamTask() fiber.Handler {
	return teamRoute(teamPathTask, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		d, err := h.teamView.GetTask(ctx, sessionUser(c), ids[0], ids[1])
		return mapper.FromTeamTaskDetailEntityToHTTPResponse(d, ids[0]), err
	})
}

// getTeamAttachment serves a file of a teammate's task as the owner's own
// route does.
func (h *handler) getTeamAttachment() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ids, ok := pathIDs(c, teamPathTask...)
		if !ok || c.Params("attachmentId") == "" {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		res, err := h.teamView.GetAttachment(ctx, sessionUser(c), ids[0], ids[1], c.Params("attachmentId"))
		if err != nil {
			return teamError(c, err)
		}
		c.Set(fiber.HeaderContentDisposition, contentDisposition(sandboxFile(c, res.MimeType), res.Filename))
		return c.Send(res.Data)
	}
}

func (h *handler) cloneTeamTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ids, ok := pathIDs(c, teamPathTask...)
		var rq view.CloneTeamTaskRequest
		if !ok || body(c, &rq) != nil {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		workspaceID := monoflake.IDFromBase62(rq.WorkspaceID).Int64()
		if workspaceID == 0 {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		userID := sessionUser(c)
		rs, err := h.teamView.CloneTask(ctx, entity.CloneTeamTaskRequest{
			UserID: userID, TeamID: ids[0], TaskID: ids[1], WorkspaceID: workspaceID, Assignee: rq.Assignee,
		})
		if err != nil {
			return teamError(c, err)
		}
		h.offerHumanTask(ctx, monoflake.ID(userID).String(), rs.Task)
		c.Set(_headerContentType, _mimeJSON)
		return c.Status(fiber.StatusCreated).Send(mapper.FromCreateTaskResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listTeamSkills() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		ss, err := h.teamView.ListSkills(ctx, sessionUser(c), ids[0], c.Query("workspace"))
		return mapper.FromTeamSkillEntitiesToHTTPResponse(ss), err
	})
}

func (h *handler) getTeamSkill() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		s, err := h.teamView.GetSkill(ctx, sessionUser(c), ids[0], c.Query("workspace"), c.Params("name"))
		return mapper.FromTeamSkillEntityToHTTPResponse(s), err
	})
}

func (h *handler) addTeamSkill() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		var rq view.AddTeamSkillRequest
		if err := body(c, &rq); err != nil {
			return nil, err
		}
		workspaceIDs := make([]int64, len(rq.WorkspaceIDs))
		for i, w := range rq.WorkspaceIDs {
			if workspaceIDs[i] = monoflake.IDFromBase62(w).Int64(); workspaceIDs[i] == 0 {
				return nil, errBadRequest
			}
		}
		ctx, cancel := newContext(c)
		defer cancel()
		s, err := h.teamView.AddSkill(ctx, sessionUser(c), ids[0], c.Query("workspace"), c.Params("name"), workspaceIDs, rq.Replace)
		return mapper.FromAddTeamSkillResponseEntityToHTTPResponse(s), err
	})
}

var teamPathTeammate = []string{"id", "userId"}

func (h *handler) listTeammateMemories() fiber.Handler {
	return teamRoute(teamPathWorkspace, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		ms, err := h.teamView.ListTeammateMemories(ctx, sessionUser(c), ids[0])
		return mapper.FromTeammateMemoryEntitiesToHTTPResponse(ms), err
	})
}

func (h *handler) setTeammateMemory() fiber.Handler {
	return teamRoute(teamPathTeammate, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		var rq view.SetTeammateMemoryRequest
		if err := body(c, &rq); err != nil || rq.Included == nil {
			return nil, errBadRequest
		}
		ctx, cancel := newContext(c)
		defer cancel()
		m, err := h.teamView.SetTeammateMemory(ctx, sessionUser(c), ids[0], ids[1], *rq.Included)
		return mapper.FromTeammateMemoryEntityToHTTPResponse(m), err
	})
}

func (h *handler) listTeamPanoDocs() fiber.Handler {
	return teamRoute(teamPathTeam, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		docs, err := h.teamView.ListPanoDocs(ctx, sessionUser(c), ids[0])
		return mapper.FromTeamPanoDocEntitiesToHTTPResponse(docs), err
	})
}

func (h *handler) getTeamPanoDoc() fiber.Handler {
	return teamRoute(teamPathPanoDoc, func(c *fiber.Ctx, ids []int64) ([]byte, error) {
		ctx, cancel := newContext(c)
		defer cancel()
		doc, err := h.teamView.GetPanoDoc(ctx, sessionUser(c), ids[0], ids[1])
		return mapper.FromTeamPanoDocEntityToHTTPResponse(doc), err
	})
}

// getTeamPanoFile serves a shared doc's file, sandboxed as a task's
// attachment is.
func (h *handler) getTeamPanoFile() fiber.Handler {
	return func(c *fiber.Ctx) error {
		ids, ok := pathIDs(c, teamPathPanoFile...)
		if !ok {
			return c.Status(fiber.StatusBadRequest).Send(_invalidPayload)
		}
		ctx, cancel := newContext(c)
		defer cancel()
		res, err := h.teamView.GetPanoFile(ctx, sessionUser(c), ids[0], ids[1], ids[2])
		if err != nil {
			return teamError(c, err)
		}
		c.Set(fiber.HeaderContentDisposition, contentDisposition(sandboxFile(c, res.MimeType), res.Filename))
		return c.Send(res.Data)
	}
}
