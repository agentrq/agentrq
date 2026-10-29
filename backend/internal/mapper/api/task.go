// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strconv"
	"strings"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToCreateTaskRequestEntity(c *fiber.Ctx) *entity.CreateTaskRequest {
	var payload view.CreateTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	// No title is fine: the controller calls the task Untitled.
	if workspaceID == 0 || payload.Task.CreatedBy == "" {
		return nil
	}

	var entityAttachments []entity.Attachment
	if len(payload.Task.Attachments) > 0 {
		for _, a := range payload.Task.Attachments {
			entityAttachments = append(entityAttachments, entity.Attachment{
				Filename: a.Filename,
				MimeType: a.MimeType,
				Data:     a.Data,
			})
		}
	}

	return &entity.CreateTaskRequest{
		Task: entity.Task{
			WorkspaceID:      workspaceID,
			CreatedBy:        payload.Task.CreatedBy,
			Assignee:         payload.Task.Assignee,
			Title:            payload.Task.Title,
			Body:             payload.Task.Body,
			Status:           payload.Task.Status,
			Attachments:      entityAttachments,
			CronSchedule:     payload.Task.CronSchedule,
			SortOrder:        payload.Task.SortOrder,
			AllowAllCommands: payload.Task.AllowAllCommands,
			ClearContext:     payload.Task.ClearContext,
			EventID:          monoflake.IDFromBase62(payload.Task.EventID).Int64(),
			WorkflowID:       monoflake.IDFromBase62(payload.Task.WorkflowID).Int64(),
		},
		PanoDocID: monoflake.IDFromBase62(payload.PanoDocID).Int64(),
	}
}

func FromCreateTaskResponseEntityToHTTPResponse(rs *entity.CreateTaskResponse) []byte {
	payload, _ := json.Marshal(view.CreateTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToGetTaskRequestEntity(c *fiber.Ctx) *entity.GetTaskRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 {
		return nil
	}
	return &entity.GetTaskRequest{WorkspaceID: workspaceID, TaskID: taskID}
}

func FromHTTPRequestToListTaskToolCallsRequestEntity(c *fiber.Ctx) *entity.ListTaskToolCallsRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	var before int64
	if s := c.Query("before"); s != "" {
		if before = monoflake.IDFromBase62(s).Int64(); before == 0 {
			return nil
		}
	}
	if workspaceID == 0 || taskID == 0 {
		return nil
	}
	return &entity.ListTaskToolCallsRequest{WorkspaceID: workspaceID, TaskID: taskID, Before: before, Limit: c.QueryInt("limit", 0)}
}

func FromHTTPRequestToGetTaskPlanRequestEntity(c *fiber.Ctx) *entity.GetTaskPlanRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 {
		return nil
	}
	return &entity.GetTaskPlanRequest{WorkspaceID: workspaceID, TaskID: taskID}
}

func FromGetTaskPlanResponseEntityToHTTPResponse(rs *entity.GetTaskPlanResponse) []byte {
	p := rs.Plan
	payload, _ := json.Marshal(view.TaskPlan{
		ID: monoflake.ID(p.ID).String(), Content: p.Content, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
	})
	return payload
}

// FromModelTaskPlanToUpdatedView is the taskPlan.updated event for a task's
// plan, or for its withdrawal when p is nil.
func FromModelTaskPlanToUpdatedView(workspaceID, taskID int64, p *model.TaskPlan) view.TaskPlanUpdated {
	ev := view.TaskPlanUpdated{WorkspaceID: monoflake.ID(workspaceID).String(), TaskID: monoflake.ID(taskID).String()}
	if p != nil {
		ev.Plan = &view.TaskPlan{
			ID: monoflake.ID(p.ID).String(), Content: p.Content, Version: p.Version, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt,
		}
	}
	return ev
}

