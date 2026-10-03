// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package crud

import (
	"context"
	"encoding/json"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
)

// elicitationMaxWait is the longest the elicit tool waits (mcp's
// elicitMaxTimeout; that package imports this one). Questions asked before
// they carried an expiresAt are past their deadline once this has passed.
const elicitationMaxWait = time.Hour

// CloseExpiredElicitation closes a question to the human that is past its
// deadline, for an answer that found nobody waiting for it.
//
// Only the deadline makes that safe: the agent may be waiting on another
// instance, so nobody waiting here proves nothing before it.
func (c *controller) CloseExpiredElicitation(ctx context.Context, req entity.CloseExpiredElicitationRequest) (*entity.CloseExpiredElicitationResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	t, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	for _, msg := range t.Messages {
		meta, ok := pendingElicitation(msg, req.RequestID)
		if !ok {
			continue
		}
		if time.Now().Before(elicitationDeadline(meta, msg.CreatedAt)) {
			return &entity.CloseExpiredElicitationResponse{}, nil
		}
		meta["status"] = "cancel"
		b, _ := json.Marshal(meta)
		if err := c.repository.UpdateMessageMetadata(ctx, req.TaskID, msg.ID, b); err != nil {
			return nil, err
		}
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       entity.ActionMessageUpdate,
			WorkspaceID:  req.WorkspaceID,
			UserID:       uid,
			ResourceType: entity.ResourceMessage,
			ResourceID:   msg.ID,
			Actor:        entity.ActorHuman,
		})
		if t, err = c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, uid); err != nil {
			return nil, err
		}
		return &entity.CloseExpiredElicitationResponse{Closed: true, Task: c.fromModelTaskToEntity(t)}, nil
	}
	return &entity.CloseExpiredElicitationResponse{}, nil
}

// pendingElicitation is msg's metadata when msg is the still-pending question
// requestID names.
func pendingElicitation(msg model.Message, requestID string) (map[string]any, bool) {
	if metadataType(msg.Metadata) != elicitationType {
		return nil, false
	}
	var meta map[string]any
	_ = json.Unmarshal(msg.Metadata, &meta)
	return meta, meta["requestId"] == requestID && meta["status"] == "pending"
}

func elicitationDeadline(meta map[string]any, askedAt time.Time) time.Time {
	if s, ok := meta["expiresAt"].(string); ok {
		if at, err := time.Parse(time.RFC3339, s); err == nil {
			return at
		}
	}
	return askedAt.Add(elicitationMaxWait)
}
