// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package api

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	mcpctrl "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/eventinstruction"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

const (
	_routePathTasks       = "/workspaces/:id/tasks"
	_routePathTask        = "/workspaces/:id/tasks/:taskID"
	_routePathRespond     = "/workspaces/:id/tasks/:taskID/respond"
	_routePathFork        = "/workspaces/:id/tasks/:taskID/fork"
	_routePathReply       = "/workspaces/:id/tasks/:taskID/reply"
	_routePathStatus      = "/workspaces/:id/tasks/:taskID/status"
	_routePathOrder       = "/workspaces/:id/tasks/:taskID/order"
	_routePathScheduled   = "/workspaces/:id/tasks/:taskID/scheduled"
	_routePathAssignee    = "/workspaces/:id/tasks/:taskID/assignee"
	_routePathWorkspace   = "/workspaces/:id/tasks/:taskID/workspace"
	_routePathAllowAll    = "/workspaces/:id/tasks/:taskID/allow_all"
	_routePathPermission  = "/workspaces/:id/tasks/:taskID/permission"
	_routePathElicitation = "/workspaces/:id/tasks/:taskID/elicitation"
	_routePathStop        = "/workspaces/:id/tasks/:taskID/stop"
	_routePathEvents      = "/workspaces/:id/events"
	_routePathAttachment  = "/workspaces/:id/tasks/:taskID/attachments/:attachmentID"
	_routePathCounts      = "/workspaces/:id/tasks/counts"
)

func (h *handler) registerTaskRoutes() error {
	h.router.Get("/tasks", h.listTasks())
	h.router.Get("/tasks/stats", h.getGlobalTaskStats())
	h.router.Post(_routePathTasks, h.createTask())
	h.router.Get(_routePathTasks, h.listTasks())
	h.router.Get(_routePathCounts, h.getWorkspaceTaskCounts())
	h.router.Get(_routePathTask, h.getTask())
	h.router.Post(_routePathRespond, h.respondToTask())
	h.router.Post(_routePathFork, h.forkTask())
	h.router.Post(_routePathReply, h.replyToTask())
	h.router.Patch(_routePathStatus, h.updateTaskStatus())
	h.router.Patch(_routePathOrder, h.updateTaskOrder())
	h.router.Patch(_routePathAssignee, h.updateTaskAssignee())
	h.router.Patch(_routePathWorkspace, h.moveTask())
	h.router.Patch(_routePathAllowAll, h.updateTaskAllowAllCommands())
	h.router.Put(_routePathScheduled, h.updateScheduledTask())
	h.router.Post(_routePathPermission, h.sendPermissionVerdict())
	h.router.Post(_routePathElicitation, h.respondToElicitation())
	h.router.Post(_routePathStop, h.stopTask())
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
			zlog.Error().Err(err).Msg("Failed to fetch attachment")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// If human created the task, notify the LLM via MCP channel
		// ONLY if status is NOT 'cron' (don't notify for template creation)
		if rq.Task.CreatedBy == "human" && rs.Task.Status != "cron" {
			if h.agentHasRoom(ctx, rq.Task.WorkspaceID, rq.UserID, rs.Task.ID) {
				h.pushTaskToAgent(ctx, rq.UserID, rs.Task)
			}
		}

		// Push SSE event
		h.bus.Publish(rq.Task.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.created",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusCreated)
		return c.Send(mapper.FromCreateTaskResponseEntityToHTTPResponse(rs))
	}
}

