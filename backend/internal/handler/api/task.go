package api

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"strings"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

const (
	_routePathTasks      = "/workspaces/:id/tasks"
	_routePathTask       = "/workspaces/:id/tasks/:taskID"
	_routePathRespond    = "/workspaces/:id/tasks/:taskID/respond"
	_routePathReply      = "/workspaces/:id/tasks/:taskID/reply"
	_routePathStatus     = "/workspaces/:id/tasks/:taskID/status"
	_routePathOrder      = "/workspaces/:id/tasks/:taskID/order"
	_routePathScheduled  = "/workspaces/:id/tasks/:taskID/scheduled"
	_routePathPermission = "/workspaces/:id/tasks/:taskID/permission"
	_routePathEvents     = "/workspaces/:id/events"
	_routePathAttachment = "/workspaces/:id/attachments/:attachmentID"
)

func (h *handler) registerTaskRoutes() error {
	h.router.Get("/tasks", h.listTasks())
	h.router.Post(_routePathTasks, h.createTask())
	h.router.Get(_routePathTasks, h.listTasks())
	h.router.Get(_routePathTask, h.getTask())
	h.router.Post(_routePathRespond, h.respondToTask())
	h.router.Post(_routePathReply, h.replyToTask())
	h.router.Patch(_routePathStatus, h.updateTaskStatus())
	h.router.Patch(_routePathOrder, h.updateTaskOrder())
	h.router.Put(_routePathScheduled, h.updateScheduledTask())
	h.router.Post(_routePathPermission, h.sendPermissionVerdict())
	h.router.Delete(_routePathTask, h.deleteTask())
	h.router.Get(_routePathEvents, h.sseEvents())
	h.router.Get(_routePathAttachment, h.getAttachment())
	return nil
}

func (h *handler) createTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToCreateTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.CreateTask(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// If human created the task, notify the LLM via MCP channel
		// ONLY if status is NOT 'cron' (don't notify for template creation)
		if rq.Task.CreatedBy == "human" && rs.Task.Status != "cron" {
			srv := h.mcpManager.Get(rq.Task.WorkspaceID, rq.UserID)
			content := fmt.Sprintf("[Task %s] %s\n%s", monoflake.ID(rs.Task.ID).String(), rs.Task.Title, rs.Task.Body)
			srv.SendChannelNotification(ctx, rs.Task.ID, content)
		}

		// Push SSE event
		h.bus.Publish(rq.Task.WorkspaceID, eventbus.Event{
			Type:    "task.created",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateTaskResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) listTasks() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToListTasksRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ListTasks(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusOK)
		return c.Send(mapper.FromListTasksResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) getTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToGetTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.GetTask(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		c.Status(http.StatusOK)
		return c.Send(mapper.FromGetTaskResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) respondToTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToRespondToTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.RespondToTask(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Notify LLM of the human's response via MCP channel
		srv := h.mcpManager.Get(rq.WorkspaceID, rq.UserID)
		content := fmt.Sprintf("[Response to task %s] action=%s", monoflake.ID(rq.TaskID).String(), rq.Action)
		if rq.Text != "" {
			content += ": " + rq.Text
		}
		srv.SendChannelNotification(ctx, rq.TaskID, content)

		// Push SSE event to human subscribers (ack)
		h.bus.Publish(rq.WorkspaceID, eventbus.Event{
			Type:    "respond.ack",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromRespondToTaskResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) replyToTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToReplyToTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.ReplyToTask(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Push reply.received SSE event to human subscribers
		h.bus.Publish(rq.WorkspaceID, eventbus.Event{
			Type:    "reply.received",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromReplyToTaskResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateTaskStatus() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateTaskStatusRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateTaskStatus(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast status update
		h.bus.Publish(rq.WorkspaceID, eventbus.Event{
			Type:    "status.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateTaskStatusResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateTaskOrder() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateTaskOrderRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateTaskOrder(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast order update
		h.bus.Publish(rq.WorkspaceID, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateTaskOrderResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) deleteTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToDeleteTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		if _, err := h.crud.DeleteTask(ctx, *rq); err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast task deletion
		h.bus.Publish(rq.WorkspaceID, eventbus.Event{
			Type:    "task.deleted",
			Payload: map[string]string{"id": monoflake.ID(rq.TaskID).String()},
		})

		c.Status(http.StatusNoContent)
		return c.Send([]byte(""))
	}
}

// sseEvents streams real-time workspace events to the human UI.
// Implements the standard text/event-stream protocol.
func (h *handler) sseEvents() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceIDParam := c.Params("id")
		workspaceID := monoflake.IDFromBase62(workspaceIDParam).Int64()
		if workspaceID == 0 {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}

		c.Set(_headerContentType, _mimeEventStream)
		c.Set("Cache-Control", "no-cache")
		c.Set("Connection", "keep-alive")
		c.Set("X-Accel-Buffering", "no")

		ch := h.bus.Subscribe(workspaceID)

		// Use Fiber's streaming response
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer h.bus.Unsubscribe(workspaceID, ch)

			// Send a heartbeat comment to establish the stream
			_, _ = fmt.Fprintf(w, ": connected to workspace %s events\n\n", workspaceIDParam)
			_ = w.Flush()

			for {
				select {
				case data, ok := <-ch:
					if !ok {
						return
					}
					_, _ = w.Write(data)
					_ = w.Flush()
				case <-ctx.Done():
					return
				}
			}
		})
		return nil
	}
}
func (h *handler) getAttachment() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		attachmentID := c.Params("attachmentID")
		if workspaceID == 0 || attachmentID == "" {
			return c.Status(http.StatusUnprocessableEntity).Send(_invalidPayload)
		}

		userID := c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		res, err := h.crud.GetAttachment(ctx, entity.GetAttachmentRequest{
			WorkspaceID:  workspaceID,
			AttachmentID: attachmentID,
			UserID:       userID,
		})
		if err != nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": err.Error()})
		}

		c.Set("Content-Type", res.MimeType)
		c.Set("Content-Disposition", fmt.Sprintf("inline; filename=\"%s\"", res.Filename))
		return c.Send(res.Data)
	}
}

func (h *handler) sendPermissionVerdict() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var rq view.SendPermissionVerdictRequest
		if err := c.BodyParser(&rq); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid request payload"})
		}

		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		userID := c.Locals("user_id").(string)

		srv := h.mcpManager.Get(workspaceID, userID)
		if srv == nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "mcp server not found"})
		}

		if err := srv.SendPermissionVerdict(c.Context(), rq.RequestID, rq.Behavior); err != nil {
			if strings.Contains(err.Error(), "(expired)") {
				return c.Status(http.StatusGone).JSON(fiber.Map{"error": "This action request has expired (server was likely restarted). The agent must re-request this action."})
			}
			return c.Status(http.StatusInternalServerError).JSON(fiber.Map{"error": err.Error()})
		}

		return c.SendStatus(http.StatusOK)
	}
}

func (h *handler) updateScheduledTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateScheduledTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateScheduledTask(ctx, *rq)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast task update
		h.bus.Publish(rq.WorkspaceID, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateScheduledTaskResponseEntityToHTTPResponse(rs))
	}
}
