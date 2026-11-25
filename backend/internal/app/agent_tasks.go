// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
)

// agentTasks is what a workspace's agent changes on its tasks over MCP.
type agentTasks struct {
	repo        base.Repository
	pubsub      pubsub.Service
	bus         *eventbus.Bus
	ids         idgen.Service
	workspaceID int64
	owner       string
}

// reply posts the agent's message to a task, starting a task that had not.
// Its attachments are already saved: the MCP server saves them first, since
// a card in the message names them by their stored links.
func (a agentTasks) reply(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error) {
	// What the agent writes is put down to its workspace.
	ctx = entity.WithAgentWorkspace(ctx, a.workspaceID)
	id := monoflake.IDFromBase62(chatID)
	if id == 0 {
		return 0, fmt.Errorf("invalid chat ID: %s", chatID)
	}
	taskID := id.Int64()
	m, err := workspaceTask(ctx, a.repo, a.workspaceID, a.owner, taskID)
	if err != nil {
		return 0, err
	}

	var attsData []byte
	if len(attachments) > 0 {
		attsData, _ = json.Marshal(attachments)
	}

	var metadataJSON datatypes.JSON
	if metadata != nil {
		if b, err := json.Marshal(metadata); err == nil {
			metadataJSON = datatypes.JSON(b)
		}
	}

	uid := monoflake.IDFromBase62(a.owner).Int64()
	// A usage footer is no sign the task was started: a turn marks its task
	// ongoing before it prompts, and a delivery stopped before its turn began
	// still ends with one (#1351), which must leave the task to be sent again.
	if m.Status == "notstarted" && !isUsageFooter(metadataJSON) {
		m.Status = "ongoing"
		_, _ = a.repo.UpdateTask(ctx, m, "Status")
	}

	msgID := a.ids.NextID()
	msg := model.Message{
		ID:          msgID,
		CreatedAt:   time.Now(),
		TaskID:      taskID,
		UserID:      uid,
		Sender:      "agent",
		Text:        text,
		Attachments: datatypes.JSON(attsData),
		Metadata:    metadataJSON,
	}

	if err := a.repo.CreateMessage(ctx, msg); err != nil {
		return 0, err
	}

	a.pubsub.Publish(context.Background(), pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event: entity.CRUDEvent{
			Action:       entity.ActionMessageCreate,
			WorkspaceID:  a.workspaceID,
			UserID:       uid,
			ResourceType: entity.ResourceMessage,
			ResourceID:   msgID,
			Actor:        entity.ActorAgent,
			Origin:       entity.OriginMCP,
		},
	})

	uid = monoflake.IDFromBase62(a.owner).Int64()
	latest, err := a.repo.GetTask(ctx, a.workspaceID, taskID, uid)
	if err == nil {
		a.bus.Publish(a.workspaceID, a.owner, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromModelTaskToView(latest),
		})
	}
	return msgID, nil
}

// editMessage changes one of the task's messages in place, with what edit
// makes of it. An edit is no new message: nothing is notified, and the page
// only reloads the task.
func (a agentTasks) editMessage(ctx context.Context, taskID, messageID int64, edit func(model.Message) (string, []byte, error)) error {
	if _, err := workspaceTask(ctx, a.repo, a.workspaceID, a.owner, taskID); err != nil {
		return err
	}
	if err := a.repo.EditMessage(ctx, taskID, messageID, edit); err != nil {
		return err
	}
	latest, err := a.repo.GetTask(ctx, a.workspaceID, taskID, monoflake.IDFromBase62(a.owner).Int64())
	if err == nil {
		a.bus.Publish(a.workspaceID, a.owner, eventbus.Event{
			Type:    "task.updated",
			Payload: mapper.FromModelTaskToView(latest),
		})
	}
	return nil
}

// isUsageFooter is whether a message's metadata makes it the task's usage
// footer.
func isUsageFooter(metadata datatypes.JSON) bool {
	var m struct {
		Type string `json:"type"`
	}
	return len(metadata) > 0 && json.Unmarshal(metadata, &m) == nil && m.Type == mcp.MessageTypeAgentUsage
}
