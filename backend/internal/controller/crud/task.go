// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/auth"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/schedule"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// AppendSelfLearningNote appends the workspace's self-learning loop note to an
// agent task body. It is idempotent: if the note is empty or the body already
// contains it, the body is returned unchanged. This prevents the note from
// accumulating multiple times when a body that already carries it is re-submitted
// (e.g. self-improving loop tasks, copied bodies, or repeated human→agent
// reassignment) — which would otherwise be returned repeatedly by the getTask tool.
func AppendSelfLearningNote(body, note string) string {
	if note == "" || strings.Contains(body, note) {
		return body
	}
	if body != "" {
		return body + "\n\n" + note
	}
	return note
}

func (c *controller) ensureActiveWorkspace(ctx context.Context, id int64, userID string) (model.Workspace, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	w, err := c.repository.GetWorkspace(ctx, id, uid)
	if err != nil {
		return model.Workspace{}, err
	}
	if w.ArchivedAt != nil {
		return model.Workspace{}, fmt.Errorf("workspace is archived and read-only")
	}
	return w, nil
}

func (c *controller) CreateTask(ctx context.Context, req entity.CreateTaskRequest) (*entity.CreateTaskResponse, error) {
	userID := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowTask(userID) {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	w, err := c.ensureActiveWorkspace(ctx, req.Task.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	// A title is optional here, for the interface's sake: the browser names an
	// untitled task afterwards. MCP and Slack still insist on one, each before
	// it gets here.
	if strings.TrimSpace(req.Task.Title) == "" {
		req.Task.Title = entity.UntitledTaskTitle
	}

	status := req.Task.Status
	if status == "" {
		if req.Task.CronSchedule != "" {
			status = "cron"
		} else {
			status = "notstarted"
		}
	}
	if !isValidTaskStatus(status) {
		return nil, fmt.Errorf("invalid task status: %s", status)
	}

	if status == "cron" {
		if req.Task.CronSchedule == "" {
			return nil, fmt.Errorf("cron_schedule is required for chronic tasks")
		}
		if err := validateCronForContext(ctx, req.Task.CronSchedule); err != nil {
			return nil, err
		}
	}

	// The supervisor's token names the account, not the calling workspace, so
	// its task is put down to the supervisor workspace, if the account has one.
	var createdByWorkspaceID int64
	if req.BySupervisor {
		sup, found, err := c.findSupervisorWorkspace(ctx, userID)
		if err != nil {
			return nil, err
		}
		if found {
			createdByWorkspaceID = sup.ID
		}
	}

	now := time.Now()

	// The attachments are filed under the task, so it needs its id first.
	taskID := c.idgen.NextID()

	// Save attachments binary to filesystem and clear Data for metadata DB storage
	c.saveAttachments(w.ContentID(), taskID, req.Task.Attachments)

	var attachJSON datatypes.JSON
	if len(req.Task.Attachments) > 0 {
		if b, err := json.Marshal(req.Task.Attachments); err == nil {
			attachJSON = datatypes.JSON(b)
		}
	}

	sortOrder := req.Task.SortOrder
	if sortOrder == 0 {
		sortOrder = float64(now.UnixMilli()) / 1000.0
	}

	allowAll := req.Task.AllowAllCommands
	if req.Task.CreatedBy == "agent" && !allowAll {
		allowAll = w.AllowAllCommands
	}

	// Same rule as allowAll above, and for the same reason: a caller that says
	// nothing gets the workspace's preference, and one that says false means
	// false. The form always sends the toggle's real state, so turning this off
	// in a workspace that defaults it on does what it looks like it does.
	clearContext := req.Task.ClearContext
	if req.Task.CreatedBy == "agent" && !clearContext {
		clearContext = w.ClearContextDefault
	}

	if req.Task.Assignee == "agent" {
		req.Task.Body = AppendSelfLearningNote(req.Task.Body, w.SelfLearningLoopNote)
	}

	// Choosing a workflow means "run this pipeline on completion", which fires
	// the workflow's own start event — so the workflow choice expands into both
	// fields, and the discriminator records which one the author picked.
	eventID := req.Task.EventID
	completionTrigger := entity.CompletionTriggerNone
	if req.Task.WorkflowID != 0 {
		wf, wfErr := c.repository.GetWorkflow(ctx, req.Task.WorkflowID, userID)
		if wfErr != nil {
			return nil, fmt.Errorf("workflow not found")
		}
		if wf.StartEventID == 0 {
			return nil, fmt.Errorf("workflow %q has no start event yet", wf.Name)
		}
		eventID = wf.StartEventID
		completionTrigger = entity.CompletionTriggerWorkflow
	} else if eventID != 0 {
		completionTrigger = entity.CompletionTriggerEvent
	}

	m := model.Task{
		ID:                    taskID,
		CreatedAt:             now,
		UpdatedAt:             now,
		UserID:                userID,
		WorkspaceID:           req.Task.WorkspaceID,
		CreatedBy:             req.Task.CreatedBy,
		Assignee:              req.Task.Assignee,
		Status:                status,
		Title:                 req.Task.Title,
		Body:                  req.Task.Body,
		Attachments:           attachJSON,
		CronSchedule:          req.Task.CronSchedule,
		ParentID:              req.Task.ParentID,
		SortOrder:             sortOrder,
		AllowAllCommands:      allowAll,
		ClearContext:          clearContext,
		EventID:               eventID,
		WorkflowID:            req.Task.WorkflowID,
		CompletionTriggerType: completionTrigger,
		CreatedByWorkspaceID:  createdByWorkspaceID,
	}
	var created model.Task
	if req.PanoDocID != 0 {
		link := model.PanoDocTask{ID: c.idgen.NextID(), CreatedAt: now, UserID: userID, DocID: req.PanoDocID, TaskID: taskID, WorkspaceID: m.WorkspaceID}
		created, err = c.repository.CreateTaskLinkingPanoDoc(ctx, m, link, now.UTC())
	} else {
		created, err = c.repository.CreateTask(ctx, m)
	}
	if err != nil {
		if errors.Is(err, base.ErrNotFound) {
			return nil, err
		}
		return nil, fmt.Errorf("create task: %w", err)
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskCreate,
		WorkspaceID:  created.WorkspaceID,
		UserID:       created.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   created.ID,
		Actor:        entity.ActorHuman,
	})
	if req.PanoDocID != 0 {
		c.emitPanoEvent(ctx, entity.ActionPanoLink, userID, 0, req.PanoDocID)
	}

	return &entity.CreateTaskResponse{Task: c.fromModelTaskToEntity(created)}, nil
}

// maxToolCallsPage caps a page of ListTaskToolCalls.
const maxToolCallsPage = 500

// ListTaskToolCalls pages in the tool calls older than the latest ones a task
// is read with.
func (c *controller) ListTaskToolCalls(ctx context.Context, req entity.ListTaskToolCallsRequest) (*entity.ListTaskToolCallsResponse, error) {
	limit := req.Limit
	if limit <= 0 {
		limit = base.TaskToolCallsPage
	}
	limit = min(limit, maxToolCallsPage)
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if _, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid); err != nil {
		return nil, err
	}
	tcs, err := c.repository.ListToolCalls(ctx, req.TaskID, req.Before, limit)
	if err != nil {
		return nil, err
	}
	return &entity.ListTaskToolCallsResponse{ToolCalls: fromModelToolCallsToEntity(tcs)}, nil
}

