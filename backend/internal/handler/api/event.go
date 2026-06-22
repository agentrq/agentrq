package api

import (
	"net/http"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func (h *handler) registerEventRoutes() {
	h.router.Post("/events", h.createEvent())
	h.router.Get("/events", h.listEvents())
	h.router.Get("/events/:id", h.getEvent())
	h.router.Patch("/events/:id", h.updateEvent())
	h.router.Delete("/events/:id", h.deleteEvent())
	h.router.Post("/events/:id/triggers", h.createEventTrigger())
	h.router.Get("/events/:id/triggers", h.listEventTriggers())
	h.router.Delete("/events/:id/triggers/:triggerID", h.deleteEventTrigger())
	h.router.Get("/events/:id/tasks", h.listTasksFromEvent())
}

func (h *handler) createEvent() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToCreateEventRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.CreateEvent(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateEventResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listEvents() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListEvents(ctx, entity.ListEventsRequest{
			UserID: c.Locals("user_id").(string),
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromListEventsResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) getEvent() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetEventRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetEvent(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromGetEventResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateEvent() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateEventRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateEvent(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromUpdateEventResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) deleteEvent() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteEventRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeleteEvent(ctx, *rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

func (h *handler) createEventTrigger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToCreateEventTriggerRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.CreateEventTrigger(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateEventTriggerResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listEventTriggers() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		eventID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if eventID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		// Verify event ownership before listing triggers.
		if _, err := h.crud.GetEvent(ctx, entity.GetEventRequest{ID: eventID, UserID: userID}); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		rs, err := h.crud.ListEventTriggers(ctx, entity.ListEventTriggersRequest{
			EventID: eventID,
			UserID:  userID,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromListEventTriggersResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) deleteEventTrigger() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteEventTriggerRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if err := h.crud.DeleteEventTrigger(ctx, *rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

func (h *handler) listTasksFromEvent() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		eventID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if eventID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		// Verify event ownership before listing resulting tasks.
		if _, err := h.crud.GetEvent(ctx, entity.GetEventRequest{ID: eventID, UserID: userID}); err != nil {
			if err == base.ErrNotFound {
				c.Status(http.StatusNotFound)
				e, _ := mapper.FromErrorToHTTPResponse(err)
				return c.Send(e)
			}
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		rs, err := h.crud.ListTasksFromEvent(ctx, entity.ListTasksFromEventRequest{
			EventID: eventID,
			UserID:  userID,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.Send(mapper.FromListTasksFromEventResponseEntityToHTTPResponse(rs))
	}
}
