package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
	"github.com/robfig/cron/v3"
	"gorm.io/datatypes"
)

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
	w, err := c.ensureActiveWorkspace(ctx, req.Task.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	// Validation
	if req.Task.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	status := req.Task.Status
	if status == "" {
		status = "notstarted"
	}
	if !isValidTaskStatus(status) {
		return nil, fmt.Errorf("invalid task status: %s", status)
	}

	if status == "cron" {
		if req.Task.CronSchedule == "" {
			return nil, fmt.Errorf("cron_schedule is required for chronic tasks")
		}
		// Validate Cron Schedule
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(req.Task.CronSchedule); err != nil {
			return nil, fmt.Errorf("invalid cron schedule: %w", err)
		}
	}

	now := time.Now()

	// Save attachments binary to filesystem and clear Data for metadata DB storage
	c.saveAttachments(req.Task.Attachments)

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

	if req.Task.Assignee == "agent" && w.SelfLearningLoopNote != "" {
		note := "\n\nSelf Learning Loop Note:\n" + w.SelfLearningLoopNote
		if req.Task.Body != "" {
			req.Task.Body += note
		} else {
			req.Task.Body = w.SelfLearningLoopNote
		}
	}

	m := model.Task{
		ID:           c.idgen.NextID(),
		CreatedAt:    now,
		UpdatedAt:    now,
		UserID:       monoflake.IDFromBase62(req.UserID).Int64(),
		WorkspaceID:  req.Task.WorkspaceID,
		CreatedBy:    req.Task.CreatedBy,
		Assignee:     req.Task.Assignee,
		Status:       status,
		Title:        req.Task.Title,
		Body:         req.Task.Body,
		Attachments:  attachJSON,
		CronSchedule: req.Task.CronSchedule,
		ParentID:     req.Task.ParentID,
		SortOrder:    sortOrder,
		AllowAllCommands: allowAll,
	}
	created, err := c.repository.CreateTask(ctx, m)
	if err != nil {
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

	return &entity.CreateTaskResponse{Task: c.fromModelTaskToEntity(created)}, nil
}

func (c *controller) GetTask(ctx context.Context, req entity.GetTaskRequest) (*entity.GetTaskResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	return &entity.GetTaskResponse{Task: c.fromModelTaskToEntity(m)}, nil
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
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	createMsg := false
	msgText := req.Text
	msgSender := "human"

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

	if createMsg && msgText != "" {
		// Save attachments
		c.saveAttachments(req.Attachments)

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
		return &entity.RespondToTaskResponse{Task: c.fromModelTaskToEntity(updated)}, nil
	}
	return &entity.RespondToTaskResponse{Task: c.fromModelTaskToEntity(latest)}, nil
}

func (c *controller) UpdateTaskStatus(ctx context.Context, req entity.UpdateTaskStatusRequest) (*entity.UpdateTaskStatusResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
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
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	m.SortOrder = req.SortOrder
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
	return &entity.UpdateTaskOrderResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) UpdateTaskAssignee(ctx context.Context, req entity.UpdateTaskAssigneeRequest) (*entity.UpdateTaskAssigneeResponse, error) {
	w, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID)
	if err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	if req.Assignee != "human" && req.Assignee != "agent" {
		return nil, fmt.Errorf("invalid assignee: %s", req.Assignee)
	}

	if req.Assignee == "agent" && m.Assignee != "agent" {
		if w.SelfLearningLoopNote != "" {
			note := "\n\nSelf Learning Loop Note:\n" + w.SelfLearningLoopNote
			if m.Body != "" {
				m.Body += note
			} else {
				m.Body = w.SelfLearningLoopNote
			}
		}
	}

	m.Assignee = req.Assignee
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

	return &entity.UpdateTaskAssigneeResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func (c *controller) UpdateTaskAllowAllCommands(ctx context.Context, req entity.UpdateTaskAllowAllCommandsRequest) (*entity.UpdateTaskAllowAllCommandsResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	m.AllowAllCommands = req.AllowAllCommands
	m.UpdatedAt = time.Now()

	updated, err := c.repository.UpdateTask(ctx, m)
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
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}

	// Save attachments
	c.saveAttachments(req.Attachments)

	var attsData []byte
	if len(req.Attachments) > 0 {
		attsData, _ = json.Marshal(req.Attachments)
	}

	// Create a new message from human
	msg := model.Message{
		ID:          c.idgen.NextID(),
		CreatedAt:   time.Now(),
		TaskID:      m.ID,
		UserID:      monoflake.IDFromBase62(req.UserID).Int64(),
		Sender:      "human",
		Text:        req.Text,
		Attachments: datatypes.JSON(attsData),
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
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
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

	// 2. Delete from DB (repository handles cascaded message delete)
	err = c.repository.DeleteTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
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
		_ = c.storage.Delete(id)
	}

	return &entity.DeleteTaskResponse{}, nil
}