func FromListTaskToolCallsResponseEntityToHTTPResponse(rs *entity.ListTaskToolCallsResponse) []byte {
	payload, _ := json.Marshal(view.ListTaskToolCallsResponse{ToolCalls: fromEntityToolCallsToView(rs.ToolCalls)})
	return payload
}

func FromGetTaskResponseEntityToHTTPResponse(rs *entity.GetTaskResponse) []byte {
	payload, _ := json.Marshal(fromGetTaskResponseEntityToView(rs))
	return payload
}

// FromGetTaskResponseEntityToMCPResponse answers the supervisor's task get:
// the task, with whether it awaits a reply.
func FromGetTaskResponseEntityToMCPResponse(rs *entity.GetTaskResponse, state entity.TaskReplyState) []byte {
	payload, _ := json.Marshal(view.SupervisorGetTaskResponse{
		GetTaskResponse: fromGetTaskResponseEntityToView(rs),
		Task:            fromTaskEntityToSupervisorView(rs.Task, state),
	})
	return payload
}

// FromListTasksResponseEntityToMCPResponse answers the supervisor's task list
// and listAll: each task with its state, states[i] being rs.Tasks[i]'s.
func FromListTasksResponseEntityToMCPResponse(rs *entity.ListTasksResponse, states []entity.TaskReplyState) []byte {
	tasks := make([]view.SupervisorTask, len(rs.Tasks))
	for i, t := range rs.Tasks {
		tasks[i] = fromTaskEntityToSupervisorView(t, states[i])
	}
	payload, _ := json.Marshal(view.SupervisorListTasksResponse{Tasks: tasks})
	return payload
}

func fromTaskEntityToSupervisorView(t entity.Task, state entity.TaskReplyState) view.SupervisorTask {
	v := view.SupervisorTask{
		Task:          FromEntityTaskToView(t),
		AwaitingReply: state.AwaitingReply,
		LastMessageBy: state.LastMessageBy,
	}
	if !state.LastMessageAt.IsZero() {
		v.LastMessageAt = &state.LastMessageAt
	}
	return v
}

func fromGetTaskResponseEntityToView(rs *entity.GetTaskResponse) view.GetTaskResponse {
	transitions := make([]view.TaskStateTransition, len(rs.StateTransitions))
	for i, tr := range rs.StateTransitions {
		transitions[i] = view.TaskStateTransition{
			FromState:  tr.FromState,
			ToState:    tr.ToState,
			Agent:      tr.Agent,
			AgentModel: tr.AgentModel,
			CreatedAt:  tr.CreatedAt,
		}
	}
	return view.GetTaskResponse{
		Task:             FromEntityTaskToView(rs.Task),
		StateTransitions: transitions,
		Timing: view.TaskTiming{
			StartedAt:           rs.Timing.StartedAt,
			ClosedAt:            rs.Timing.ClosedAt,
			StartToCloseSeconds: rs.Timing.StartToCloseSeconds,
			BlockedSeconds:      rs.Timing.BlockedSeconds,
			NeedsInputSeconds:   rs.Timing.NeedsInputSeconds,
			WorkedSeconds:       rs.Timing.WorkedSeconds,
		},
	}
}

// FromTaskEntityToTaskMessagesMCPResponse answers the supervisor's task
// messages: msgs, already chosen from t's, and total, how many matched before
// the limit.
func FromTaskEntityToTaskMessagesMCPResponse(t entity.Task, msgs []entity.Message, total int) []byte {
	res := view.TaskMessagesResponse{
		Status:    t.Status,
		UpdatedAt: t.UpdatedAt,
		Total:     total,
		Messages:  make([]view.TaskMessage, len(msgs)),
	}
	for i, m := range msgs {
		res.Messages[i] = view.TaskMessage{
			ID:        monoflake.ID(m.ID).String(),
			CreatedAt: m.CreatedAt,
			Sender:    m.Sender,
			Text:      m.Text,
		}
		for _, a := range m.Attachments {
			res.Messages[i].Attachments = append(res.Messages[i].Attachments, a.Filename)
		}
	}
	payload, _ := json.Marshal(res)
	return payload
}

