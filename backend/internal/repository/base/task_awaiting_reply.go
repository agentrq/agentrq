// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"gorm.io/gorm"
)

// A task awaits a reply, for the supervisor, while a question to the human (a
// permission request or an elicitation) is still pending, or when the newest
// message of its conversation is a plain agent message ending in "?", trailing
// spaces, tabs and line breaks aside. The conversation is what the
// supervisor's messages action reads: plain messages and elicitations, never
// the agent's thoughts, usage or notices. coremcp's replyState is the same
// rule in Go, over the messages awaitingReplyMessages loads.
const (
	pgAwaitingReply = "(EXISTS (SELECT 1 FROM messages WHERE messages.task_id = tasks.id AND " + pgPendingRequest + ")" +
		" OR EXISTS (SELECT 1 FROM messages q WHERE q.id = (SELECT MAX(c.id) FROM messages c WHERE c.task_id = tasks.id AND " + pgConversation + ")" +
		" AND q.sender = 'agent' AND COALESCE(q.metadata->>'type', '') = ''" +
		" AND RTRIM(q.text, ' ' || chr(9) || chr(10) || chr(13)) LIKE ?))"
	sqliteAwaitingReply = "(EXISTS (SELECT 1 FROM messages WHERE messages.task_id = tasks.id AND " + sqlitePendingRequest + ")" +
		" OR EXISTS (SELECT 1 FROM messages q WHERE q.id = (SELECT MAX(c.id) FROM messages c WHERE c.task_id = tasks.id AND " + sqliteConversation + ")" +
		" AND q.sender = 'agent' AND COALESCE(CASE WHEN json_valid(q.metadata) THEN json_extract(q.metadata, '$.type') END, '') = ''" +
		" AND RTRIM(q.text, char(32, 9, 10, 13)) LIKE ?))"

	// endsWithQuestionMark is the LIKE pattern for the clauses' one value.
	endsWithQuestionMark = "%?"

	pgConversation     = "COALESCE(c.metadata->>'type', '') IN ('', 'elicitation_request')"
	sqliteConversation = "COALESCE(CASE WHEN json_valid(c.metadata) THEN json_extract(c.metadata, '$.type') END, '') IN ('', 'elicitation_request')"

	pgReplyMessages     = "((" + pgPendingRequest + ") OR id IN (SELECT MAX(c.id) FROM messages c WHERE c.task_id IN ? AND " + pgConversation + " GROUP BY c.task_id))"
	sqliteReplyMessages = "((" + sqlitePendingRequest + ") OR id IN (SELECT MAX(c.id) FROM messages c WHERE c.task_id IN ? AND " + sqliteConversation + " GROUP BY c.task_id))"
)

// awaitingReplyClause keeps the tasks awaiting a reply; its one value is
// endsWithQuestionMark.
func awaitingReplyClause(db *gorm.DB) string {
	if db.Dialector.Name() == "postgres" {
		return pgAwaitingReply
	}
	return sqliteAwaitingReply
}

// awaitingReplyMessages gives each task the only messages the rule reads: its
// pending requests and the newest message of its conversation, oldest first.
func (r *repository) awaitingReplyMessages(ctx context.Context, tasks []model.Task) error {
	db := r.conn(ctx)
	clause := sqliteReplyMessages
	if db.Dialector.Name() == "postgres" {
		clause = pgReplyMessages
	}
	ids := make([]int64, len(tasks))
	byID := make(map[int64]*model.Task, len(tasks))
	for i := range tasks {
		ids[i] = tasks[i].ID
		byID[tasks[i].ID] = &tasks[i]
	}
	var msgs []model.Message
	if err := db.Where("task_id IN ?", ids).Where(clause, ids).Order("id asc").Find(&msgs).Error; err != nil {
		return err
	}
	for _, m := range msgs {
		t := byID[m.TaskID]
		t.Messages = append(t.Messages, m)
	}
	return nil
}