func (c *controller) UpdateMessageMetadata(ctx context.Context, req entity.UpdateMessageMetadataRequest) error {
	// Let's verify task access
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	_, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
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
		ID:           m.ID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		WorkspaceID:  m.WorkspaceID,
		UserID:       m.UserID,
		CreatedBy:    m.CreatedBy,
		Assignee:     m.Assignee,
		Status:       m.Status,
		Title:        m.Title,
		Body:         m.Body,
		Response:     m.Response,
		ReplyText:    m.ReplyText,
		Attachments:  atts,
		Messages:     msgs,
		CronSchedule: m.CronSchedule,
		ParentID:     m.ParentID,
		SortOrder:    m.SortOrder,
		AllowAllCommands: m.AllowAllCommands,
	}
}

func (c *controller) GetAttachment(ctx context.Context, req entity.GetAttachmentRequest) (*entity.GetAttachmentResponse, error) {
	// Need to check if user has access to the workspace
	// we just list all tasks for user and workspace and search attachmentid inside them
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	tasks, err := c.repository.ListTasks(ctx, entity.ListTasksRequest{WorkspaceID: req.WorkspaceID}, uid)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch tasks for attachment verification: %w", err)
	}

	for _, t := range tasks {
		if len(t.Attachments) > 0 {
			var atts []entity.Attachment
			if err := json.Unmarshal(t.Attachments, &atts); err == nil {
				for _, a := range atts {
					if a.ID == req.AttachmentID {
						data, _ := c.storage.LoadRaw(a.ID)
						return &entity.GetAttachmentResponse{Data: data, Filename: a.Filename, MimeType: a.MimeType}, nil
					}
				}
			}
		}
		for _, m := range t.Messages {
			if len(m.Attachments) > 0 {
				var atts []entity.Attachment
				if err := json.Unmarshal(m.Attachments, &atts); err == nil {
					for _, a := range atts {
						if a.ID == req.AttachmentID {
							data, _ := c.storage.LoadRaw(a.ID)
							return &entity.GetAttachmentResponse{Data: data, Filename: a.Filename, MimeType: a.MimeType}, nil
						}
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("attachment not found or access denied")
}

func (c *controller) saveAttachments(atts []entity.Attachment) {
	for i := range atts {
		if atts[i].ID == "" {
			atts[i].ID = monoflake.ID(c.idgen.NextID()).String()
		}
		if atts[i].Data != "" {
			_ = c.storage.Save(atts[i].ID, atts[i].Data)
			atts[i].Data = "" // clear from metadata
		}
	}
}
func (c *controller) UpdateScheduledTask(ctx context.Context, req entity.UpdateScheduledTaskRequest) (*entity.UpdateScheduledTaskResponse, error) {
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	m, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	if m.Status != "cron" {
		return nil, fmt.Errorf("only chronic tasks can be edited this way")
	}

	if req.CronSchedule == "" {
		m.Status = "notstarted"
		m.CronSchedule = ""
	} else {
		// Validate Cron Schedule
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(req.CronSchedule); err != nil {
			return nil, fmt.Errorf("invalid cron schedule: %w", err)
		}
		m.CronSchedule = req.CronSchedule
	}

	m.Title = req.Title
	m.Body = req.Body
	m.Assignee = req.Assignee
	m.AllowAllCommands = req.AllowAllCommands
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
	return &entity.UpdateScheduledTaskResponse{Task: c.fromModelTaskToEntity(updated)}, nil
}

func isValidTaskStatus(status string) bool {
	switch status {
	case "notstarted", "ongoing", "completed", "rejected", "cron", "blocked":
		return true
	}
	return false
}
