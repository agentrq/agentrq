// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// GetHookRegistrationByID returns a registration whatever its account, for
// a request to its URL, or ErrNotFound.
func (r *repository) GetHookRegistrationByID(ctx context.Context, id int64) (model.HookRegistration, error) {
	var reg model.HookRegistration
	err := r.conn(ctx).Where("id = ?", id).First(&reg).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.HookRegistration{}, ErrNotFound
	}
	return reg, err
}

// CreateHookDelivery queues a request.
func (r *repository) CreateHookDelivery(ctx context.Context, d model.HookDelivery) error {
	return r.conn(ctx).Create(&d).Error
}

// CountQueuedHookDeliveries counts a registration's requests waiting to be
// processed.
func (r *repository) CountQueuedHookDeliveries(ctx context.Context, registrationID int64) (int64, error) {
	var n int64
	err := r.conn(ctx).Model(&model.HookDelivery{}).
		Where("registration_id = ? AND status = ?", registrationID, model.HookDeliveryQueued).Count(&n).Error
	return n, err
}

// ListDueHookDeliveries returns the ids and statuses of up to limit
// deliveries a worker may claim at now, earliest first: queued ones due, and
// processing or routed ones whose lease has ended.
func (r *repository) ListDueHookDeliveries(ctx context.Context, now time.Time, limit int) ([]model.HookDelivery, error) {
	var ds []model.HookDelivery
	err := r.conn(ctx).Select("id", "status").
		Where("status IN ? AND next_attempt_at <= ?", []int16{model.HookDeliveryQueued, model.HookDeliveryProcessing, model.HookDeliveryRouted}, now).
		Order("next_attempt_at").Limit(limit).Find(&ds).Error
	return ds, err
}

// ClaimHookDelivery takes a queued delivery that is due, or one whose
// processing lease has ended, for one attempt lasting until leaseUntil.
// False means another worker has it, or it is not due.
func (r *repository) ClaimHookDelivery(ctx context.Context, id int64, now, leaseUntil time.Time) (bool, error) {
	res := r.conn(ctx).Model(&model.HookDelivery{}).
		Where("id = ? AND status IN ? AND next_attempt_at <= ?", id, []int16{model.HookDeliveryQueued, model.HookDeliveryProcessing}, now).
		Updates(map[string]any{"status": model.HookDeliveryProcessing, "next_attempt_at": leaseUntil, "attempts": gorm.Expr("attempts + 1")})
	return res.RowsAffected == 1, res.Error
}

// ClaimRoutedHookDelivery takes a routed delivery whose lease has ended, to
// publish its pending events, until leaseUntil.
func (r *repository) ClaimRoutedHookDelivery(ctx context.Context, id int64, now, leaseUntil time.Time) (bool, error) {
	res := r.conn(ctx).Model(&model.HookDelivery{}).
		Where("id = ? AND status = ? AND next_attempt_at <= ?", id, model.HookDeliveryRouted, now).
		Update("next_attempt_at", leaseUntil)
	return res.RowsAffected == 1, res.Error
}

// GetHookDelivery returns a delivery, or ErrNotFound.
func (r *repository) GetHookDelivery(ctx context.Context, id int64) (model.HookDelivery, error) {
	var d model.HookDelivery
	err := r.conn(ctx).Where("id = ?", id).First(&d).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.HookDelivery{}, ErrNotFound
	}
	return d, err
}

// UpdateHookDelivery saves what processing a delivery decided: its status,
// next attempt, request, pending events, event names, error and duration.
// It returns ErrLeaseLost when the delivery has been claimed since d was read.
func (r *repository) UpdateHookDelivery(ctx context.Context, d model.HookDelivery) error {
	return saveHookDelivery(r.conn(ctx), d)
}

