// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// a2uiType is the metadata type of an agent's message that carries a card
// (mcp.MessageTypeA2UI; that package imports this one).
const a2uiType = "a2ui"

// SendA2uiAction posts a click on a card's Button as the person's reply, and
// marks the card answered: a card is answered once.
func (c *controller) SendA2uiAction(ctx context.Context, req entity.SendA2uiActionRequest) (*entity.SendA2uiActionResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowMessage(uid) {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	m, err := c.repository.GetTaskRow(ctx, req.WorkspaceID, req.TaskID, uid)
	if err != nil {
		return nil, err
	}
	src, err := c.repository.SystemGetMessage(ctx, req.MessageID)
	if err != nil {
		return nil, err
	}
	if src.TaskID != m.ID {
		return nil, base.ErrNotFound
	}

	var meta map[string]any
	if json.Unmarshal(src.Metadata, &meta) != nil || meta["type"] != a2uiType {
		return nil, &entity.A2uiActionError{Message: "this message carries no card"}
	}
	if meta["surfaceId"] != req.SurfaceID {
		return nil, &entity.A2uiActionError{Message: fmt.Sprintf("this message's card is not surface %q", req.SurfaceID)}
	}
	if meta["answered"] != nil {
		return nil, errA2uiAnswered
	}
	components := cardComponents(src.Metadata)
	button := components[req.SourceComponentID]
	if button["component"] != "Button" || eventOf(button)["name"] != req.Name {
		return nil, &entity.A2uiActionError{Message: fmt.Sprintf("the card has no Button %q with action %q", req.SourceComponentID, req.Name)}
	}

	meta["answered"] = map[string]any{"name": req.Name, "at": time.Now().UTC()}
	b, _ := json.Marshal(meta)
	if ok, err := c.repository.AnswerMessage(ctx, req.TaskID, src.ID, b); err != nil {
		return nil, err
	} else if !ok {
		return nil, errA2uiAnswered
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMessageUpdate,
		WorkspaceID:  req.WorkspaceID,
		UserID:       uid,
		ResourceType: entity.ResourceMessage,
		ResourceID:   src.ID,
		Actor:        entity.ActorHuman,
	})

	text := actionText(req, button, components)
	// Not a type: typed metadata reads as no conversation in several places,
	// and this is the person's reply.
	action, _ := json.Marshal(map[string]any{"a2uiAction": map[string]any{
		"messageId": monoflake.ID(src.ID).String(),
		"surfaceId": req.SurfaceID,
		"name":      req.Name,
		"context":   req.Context,
	}})
	msg := model.Message{
		ID:        c.idgen.NextID(),
		CreatedAt: time.Now(),
		TaskID:    m.ID,
		UserID:    uid,
		Sender:    "human",
		Text:      text,
		Metadata:  datatypes.JSON(action),
	}
	t, err := c.postHumanMessage(ctx, req.WorkspaceID, m, msg)
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionA2UIAction,
		WorkspaceID:  req.WorkspaceID,
		UserID:       uid,
		ResourceType: entity.ResourceMessage,
		ResourceID:   msg.ID,
		Actor:        entity.ActorHuman,
	})
	return &entity.SendA2uiActionResponse{Task: t, MessageID: msg.ID, Text: text}, nil
}

var errA2uiAnswered = &entity.A2uiActionError{Message: "this card is already answered", Conflict: true}

// cardComponents is a stored card's components by id, each the latest
// definition of its id, as the page draws them.
func cardComponents(metadata []byte) map[string]map[string]any {
	var card struct {
		Messages []struct {
			UpdateComponents struct {
				Components []map[string]any `json:"components"`
			} `json:"updateComponents"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(metadata, &card) // the caller has read it as an object
	out := map[string]map[string]any{}
	for _, m := range card.Messages {
		for _, c := range m.UpdateComponents.Components {
			if id, ok := c["id"].(string); ok {
				out[id] = c
			}
		}
	}
	return out
}

func eventOf(button map[string]any) map[string]any {
	action, _ := button["action"].(map[string]any)
	event, _ := action["event"].(map[string]any)
	return event
}

// actionText is the reply a click posts: the page's resolved userMessage, else
// the Button's literal one, else its label, else the action's name.
func actionText(req entity.SendA2uiActionRequest, button map[string]any, components map[string]map[string]any) string {
	if s := strings.TrimSpace(req.UserMessage); s != "" {
		return s
	}
	if s, ok := eventOf(button)["userMessage"].(string); ok && strings.TrimSpace(s) != "" {
		return strings.TrimSpace(s)
	}
	child, _ := button["child"].(string)
	if label := components[child]; label["component"] == "Text" {
		if s, ok := label["text"].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return req.Name
}
