package api

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	view "github.com/hasmcp/agentrq/backend/internal/data/view/api"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
)

func FromHTTPRequestToCreateTaskRequestEntity(c *fiber.Ctx) *entity.CreateTaskRequest {
	var payload view.CreateTaskRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workspaceID == 0 || payload.Task.Title == "" || payload.Task.CreatedBy == "" {
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
			WorkspaceID:   workspaceID,
			CreatedBy:     payload.Task.CreatedBy,
			Assignee:      payload.Task.Assignee,
			Title:         payload.Task.Title,
			Body:          payload.Task.Body,
			Status:        payload.Task.Status,
			Attachments:   entityAttachments,
			CronSchedule:  payload.Task.CronSchedule,
			SortOrder:     payload.Task.SortOrder,
		},
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

func FromGetTaskResponseEntityToHTTPResponse(rs *entity.GetTaskResponse) []byte {
	payload, _ := json.Marshal(view.GetTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}

func FromHTTPRequestToListTasksRequestEntity(c *fiber.Ctx) *entity.ListTasksRequest {
	workspaceID := monoflake.IDFromBase62(c.Params("id")).Int64()
	
	statusStr := c.Query("status")
	var status []string
	if statusStr != "" {
		status = strings.Split(statusStr, ",")
	}
	
	limit, _ := strconv.Atoi(c.Query("limit"))
	offset, _ := strconv.Atoi(c.Query("offset"))

	return &entity.ListTasksRequest{
		WorkspaceID: workspaceID,
		CreatedBy:   c.Query("created_by"),
		Status:      status,
		Filter:      c.Query("filter"),
		Limit:       limit,
		Offset:      offset,
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
		WorkspaceID:   workspaceID,
		TaskID:      taskID,
		Action:      payload.Response.Action,
		Text:        payload.Response.Text,
		Attachments: entityAttachments,
	}
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
		TaskID:    taskID,
		Status:    payload.Status.Value,
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
		TaskID:    taskID,
		SortOrder: payload.Order.Value,
	}
}

func FromUpdateTaskOrderResponseEntityToHTTPResponse(rs *entity.UpdateTaskOrderResponse) []byte {
	payload, _ := json.Marshal(view.UpdateTaskOrderResponse{Task: FromEntityTaskToView(rs.Task)})
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
		WorkspaceID:   workspaceID,
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
		ID:          monoflake.ID(t.ID).String(),
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		WorkspaceID: workspaceID,
		CreatedBy:   t.CreatedBy,
		Assignee:    t.Assignee,
		Status:      t.Status,
		Title:       t.Title,
		Body:        t.Body,
		Response:    t.Response,
		ReplyText:   t.ReplyText,
		Attachments: fromEntityAttachmentsToView(t.Attachments),
		Messages:    fromEntityMessagesToView(t.Messages),
		CronSchedule: t.CronSchedule,
		SortOrder:    t.SortOrder,
	}
	if t.ParentID != 0 {
		res.ParentID = monoflake.ID(t.ParentID).String()
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
			UserID:      m.UserID,
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
		}
	}
	return res
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
			UserID:      m.UserID,
			Sender:      m.Sender,
			Text:        m.Text,
			Attachments: msgAtts,
			Metadata:    m.Metadata,
		}
	}

	res := view.Task{
		ID:          monoflake.ID(t.ID).String(),
		CreatedAt:   t.CreatedAt,
		UpdatedAt:   t.UpdatedAt,
		WorkspaceID: workspaceID,
		CreatedBy:   t.CreatedBy,
		Assignee:    t.Assignee,
		Status:      t.Status,
		Title:       t.Title,
		Body:        t.Body,
		Response:    t.Response,
		ReplyText:   t.ReplyText,
		Attachments: atts,
		Messages:    msgs,
		CronSchedule: t.CronSchedule,
		SortOrder:    t.SortOrder,
	}
	if t.ParentID != 0 {
		res.ParentID = monoflake.ID(t.ParentID).String()
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
		WorkspaceID:  workspaceID,
		TaskID:       taskID,
		Title:        payload.Task.Title,
		Body:         payload.Task.Body,
		Assignee:     payload.Task.Assignee,
		CronSchedule: payload.Task.CronSchedule,
	}
}

func FromUpdateScheduledTaskResponseEntityToHTTPResponse(rs *entity.UpdateScheduledTaskResponse) []byte {
	payload, _ := json.Marshal(view.UpdateScheduledTaskResponse{Task: FromEntityTaskToView(rs.Task)})
	return payload
}