// saveHookDelivery writes d only while its attempt is still the latest: every
// claim of a queued or processing delivery counts one, so a worker whose
// lease ended and was claimed again writes nothing.
func saveHookDelivery(db *gorm.DB, d model.HookDelivery) error {
	res := db.Model(&model.HookDelivery{ID: d.ID}).Where("attempts = ?", d.Attempts).
		Select("status", "next_attempt_at", "request", "pending", "event_names", "error", "duration_ms").
		Updates(&d)
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrLeaseLost
	}
	return res.Error
}

// RouteHookDelivery commits a delivery's events in one transaction: it marks
// each dedup key, keeping only the events whose key is new or that have
// none, stores those as pending and clears the request. The delivery is
// routed, or a duplicate when nothing is kept. It returns the events kept,
// or ErrLeaseLost, keeping no dedup key, when another worker has claimed the
// delivery since d was read.
func (r *repository) RouteHookDelivery(ctx context.Context, d model.HookDelivery, events []model.HookPendingEvent) ([]model.HookPendingEvent, error) {
	kept := []model.HookPendingEvent{}
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		kept = kept[:0]
		for _, e := range events {
			if e.DedupKey != "" {
				res := tx.Clauses(clause.OnConflict{DoNothing: true}).
					Create(&model.HookDeliveryKey{RegistrationID: d.RegistrationID, DedupKey: e.DedupKey, CreatedAt: d.ReceivedAt})
				if res.Error != nil {
					return res.Error
				}
				if res.RowsAffected == 0 {
					continue
				}
			}
			kept = append(kept, e)
		}
		d.Status, d.Request, d.Pending = model.HookDeliveryRouted, nil, nil
		if len(kept) == 0 {
			d.Status = model.HookDeliveryDuplicate
		} else {
			d.Pending, _ = json.Marshal(kept) // plain data
		}
		names := make([]string, len(kept))
		for i, e := range kept {
			names[i] = e.HookEvent
		}
		b, _ := json.Marshal(names)
		d.EventNames = string(b)
		return saveHookDelivery(tx, d)
	})
	if err != nil {
		return nil, err
	}
	return kept, nil
}

// ListHookDeliveries returns up to limit of a registration's deliveries
// older than beforeID (0 for the newest), newest first, without their
// request or pending events.
func (r *repository) ListHookDeliveries(ctx context.Context, registrationID, userID, beforeID int64, limit int) ([]model.HookDelivery, error) {
	var ds []model.HookDelivery
	q := r.conn(ctx).Omit("request", "pending").Where("registration_id = ? AND user_id = ?", registrationID, userID)
	if beforeID > 0 {
		q = q.Where("id < ?", beforeID)
	}
	err := q.Order("id desc").Limit(limit).Find(&ds).Error
	return ds, err
}

// ReplayHookDelivery queues a failed delivery again, due at now, and returns
// ErrNotFound when there is none to replay. A failed delivery keeps its
// request until the retention deletes it.
func (r *repository) ReplayHookDelivery(ctx context.Context, id, registrationID, userID int64, now time.Time) error {
	res := r.conn(ctx).Model(&model.HookDelivery{}).
		Where("id = ? AND registration_id = ? AND user_id = ? AND status = ?", id, registrationID, userID, model.HookDeliveryFailed).
		Updates(map[string]any{"status": model.HookDeliveryQueued, "attempts": 0, "next_attempt_at": now, "error": ""})
	if res.Error == nil && res.RowsAffected == 0 {
		return ErrNotFound
	}
	return res.Error
}

// DeleteHookDeliveriesBefore removes the deliveries received, and the dedup
// keys made, before the retention's start.
func (r *repository) DeleteHookDeliveriesBefore(ctx context.Context, before time.Time) error {
	if err := r.conn(ctx).Where("received_at < ?", before).Delete(&model.HookDelivery{}).Error; err != nil {
		return err
	}
	return r.conn(ctx).Where("created_at < ?", before).Delete(&model.HookDeliveryKey{}).Error
}