func FromHTTPRequestToListTasksRequestEntity(c *fiber.Ctx) *entity.ListTasksRequest {
	var workspaceID int64
	if idParam := c.Params("id"); idParam != "" {
		workspaceID = monoflake.IDFromBase62(idParam).Int64()
		if workspaceID == 0 {
			return nil
		}
	}

	statusStr := c.Query("status")
	var status []string
	if statusStr != "" {
		status = strings.Split(statusStr, ",")
	}

	limitStr := c.Query("limit")
	limit := 10 // default limit
	if limitStr != "" {
		limit, _ = strconv.Atoi(limitStr)
		if limit <= 0 {
			limit = 10
		}
	}
	if limit > 50 {
		limit = 50
	}
	offset, _ := strconv.Atoi(c.Query("offset"))

	return &entity.ListTasksRequest{
		WorkspaceID:     workspaceID,
		CreatedBy:       c.Query("created_by"),
		Status:          status,
		Filter:          c.Query("filter"),
		Limit:           limit,
		Offset:          offset,
		PreloadMessages: true,
	}
}

func FromListTasksResponseEntityToHTTPResponse(rs *entity.ListTasksResponse) []byte {
	tasks := make([]view.Task, len(rs.Tasks))
	for i, t := range rs.Tasks {
		tasks[i] = FromEntityTaskToView(t)
	}
	payload, _ := json.Marshal(view.ListTasksResponse{Tasks: tasks})
	return payload
}

func FromHTTPRequestToRespondToTaskRequestEntity(c *fiber.Ctx) *entity.RespondToTaskRequest {
	var payload view.RespondToTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 || payload.Response.Action == "" {
		return nil
	}
	var entityAttachments []entity.Attachment
	for _, a := range payload.Response.Attachments {
		entityAttachments = append(entityAttachments, entity.Attachment{
			ID:       a.ID,
			Filename: a.Filename,
			MimeType: a.MimeType,
			Data:     a.Data,
		})
	}

	return &entity.RespondToTaskRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Action:      payload.Response.Action,
		Text:        payload.Response.Text,
		Attachments: entityAttachments,
	}
}

func FromHTTPRequestToForkTaskRequestEntity(c *fiber.Ctx) *entity.ForkTaskRequest {
	var payload view.ForkTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	messageID := monoflake.IDFromBase62(payload.MessageID).Int64()
	if workspaceID == 0 || taskID == 0 || messageID == 0 {
		return nil
	}
	return &entity.ForkTaskRequest{WorkspaceID: workspaceID, TaskID: taskID, MessageID: messageID}
}