// GetTaskPlan reads the plan of a task of the caller's, ErrNotFound when the
// task has none.
func (c *controller) GetTaskPlan(ctx context.Context, req entity.GetTaskPlanRequest) (*entity.GetTaskPlanResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if _, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid); err != nil {
		return nil, err
	}
	p, err := c.repository.GetTaskPlan(ctx, req.TaskID)
	if err != nil {
		return nil, err
	}
	return &entity.GetTaskPlanResponse{Plan: entity.TaskPlan{
		ID: p.ID, Content: p.Content, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	}}, nil
}

func fromModelToolCallsToEntity(tcs []model.ToolCall) []entity.ToolCall {
	res := make([]entity.ToolCall, len(tcs))
	for i, tc := range tcs {
		res[i] = entity.ToolCall{
			ID:           tc.ID,
			CreatedAt:    tc.CreatedAt,
			TaskID:       tc.TaskID,
			WorkspaceID:  tc.WorkspaceID,
			ToolName:     tc.ToolName,
			Description:  tc.Description,
			InputPreview: tc.InputPreview,
			Status:       tc.Status,
		}
	}
	return res
}

func (c *controller) GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	rows, err := c.repository.ListTaskStateTransitions(ctx, m.ID)
	if err != nil {
		return nil, err
	}
	agents, models, err := c.agentNames(ctx, rows)
	if err != nil {
		return nil, err
	}
	transitions := make([]entity.TaskStateTransition, len(rows))
	for i, r := range rows {
		transitions[i] = entity.TaskStateTransition{
			FromState:  r.FromState.String(),
			ToState:    r.ToState.String(),
			Agent:      agents[r.AgentID],
			AgentModel: models[r.AgentModelID],
			CreatedAt:  r.CreatedAt,
		}
	}
	return &entity.GetTaskResponse{
		Task:             c.fromModelTaskToEntity(m),
		StateTransitions: transitions,
		Timing:           taskTiming(rows, time.Now()),
	}, nil
}

