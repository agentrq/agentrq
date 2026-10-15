// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package app

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// agentTasks is what a workspace's agent changes on its tasks over MCP.
type agentTasks struct {
	repo        base.Repository
	pubsub      pubsub.Service
	bus         *eventbus.Bus
	storage     storage.Service
	ids         idgen.Service
	workspaceID int64
	contentID   int64
	owner       string
}

// reply posts the agent's message to a task, starting a task that had not.
func (a agentTasks) reply(ctx context.Context, chatID string, text string, attachments []entity.Attachment, metadata any) (int64, error) {
	id := monoflake.IDFromBase62(chatID)
	if id == 0 {
		return 0, fmt.Errorf("invalid chat ID: %s", chatID)
	}
	taskID := id.Int64()
	m, err := workspaceTask(ctx, a.repo, a.workspaceID, a.owner, taskID)
	if err != nil {
		return 0, err
	}

	crud.SaveAttachments(a.storage, a.ids, a.contentID, taskID, attachments)

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
	if m.Status == "notstarted" {
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
