// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package base

import (
	"context"
	"time"

	"github.com/agentrq/agentrq/backend/internal/data/model"
)

// CreateEnrolmentCode stores a new, unused enrolment code.
func (r *repository) CreateEnrolmentCode(ctx context.Context, c model.EnrolmentCode) (model.EnrolmentCode, error) {
	if err := r.conn(ctx).Create(&c).Error; err != nil {
		return model.EnrolmentCode{}, err
	}
	return c, nil
}

// GetEnrolmentCode finds a code by its hash.
//
// Looked up by hash rather than by value because the plain code is never
// stored: the row is the only record, and it holds a hash. A miss returns
// gorm.ErrRecordNotFound and the caller answers "rejected" without saying
// whether the code was wrong, expired or already used — the difference is not
// something a caller should be able to probe.
func (r *repository) GetEnrolmentCode(ctx context.Context, codeHash string) (model.EnrolmentCode, error) {
	var c model.EnrolmentCode
	if err := r.conn(ctx).Where("code_hash = ?", codeHash).First(&c).Error; err != nil {
		return model.EnrolmentCode{}, err
	}
	return c, nil
}

// ConsumeEnrolmentCode marks a code used, and does so **only if it is still
// unused**.
//
// The `used_at IS NULL` in the WHERE clause is what makes the code single-use
// under concurrency. Reading the row, checking it in Go and then writing would
// let two requests arriving together both pass the check and both enrol — a
// race that is rare, silent, and produces two machines sharing one code.
//
// A zero RowsAffected means somebody else got there first, and the caller must
// treat that as a rejection rather than retrying.
func (r *repository) ConsumeEnrolmentCode(ctx context.Context, id, machineID int64, at time.Time) (bool, error) {
	res := r.conn(ctx).Model(&model.EnrolmentCode{}).
		Where("id = ? AND used_at IS NULL", id).
		Updates(map[string]any{"used_at": at, "machine_id": machineID})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected == 1, nil
}

// CreateMachine stores a newly enrolled machine.
func (r *repository) CreateMachine(ctx context.Context, m model.Machine) (model.Machine, error) {
	if err := r.conn(ctx).Create(&m).Error; err != nil {
		return model.Machine{}, err
	}
	return m, nil
}

// GetMachineByTokenHash is how every daemon request identifies itself.
//
// The lookup is by hash because that is all that is stored. Disabled machines
// are returned rather than hidden: the caller needs to tell "this token is
// unknown" from "this machine was turned off", and only the second of those
// deserves an explanation.
func (r *repository) GetMachineByTokenHash(ctx context.Context, tokenHash string) (model.Machine, error) {
	var m model.Machine
	if err := r.conn(ctx).Where("token_hash = ?", tokenHash).First(&m).Error; err != nil {
		return model.Machine{}, err
	}
	return m, nil
}

// GetMachine reads one machine belonging to a user.
//
// Scoped by user id in the query rather than checked afterwards: a read that
// fetches first and compares later is one early return away from leaking
// another account's row.
func (r *repository) GetMachine(ctx context.Context, id, userID int64) (model.Machine, error) {
	var m model.Machine
	if err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&m).Error; err != nil {
		return model.Machine{}, err
	}
	return m, nil
}

// ListMachines returns a user's machines, newest first.
func (r *repository) ListMachines(ctx context.Context, userID int64) ([]model.Machine, error) {
	var out []model.Machine
	err := r.conn(ctx).Where("user_id = ?", userID).Order("created_at DESC").Find(&out).Error
	return out, err
}

// UpdateMachine writes a machine back.
func (r *repository) UpdateMachine(ctx context.Context, m model.Machine) (model.Machine, error) {
	if err := r.conn(ctx).Save(&m).Error; err != nil {
		return model.Machine{}, err
	}
	return m, nil
}

// DeleteMachine removes a machine, which is how a token is revoked.
func (r *repository) DeleteMachine(ctx context.Context, id, userID int64) error {
	return r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.Machine{}).Error
}

// TouchMachine records a heartbeat and which backend instance holds the socket.
//
// A narrow update rather than a full Save: a heartbeat arrives every few
// seconds per machine, and writing every column each time would overwrite a
// rename or a disable that landed in between.
func (r *repository) TouchMachine(ctx context.Context, id int64, at time.Time, instanceID string) error {
	return r.conn(ctx).Model(&model.Machine{}).
		Where("id = ?", id).
		Updates(map[string]any{
			"last_seen_at": at,
			"instance_id":  instanceID,
			"connected_at": at,
		}).Error
}

// ReleaseMachine clears the instance pairing when a socket closes.
//
// Conditional on still being the holder: an instance that lost the socket to a
// reconnect elsewhere must not clear the pairing the new holder just wrote.
// Without the guard, a slow disconnect on the old instance silently unroutes a
// machine that is connected and healthy.
func (r *repository) ReleaseMachine(ctx context.Context, id int64, instanceID string) error {
	return r.conn(ctx).Model(&model.Machine{}).
		Where("id = ? AND instance_id = ?", id, instanceID).
		Updates(map[string]any{"instance_id": "", "connected_at": nil}).Error
}