// agentNames looks up the names of the agents and models a task's history
// refers to, without a trip to the repository when it refers to none.
func (c *controller) agentNames(ctx context.Context, rows []model.TaskStateTransition) (agents, models map[int64]string, err error) {
	var agentIDs, modelIDs []int64
	seenAgents, seenModels := make(map[int64]bool), make(map[int64]bool)
	for _, r := range rows {
		if r.AgentID != 0 && !seenAgents[r.AgentID] {
			seenAgents[r.AgentID] = true
			agentIDs = append(agentIDs, r.AgentID)
		}
		if r.AgentModelID != 0 && !seenModels[r.AgentModelID] {
			seenModels[r.AgentModelID] = true
			modelIDs = append(modelIDs, r.AgentModelID)
		}
	}
	if len(agentIDs) == 0 {
		return nil, nil, nil
	}
	if c.taskAgents == nil {
		return nil, nil, nil
	}
	return c.taskAgents.Names(ctx, agentIDs, modelIDs)
}

// taskTiming is the entity form of model.TaskTimingOf, which the latency
// statistics are built from too.
func taskTiming(rows []model.TaskStateTransition, now time.Time) entity.TaskTiming {
	t := model.TaskTimingOf(rows, now)
	return entity.TaskTiming{
		StartedAt:           t.StartedAt,
		ClosedAt:            t.ClosedAt,
		StartToCloseSeconds: t.StartToCloseSeconds,
		BlockedSeconds:      t.BlockedSeconds,
		NeedsInputSeconds:   t.NeedsInputSeconds,
		WorkedSeconds:       t.WorkedSeconds,
	}
}

func (c *controller) ListTasks(ctx context.Context, req entity.ListTasksRequest) (*entity.ListTasksResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	ms, err := c.repository.ListTasks(ctx, req, uid)
	if err != nil {
		return nil, err
	}
	tasks := make([]entity.Task, len(ms))
	for i, m := range ms {
		tasks[i] = c.fromModelTaskToEntity(m)
	}
	return &entity.ListTasksResponse{Tasks: tasks}, nil
}