// agentHasRoom reports whether the workspace's agent is running fewer tasks
// than it will run at once, not counting skipID.
func (h *handler) agentHasRoom(ctx context.Context, workspaceID int64, userID string, skipID int64) bool {
	// Held back only while the agent is actually working. A task
	// already waiting in the queue is not a reason to withhold this
	// one: the agent may never pick that one up, and this used to mean
	// a task created behind it was never pushed at all — not by this
	// handler, which skipped it, and not by StartPoller, which offered
	// the queue's oldest task and only that one. Between them a single
	// task the agent ignored hid every task created after it.
	// Held back only when the agent is already running as many tasks
	// as it will run at once. Asked of the database as the counting
	// question it is, bounded by that limit: listing the workspace
	// unfiltered pulled up to a hundred whole tasks — bodies,
	// responses, and a JSON unmarshal of every task's attachments —
	// on every task creation, to find out whether one row existed.
	// Workspace, user and status are exactly the columns
	// idx_tasks_dequeue covers.
	//
	// That call was also wrong, not merely wasteful: its implicit
	// hundred-row limit took the *most recent* tasks, so an ongoing
	// task older than those was invisible and the push went out as
	// though the agent were idle.
	//
	// One row more than the limit is asked for because the task just
	// created is skipped below — a task created directly as ongoing
	// sorts first on this query's `updated_at desc` and would
	// otherwise fill a slot in the answer.
	limit := agentTaskConcurrency(h.mcpManager, workspaceID)
	listRs, listErr := h.crud.ListTasks(ctx, entity.ListTasksRequest{
		WorkspaceID: workspaceID,
		UserID:      userID,
		Status:      []string{"ongoing"},
		Limit:       limit + 1,
	})
	ongoing := 0
	if listErr == nil {
		for _, t := range listRs.Tasks {
			if t.ID == skipID {
				continue // skip the newly created task itself
			}
			ongoing++
		}
	}
	// A listing that failed leaves the count at zero and the task is
	// pushed. Withholding work from an agent because a count could
	// not be taken is the worse of the two mistakes: the poller
	// re-offers a task the agent was not ready for, and nothing
	// re-offers one that was never sent.
	return ongoing < limit
}

// pushTaskToAgent hands a task to the workspace's agent now, rather than on
// the poller's next tick.
func (h *handler) pushTaskToAgent(ctx context.Context, userID string, t entity.Task) {
	srv := h.mcpManager.Get(t.WorkspaceID, userID)
	content := fmt.Sprintf("[Task %s] %s\n%s", monoflake.ID(t.ID).String(), t.Title, t.Body)
	if atts := formatAttachments(t.Attachments); atts != "" {
		content += "\n" + atts
	}
	// A task bound to a workflow already carries it on the task row, so
	// the instruction only has to name the task: publishing with that
	// ID starts the run explicitly rather than by inference.
	if t.EventID != 0 {
		if ev, evErr := h.crud.GetEvent(ctx, entity.GetEventRequest{ID: t.EventID, UserID: userID}); evErr == nil {
			content += eventinstruction.Build(eventinstruction.Params{
				EventName:         ev.Event.Name,
				TaskID:            monoflake.ID(t.ID).String(),
				PayloadGuidelines: ev.Event.PayloadGuidelines,
			})
		} else {
			zlog.Warn().Err(evErr).Int64("eventID", t.EventID).Int64("taskID", t.ID).
				Msg("failed to resolve linked event, on-completion publishEvent instruction omitted")
		}
	}
	// This is the same "hand the agent its next task" push StartPoller
	// makes, only immediate rather than on its next tick — so it must
	// clear first for the same reason the poller does: the agent has
	// to read the task on a clean context, and clearing after it has
	// already been handed the task would throw the task away.
	//
	// Nothing records the push. The poller goes on offering the task
	// until the agent moves it to ongoing, which is what makes a push
	// made while nothing was attached recoverable. The clear is the
	// half that must not repeat, and clearContextFor remembers it.
	srv.ClearContextForTask(ctx, t.ID, t.ClearContext)
	srv.SendChannelNotification(ctx, t.ID, content)
}

// agentTaskConcurrency is how many tasks the workspace's connected agent will
// run at once, as the gateway itself last reported it.
//
// One when nothing has reported a limit — a workspace with nothing attached,
// or a gateway too old to say. Reading silence as "as many as you like" would
// hand a queue to an agent that can only take one, and there is no way to take
// that back; reading it as one costs at most a minute, because the poller
// offers the rest as soon as a slot frees.
func agentTaskConcurrency(m mcpManager, workspaceID int64) int {
	if m == nil {
		return 1
	}
	if c := m.AgentConcurrency(workspaceID); c != nil && c.MaxConcurrency > 0 {
		return c.MaxConcurrency
	}
	return 1
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
			zlog.Error().Err(err).Msg("Failed to get task")
			c.Set(_headerContentType, _mimeJSON)
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
			zlog.Error().Err(err).Msg("Failed to respond to task")
			c.Set(_headerContentType, _mimeJSON)
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
		if atts := formatAttachments(rq.Attachments); atts != "" {
			content += "\n" + atts
		}
		srv.SendChannelNotification(ctx, rq.TaskID, content)

		// Push SSE event to human subscribers (ack)
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "respond.ack",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromRespondToTaskResponseEntityToHTTPResponse(rs))
	}
}

