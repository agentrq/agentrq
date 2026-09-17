// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

package base

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"

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
// GetMachine reads one machine belonging to a user.
//
// A miss is translated to [ErrNotFound] rather than passed on as gorm's own
// error, which the HTTP mapper does not recognise and therefore renders as a
// 500. "Not found" and "the server is broken" are different answers, and only
// one of them is true when somebody follows a stale link.
func (r *repository) GetMachine(ctx context.Context, id, userID int64) (model.Machine, error) {
	var m model.Machine
	if err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&m).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Machine{}, ErrNotFound
		}
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

// CreateSession records a session the daemon has been asked to start.
func (r *repository) CreateSession(ctx context.Context, s model.Session) (model.Session, error) {
	if err := r.conn(ctx).Create(&s).Error; err != nil {
		return model.Session{}, err
	}
	return s, nil
}

// GetSession reads one session belonging to a user.
//
// Scoped by the query rather than by a check after it, so a session belonging
// to somebody else is simply not found — which is both the right answer and
// the one that says least.
func (r *repository) GetSession(ctx context.Context, id, userID int64) (model.Session, error) {
	var s model.Session
	if err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&s).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Session{}, ErrNotFound
		}
		return model.Session{}, err
	}
	return s, nil
}

// ListSessionsByMachine returns a machine's sessions, newest first.
func (r *repository) ListSessionsByMachine(ctx context.Context, machineID, userID int64) ([]model.Session, error) {
	var out []model.Session
	err := r.conn(ctx).Where("machine_id = ? AND user_id = ?", machineID, userID).
		Order("created_at DESC").Find(&out).Error
	return out, err
}

// ActiveSessionForWorkspace finds a session that has not finished.
//
// Used to answer "does this workspace already have an agent?" from the
// database, which is the half of that question that survives a backend
// restart. The live socket answers the other half.
func (r *repository) ActiveSessionForWorkspace(ctx context.Context, workspaceID, userID int64) (model.Session, error) {
	var s model.Session
	err := r.conn(ctx).
		Where("workspace_id = ? AND user_id = ? AND status IN ?", workspaceID, userID, []string{"starting", "running"}).
		Order("created_at DESC").First(&s).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.Session{}, ErrNotFound
		}
		return model.Session{}, err
	}
	return s, nil
}

// UpdateSessionState records what the daemon reported.
//
// A narrow update rather than a full save: state reports arrive while a person
// may be renaming things, and writing every column would overwrite whatever
// landed in between.
func (r *repository) UpdateSessionState(ctx context.Context, id int64, status string, exitCode *int, endedAt *time.Time, restored bool) error {
	fields := map[string]any{"status": status, "updated_at": time.Now()}
	if exitCode != nil {
		fields["exit_code"] = *exitCode
	}
	if endedAt != nil {
		fields["ended_at"] = *endedAt
	}
	// Set, never cleared. A restored session goes on to report running and
	// then exiting like any other, and those later reports say nothing about
	// how it started — writing false would erase the one fact that explains
	// why somebody's scrollback is empty.
	if restored {
		fields["restored"] = true
	}
	return r.conn(ctx).Model(&model.Session{}).Where("id = ?", id).Updates(fields).Error
}

// RecordMachineVersion stores what a daemon says it is running, and clears an
// offer it has caught up with.
//
// Called from the hello, which is the only message that says what version is
// actually running. Without it a machine that has just updated keeps showing
// the version it replaced and an offer it has already taken.
func (r *repository) RecordMachineVersion(ctx context.Context, id int64, version string) error {
	if version == "" {
		return nil
	}
	return r.conn(ctx).Model(&model.Session{}).Session(&gorm.Session{}).
		Table("machines").
		Where("id = ?", id).
		Updates(map[string]any{
			"version": version,
			// Cleared only when it is the version now running: a machine that
			// is still behind must keep showing what it could update to.
			"available_version": gorm.Expr("CASE WHEN available_version = ? THEN '' ELSE available_version END", version),
		}).Error
}

// RecordMachineMetrics stores the latest snapshot and nothing historical.
//
// Latest only, deliberately: writing every heartbeat to a table would grow
// forever to power a widget nobody has asked for. If a graph is wanted later,
// a bounded in-memory ring is the right trade — it empties on restart, which
// is fine for something that is only ever about right now.
//
// A narrow update for the same reason TouchMachine is one: these arrive every
// few seconds, and writing every column would overwrite a rename or a disable
// that landed in between.
func (r *repository) RecordMachineMetrics(ctx context.Context, m model.Machine) error {
	return r.conn(ctx).Model(&model.Machine{}).
		Where("id = ?", m.ID).
		Updates(map[string]any{
			"mem_total":     m.MemTotal,
			"mem_available": m.MemAvailable,
			"cpu_percent":   m.CPUPercent,
			"load_avg":      m.LoadAvg,
			"uptime_sec":    m.UptimeSec,
			"disks":         m.Disks,
			"metrics_at":    m.MetricsAt,
		}).Error
}

// DeleteFinishedSession removes a session that has ended.
//
// A session row is operational state — "what is running here" — rather than a
// record of what happened, and the record is the audit log. Keeping the rows
// turns the machine page into a list of everything that has ever run and the
// table into one that grows forever.
//
// Scoped to a terminal status so a live session can never be deleted by a
// mistimed report.
func (r *repository) DeleteFinishedSession(ctx context.Context, id int64) error {
	return r.conn(ctx).
		Where("id = ? AND status IN ?", id, []string{"exited", "killed", "failed"}).
		Delete(&model.Session{}).Error
}

// ReconcileSessions ends the sessions a machine is no longer running.
//
// The daemon says what it is supervising; anything this machine still has
// marked live and the daemon did not name has ended without anybody being
// told — most often because the daemon restarted. Left alone, those rows sit
// as "running" forever and block the workspace's next launch.
//
// Scoped to one machine, so a daemon can only ever correct its own rows.
func (r *repository) ReconcileSessions(ctx context.Context, machineID int64, running []int64, at time.Time) error {
	q := r.conn(ctx).
		Where("machine_id = ? AND status IN ?", machineID, []string{"starting", "running"})
	if len(running) > 0 {
		q = q.Where("id NOT IN ?", running)
	}
	// Deleted rather than marked exited, for the same reason a session is
	// removed when it ends: these are rows for agents that are definitely not
	// running, on a page whose question is "what is running here".
	return q.Delete(&model.Session{}).Error
}

// CountLiveSessionsByUser counts the sessions still running, per machine.
//
// One query for every machine rather than one per machine: the list page shows
// a count beside each row, and doing this in a loop is how a page with twenty
// machines becomes twenty-one queries.
func (r *repository) CountLiveSessionsByUser(ctx context.Context, userID int64) (map[int64]int, error) {
	var rows []struct {
		MachineID int64
		N         int
	}
	err := r.conn(ctx).Model(&model.Session{}).
		Select("machine_id, count(*) as n").
		Where("user_id = ? AND status IN ?", userID, []string{"starting", "running"}).
		Group("machine_id").Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make(map[int64]int, len(rows))
	for _, row := range rows {
		out[row.MachineID] = row.N
	}
	return out, nil
}

// RecordAvailableVersion stores a release a daemon has found, or clears it.
//
// A narrow update, like the other daemon-driven writes: these arrive while a
// person may be renaming or disabling the machine, and writing every column
// would overwrite whatever landed in between.
func (r *repository) RecordAvailableVersion(ctx context.Context, id int64, version string) error {
	return r.conn(ctx).Model(&model.Machine{}).
		Where("id = ?", id).
		Update("available_version", version).Error
}