func (c *controller) RespondToTask(ctx context.Context, req entity.RespondToTaskRequest) (*entity.RespondToTaskResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowMessage(uid) {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	w, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	createMsg := false
	msgText := req.Text
	msgSender := "human"
	var changed []string

	switch req.Action {
	case "allow", "allow_all":
		// Enforce single ongoing task per workspace
		uid := monoflake.IDFromBase62(req.UserID).Int64()
		tasks, err := c.repository.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: req.WorkspaceID}, uid)
		if err == nil {
			for _, t := range tasks {
				if t.Status == "ongoing" && t.ID != req.TaskID {
					return nil, fmt.Errorf("another task is already ongoing in this workspace")
				}
			}
		}
		m.Status = "ongoing"
		changed = append(changed, "Status")
		createMsg = true
		if msgText == "" {
			msgText = "Human approved this task."
		}
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionTaskApproveManual,
			WorkspaceID:  req.WorkspaceID,
			UserID:       uid,
			ResourceType: entity.ResourceTask,
			ResourceID:   req.TaskID,
			Actor:        entity.ActorHuman,
		})
	case "reject":
		m.Status = "rejected"
		changed = append(changed, "Status")
		createMsg = true
		if msgText == "" {
			msgText = "Human rejected this task."
		}
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionTaskRejectManual,
			WorkspaceID:  req.WorkspaceID,
			UserID:       uid,
			ResourceType: entity.ResourceTask,
			ResourceID:   req.TaskID,
			Actor:        entity.ActorHuman,
		})
	case "text":
		// Just a message, don't necessarily change status unless indicated
		createMsg = true
	default:
		return nil, fmt.Errorf("unknown action: %s", req.Action)
	}

	if createMsg && (msgText != "" || len(req.Attachments) > 0) {
		// Save attachments
		c.saveAttachments(w.ContentID(), req.TaskID, req.Attachments)

		var attsData []byte
		if len(req.Attachments) > 0 {
			attsData, _ = json.Marshal(req.Attachments)
		}

		msg := model.Message{
			ID:          c.idgen.NextID(),
			CreatedAt:   time.Now(),
			TaskID:      m.ID,
			UserID:      monoflake.IDFromBase62(req.UserID).Int64(),
			Sender:      msgSender,
			Text:        msgText,
			Attachments: attsData,
		}
		if err := c.repository.CreateMessage(ctx, msg); err != nil {
			return nil, err
		}
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionMessageCreate,
			WorkspaceID:  req.WorkspaceID,
			UserID:       msg.UserID,
			ResourceType: entity.ResourceMessage,
			ResourceID:   msg.ID,
			Actor:        entity.ActorHuman,
		})
	}

	m.UpdatedAt = time.Now()
	updated, err := c.repository.UpdateTask(ctx, m, changed...)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	// Fetch latest state with messages
	uid = monoflake.IDFromBase62(req.UserID).Int64()
	latest, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return &entity.RespondToTaskResponse{Task: c.fromModelTaskToEntity(updated)}, nil
	}
	return &entity.RespondToTaskResponse{Task: c.fromModelTaskToEntity(latest)}, nil
}