// forkTask copies a conversation, up to one of its messages, into a new task.
//
// The fork is ongoing and handed to the agent straight away when the agent has
// room for it. When it has not — usually because the task being forked is the
// one it is working on — the fork waits as notstarted: the poller only offers
// notstarted tasks, so an ongoing fork nobody pushed would never be delivered.
func (h *handler) forkTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToForkTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		room := h.agentHasRoom(ctx, rq.WorkspaceID, rq.UserID, 0)
		rq.Status = "notstarted"
		if room {
			rq.Status = "ongoing"
		}
		rs, err := h.crud.ForkTask(ctx, *rq)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to fork task")
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		if room {
			h.pushTaskToAgent(ctx, rq.UserID, rs.Task)
		}

		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.created",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusCreated)
		return c.Send(mapper.FromForkTaskResponseEntityToHTTPResponse(rs))
	}
}

// replyChannelContent composes what the agent is told when a human replies.
//
// Ordinary text is wrapped in an envelope naming the task, which is what gives
// an agent reading a stream of notifications the context to answer in.
//
// A slash command is not wrapped. ACP runs a command as ordinary prompt text
// and the agent matches the command at the *start* of what it is given (see the
// spec's "Running commands"), so `[Reply to task 0iOq7fDWLPl] /compact` is not a
// command at all — it is a sentence mentioning one. Nothing is lost by dropping
// the envelope: the gateway takes the task from the notification's `chat_id`
// metadata, never from the prose.
//
// The text has to name a command the agent actually advertised. That is the
// whole guard: without it `/Users/mt/thing is broken` or `/etc/hosts looks
// wrong` would be delivered stripped of their context on the strength of a
// leading slash. An agent that advertises nothing — anything that is not an ACP
// agent — therefore behaves exactly as it did before.
func replyChannelContent(
	taskID int64,
	text string,
	attachments []entity.Attachment,
	commands *mcpctrl.AgentCommandsSnapshot,
) string {
	// Leading whitespace is trimmed rather than disqualifying: the point is for
	// the command to lead the prompt, and " /compact" would defeat that while
	// plainly meaning the same thing.
	trimmed := strings.TrimLeft(text, " \t\r\n")

	content := fmt.Sprintf("[Reply to task %s] %s", monoflake.ID(taskID).String(), text)
	if isAdvertisedCommand(trimmed, commands) {
		content = trimmed
	}

	// Attachments are listed after the message either way. They are their own
	// lines, so they never come between the command and the start of the
	// prompt.
	if atts := formatAttachments(attachments); atts != "" {
		content += "\n" + atts
	}
	return content
}

