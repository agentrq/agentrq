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

// replyCard validates a reply's card against the files already saved with
// it, and returns the message metadata that carries it.
func (ps *WorkspaceServer) replyCard(ctx context.Context, taskID int64, ui json.RawMessage, atts []entity.Attachment) (*A2UIMetadata, error) {
	validator := ps.a2ui
	if validator == nil {
		var err error
		if validator, err = sharedA2UI(); err != nil {
			return nil, fmt.Errorf("ui is not available on this server (%v): send the reply without it", err)
		}
	}
	surface, err := validator.Validate(ctx, ui, a2ui.Options{Interactive: true, Attachments: ps.attachmentURLs(taskID, atts)})
	if err != nil {
		return nil, err
	}
	return &A2UIMetadata{Type: MessageTypeA2UI, Version: a2uiVersion, SurfaceID: surface.SurfaceID, Messages: surface.Messages}, nil
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

// emitA2UIReply counts a reply that carried a card.
func (ps *WorkspaceServer) emitA2UIReply(ctx context.Context, msgID int64) {
	if ps.pubsub == nil {
		return
	}
	ps.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event: entity.CRUDEvent{
			Action:       entity.ActionA2UIReply,
			WorkspaceID:  ps.workspaceID,
			UserID:       monoflake.IDFromBase62(ps.userID).Int64(),
			ResourceType: entity.ResourceMessage,
			ResourceID:   msgID,
			Actor:        entity.ActorAgent,
			Origin:       entity.OriginMCP,
		},
	})
}

// isA2UIMessage is whether a message's metadata carries a card.
func isA2UIMessage(metadata []byte) bool {
	var m struct {
		Type string `json:"type"`
	}
	return len(metadata) > 0 && json.Unmarshal(metadata, &m) == nil && m.Type == MessageTypeA2UI
}