func (c *controller) UpdateTaskStatus(ctx context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	if m.Status == "cron" {
		return nil, fmt.Errorf("cannot update status of a chronic task template; it must remain in 'cron' state")
	}

	if !isValidTaskStatus(req.Status) {
		return nil, fmt.Errorf("invalid task status: %s", req.Status)
	}

	if req.Status == "ongoing" {
		// Enforce single ongoing task per workspace
		uid := monoflake.IDFromBase62(req.UserID).Int64()
		tasks, err := c.repository.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: req.WorkspaceID}, uid)
		if err == nil {
			for _, t := range tasks {
				if t.Status == "ongoing" && t.ID != req.TaskID {
					return nil, fmt.Errorf("another task is already ongoing in this workspace")
				}
			}
		}
	}

	m.Status = req.Status
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, "Status")
	if err != nil {
		return nil, err
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	if updated.Status == "completed" || updated.Status == "done" {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionTaskComplete,
			WorkspaceID:  updated.WorkspaceID,
			UserID:       updated.UserID,
			ResourceType: entity.ResourceTask,
			ResourceID:   updated.ID,
			Actor:        entity.ActorHuman,
		})
	}

	return &entity.UpdateTaskStatusResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) UpdateTaskOrder(ctx context.Context, req entity.UpdateTaskOrderRequest) (*entity.UpdateTaskOrderResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	m.SortOrder = req.SortOrder
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, "SortOrder")
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.UpdateTaskOrderResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) UpdateTaskAssignee(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	w, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	if req.Assignee != "human" && req.Assignee != "agent" {
		return nil, fmt.Errorf("invalid assignee: %s", req.Assignee)
	}

	changed := []string{"Assignee"}
	if req.Assignee == "agent" && m.Assignee != "agent" {
		m.Body = AppendSelfLearningNote(m.Body, w.SelfLearningLoopNote)
		changed = append(changed, "Body")
	}

	m.Assignee = req.Assignee
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, changed...)
	if err != nil {
		return nil, err
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	return &entity.UpdateTaskAssigneeResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

// UpdateTaskTitle renames a task in any status, but only within
// entity.TaskTitleEditWindow of its creation.
func (c *controller) UpdateTaskTitle(ctx context.Context, req entity.UpdateTaskTitleRequest) (*entity.UpdateTaskTitleResponse, error) {
	title := strings.TrimSpace(req.Title)
	if title == "" {
		return nil, fmt.Errorf("title is required")
	}
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	if time.Since(m.CreatedAt) > entity.TaskTitleEditWindow {
		return nil, entity.ErrTaskTitleLocked
	}

	m.Title = title
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, "Title")
	if err != nil {
		return nil, err
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskTitleUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	return &entity.UpdateTaskTitleResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) MoveTask(ctx context.Context, req entity.MoveTaskRequest) (*entity.MoveTaskResponse, error) {
	if req.DestinationWorkspaceID == req.WorkspaceID {
		return nil, fmt.Errorf("task is already in this workspace")
	}
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	// Ownership of the destination is checked independently of the source so a
	// task can never be moved into a workspace the caller doesn't own.
	if _, err := c.ensureActiveWorkspace(ctx, req.DestinationWorkspaceID, req.UserID); err != nil {
		return nil, err
	}

	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	m.WorkspaceID = req.DestinationWorkspaceID
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, "WorkspaceID")
	if err != nil {
		return nil, err
	}

	// ToolCall.WorkspaceID is denormalized from the task at creation time and
	// is never touched again otherwise, so it must be reconciled here or
	// existing tool-call rows keep pointing at the task's old workspace.
	if err := c.repository.UpdateToolCallsWorkspaceID(ctx, updated.ID, updated.WorkspaceID); err != nil {
		return nil, err
	}

	// SlackTaskThread.WorkspaceID is denormalized the same way and is used to
	// route inbound Slack replies (see HandleSlackEvent) — reconcile it too so
	// a moved task doesn't strand its thread pointing at the old workspace.
	if err := c.repository.UpdateSlackTaskThreadWorkspaceID(ctx, updated.ID, updated.WorkspaceID); err != nil {
		return nil, err
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	return &entity.MoveTaskResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) UpdateTaskAllowAllCommands(ctx context.Context, req entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	m.AllowAllCommands = req.AllowAllCommands
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, "AllowAllCommands")
	if err != nil {
		return nil, err
	}

	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskAllowAllCommandsToggle,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	return &entity.UpdateTaskAllowAllCommandsResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) ReplyToTask(ctx context.Context, req entity.ReplyToTaskRequest) (*entity.ReplyToTaskResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowMessage(uid) {
		return nil, fmt.Errorf("rate limit exceeded")
	}

	w, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	// Save attachments
	c.saveAttachments(w.ContentID(), req.TaskID, req.Attachments)

	var attsData []byte
	if len(req.Attachments) > 0 {
		attsData, _ = json.Marshal(req.Attachments)
	}

	sender := "human"
	var metadataJSON datatypes.JSON
	if entity.GetOrigin(ctx) == entity.OriginSlack {
		sender = "slack"
		if req.SlackUser != "" {
			metaMap := map[string]any{
				"slack_user": req.SlackUser,
			}
			b, _ := json.Marshal(metaMap)
			metadataJSON = datatypes.JSON(b)
		}
	}

	// Create a new message from human/slack
	msg := model.Message{
		ID:          c.idgen.NextID(),
		CreatedAt:   time.Now(),
		TaskID:      m.ID,
		UserID:      monoflake.IDFromBase62(req.UserID).Int64(),
		Sender:      sender,
		Text:        req.Text,
		Attachments: datatypes.JSON(attsData),
		Metadata:    metadataJSON,
	}
	if err := c.repository.CreateMessage(ctx, msg); err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMessageCreate,
		WorkspaceID:  req.WorkspaceID,
		UserID:       msg.UserID,
		ResourceType: entity.ResourceMessage,
		ResourceID:   msg.ID,
		Actor:        entity.ActorHuman,
	})

	m.UpdatedAt = time.Now()
	updated, err := c.repository.UpdateTask(ctx, m)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})

	// Fetch latest state with messages
	uid = monoflake.IDFromBase62(req.UserID).Int64()
	latest, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return &entity.ReplyToTaskResponse{Task: c.fromModelTaskToEntity(updated)}, nil
	}
	return &entity.ReplyToTaskResponse{Task: c.fromModelTaskToEntity(latest)}, nil
}

