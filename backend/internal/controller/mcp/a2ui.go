// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
)

// MessageTypeA2UI marks a message that carries a card drawn with A2UI.
const MessageTypeA2UI = "a2ui"

// a2uiVersion is the A2UI version a card is validated against.
const a2uiVersion = "1.0"

// A2UIMetadata is what a message with a card stores as its metadata: the
// validated messages of its one surface, media resolved to stored files.
type A2UIMetadata struct {
	Type      string            `json:"type"`
	Version   string            `json:"version"`
	SurfaceID string            `json:"surfaceId"`
	Messages  []json.RawMessage `json:"messages"`
}

// sharedA2UI is the validator every server uses, loaded at the first card:
// the schemas take ~200 ms to resolve, so once per process.
var sharedA2UI = sync.OnceValues(a2ui.New)

// elicitModeA2UI is the elicit mode whose question is drawn as a card.
const elicitModeA2UI = "a2ui"

// replyCard validates a reply's card against the files already saved with
// it, and returns the message metadata that carries it.
func (ps *WorkspaceServer) replyCard(ctx context.Context, taskID int64, ui json.RawMessage, atts []entity.Attachment) (*A2UIMetadata, error) {
	surface, err := ps.validateCard(ctx, ui, ps.attachmentURLs(taskID, atts), "send the reply without it")
	if err != nil {
		return nil, err
	}
	return &A2UIMetadata{Type: MessageTypeA2UI, Version: a2uiVersion, SurfaceID: surface.SurfaceID, Messages: surface.Messages}, nil
}

// validateCard checks a card, its media resolved from attachments; without
// the validator, the error says to do instead.
func (ps *WorkspaceServer) validateCard(ctx context.Context, ui json.RawMessage, attachments map[string]string, instead string) (*a2ui.Surface, error) {
	validator := ps.a2ui
	if validator == nil {
		var err error
		if validator, err = sharedA2UI(); err != nil {
			return nil, fmt.Errorf("ui is not available on this server (%v): %s", err, instead)
		}
	}
	return validator.Validate(ctx, ui, a2ui.Options{Interactive: true, Attachments: attachments})
}

// attachmentURLs maps each saved file of a reply to where the page reads it:
// its public link, or the task's download route when the store makes none.
func (ps *WorkspaceServer) attachmentURLs(taskID int64, atts []entity.Attachment) map[string]string {
	urls := make(map[string]string, len(atts))
	for _, a := range atts {
		if a.ID == "" || a.Filename == "" {
			continue
		}
		u := a.URL
		if u == "" {
			u = fmt.Sprintf("/api/v1/workspaces/%s/tasks/%s/attachments/%s", monoflake.ID(ps.workspaceID).String(), monoflake.ID(taskID).String(), a.ID)
		}
		urls[a.Filename] = u
	}
	return urls
}

// deleteAttachments removes the files saved for a reply that was not posted.
func (ps *WorkspaceServer) deleteAttachments(taskID int64, atts []entity.Attachment) {
	for _, a := range atts {
		if a.ID != "" {
			storage.DeleteAttachment(ps.storage, ps.contentID(), taskID, a.ID)
		}
	}
}

// emitA2UI counts a message that carried a card: a reply, or a question.
func (ps *WorkspaceServer) emitA2UI(ctx context.Context, action entity.Action, msgID int64) {
	if ps.pubsub == nil {
		return
	}
	ps.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event: entity.CRUDEvent{
			Action:       action,
			WorkspaceID:  ps.workspaceID,
			UserID:       monoflake.IDFromBase62(ps.userID).Int64(),
			ResourceType: entity.ResourceMessage,
			ResourceID:   msgID,
			Actor:        entity.ActorAgent,
			Origin:       entity.OriginMCP,
		},
	})
}

// agentMetadata is what an agent reading the thread gets of a message's
// metadata, and whether it carries a card. A card is for the page: a reply's
// is left out, and a question drawn as one keeps all but its messages.
func agentMetadata(metadata []byte) (string, bool) {
	var m map[string]json.RawMessage
	if json.Unmarshal(metadata, &m) != nil {
		return string(metadata), false
	}
	var typ, mode string
	_ = json.Unmarshal(m["type"], &typ)
	_ = json.Unmarshal(m["mode"], &mode)
	switch {
	case typ == MessageTypeA2UI:
		return "", true
	case typ == "elicitation_request" && mode == elicitModeA2UI:
		delete(m, "messages")
		b, _ := json.Marshal(m)
		return string(b), true
	}
	return string(metadata), false
}
