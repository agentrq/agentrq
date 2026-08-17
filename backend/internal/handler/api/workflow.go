package api

import (
	"errors"
	"net/http"

	_crud "github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/gofiber/fiber/v2"
)

// registerWorkflowRoutes mounts the experimental workflow graph API.
//
// These are Fiber routes only. The stdlib mux in app.go takes precedence for
// exact path matches, so registering any of these there would silently shadow
// the handler and hang the request — see the routing pitfall in AGENTS.md.
func (h *handler) registerWorkflowRoutes() {
	h.router.Post("/workflows", h.createWorkflow())
	h.router.Get("/workflows", h.listWorkflows())
	h.router.Get("/workflows/:id", h.getWorkflow())
	h.router.Patch("/workflows/:id", h.updateWorkflow())
	h.router.Delete("/workflows/:id", h.deleteWorkflow())
	h.router.Post("/workflows/:id/steps", h.createWorkflowStep())
	h.router.Get("/workflows/:id/steps", h.listWorkflowSteps())
	h.router.Delete("/workflows/:id/steps/:stepID", h.deleteWorkflowStep())
	h.router.Get("/workflows/:id/tasks", h.listTasksFromWorkflow())
	h.router.Get("/workflows/:id/text", h.getWorkflowText())
	h.router.Put("/workflows/:id/text", h.replaceWorkflowFromText())
}

func (h *handler) createWorkflow() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToCreateWorkflowRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.CreateWorkflow(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateWorkflowResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listWorkflows() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListWorkflows(ctx, entity.ListWorkflowsRequest{
			UserID: c.Locals("user_id").(string),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromListWorkflowsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) getWorkflow() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetWorkflowRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetWorkflow(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromGetWorkflowResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateWorkflow() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateWorkflowRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateWorkflow(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromUpdateWorkflowResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) deleteWorkflow() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteWorkflowRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeleteWorkflow(ctx, *rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusNoContent)
		return nil
	}
}

func (h *handler) createWorkflowStep() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToCreateWorkflowStepRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.CreateWorkflowStep(ctx, *rq)
		if err != nil {
			// A rejected cycle is the user drawing an invalid graph, not a
			// server fault: surface it as 409 so the editor can point at the
			// offending edge instead of showing a generic failure.
			if errors.Is(err, _crud.ErrWorkflowCycle) {
				e, _ := mapper.FromErrorToHTTPResponse(err)
				c.Status(http.StatusConflict)
				return c.Send(e)
			}
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateWorkflowStepResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listWorkflowSteps() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToListWorkflowStepsRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListWorkflowSteps(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromListWorkflowStepsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) deleteWorkflowStep() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteWorkflowStepRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeleteWorkflowStep(ctx, *rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusNoContent)
		return nil
	}
}

func (h *handler) getWorkflowText() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetWorkflowTextRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetWorkflowText(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromGetWorkflowTextResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) replaceWorkflowFromText() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToReplaceWorkflowFromTextRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ReplaceWorkflowFromText(ctx, *rq)
		if err != nil {
			// A malformed document is the user mistyping, not a server fault.
			// 422 with the line number lets the editor mark the exact row.
			var textErr *_crud.WorkflowTextError
			if errors.As(err, &textErr) {
				c.Status(http.StatusUnprocessableEntity)
				return c.Send(mapper.FromWorkflowTextErrorToHTTPResponse(textErr.Msg, textErr.Line))
			}
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromReplaceWorkflowFromTextResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listTasksFromWorkflow() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToListTasksFromWorkflowRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListTasksFromWorkflow(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromListTasksFromWorkflowResponseEntityToHTTPResponse(rs))
	}
}