func (c *controller) DeleteTask(ctx context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error) {
	w, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}

	// 1. Get task information to collect attachment IDs before deleting
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	t, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	var attachmentIDs []string
	var atts []entity.Attachment
	if len(t.Attachments) > 0 {
		if err := json.Unmarshal(t.Attachments, &atts); err == nil {
			for _, a := range atts {
				if a.ID != "" {
					attachmentIDs = append(attachmentIDs, a.ID)
				}
			}
		}
	}
	for _, m := range t.Messages {
		var mAtts []entity.Attachment
		if len(m.Attachments) > 0 {
			if err := json.Unmarshal(m.Attachments, &mAtts); err == nil {
				for _, a := range mAtts {
					if a.ID != "" {
						attachmentIDs = append(attachmentIDs, a.ID)
					}
				}
			}
		}
	}

	// The delete unlinks the task from its Pano docs, so read them first.
	// Best effort: a doc's count is recounted on its next save anyway.
	docIDs, _ := c.repository.ListPanoDocIDsOfTask(ctx, uid, req.TaskID)

	// 2. Delete from DB (repository handles cascaded message delete)
	err = c.repository.DeleteTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	// Its items lose their assignee, so their docs' counts are recounted.
	_, _ = c.syncPanoDocs(ctx, uid, docIDs)
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskDelete,
		WorkspaceID:  req.WorkspaceID,
		UserID:       uid,
		ResourceType: entity.ResourceTask,
		ResourceID:   req.TaskID,
		Actor:        entity.ActorHuman,
	})

	// 3. Purge storage files
	for _, id := range attachmentIDs {
		storage.DeleteAttachment(c.storage, w.ContentID(), req.TaskID, id)
	}

	return &entity.DeleteTaskResponse{}, nil
}