// isAdvertisedCommand reports whether the text opens with a slash command the
// connected agent said it accepts.
func isAdvertisedCommand(text string, commands *mcpctrl.AgentCommandsSnapshot) bool {
	if commands == nil || !strings.HasPrefix(text, "/") {
		return false
	}

	// Everything up to the first whitespace, so "/web agent protocol" is the
	// "web" command carrying an argument.
	name := strings.TrimPrefix(text, "/")
	if i := strings.IndexAny(name, " \t\r\n"); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return false
	}

	for _, c := range commands.Commands {
		if c.Name == name {
			return true
		}
	}
	return false
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
			zlog.Error().Err(err).Msg("Failed to reply to task")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Notify LLM of the human's reply via MCP channel
		srv := h.mcpManager.Get(rq.WorkspaceID, rq.UserID)
		content := replyChannelContent(rq.TaskID, rq.Text, rq.Attachments, h.mcpManager.AgentCommands(rq.WorkspaceID))
		srv.SendChannelNotification(ctx, rq.TaskID, content)

		// Push reply.received SSE event to human subscribers
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
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
			zlog.Error().Err(err).Msg("Failed to update task status")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast status update
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
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
			zlog.Error().Err(err).Msg("Failed to update task order")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast order update
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateTaskOrderResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateTaskAssignee() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateTaskAssigneeRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateTaskAssignee(ctx, *rq)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to update task assignee")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast task update
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		// Notify agent if reassigned to agent
		if rq.Assignee == "agent" {
			srv := h.mcpManager.Get(rq.WorkspaceID, rq.UserID)
			content := fmt.Sprintf("[Task reassigned to agent] %s", rs.Task.Title)
			// Same reasoning as the immediate push in createTask: clear before
			// pushing, and leave the push itself unrecorded so StartPoller goes
			// on offering the task until the agent takes it.
			srv.ClearContextForTask(ctx, rs.Task.ID, rs.Task.ClearContext)
			srv.SendChannelNotification(ctx, rs.Task.ID, content)
		}

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateTaskAssigneeResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) moveTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToMoveTaskRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.MoveTask(ctx, *rq)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to move task")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// The task leaves the source workspace and appears in the destination one.
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.deleted",
			Payload: map[string]string{"id": monoflake.ID(rq.TaskID).String()},
		})
		h.bus.Publish(rq.DestinationWorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.created",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromMoveTaskResponseEntityToHTTPResponse(rs))
	}
}

