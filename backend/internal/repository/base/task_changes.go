// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// A task changed after since when its row was written after it, or a message
// of its conversation was. A message never touches its task's row, so the
// second half is what catches a reply. A UNION, not an OR, so that
// idx_tasks_user_id_updated_at and idx_messages_user_id_created_at make both
// halves range scans rather than a read of every task of the account.
const (
	taskChangedClause = "id IN (SELECT t.id FROM tasks t WHERE t.user_id = ? AND t.updated_at > ? UNION SELECT c.task_id FROM messages c WHERE c.user_id = ? AND c.created_at > ? AND "
	pgTaskChanged     = taskChangedClause + pgConversation + ")"
	sqliteTaskChanged = taskChangedClause + sqliteConversation + ")"

	// A task's change time: its row's, or its conversation's newest
	// message's, whichever is later. coremcp computes the same from the
	// task's reply messages, to say where a cut list stopped.
	pgTaskChangedAt     = "GREATEST(tasks.updated_at, (SELECT MAX(c.created_at) FROM messages c WHERE c.task_id = tasks.id AND " + pgConversation + ")), tasks.id"
	sqliteTaskChangedAt = "MAX(tasks.updated_at, COALESCE((SELECT MAX(c.created_at) FROM messages c WHERE c.task_id = tasks.id AND " + sqliteConversation + "), tasks.updated_at)), tasks.id"

	pgNewMessages     = "c.task_id IN ? AND c.created_at > ? AND " + pgConversation
	sqliteNewMessages = "c.task_id IN ? AND c.created_at > ? AND " + sqliteConversation
)

// ListTaskChanges returns the user's tasks that changed after req.Since, the
// least recently changed first, at most req.Limit, each with the messages
// awaitingReplyMessages gives it.
func (r *repository) ListTaskChanges(ctx context.Context, req entity.ListTaskChangesRequest, userID int64) ([]model.Task, error) {
	db := r.conn(ctx)
	changed, order := sqliteTaskChanged, sqliteTaskChangedAt
	if db.Dialector.Name() == "postgres" {
		changed, order = pgTaskChanged, pgTaskChangedAt
	}
	// SQLite compares times as text, which holds only in the zone they were
	// written in: the server's own.
	since := req.Since.Local()
	q := db.Where("user_id = ?", userID).Where(changed, userID, since, userID, since)
	if req.WorkspaceID != 0 {
		q = q.Where("workspace_id = ?", req.WorkspaceID)
	}
	if req.CreatedByWorkspaceID != 0 {
		q = q.Where("created_by_workspace_id = ?", req.CreatedByWorkspaceID)
	}
	var tasks []model.Task
	if err := q.Order(order).Limit(req.Limit).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return tasks, nil
	}
	return tasks, r.awaitingReplyMessages(ctx, tasks)
}

// CountNewMessages counts, by task, the messages of each task's conversation
// created after since. A task with none is left out.
func (r *repository) CountNewMessages(ctx context.Context, taskIDs []int64, since time.Time) (map[int64]int, error) {
	db := r.conn(ctx)
	clause := sqliteNewMessages
	if db.Dialector.Name() == "postgres" {
		clause = pgNewMessages
	}
	var rows []struct {
		TaskID int64
		N      int
	}
	if err := db.Table("messages c").Select("c.task_id AS task_id, COUNT(*) AS n").
		Where(clause, taskIDs, since.Local()).Group("c.task_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.TaskID] = row.N
	}
	return out, nil
}

// ListStatusChanges returns, by task, when its status last changed after
// since. A task whose status did not is left out. A wait on the human is not
// a status: the history's needsinput rows leave the task's status as it was,
// unless the task left the wait in another one.
func (r *repository) ListStatusChanges(ctx context.Context, taskIDs []int64, since time.Time) (map[int64]time.Time, error) {
	db := r.conn(ctx)
	// The rows after since, and the one before them that says where each
	// task stood at since.
	before := db.Model(&model.TaskStateTransition{}).Select("MAX(id)").
		Where("task_id IN ? AND created_at <= ?", taskIDs, since.Local()).Group("task_id")
	var rows []model.TaskStateTransition
	if err := db.Where("task_id IN ?", taskIDs).Where("(created_at > ? OR id IN (?))", since.Local(), before).
		Order("id asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	status := map[int64]model.TaskState{}
	out := map[int64]time.Time{}
	for _, row := range rows {
		s := row.ToState
		if s == model.TaskStateNeedsInput {
			s = row.FromState
		}
		if s != status[row.TaskID] && row.CreatedAt.After(since) {
			out[row.TaskID] = row.CreatedAt
		}
		status[row.TaskID] = s
	}
	return out, nil
}
