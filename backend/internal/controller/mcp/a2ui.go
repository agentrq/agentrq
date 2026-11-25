// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/mustafaturan/monoflake"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/service/a2ui"
	"github.com/agentrq/agentrq/backend/internal/service/mcp"
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
	validator, err := ps.cardValidator(instead)
	if err != nil {
		return nil, err
	}
	return validator.Validate(ctx, ui, a2ui.Options{Interactive: true, Attachments: attachments})
}

// cardValidator is the validator; without it, the error says to do instead.
func (ps *WorkspaceServer) cardValidator(instead string) (a2ui.Service, error) {
	if ps.a2ui != nil {
		return ps.a2ui, nil
	}
	validator, err := sharedA2UI()
	if err != nil {
		return nil, fmt.Errorf("ui is not available on this server (%v): %s", err, instead)
	}
	return validator, nil
}

// updateCard applies a reply's ui that creates no surface to the task's
// newest card with that surface id, and makes text the message's text. It is
// an edit: no new message, so nobody is notified.
func (ps *WorkspaceServer) updateCard(ctx context.Context, req *mcp.CallToolRequest, taskID int64, surfaceID string, params ReplyParams) (*mcp.CallToolResult, any, error) {
	fail := func(err error) (*mcp.CallToolResult, any, error) {
		return &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: err.Error()}}}, nil, nil
	}
	if len(params.Attachments) > 0 {
		return fail(fmt.Errorf("ui updates card %q, which shows only the files its own reply attached: send the update without attachments, or a new card that starts with createSurface", surfaceID))
	}
	msgID, err := ps.findCard(ctx, taskID, surfaceID)
	if err != nil {
		return fail(err)
	}
	validator, err := ps.cardValidator("send the reply without it")
	if err != nil {
		return fail(err)
	}
	var merged []byte
	err = ps.editMessage(ctx, taskID, msgID, func(m model.Message) (string, []byte, error) {
		text, metadata, err := ps.mergeCard(ctx, validator, m, surfaceID, params)
		merged = metadata
		return text, metadata, err
	})
	if err != nil {
		return fail(err)
	}
	ps.emitA2UI(ctx, entity.ActionA2UIUpdate, msgID)
	ps.emitA2UIResource(ctx, params, msgID)
	ps.emitA2UIChart(ctx, json.RawMessage(merged), msgID)
	ps.noteSessionTask(req, taskID)
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("card %q updated in message %s", surfaceID, monoflake.ID(msgID).String())}},
	}, nil, nil
}

// findCard is the task's newest message whose card is surfaceID.
func (ps *WorkspaceServer) findCard(ctx context.Context, taskID int64, surfaceID string) (int64, error) {
	task, err := ps.getTask(ctx, taskID)
	if err != nil {
		return 0, fmt.Errorf("failed to get task: %v", err)
	}
	for i := len(task.Messages) - 1; i >= 0; i-- {
		var meta struct {
			Type      string `json:"type"`
			Mode      string `json:"mode"`
			SurfaceID string `json:"surfaceId"`
		}
		if json.Unmarshal(task.Messages[i].Metadata, &meta) != nil || meta.SurfaceID != surfaceID {
			continue
		}
		switch {
		case meta.Type == MessageTypeA2UI:
			return task.Messages[i].ID, nil
		case meta.Type == "elicitation_request" && meta.Mode == elicitModeA2UI:
			return 0, fmt.Errorf("card %q is a question asked with elicit, which cannot be updated: send a new card that starts with createSurface", surfaceID)
		}
	}
	return 0, fmt.Errorf("no card with surfaceId %q in task %s: a ui without createSurface updates a card already in the task; start a new one with createSurface", surfaceID, monoflake.ID(taskID).String())
}