func (h *handler) updateTaskAllowAllCommands() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		rq := mapper.FromHTTPRequestToUpdateTaskAllowAllCommandsRequestEntity(c)
		if rq == nil {
			c.Status(http.StatusUnprocessableEntity)
			return c.Send(_invalidPayload)
		}
		rq.UserID = c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()
		rs, err := h.crud.UpdateTaskAllowAllCommands(ctx, *rq)
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to update task allow all")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast task update
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateTaskAllowAllCommandsResponseEntityToHTTPResponse(rs))
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
			zlog.Error().Err(err).Msg("Failed to delete task")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast task deletion
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
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

		userID := c.Locals("user_id").(string)

		// Verify workspace access
		// Use request context for the immediate authorization check
		authCtx, cancelAuth := newContext(c)
		defer cancelAuth()
		if ok, err := h.crud.CheckWorkspaceAccess(authCtx, workspaceID, userID); err != nil || !ok {
			return c.Status(http.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}

		ch := h.bus.Subscribe(workspaceID, userID)

		// Use Fiber's streaming response
		c.Context().SetBodyStreamWriter(func(w *bufio.Writer) {
			// Inside the stream writer, create a long-lived context
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			defer h.bus.Unsubscribe(workspaceID, userID, ch)

			// Send a heartbeat comment to establish the stream
			_, _ = fmt.Fprintf(w, ": connected to workspace %s events\n\n", workspaceIDParam)
			_ = w.Flush()

			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()

			for {
				select {
				case data, ok := <-ch:
					if !ok {
						return
					}
					_, _ = w.Write(data)
					_ = w.Flush()
				case <-ticker.C:
					_, _ = w.Write([]byte(": agentrq\n\n"))
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
		taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
		attachmentID := c.Params("attachmentID")
		if workspaceID == 0 || taskID == 0 || attachmentID == "" {
			return c.Status(http.StatusUnprocessableEntity).Send(_invalidPayload)
		}

		userID := c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		res, err := h.crud.GetAttachment(ctx, entity.GetAttachmentRequest{
			WorkspaceID:  workspaceID,
			TaskID:       taskID,
			AttachmentID: attachmentID,
			UserID:       userID,
		})
		if err != nil {
			zlog.Error().Err(err).Msg("Failed to fetch attachment")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		c.Set("Content-Type", res.MimeType)
		c.Set("Content-Disposition", contentDisposition(res.Filename))
		return c.Send(res.Data)
	}
}

// contentDisposition builds a Content-Disposition value that is safe to put in
// an HTTP header.
//
// Header values are US-ASCII. Filenames are not: a macOS screenshot is named
// "Screenshot 2026-08-30 at 5.51.27 PM.png", where the space before PM is
// U+202F NARROW NO-BREAK SPACE. Interpolating that straight into the header
// emitted a multi-byte character in a place the protocol does not allow one,
// and the desktop app's HTTP stack rejects such a header outright rather than
// mangling it -- taking down its main process with
//
//	TypeError: Cannot convert argument to a ByteString because the character
//	at index 50 has a value of 8239 which is greater than 255
//
// Browsers were more forgiving and showed a mis-decoded name, so this only ever
// looked like a desktop bug.
//
// Both forms are emitted, as RFC 6266 section 5 recommends: a plain ASCII
// filename any client can read, and the exact name in the extended form for
// clients that understand it. Building the quoted string by hand also closes
// the injection the old fmt.Sprintf allowed, where a filename containing a
// quote could end the parameter early and append parameters of its own.
func contentDisposition(filename string) string {
	fallback := asciiFilename(filename)
	if fallback == "" {
		fallback = "download"
	}

	disposition := `inline; filename="` + fallback + `"`
	// Only worth the extended form when there is a real name that the ASCII
	// fallback does not already carry exactly.
	if filename != "" && fallback != filename {
		disposition += "; filename*=UTF-8''" + rfc5987Encode(filename)
	}
	return disposition
}

// asciiFilename reduces a filename to what may appear inside a quoted header
// value: printable US-ASCII, with anything else replaced rather than dropped so
// the name keeps its shape. The quote and backslash go too -- one would end the
// string early, the other escape what follows -- as do control characters,
// since a carriage return would split the header in two.
func asciiFilename(name string) string {
	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		switch {
		case r < 0x20 || r > 0x7e, r == '"', r == '\\':
			b.WriteByte('_')
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// rfc5987Encode percent-encodes the UTF-8 bytes of s, leaving only the
// attr-char set from RFC 5987 section 3.2.1 untouched.
func rfc5987Encode(s string) string {
	const hex = "0123456789ABCDEF"

	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			strings.IndexByte("!#$&+-.^_`|~", c) >= 0:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&0x0f])
		}
	}
	return b.String()
}

func (h *handler) sendPermissionVerdict() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var rq view.SendPermissionVerdictRequest
		if err := c.BodyParser(&rq); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid request payload"})
		}

		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		userID := c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if ok, err := h.crud.CheckWorkspaceAccess(ctx, workspaceID, userID); err != nil || !ok {
			return c.Status(http.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}

		srv := h.mcpManager.Get(workspaceID, userID)
		if srv == nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "mcp server not found"})
		}

		var taskID int64
		if idParam := c.Params("taskID"); idParam != "" {
			if id := monoflake.IDFromBase62(idParam); id != 0 {
				taskID = id.Int64()
			}
		}

		if err := srv.SendPermissionVerdictFrom(c.Context(), taskID, rq.RequestID, rq.Behavior, rq.DecidedBy); err != nil {
			if strings.Contains(err.Error(), "(expired)") {
				return c.Status(http.StatusGone).JSON(fiber.Map{"error": "This action request has expired (server was likely restarted). The agent must re-request this action."})
			}
			// Told apart from a server-side failure: both of these mean the
			// request was malformed, and answering 500 would have the desktop
			// app retrying something that can never succeed.
			if errors.Is(err, mcpctrl.ErrBadDecider) || errors.Is(err, mcpctrl.ErrExtensionCannotRemember) {
				return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": err.Error()})
			}
			zlog.Error().Err(err).Msg("Failed to send permission verdict")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		return c.SendStatus(http.StatusOK)
	}
}

