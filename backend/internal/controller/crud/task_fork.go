// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package crud

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/storage"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
	"gorm.io/datatypes"
)

const forkTitlePrefix = "Fork: "

// ForkTask copies a task, and its conversation up to and including one
// message, into a new task in the same workspace.
func (c *controller) ForkTask(ctx context.Context, req entity.ForkTaskRequest) (*entity.ForkTaskResponse, error) {
	userID := monoflake.IDFromBase62(req.UserID).Int64()
	if c.limiter != nil && !c.limiter.AllowTask(userID) {
		return nil, fmt.Errorf("rate limit exceeded")
	}
	if req.Status != "ongoing" && req.Status != "notstarted" {
		return nil, fmt.Errorf("invalid task status: %s", req.Status)
	}
	if _, err := c.ensureActiveWorkspace(ctx, req.WorkspaceID, req.UserID); err != nil {
		return nil, err
	}
	src, err := c.repository.GetTask(ctx, req.WorkspaceID, req.TaskID, userID)
	if err != nil {
		return nil, err
	}

	// GetTask preloads messages in no particular order.
	msgs := append([]model.Message(nil), src.Messages...)
	sort.SliceStable(msgs, func(i, j int) bool {
		if !msgs[i].CreatedAt.Equal(msgs[j].CreatedAt) {
			return msgs[i].CreatedAt.Before(msgs[j].CreatedAt)
		}
		return msgs[i].ID < msgs[j].ID
	})
	cut := -1
	for i, m := range msgs {
		if m.ID == req.MessageID {
			cut = i
			break
		}
	}
	if cut < 0 {
		return nil, base.ErrNotFound
	}
	msgs = conversationOnly(msgs[:cut+1])

	// Deleting a task deletes its attachment files, so the fork gets copies
	// under its own IDs rather than sharing the source's.
	var copied []string
	copyAtts := func(raw datatypes.JSON) datatypes.JSON {
		out, ids := c.copyAttachments(raw)
		copied = append(copied, ids...)
		return out
	}

	now := time.Now()
	fork := model.Task{
		ID:               c.idgen.NextID(),
		CreatedAt:        now,
		UpdatedAt:        now,
		UserID:           userID,
		WorkspaceID:      src.WorkspaceID,
		CreatedBy:        "human",
		Assignee:         src.Assignee,
		Status:           req.Status,
		Title:            forkTitle(src.Title),
		Body:             forkBody(src.Body, src.ID),
		Attachments:      copyAtts(src.Attachments),
		SortOrder:        float64(now.UnixMilli()) / 1000.0,
		AllowAllCommands: src.AllowAllCommands,
		ClearContext:     src.ClearContext,
	}
	copies := make([]model.Message, len(msgs))
	for i, m := range msgs {
		copies[i] = model.Message{
			ID:          c.idgen.NextID(),
			CreatedAt:   m.CreatedAt,
			TaskID:      fork.ID,
			UserID:      m.UserID,
			Sender:      m.Sender,
			Text:        m.Text,
			Attachments: copyAtts(m.Attachments),
			Metadata:    forkMetadata(m.Metadata),
		}
	}

	created, err := c.repository.CreateTaskWithMessages(ctx, fork, copies)
	if err != nil {
		for _, id := range copied {
			_ = c.storage.Delete(id)
		}
		return nil, fmt.Errorf("fork task: %w", err)
	}
	created.Messages = copies

	for _, action := range []entity.Action{entity.ActionTaskCreate, entity.ActionTaskFork} {
		c.emitEvent(ctx, entity.CRUDEvent{
			Action:       action,
			WorkspaceID:  created.WorkspaceID,
			UserID:       created.UserID,
			ResourceType: entity.ResourceTask,
			ResourceID:   created.ID,
			Actor:        entity.ActorHuman,
		})
	}

	return &entity.ForkTaskResponse{Task: c.fromModelTaskToEntity(created)}, nil
}

// copyAttachments stores a copy of each attachment's file under a new ID and
// returns the rewritten metadata with the new IDs. A file that can no longer
// be read — attachments are cleaned up after a while — is left out rather
// than failing the fork.
func (c *controller) copyAttachments(raw datatypes.JSON) (datatypes.JSON, []string) {
	if len(raw) == 0 {
		return nil, nil
	}
	var atts []entity.Attachment
	if err := json.Unmarshal(raw, &atts); err != nil || len(atts) == 0 {
		return nil, nil
	}
	out := make([]entity.Attachment, 0, len(atts))
	ids := make([]string, 0, len(atts))
	for _, a := range atts {
		data, err := c.storage.LoadRaw(a.ID)
		if err != nil {
			zlog.Warn().Err(err).Str("attachmentID", a.ID).Msg("fork: attachment file missing, skipped")
			continue
		}
		id := monoflake.ID(c.idgen.NextID()).String()
		link, err := storage.SaveAttachment(c.storage, id, base64.StdEncoding.EncodeToString(data), a.MimeType)
		if err != nil {
			zlog.Warn().Err(err).Str("attachmentID", a.ID).Msg("fork: attachment copy failed, skipped")
			continue
		}
		ids = append(ids, id)
		out = append(out, entity.Attachment{ID: id, Filename: a.Filename, MimeType: a.MimeType, URL: link})
	}
	if len(out) == 0 {
		return nil, ids
	}
	b, _ := json.Marshal(out)
	return datatypes.JSON(b), ids
}

// The conversation is the messages people and the agent wrote, the questions
// the agent asked with their answers, and its plans — the one piece of its
// telemetry the thread shows. Permission requests, thoughts and usage describe
// a run of the source task, not the conversation, and are left behind.
func conversationOnly(msgs []model.Message) []model.Message {
	out := make([]model.Message, 0, len(msgs))
	for _, m := range msgs {
		switch metadataType(m.Metadata) {
		case "", elicitationType, planType:
			out = append(out, m)
		}
	}
	return out
}

const (
	elicitationType = "elicitation_request"
	// mcp.MessageTypeAgentPlan; that package imports this one.
	planType = "agent_plan"
)

func metadataType(raw datatypes.JSON) string {
	if len(raw) == 0 {
		return ""
	}
	var meta struct {
		Type string `json:"type"`
	}
	_ = json.Unmarshal(raw, &meta)
	return meta.Type
}

// forkMetadata closes a question still waiting for an answer: the agent that
// asked it is waiting in the source task's run, so an answer given in the fork
// would reach nobody.
func forkMetadata(raw datatypes.JSON) datatypes.JSON {
	if metadataType(raw) != elicitationType {
		return raw
	}
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil || meta["status"] != "pending" {
		return raw
	}
	meta["status"] = "cancel"
	b, _ := json.Marshal(meta)
	return b
}

func forkTitle(title string) string {
	if strings.HasPrefix(title, forkTitlePrefix) {
		return title
	}
	t := forkTitlePrefix + title
	// The column is varchar(255).
	if r := []rune(t); len(r) > 255 {
		t = string(r[:255])
	}
	return t
}

// forkBody says where the task came from, in the body because that is what
// every route to the agent — the immediate push and the poller — sends it.
func forkBody(body string, sourceID int64) string {
	note := fmt.Sprintf("Forked from task %s. Its conversation, up to the message it was forked at, is copied into this task: read it (getTask with includeConversation) before you continue.",
		monoflake.ID(sourceID).String())
	if body == "" {
		return note
	}
	return body + "\n\n" + note
}