// mergeCard is the text and metadata of m once params' ui is applied to its
// card, read under the message's lock: a card answered or changed since it
// was found is refused.
func (ps *WorkspaceServer) mergeCard(ctx context.Context, validator a2ui.Service, m model.Message, surfaceID string, params ReplyParams) (string, []byte, error) {
	var meta struct {
		A2UIMetadata
		Answered json.RawMessage `json:"answered"`
	}
	if json.Unmarshal(m.Metadata, &meta) != nil || meta.Type != MessageTypeA2UI || meta.SurfaceID != surfaceID {
		return "", nil, fmt.Errorf("card %q is no longer in message %s: start a new card with createSurface", surfaceID, monoflake.ID(m.ID).String())
	}
	if len(meta.Answered) > 0 && string(meta.Answered) != "null" {
		return "", nil, fmt.Errorf("card %q was answered, so it cannot be updated: send a new card that starts with createSurface", surfaceID)
	}
	var atts []entity.Attachment
	_ = json.Unmarshal(m.Attachments, &atts)
	surface, err := validator.Merge(ctx, &a2ui.Surface{SurfaceID: surfaceID, Messages: meta.Messages}, params.UI,
		a2ui.Options{Interactive: true, Attachments: ps.attachmentURLs(m.TaskID, atts)})
	if err != nil {
		return "", nil, err
	}
	if surface.Deleted {
		// The message keeps its text and drops the card.
		return params.Text, nil, nil
	}
	b, _ := json.Marshal(A2UIMetadata{Type: MessageTypeA2UI, Version: a2uiVersion, SurfaceID: surfaceID, Messages: surface.Messages})
	return params.Text, b, nil
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

// a2uiMimeType marks an embedded resource that carries A2UI messages.
const a2uiMimeType = "application/a2ui+json"

// addA2UICapability advertises the cards this server draws, as A2UI's MCP
// binding names them: version 1.0 of the basic catalog, and of ours.
func addA2UICapability(caps *mcp.ServerCapabilities) {
	caps.A2UI = map[string]any{
		"rendererCapabilities": map[string]any{
			a2uiVersion: map[string]any{"supportedCatalogIds": a2ui.CatalogIDs},
		},
	}
}

// replyUI is a reply's ui as A2UI messages, and whether it came as A2UI's
// MCP binding: an embedded resource whose text (or blob) is the array, which
// an agent framework emitting A2UI posts unchanged.
func replyUI(ui json.RawMessage) (json.RawMessage, bool, error) {
	trimmed := bytes.TrimSpace(ui)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return ui, false, nil
	}
	var r struct {
		Type     string `json:"type"`
		Resource struct {
			MimeType string `json:"mimeType"`
			Text     string `json:"text"`
			Blob     []byte `json:"blob"`
		} `json:"resource"`
	}
	if json.Unmarshal(trimmed, &r) != nil || r.Type != "resource" || r.Resource.MimeType != a2uiMimeType {
		return nil, false, fmt.Errorf(`ui is a JSON array of A2UI messages, or an embedded resource {"type": "resource", "resource": {"mimeType": %q, "text": "<that array>"}}`, a2uiMimeType)
	}
	if r.Resource.Text != "" {
		return json.RawMessage(r.Resource.Text), true, nil
	}
	if len(r.Resource.Blob) > 0 {
		return r.Resource.Blob, true, nil
	}
	return nil, false, errors.New("ui's resource has no text: its text is the JSON array of A2UI messages")
}

// emitA2UIChart counts a card, stored as metadata, that draws a Chart.
func (ps *WorkspaceServer) emitA2UIChart(ctx context.Context, metadata any, msgID int64) {
	var card struct {
		Messages []json.RawMessage `json:"messages"`
	}
	b, _ := json.Marshal(metadata) // metadata the server just stored
	_ = json.Unmarshal(b, &card)
	if a2ui.HasChart(&a2ui.Surface{Messages: card.Messages}) {
		ps.emitA2UI(ctx, entity.ActionA2UIChart, msgID)
	}
}

// emitA2UIResource counts a card that came as A2UI's MCP binding.
func (ps *WorkspaceServer) emitA2UIResource(ctx context.Context, params ReplyParams, msgID int64) {
	if params.viaResource {
		ps.emitA2UI(ctx, entity.ActionA2UIResource, msgID)
	}
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
