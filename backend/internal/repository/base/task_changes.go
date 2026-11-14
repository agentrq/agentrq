// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"time"

	"gorm.io/gorm/clause"

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

	// Leaving out one workspace's own writes, a task changed after since
	// when a message of its conversation, or a move of its status, not put
	// down to that workspace was: a row write is no one's in particular.
	// idx_task_state_transitions_user_id_created_at makes the second half a
	// range scan too.
	othersMessagesClause = "id IN (SELECT c.task_id FROM messages c WHERE c.user_id = ? AND c.created_at > ? AND c.by_workspace_id <> ? AND "
	othersMovesClause    = " UNION SELECT s.task_id FROM task_state_transitions s WHERE s.user_id = ? AND s.created_at > ? AND s.by_workspace_id <> ?)"
	pgOthersChanged      = othersMessagesClause + pgConversation + othersMovesClause
	sqliteOthersChanged  = othersMessagesClause + sqliteConversation + othersMovesClause

	// The time of a task's newest change by the others: what ListStatusChanges
	// and ListNewestMessages give coremcp, to say where a cut list stopped.
	// SQLite's MAX is NULL when either is, and '' sorts before any time.
	othersMessageAt       = "(SELECT MAX(c.created_at) FROM messages c WHERE c.task_id = tasks.id AND c.by_workspace_id <> ? AND "
	othersMoveAt          = "(SELECT MAX(s.created_at) FROM task_state_transitions s WHERE s.task_id = tasks.id AND s.by_workspace_id <> ?)"
	pgOthersChangedAt     = "GREATEST(" + othersMessageAt + pgConversation + "), " + othersMoveAt + "), tasks.id"
	sqliteOthersChangedAt = "MAX(COALESCE(" + othersMessageAt + sqliteConversation + "), ''), COALESCE(" + othersMoveAt + ", '')), tasks.id"

	pgNewestMessages     = "id IN (SELECT MAX(c.id) FROM messages c WHERE c.task_id IN ? AND c.created_at > ? AND c.by_workspace_id <> ? AND " + pgConversation + " GROUP BY c.task_id)"
	sqliteNewestMessages = "id IN (SELECT MAX(c.id) FROM messages c WHERE c.task_id IN ? AND c.created_at > ? AND c.by_workspace_id <> ? AND " + sqliteConversation + " GROUP BY c.task_id)"
)

// ListTaskChanges returns the user's tasks that changed after req.Since, the
// least recently changed first, at most req.Limit, each with the messages
// awaitingReplyMessages gives it. With excludeWorkspaceID, only the changes
// not put down to that workspace count.
func (r *repository) ListTaskChanges(ctx context.Context, req entity.ListTaskChangesRequest, userID, excludeWorkspaceID int64) ([]model.Task, error) {
	db := r.conn(ctx)
	pg := db.Dialector.Name() == "postgres"
	// SQLite compares times as text, which holds only in the zone they were
	// written in: the server's own.
	since := req.Since.Local()
	q := db.Where("user_id = ?", userID)
	if excludeWorkspaceID == 0 {
		changed, order := sqliteTaskChanged, sqliteTaskChangedAt
		if pg {
			changed, order = pgTaskChanged, pgTaskChangedAt
		}
		q = q.Where(changed, userID, since, userID, since).Order(order)
	} else {
		changed, order := sqliteOthersChanged, sqliteOthersChangedAt
		if pg {
			changed, order = pgOthersChanged, pgOthersChangedAt
		}
		q = q.Where(changed, userID, since, excludeWorkspaceID, userID, since, excludeWorkspaceID).
			Order(clause.OrderBy{Expression: clause.Expr{SQL: order, Vars: []any{excludeWorkspaceID, excludeWorkspaceID}, WithoutParentheses: true}})
	}
	if req.WorkspaceID != 0 {
		q = q.Where("workspace_id = ?", req.WorkspaceID)
	}
	if req.CreatedByWorkspaceID != 0 {
		q = q.Where("created_by_workspace_id = ?", req.CreatedByWorkspaceID)
	}
	var tasks []model.Task
	if err := q.Limit(req.Limit).Find(&tasks).Error; err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		return tasks, nil
	}
	return tasks, r.awaitingReplyMessages(ctx, tasks)
}

// CountNewMessages counts, by task, the messages of each task's conversation
// created after since, but for excludeWorkspaceID's when not 0. A task with
// none is left out.
func (r *repository) CountNewMessages(ctx context.Context, taskIDs []int64, since time.Time, excludeWorkspaceID int64) (map[int64]int, error) {
	db := r.conn(ctx)
	clause := sqliteNewMessages
	if db.Dialector.Name() == "postgres" {
		clause = pgNewMessages
	}
	var rows []struct {
		TaskID int64
		N      int
	}
	q := db.Table("messages c").Select("c.task_id AS task_id, COUNT(*) AS n").Where(clause, taskIDs, since.Local())
	if excludeWorkspaceID != 0 {
		q = q.Where("c.by_workspace_id <> ?", excludeWorkspaceID)
	}
	if err := q.Group("c.task_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.TaskID] = row.N
	}
	return out, nil
}

// ListNewestMessages returns, by task, the newest message of each task's
// conversation created after since that is not put down to
// excludeWorkspaceID. A task with none is left out.
func (r *repository) ListNewestMessages(ctx context.Context, taskIDs []int64, since time.Time, excludeWorkspaceID int64) (map[int64]model.Message, error) {
	db := r.conn(ctx)
	clause := sqliteNewestMessages
	if db.Dialector.Name() == "postgres" {
		clause = pgNewestMessages
	}
	var msgs []model.Message
	if err := db.Where(clause, taskIDs, since.Local(), excludeWorkspaceID).Find(&msgs).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]model.Message, len(msgs))
	for _, m := range msgs {
		out[m.TaskID] = m
	}
	return out, nil
}

// ListStatusChanges returns, by task, when its status last changed after
// since, and when its history last moved after since at all. A task whose
// status did not, or whose history did not, is left out of that map. A wait on
// the human is not a status: the history's needsinput rows leave the task's
// status as it was, unless the task left the wait in another one. With
// excludeWorkspaceID, the rows put down to that workspace still say where a
// task stood, but are no change.
func (r *repository) ListStatusChanges(ctx context.Context, taskIDs []int64, since time.Time, excludeWorkspaceID int64) (changed, moved map[int64]time.Time, err error) {
	db := r.conn(ctx)
	// The rows after since, and the one before them that says where each
	// task stood at since.
	before := db.Model(&model.TaskStateTransition{}).Select("MAX(id)").
		Where("task_id IN ? AND created_at <= ?", taskIDs, since.Local()).Group("task_id")
	var rows []model.TaskStateTransition
	if err := db.Where("task_id IN ?", taskIDs).Where("(created_at > ? OR id IN (?))", since.Local(), before).
		Order("id asc").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	status := map[int64]model.TaskState{}
	changed, moved = map[int64]time.Time{}, map[int64]time.Time{}
	for _, row := range rows {
		s := row.ToState
		if s == model.TaskStateNeedsInput {
			s = row.FromState
		}
		if row.CreatedAt.After(since) && (excludeWorkspaceID == 0 || row.ByWorkspaceID != excludeWorkspaceID) {
			moved[row.TaskID] = row.CreatedAt
			if s != status[row.TaskID] {
				changed[row.TaskID] = row.CreatedAt
			}
		}
		status[row.TaskID] = s
	}
	return changed, moved, nil
}
