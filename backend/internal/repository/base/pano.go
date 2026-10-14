// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// ListPanoDocs returns an account's Pano docs that have not expired by now:
// the ones the person wrote first, then the agents', each the most recently
// updated first.
func (r *repository) ListPanoDocs(ctx context.Context, userID int64, now time.Time) ([]model.PanoDoc, error) {
	var docs []model.PanoDoc
	err := r.conn(ctx).Where("user_id = ? AND expires_at > ?", userID, now).
		Order("CASE WHEN created_by_workspace_id = 0 THEN 0 ELSE 1 END, updated_at desc").Omit("markdown").Find(&docs).Error
	return docs, err
}

// GetPanoDoc returns one of an account's Pano docs, or ErrNotFound when there
// is none or it expired by now.
func (r *repository) GetPanoDoc(ctx context.Context, userID, id int64, now time.Time) (model.PanoDoc, error) {
	var d model.PanoDoc
	err := r.conn(ctx).Where("id = ? AND user_id = ? AND expires_at > ?", id, userID, now).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.PanoDoc{}, ErrNotFound
	}
	return d, err
}

// SavePanoDoc writes d, adds the linked files in add, drops the files whose
// ids are in drop and links the tasks in links, in one transaction. An update is written only if the
// doc is still at d.Version, the one its writer read, and moves it to the
// next: a doc another writer got to first is ErrVersionConflict, one that is
// gone ErrNotFound.
func (r *repository) SavePanoDoc(ctx context.Context, d model.PanoDoc, create bool, add []model.PanoFile, drop []int64, links []model.PanoDocTask) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if create {
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
		} else {
			res := tx.Model(&model.PanoDoc{}).Where("id = ? AND user_id = ? AND expires_at > ? AND version = ?", d.ID, d.UserID, d.UpdatedAt, d.Version).Updates(map[string]any{
				"version":                 gorm.Expr("version + 1"),
				"updated_at":              d.UpdatedAt,
				"title":                   d.Title,
				"excerpt":                 d.Excerpt,
				"markdown":                d.Markdown,
				"size_bytes":              d.SizeBytes,
				"updated_by_workspace_id": d.UpdatedByWorkspaceID,
				"ttl_days":                d.TTLDays,
				"expires_at":              d.ExpiresAt,
				"color":                   d.Color,
				"checklist_done":          d.ChecklistDone,
				"checklist_total":         d.ChecklistTotal,
			})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				if err := panoDocExists(tx, d.UserID, d.ID, d.UpdatedAt); err != nil {
					return err
				}
				return ErrVersionConflict
			}
		}
		if len(drop) > 0 {
			if err := tx.Where("doc_id = ? AND id IN ?", d.ID, drop).Delete(&model.PanoFile{}).Error; err != nil {
				return err
			}
		}
		if len(add) > 0 {
			if err := tx.Create(&add).Error; err != nil {
				return err
			}
		}
		_, err := linkPanoDocTasks(tx, links)
		return err
	})
}

// PanoTaskStatus is a task linked to a doc, with its status now. DocID is
// set only where several docs' tasks are read at once.
type PanoTaskStatus struct {
	DocID       int64
	TaskID      int64
	WorkspaceID int64
	Status      string
}

// linkPanoDocTasks adds the links not already there, and returns how many it
// added.
func linkPanoDocTasks(tx *gorm.DB, links []model.PanoDocTask) (int64, error) {
	if len(links) == 0 {
		return 0, nil
	}
	res := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "doc_id"}, {Name: "task_id"}}, DoNothing: true}).Create(&links)
	return res.RowsAffected, res.Error
}

// LinkPanoDocTasks adds the links not already there, and returns how many it
// added.
func (r *repository) LinkPanoDocTasks(ctx context.Context, links []model.PanoDocTask) (int64, error) {
	return linkPanoDocTasks(r.conn(ctx), links)
}

// ListPanoDocTasks returns the tasks linked to the docs that still exist,
// with their doc and status, each doc's oldest link first: one query for a
// whole board.
func (r *repository) ListPanoDocTasks(ctx context.Context, docIDs []int64) ([]PanoTaskStatus, error) {
	var out []PanoTaskStatus
	if len(docIDs) == 0 {
		return out, nil
	}
	err := r.conn(ctx).Table("pano_doc_tasks").
		Select("pano_doc_tasks.doc_id, pano_doc_tasks.task_id, tasks.workspace_id, tasks.status").
		Joins("JOIN tasks ON tasks.id = pano_doc_tasks.task_id AND tasks.user_id = pano_doc_tasks.user_id").
		Where("pano_doc_tasks.doc_id IN ?", docIDs).
		Order("pano_doc_tasks.doc_id, pano_doc_tasks.created_at asc, pano_doc_tasks.id asc").
		Scan(&out).Error
	return out, err
}

// ListPanoTaskStatuses returns those of ids that are an account's tasks, with
// their status.
func (r *repository) ListPanoTaskStatuses(ctx context.Context, userID int64, ids []int64) ([]PanoTaskStatus, error) {
	var out []PanoTaskStatus
	if len(ids) == 0 {
		return out, nil
	}
	err := r.conn(ctx).Model(&model.Task{}).Select("id AS task_id, workspace_id, status").
		Where("user_id = ? AND id IN ?", userID, ids).Scan(&out).Error
	return out, err
}

// ListPanoDocIDsOfTask returns the ids of the docs a task is linked to.
func (r *repository) ListPanoDocIDsOfTask(ctx context.Context, taskID int64) ([]int64, error) {
	var ids []int64
	err := r.conn(ctx).Model(&model.PanoDocTask{}).Where("task_id = ?", taskID).Order("doc_id").Pluck("doc_id", &ids).Error
	return ids, err
}