func (c *controller) UpdateMessageMetadata(ctx context.Context, req entity.UpdateMessageMetadataRequest) error {
	// Let's verify task access
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	_, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return err
	}

	b, err := json.Marshal(req.Metadata)
	if err != nil {
		return err
	}

	if err := c.repository.UpdateMessageMetadata(ctx, req.TaskID, req.MessageID, b); err != nil {
		return err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMessageUpdate,
		WorkspaceID:  req.WorkspaceID,
		UserID:       uid,
		ResourceType: entity.ResourceMessage,
		ResourceID:   req.MessageID,
		Actor:        entity.ActorHuman,
	})
	return nil
}

func (c *controller) fromModelTaskToEntity(m model.Task) entity.Task {
	var atts []entity.Attachment
	if len(m.Attachments) > 0 {
		_ = json.Unmarshal(m.Attachments, &atts)
	}

	msgs := make([]entity.Message, len(m.Messages))
	for i, msg := range m.Messages {
		var msgAtts []entity.Attachment
		if len(msg.Attachments) > 0 {
			_ = json.Unmarshal(msg.Attachments, &msgAtts)
		}
		var metadata any
		if len(msg.Metadata) > 0 {
			_ = json.Unmarshal(msg.Metadata, &metadata)
		}
		msgs[i] = entity.Message{
			ID:          msg.ID,
			CreatedAt:   msg.CreatedAt,
			TaskID:      msg.TaskID,
			UserID:      msg.UserID,
			Sender:      msg.Sender,
			Text:        msg.Text,
			Attachments: msgAtts,
			Metadata:    metadata,
		}
	}

	return entity.Task{
		ID:                    m.ID,
		CreatedAt:             m.CreatedAt,
		UpdatedAt:             m.UpdatedAt,
		WorkspaceID:           m.WorkspaceID,
		UserID:                m.UserID,
		CreatedBy:             m.CreatedBy,
		Assignee:              m.Assignee,
		Status:                m.Status,
		Title:                 m.Title,
		Body:                  m.Body,
		Response:              m.Response,
		ReplyText:             m.ReplyText,
		Attachments:           atts,
		Messages:              msgs,
		ToolCalls:             fromModelToolCallsToEntity(m.ToolCalls),
		ToolCallCount:         m.ToolCallCount,
		CronSchedule:          m.CronSchedule,
		ParentID:              m.ParentID,
		SortOrder:             m.SortOrder,
		AllowAllCommands:      m.AllowAllCommands,
		ClearContext:          m.ClearContext,
		EventID:               m.EventID,
		WorkflowID:            m.WorkflowID,
		WorkflowDepth:         m.WorkflowDepth,
		CompletionTriggerType: m.CompletionTriggerType,
		CreatedByWorkspaceID:  m.CreatedByWorkspaceID,
	}
}

func (c *controller) GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()

	// Load task with messages — also verifies the user owns the task (GetTask filters by user_id).
	t, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, base.ErrNotFound
	}

	lists := []datatypes.JSON{t.Attachments}
	for _, m := range t.Messages {
		lists = append(lists, m.Attachments)
	}
	for _, raw := range lists {
		if len(raw) == 0 {
			continue
		}
		var atts []entity.Attachment
		if json.Unmarshal(raw, &atts) != nil {
			continue
		}
		for _, a := range atts {
			if a.ID != req.AttachmentID {
				continue
			}
			res := &entity.GetAttachmentResponse{Filename: a.Filename, MimeType: a.MimeType, URL: a.URL}
			if req.LinkOnly && a.URL != "" {
				return res, nil
			}
			contentID, err := c.ContentWorkspaceID(ctx, t.WorkspaceID, uid)
			if err != nil {
				return nil, base.ErrNotFound
			}
			data, err := storage.LoadAttachment(c.storage, contentID, t.ID, a.ID)
			if err != nil {
				return nil, base.ErrNotFound
			}
			res.Data = data
			return res, nil
		}
	}

	return nil, base.ErrNotFound
}

func (c *controller) saveAttachments(workspaceID, taskID int64, atts []entity.Attachment) {
	SaveAttachments(c.storage, c.idgen, workspaceID, taskID, atts)
}