// stopTask asks whatever agent is working on a task to stop.
//
// Nothing about the task itself changes: this is a message to the agent, and
// the agent decides what its own state becomes.
//
// Only an agent that can actually be stopped is asked. Stopping travels over a
// notification this server invents, and a client not built to listen for it —
// Claude Code speaking MCP directly, for one — would drop it without a word.
// Such an agent is instead refused the approval it is standing at, which is as
// close to a stop as it can be brought.
func (h *handler) stopTask() fiber.Handler {
	return func(c *fiber.Ctx) error {
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		userID := c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if ok, err := h.crud.CheckWorkspaceAccess(ctx, workspaceID, userID); err != nil || !ok {
			return c.Status(http.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}

		taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
		if taskID == 0 {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid task id"})
		}

		srv := h.mcpManager.Get(workspaceID, userID)
		if srv == nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "mcp server not found"})
		}

		// How much of a stop was possible depends on what is connected, and
		// the human is told which it was: reporting a plain success for an
		// agent that never stopped would leave them believing a task had
		// ended while it is still running.
		outcome := srv.SendCancelNotification(c.Context(), taskID)
		if !outcome.Acted() {
			return c.Status(http.StatusConflict).JSON(fiber.Map{
				"error": "the connected agent does not support being stopped",
			})
		}
		return c.Status(http.StatusOK).JSON(fiber.Map{
			"stopped":         outcome.Stopped,
			"approvalsDenied": outcome.ApprovalsDenied,
		})
	}
}

func (h *handler) respondToElicitation() fiber.Handler {
	return func(c *fiber.Ctx) error {
		var rq view.RespondToElicitationRequest
		if err := c.BodyParser(&rq); err != nil {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "invalid request payload"})
		}
		if rq.RequestID == "" || (rq.Action != "accept" && rq.Action != "decline" && rq.Action != "cancel") {
			return c.Status(http.StatusBadRequest).JSON(fiber.Map{"error": "requestId and a valid action ('accept', 'decline', or 'cancel') are required"})
		}

		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		userID := c.Locals("user_id").(string)

		ctx, cancel := newContext(c)
		defer cancel()
		if ok, err := h.crud.CheckWorkspaceAccess(ctx, workspaceID, userID); err != nil || !ok {
			return c.Status(http.StatusForbidden).JSON(fiber.Map{"error": "forbidden"})
		}

		srv := h.mcpManager.Get(workspaceID, userID)
		if srv == nil {
			return c.Status(http.StatusNotFound).JSON(fiber.Map{"error": "mcp server not found"})
		}

		if err := srv.RespondToElicitation(rq.RequestID, rq.Action, rq.Content); err != nil {
			return c.Status(http.StatusGone).JSON(fiber.Map{"error": "This request has expired (the agent stopped waiting, or the server restarted). The agent must ask again."})
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
			zlog.Error().Err(err).Msg("Failed to update scheduled task")
			c.Set(_headerContentType, _mimeJSON)
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}

		// Broadcast task update
		h.bus.Publish(rq.WorkspaceID, rq.UserID, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromEntityTaskToView(rs.Task),
		})

		c.Status(http.StatusOK)
		return c.Send(mapper.FromUpdateScheduledTaskResponseEntityToHTTPResponse(rs))
	}
}

// formatAttachments builds a compact attachment summary for LLM notifications,
// listing each attachment id, name, and type so the agent can call downloadAttachment.
func formatAttachments(atts []entity.Attachment) string {
	if len(atts) == 0 {
		return ""
	}
	parts := make([]string, 0, len(atts))
	for _, a := range atts {
		if a.ID != "" {
			part := fmt.Sprintf("  - id=%s name=%s type=%s", a.ID, a.Filename, a.MimeType)
			if a.URL != "" {
				part += " url=" + a.URL
			}
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return "Attachments:\n" + strings.Join(parts, "\n")
}

func (h *handler) getGlobalTaskStats() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		userID := c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		rs, err := h.crud.GetGlobalTaskStats(ctx, userID)
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.JSON(rs)
	}
}

func (h *handler) getWorkspaceTaskCounts() fiber.Handler {
	return func(c *fiber.Ctx) error {
		c.Set(_headerContentType, _mimeJSON)
		workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
		if workspaceID == 0 {
			return c.Status(http.StatusUnprocessableEntity).Send(_invalidPayload)
		}
		userID := c.Locals("user_id").(string)
		ctx, cancel := newContext(c)
		defer cancel()

		counts, err := h.crud.GetWorkspaceTaskCounts(ctx, entity.GetWorkspaceTaskCountsRequest{
			WorkspaceID: workspaceID,
			UserID:      userID,
		})
		if err != nil {
			e, status := mapper.FromErrorToHTTPResponse(err)
			c.Status(status)
			return c.Send(e)
		}
		return c.JSON(counts)
	}
}