func FromForkTaskResponseEntityToHTTPResponse(rs *entity.ForkTaskResponse) []byte {
	payload, _ := json.Marshal(view.ForkTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromRespondToTaskResponseEntityToHTTPResponse(rs *entity.RespondToTaskResponse) []byte {
	payload, _ := json.Marshal(view.RespondToTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToUpdateTaskStatusRequestEntity(c *fiber.Ctx) *entity.UpdateTaskStatusRequest {
	var payload view.UpdateTaskStatusRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 || payload.Status.Value == "" {
		return nil
	}
	return &entity.UpdateTaskStatusRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Status:      payload.Status.Value,
	}
}

func FromUpdateTaskStatusResponseEntityToHTTPResponse(rs *entity.UpdateTaskStatusResponse) []byte {
	payload, _ := json.Marshal(view.UpdateTaskStatusResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToUpdateTaskOrderRequestEntity(c *fiber.Ctx) *entity.UpdateTaskOrderRequest {
	var payload view.UpdateTaskOrderRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 {
		return nil
	}
	return &entity.UpdateTaskOrderRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		SortOrder:   payload.Order.Value,
	}
}

func FromUpdateTaskOrderResponseEntityToHTTPResponse(rs *entity.UpdateTaskOrderResponse) []byte {
	payload, _ := json.Marshal(view.UpdateTaskOrderResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToUpdateTaskAssigneeRequestEntity(c *fiber.Ctx) *entity.UpdateTaskAssigneeRequest {
	var payload view.UpdateTaskAssigneeRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 || payload.Assignee.Value == "" {
		return nil
	}
	return &entity.UpdateTaskAssigneeRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Assignee:    payload.Assignee.Value,
	}
}

func FromUpdateTaskAssigneeResponseEntityToHTTPResponse(rs *entity.UpdateTaskAssigneeResponse) []byte {
	payload, _ := json.Marshal(view.UpdateTaskAssigneeResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToUpdateTaskTitleRequestEntity(c *fiber.Ctx) *entity.UpdateTaskTitleRequest {
	var payload view.UpdateTaskTitleRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	title := strings.TrimSpace(payload.Title.Value)
	if workspaceID == 0 || taskID == 0 || title == "" {
		return nil
	}
	return &entity.UpdateTaskTitleRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Title:       title,
	}
}

func FromUpdateTaskTitleResponseEntityToHTTPResponse(rs *entity.UpdateTaskTitleResponse) []byte {
	payload, _ := json.Marshal(view.UpdateTaskTitleResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToMoveTaskRequestEntity(c *fiber.Ctx) *entity.MoveTaskRequest {
	var payload view.MoveTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	destinationWorkspaceID := monoflake.IDFromBase62(payload.Workspace.Value).Int64()
	if workspaceID == 0 || taskID == 0 || destinationWorkspaceID == 0 {
		return nil
	}
	return &entity.MoveTaskRequest{
		WorkspaceID:            workspaceID,
		TaskID:                 taskID,
		DestinationWorkspaceID: destinationWorkspaceID,
	}
}

func FromMoveTaskResponseEntityToHTTPResponse(rs *entity.MoveTaskResponse) []byte {
	payload, _ := json.Marshal(view.MoveTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToUpdateTaskAllowAllCommandsRequestEntity(c *fiber.Ctx) *entity.UpdateTaskAllowAllCommandsRequest {
	var payload view.UpdateTaskAllowAllCommandsRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 {
		return nil
	}
	return &entity.UpdateTaskAllowAllCommandsRequest{
		WorkspaceID:      workspaceID,
		TaskID:           taskID,
		AllowAllCommands: payload.AllowAll.Value,
	}
}

func FromUpdateTaskAllowAllCommandsResponseEntityToHTTPResponse(rs *entity.UpdateTaskAllowAllCommandsResponse) []byte {
	payload, _ := json.Marshal(view.UpdateTaskAllowAllCommandsResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToReplyToTaskRequestEntity(c *fiber.Ctx) *entity.ReplyToTaskRequest {
	var payload view.ReplyToTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 || payload.Reply.Text == "" {
		return nil
	}
	var entityAttachments []entity.Attachment
	for _, a := range payload.Reply.Attachments {
		entityAttachments = append(entityAttachments, entity.Attachment{
			ID:       a.ID,
			Filename: a.Filename,
			MimeType: a.MimeType,
			Data:     a.Data,
		})
	}

	return &entity.ReplyToTaskRequest{
		WorkspaceID: workspaceID,
		TaskID:      taskID,
		Text:        payload.Reply.Text,
		Attachments: entityAttachments,
	}
}

func FromReplyToTaskResponseEntityToHTTPResponse(rs *entity.ReplyToTaskResponse) []byte {
	payload, _ := json.Marshal(view.ReplyToTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToDeleteTaskRequestEntity(c *fiber.Ctx) *entity.DeleteTaskRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 {
		return nil
	}
	return &entity.DeleteTaskRequest{WorkspaceID: workspaceID, TaskID: taskID}
}

func FromEntityTaskToView(t entity.Task) view.Task {
	var workspaceID string
	if t.WorkspaceID != 0 {
		workspaceID = monoflake.ID(t.WorkspaceID).String()
	}
	res := view.Task{
		ID:               monoflake.ID(t.ID).String(),
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
		WorkspaceID:      workspaceID,
		CreatedBy:        t.CreatedBy,
		Assignee:         t.Assignee,
		Status:           t.Status,
		Title:            t.Title,
		Body:             t.Body,
		Response:         t.Response,
		ReplyText:        t.ReplyText,
		Attachments:      fromEntityAttachmentsToView(t.Attachments),
		Messages:         fromEntityMessagesToView(t.Messages),
		ToolCalls:        fromEntityToolCallsToView(t.ToolCalls),
		ToolCallCount:    t.ToolCallCount,
		CronSchedule:     t.CronSchedule,
		SortOrder:        t.SortOrder,
		AllowAllCommands: t.AllowAllCommands,
		ClearContext:     t.ClearContext,
	}
	if t.ParentID != 0 {
		res.ParentID = monoflake.ID(t.ParentID).String()
	}
	if t.EventID != 0 {
		res.EventID = monoflake.ID(t.EventID).String()
	}
	// Both are needed to label the task: a workflow choice also sets EventID to
	// that workflow's start event, so the id alone cannot say which was picked.
	if t.WorkflowID != 0 {
		res.WorkflowID = monoflake.ID(t.WorkflowID).String()
	}
	res.CompletionTriggerType = t.CompletionTriggerType
	if t.CreatedByWorkspaceID != 0 {
		res.CreatedByWorkspaceID = monoflake.ID(t.CreatedByWorkspaceID).String()
	}
	return res
}

func fromEntityToolCallsToView(tcs []entity.ToolCall) []view.ToolCall {
	res := make([]view.ToolCall, len(tcs))
	for i, tc := range tcs {
		res[i] = view.ToolCall{
			ID:           monoflake.ID(tc.ID).String(),
			CreatedAt:    tc.CreatedAt,
			TaskID:       monoflake.ID(tc.TaskID).String(),
			ToolName:     tc.ToolName,
			Description:  tc.Description,
			InputPreview: tc.InputPreview,
			Status:       tc.Status,
		}
	}
	return res
}

func fromEntityMessagesToView(msgs []entity.Message) []view.Message {
	res := make([]view.Message, len(msgs))
	for i, m := range msgs {
		res[i] = view.Message{
			ID:          monoflake.ID(m.ID).String(),
			CreatedAt:   m.CreatedAt,
			TaskID:      monoflake.ID(m.TaskID).String(),
			UserID:      monoflake.ID(m.UserID).String(),
			Sender:      m.Sender,
			Text:        m.Text,
			Attachments: fromEntityAttachmentsToView(m.Attachments),
			Metadata:    m.Metadata,
		}
	}
	return res
}

func fromEntityAttachmentsToView(atts []entity.Attachment) []view.Attachment {
	res := make([]view.Attachment, len(atts))
	for i, a := range atts {
		res[i] = view.Attachment{
			ID:       a.ID,
			Filename: a.Filename,
			MimeType: a.MimeType,
			Data:     a.Data,
			URL:      a.URL,
		}
	}
	return res
}

func FromModelToolCallToView(tc model.ToolCall) view.ToolCall {
	return view.ToolCall{
		ID:           monoflake.ID(tc.ID).String(),
		CreatedAt:    tc.CreatedAt,
		TaskID:       monoflake.ID(tc.TaskID).String(),
		ToolName:     tc.ToolName,
		Description:  tc.Description,
		InputPreview: tc.InputPreview,
		Status:       tc.Status,
	}
}

func FromModelToolCallToUpdatedView(workspaceID int64, tc model.ToolCall) view.ToolCallUpdated {
	return view.ToolCallUpdated{
		WorkspaceID: monoflake.ID(workspaceID).String(),
		ToolCall:    FromModelToolCallToView(tc),
	}
}

func FromModelTaskToView(t model.Task) view.Task {
	var workspaceID string
	if t.WorkspaceID != 0 {
		workspaceID = monoflake.ID(t.WorkspaceID).String()
	}

	var atts []view.Attachment
	if len(t.Attachments) > 0 {
		var modelAtts []view.Attachment
		if err := json.Unmarshal(t.Attachments, &modelAtts); err == nil {
			atts = modelAtts
		}
	}

	msgs := make([]view.Message, len(t.Messages))
	for i, m := range t.Messages {
		var msgAtts []view.Attachment
		if len(m.Attachments) > 0 {
			_ = json.Unmarshal(m.Attachments, &msgAtts)
		}
		msgs[i] = view.Message{
			ID:          monoflake.ID(m.ID).String(),
			CreatedAt:   m.CreatedAt,
			TaskID:      monoflake.ID(m.TaskID).String(),
			UserID:      monoflake.ID(m.UserID).String(),
			Sender:      m.Sender,
			Text:        m.Text,
			Attachments: msgAtts,
			Metadata:    m.Metadata,
		}
	}

	toolCalls := make([]view.ToolCall, len(t.ToolCalls))
	for i, tc := range t.ToolCalls {
		toolCalls[i] = FromModelToolCallToView(tc)
	}

	res := view.Task{
		ID:               monoflake.ID(t.ID).String(),
		CreatedAt:        t.CreatedAt,
		UpdatedAt:        t.UpdatedAt,
		WorkspaceID:      workspaceID,
		CreatedBy:        t.CreatedBy,
		Assignee:         t.Assignee,
		Status:           t.Status,
		Title:            t.Title,
		Body:             t.Body,
		Response:         t.Response,
		ReplyText:        t.ReplyText,
		Attachments:      atts,
		Messages:         msgs,
		ToolCalls:        toolCalls,
		ToolCallCount:    t.ToolCallCount,
		CronSchedule:     t.CronSchedule,
		SortOrder:        t.SortOrder,
		AllowAllCommands: t.AllowAllCommands,
		ClearContext:     t.ClearContext,
	}
	if t.ParentID != 0 {
		res.ParentID = monoflake.ID(t.ParentID).String()
	}
	if t.EventID != 0 {
		res.EventID = monoflake.ID(t.EventID).String()
	}
	// Live SSE pushes go through this mapper too, so a task that appears via
	// the event stream must carry the same trigger labelling as one fetched
	// over REST — otherwise the same row renders differently by arrival path.
	if t.WorkflowID != 0 {
		res.WorkflowID = monoflake.ID(t.WorkflowID).String()
	}
	res.CompletionTriggerType = t.CompletionTriggerType
	if t.CreatedByWorkspaceID != 0 {
		res.CreatedByWorkspaceID = monoflake.ID(t.CreatedByWorkspaceID).String()
	}
	return res
}
func FromHTTPRequestToUpdateScheduledTaskRequestEntity(c *fiber.Ctx) *entity.UpdateScheduledTaskRequest {
	var payload view.UpdateScheduledTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	taskID := monoflake.IDFromBase62(c.Params("taskID")).Int64()
	if workspaceID == 0 || taskID == 0 || payload.Task.Title == "" {
		return nil
	}
	return &entity.UpdateScheduledTaskRequest{
		WorkspaceID:      workspaceID,
		TaskID:           taskID,
		Title:            payload.Task.Title,
		Body:             payload.Task.Body,
		Assignee:         payload.Task.Assignee,
		CronSchedule:     payload.Task.CronSchedule,
		AllowAllCommands: payload.Task.AllowAllCommands,
		ClearContext:     payload.Task.ClearContext,
	}
}

func FromUpdateScheduledTaskResponseEntityToHTTPResponse(rs *entity.UpdateScheduledTaskResponse) []byte {
	payload, _ := json.Marshal(view.UpdateScheduledTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}
