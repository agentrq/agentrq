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

	// The click is checked against the card as it is under its row's lock,
	// and the reply is stored with the mark: an agent's update or deletion
	// is never overwritten, and a card is never answered without its reply.
	answered, _ := json.Marshal(map[string]any{"name": req.Name, "at": time.Now().UTC()})
	var msg model.Message
	err = c.repository.AnswerCard(ctx, m.ID, req.MessageID, func(src model.Message) (model.Message, []byte, error) {
		text, err := clickText(req, src.Metadata)
		if err != nil {
			return model.Message{}, nil, err
		}
		// Not a type: typed metadata reads as no conversation in several
		// places, and this is the person's reply.
		action, _ := json.Marshal(map[string]any{"a2uiAction": map[string]any{
			"messageId": monoflake.ID(src.ID).String(),
			"surfaceId": req.SurfaceID,
			"name":      req.Name,
			"context":   req.Context,
		}})
		msg = model.Message{
			ID:        c.idgen.NextID(),
			CreatedAt: time.Now(),
			TaskID:    m.ID,
			UserID:    uid,
			Sender:    "human",
			Text:      text,
			Metadata:  datatypes.JSON(action),
		}
		return msg, answered, nil
	})
	if err != nil {
		return nil, err
	}
	c.emitEvent(ctx, entity.CRUDEvent{
		Action:       entity.ActionMessageUpdate,
		WorkspaceID:  req.WorkspaceID,
		UserID:       uid,
		ResourceType: entity.ResourceMessage,
		ResourceID:   req.MessageID,
		Actor:        entity.ActorHuman,
	})
	t, err := c.humanMessagePosted(ctx, req.WorkspaceID, m, msg)
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
	return &entity.SendA2uiActionResponse{Task: t, MessageID: msg.ID, Text: msg.Text}, nil
}

// clickText checks a click against the card stored as metadata, and is the
// reply it posts.
func clickText(req entity.SendA2uiActionRequest, metadata []byte) (string, error) {
	var meta map[string]any
	if json.Unmarshal(metadata, &meta) != nil || meta["type"] != a2uiType {
		return "", &entity.A2uiActionError{Message: "this message carries no card"}
	}
	if meta["surfaceId"] != req.SurfaceID {
		return "", &entity.A2uiActionError{Message: fmt.Sprintf("this message's card is not surface %q", req.SurfaceID)}
	}
	if meta["answered"] != nil {
		return "", errA2uiAnswered
	}
	components := cardComponents(metadata)
	button := components[req.SourceComponentID]
	if button["component"] != "Button" || eventOf(button)["name"] != req.Name {
		return "", &entity.A2uiActionError{Message: fmt.Sprintf("the card has no Button %q with action %q", req.SourceComponentID, req.Name)}
	}
	return actionText(req, button, components), nil
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
