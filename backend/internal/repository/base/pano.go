// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

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

// SavePanoDoc writes d, adds the linked files in add and drops the files
// whose ids are in drop, in one transaction. An update of a doc that is gone
// is ErrNotFound.
func (r *repository) SavePanoDoc(ctx context.Context, d model.PanoDoc, create bool, add []model.PanoFile, drop []int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if create {
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
		} else {
			res := tx.Model(&model.PanoDoc{}).Where("id = ? AND user_id = ? AND expires_at > ?", d.ID, d.UserID, d.UpdatedAt).Updates(map[string]any{
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
				return ErrNotFound
			}
		}
		if len(drop) > 0 {
			if err := tx.Where("doc_id = ? AND id IN ?", d.ID, drop).Delete(&model.PanoFile{}).Error; err != nil {
				return err
			}
		}
		if len(add) > 0 {
			return tx.Create(&add).Error
		}
		return nil
	})
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
