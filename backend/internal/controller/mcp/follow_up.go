// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package mcp

import (
	"context"
	"sort"
	"strings"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// followUpHeader opens the follow-ups a supervisor posted to a task, which go
// before its body: an agent handed a reopened task, often on a cleared
// context, would otherwise redo the body and never see what was asked next.
const followUpHeader = "Follow-up from supervisor (do this; the original task is below for context):"

// followUpBlock is t's follow-ups, oldest first, as a line block to put after
// its ID, or "" when it has none. t's messages are read when it came without
// them, as a listed or dequeued task does.
func (ps *WorkspaceServer) followUpBlock(ctx context.Context, t model.Task) string {
	msgs := t.Messages
	if msgs == nil && ps.getTask != nil {
		if full, err := ps.getTask(ctx, t.ID); err == nil {
			msgs = full.Messages
		}
	}
	var followUps []model.Message
	for _, m := range msgs {
		if crud.IsFollowUp(m) {
			followUps = append(followUps, m)
		}
	}
	if len(followUps) == 0 {
		return ""
	}
	sort.Slice(followUps, func(i, j int) bool { return followUps[i].ID < followUps[j].ID })
	var sb strings.Builder
	sb.WriteString("\n" + followUpHeader)
	for _, m := range followUps {
		sb.WriteString("\n" + m.Text)
	}
	return sb.String()
}