// ListOngoingAgentTaskIDs returns up to limit ids of a workspace's ongoing
// tasks assigned to its agent.
func (r *repository) ListOngoingAgentTaskIDs(ctx context.Context, userID, workspaceID int64, limit int) ([]int64, error) {
	var ids []int64
	err := r.conn(ctx).Model(&model.Task{}).
		Where("workspace_id = ? AND user_id = ? AND status = ? AND assignee = ?", workspaceID, userID, "ongoing", "agent").
		Limit(limit).Pluck("id", &ids).Error
	return ids, err
}

// SetPanoChecklistDone writes how many of a doc's checklist items are ticked,
// after a task its items are assigned to changed status. It is no save: the
// markdown, the version and updated_at stay as they are.
func (r *repository) SetPanoChecklistDone(ctx context.Context, userID, docID int64, done int) error {
	return r.conn(ctx).Model(&model.PanoDoc{}).Where("id = ? AND user_id = ?", docID, userID).UpdateColumn("checklist_done", done).Error
}

// ListPanoDocsNamingTasks returns up to limit docs of any account, with their
// markdown, whose markdown holds a link to a task, from the id after afterID
// on: what the backfill of their links reads, a page at a time.
func (r *repository) ListPanoDocsNamingTasks(ctx context.Context, afterID int64, limit int) ([]model.PanoDoc, error) {
	var docs []model.PanoDoc
	err := r.conn(ctx).Where("id > ? AND markdown LIKE ?", afterID, "%/tasks/%").Order("id").Limit(limit).Find(&docs).Error
	return docs, err
}

// CreateTaskLinkingPanoDoc creates a task linked to one of its account's
// docs, in one transaction: a doc that is gone or expired by now is
// ErrNotFound, and no task is created.
func (r *repository) CreateTaskLinkingPanoDoc(ctx context.Context, t model.Task, link model.PanoDocTask, now time.Time) (model.Task, error) {
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := panoDocExists(tx, t.UserID, link.DocID, now); err != nil {
			return err
		}
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if err := recordTaskStateTransition(tx, t, model.TaskStateNone, model.TaskStateFromStatus(t.Status)); err != nil {
			return err
		}
		_, err := linkPanoDocTasks(tx, []model.PanoDocTask{link})
		return err
	})
	if err != nil {
		return model.Task{}, err
	}
	return t, nil
}

// ListPanoFiles returns a doc's files, linked and attached, oldest first.
func (r *repository) ListPanoFiles(ctx context.Context, docID int64) ([]model.PanoFile, error) {
	var files []model.PanoFile
	err := r.conn(ctx).Where("doc_id = ?", docID).Order("created_at asc, id asc").Find(&files).Error
	return files, err
}

// AddPanoFiles adds files to one of an account's docs, or is ErrNotFound when
// the doc is gone or expired.
func (r *repository) AddPanoFiles(ctx context.Context, userID, docID int64, files []model.PanoFile) error {
	if len(files) == 0 {
		return nil
	}
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := panoDocExists(tx, userID, docID, files[0].CreatedAt); err != nil {
			return err
		}
		return tx.Create(&files).Error
	})
}

// ListPanoComments returns a doc's comments, oldest first.
func (r *repository) ListPanoComments(ctx context.Context, docID int64) ([]model.PanoComment, error) {
	var comments []model.PanoComment
	err := r.conn(ctx).Where("doc_id = ?", docID).Order("created_at asc, id asc").Find(&comments).Error
	return comments, err
}

// AddPanoComment adds a comment to one of an account's docs, or is
// ErrNotFound when the doc is gone or expired.
func (r *repository) AddPanoComment(ctx context.Context, userID int64, c model.PanoComment) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := panoDocExists(tx, userID, c.DocID, c.CreatedAt); err != nil {
			return err
		}
		return tx.Create(&c).Error
	})
}

// DeletePanoDoc deletes one of an account's docs with its files and comments,
// and returns the storage ids of its files for the caller to purge once this
// has committed.
func (r *repository) DeletePanoDoc(ctx context.Context, userID, id int64) ([]string, error) {
	var storageIDs []string
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var d model.PanoDoc
		err := tx.Select("id").Where("id = ? AND user_id = ?", id, userID).First(&d).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if err := tx.Model(&model.PanoFile{}).Where("doc_id = ?", id).Pluck("storage_id", &storageIDs).Error; err != nil {
			return err
		}
		if err := tx.Where("doc_id = ?", id).Delete(&model.PanoFile{}).Error; err != nil {
			return err
		}
		if err := tx.Where("doc_id = ?", id).Delete(&model.PanoComment{}).Error; err != nil {
			return err
		}
		if err := tx.Where("doc_id = ?", id).Delete(&model.PanoDocTask{}).Error; err != nil {
			return err
		}
		if err := tx.Delete(&model.PanoDoc{}, id).Error; err != nil {
			return err
		}
		return nil
	})
	return storageIDs, err
}

// ListExpiredPanoDocs returns up to limit docs of any account that expired
// by now, for the sweep to delete.
func (r *repository) ListExpiredPanoDocs(ctx context.Context, now time.Time, limit int) ([]model.PanoDoc, error) {
	var docs []model.PanoDoc
	err := r.conn(ctx).Where("expires_at <= ?", now).Order("expires_at asc").Limit(limit).Omit("markdown").Find(&docs).Error
	return docs, err
}

func panoDocExists(tx *gorm.DB, userID, docID int64, now time.Time) error {
	var n int64
	if err := tx.Model(&model.PanoDoc{}).Where("id = ? AND user_id = ? AND expires_at > ?", docID, userID, now).Count(&n).Error; err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return nil
}