// SaveAttachments stores each attachment of a task that carries data, and
// records its public link when the store makes one. A url the caller sent is
// dropped: only the store makes links, so nobody can plant one.
func SaveAttachments(store storage.Service, ids idgen.Service, workspaceID, taskID int64, atts []entity.Attachment) {
	for i := range atts {
		atts[i].URL = ""
		if atts[i].Data != "" {
			// Always generate a server-controlled ID; never trust caller-provided IDs.
			// This prevents slug-keyed files that break the download path.
			atts[i].ID = monoflake.ID(ids.NextID()).String()
			atts[i].URL, _ = storage.SaveAttachment(store, workspaceID, taskID, atts[i].ID, atts[i].Data, atts[i].MimeType)
			atts[i].Data = "" // clear from metadata
		}
	}
}
func (c *controller) UpdateScheduledTask(ctx context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	if m.Status != "cron" && m.Status != "notstarted" {
		return nil, fmt.Errorf("only chronic or not started tasks can be edited")
	}

	if req.CronSchedule == "" {
		m.Status = "notstarted"
		m.CronSchedule = ""
	} else {
		if err := validateCronForContext(ctx, req.CronSchedule); err != nil {
			return nil, err
		}
		m.Status = "cron"
		m.CronSchedule = req.CronSchedule
	}

	m.Title = req.Title
	m.Body = req.Body
	m.Assignee = req.Assignee
	m.AllowAllCommands = req.AllowAllCommands
	m.ClearContext = req.ClearContext
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m, "Status", "CronSchedule", "Title", "Body", "Assignee", "AllowAllCommands", "ClearContext")
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionTaskUpdate,
		WorkspaceID:  updated.WorkspaceID,
		UserID:       updated.UserID,
		ResourceType: entity.ResourceTask,
		ResourceID:   updated.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.UpdateScheduledTaskResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

// ValidTaskStatuses is every status a task may be moved to.
//
// Exported because it is also what the MCP tool schemas advertise to agents,
// and those are struct tags — which the compiler will not let us build from a
// slice, so they have to repeat the list by hand. This is the list they are
// tested against (see TestToolSchemaEnumsMatchTheValidators), so the copy in a
// tag cannot drift from what the server actually accepts. It has: an agent was
// once told it could use `done` and `failed`, neither of which validates, and
// never told about `blocked`, which is how it asks a human for help.
var ValidTaskStatuses = []string{
	"notstarted",
	"ongoing",
	"blocked",
	"completed",
	"rejected",
	"cron",
}

// ValidTaskResponseActions is every action RespondToTask accepts. Kept here
// beside the statuses, and tested against the tool schema, for the same reason.
//
// Note `reject` rather than `deny`: the schema advertised `deny` for a while,
// which the switch in RespondToTask has never handled.
var ValidTaskResponseActions = []string{
	"allow",
	"allow_all",
	"reject",
	"text",
}

func isValidTaskStatus(status string) bool {
	return slices.Contains(ValidTaskStatuses, status)
}

func validateCronForContext(ctx context.Context, cronSchedule string) error {
	if auth.ContextHasAudience(ctx, auth.ActorHumanAudience) {
		return schedule.ValidateCronSyntax(cronSchedule)
	}
	return schedule.ValidateCronGranularity(cronSchedule)
}

func (c *controller) GetGlobalTaskStats(ctx context.Context, userID string) (*entity.GlobalTaskStatsResponse, error) {
	uid := monoflake.IDFromBase62(userID).Int64()
	res, err := c.repository.GetGlobalTaskStats(ctx, uid)
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (c *controller) GetWorkspaceTaskCounts(ctx context.Context, req entity.GetWorkspaceTaskCountsRequest) (map[string]int64, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	return c.repository.GetWorkspaceTaskCountsByCategory(ctx, req.WorkspaceID, uid)
}
