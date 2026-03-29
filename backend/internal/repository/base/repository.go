package base

import (
	"context"
	"errors"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"gorm.io/gorm"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	// Workspace
	CreateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error)
	GetWorkspace(ctx context.Context, id int64, userID int64) (model.Workspace, error)
	ListWorkspaces(ctx context.Context, userID int64, includeArchived bool) ([]model.Workspace, error)
	DeleteWorkspace(ctx context.Context, id int64, userID int64) error
	UpdateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error)

	// Task
	CreateTask(ctx context.Context, t model.Task) (model.Task, error)
	GetTask(ctx context.Context, workspaceID, taskID int64, userID int64) (model.Task, error)
	ListTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error)
	UpdateTask(ctx context.Context, t model.Task) (model.Task, error)
	DeleteTask(ctx context.Context, workspaceID, taskID int64, userID int64) error

	// Message
	CreateMessage(ctx context.Context, m model.Message) error
	ListMessages(ctx context.Context, taskID int64) ([]model.Message, error)
	UpdateMessageMetadata(ctx context.Context, messageID int64, metadata []byte) error

	SystemGetWorkspace(ctx context.Context, id int64) (model.Workspace, error)
	SystemListTasksByStatus(ctx context.Context, status string) ([]model.Task, error)
	SystemCheckTaskExists(ctx context.Context, workspaceID, parentID int64, status string) (bool, error)
	GetDailyStats(ctx context.Context, workspaceID int64, days int) ([]entity.DailyStat, error)
	GetWorkspaceTaskCounts(ctx context.Context, workspaceID int64) (int64, int64, error)

	// User
	FindUserByEmail(ctx context.Context, email string) (model.User, error)
	CreateUser(ctx context.Context, u model.User) (model.User, error)
	UpdateUser(ctx context.Context, u model.User) (model.User, error)
}

type repository struct {
	db dbconn.DBConn
}

func New(db dbconn.DBConn) Repository {
	return &repository{db: db}
}

func (r *repository) conn(ctx context.Context) *gorm.DB {
	return r.db.Conn(ctx).WithContext(ctx)
}

// ── Workspaces ──────────────────────────────────────────────────────────────────

func (r *repository) CreateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error) {
	if err := r.conn(ctx).Create(&p).Error; err != nil {
		return model.Workspace{}, err
	}
	return p, nil
}

func (r *repository) GetWorkspace(ctx context.Context, id int64, userID int64) (model.Workspace, error) {
	var p model.Workspace
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&p).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Workspace{}, ErrNotFound
	}
	return p, err
}

func (r *repository) ListWorkspaces(ctx context.Context, userID int64, includeArchived bool) ([]model.Workspace, error) {
	var workspaces []model.Workspace
	query := r.conn(ctx).Where("user_id = ?", userID)
	if !includeArchived {
		query = query.Where("archived_at IS NULL")
	}
	err := query.Order("created_at desc").Find(&workspaces).Error
	return workspaces, err
}

func (r *repository) UpdateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error) {
	if err := r.conn(ctx).Save(&p).Error; err != nil {
		return model.Workspace{}, err
	}
	return p, nil
}

func (r *repository) DeleteWorkspace(ctx context.Context, id int64, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Delete all messages for all tasks in this workspace
		if err := tx.Where("task_id IN (?)", tx.Model(&model.Task{}).Select("id").Where("workspace_id = ?", id)).Delete(&model.Message{}).Error; err != nil {
			return err
		}

		// 2. Delete all tasks in this workspace
		if err := tx.Where("workspace_id = ?", id).Delete(&model.Task{}).Error; err != nil {
			return err
		}

		// 3. Delete the workspace itself
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&model.Workspace{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (r *repository) CreateTask(ctx context.Context, t model.Task) (model.Task, error) {
	if err := r.conn(ctx).Create(&t).Error; err != nil {
		return model.Task{}, err
	}
	return t, nil
}

func (r *repository) GetTask(ctx context.Context, workspaceID, taskID int64, userID int64) (model.Task, error) {
	var t model.Task
	err := r.conn(ctx).
		Preload("Messages").
		Where("id = ? AND workspace_id = ? AND user_id = ?", taskID, workspaceID, userID).
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Task{}, ErrNotFound
	}
	return t, err
}

func (r *repository) ListTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error) {
	var tasks []model.Task
	q := r.conn(ctx).Preload("Messages").Where("user_id = ?", userID)

	if req.WorkspaceID != 0 {
		q = q.Where("workspace_id = ?", req.WorkspaceID)
	}
	if req.CreatedBy != "" {
		q = q.Where("created_by = ?", req.CreatedBy)
	}
	if len(req.Status) > 0 {
		q = q.Where("status IN ?", req.Status)
	}

	if req.Filter == "pending_approval" {
		// Find tasks whose most recent message is a permission_request.
		// PostgreSQL: JSONB columns don't support LIKE; cast to text or use @> containment.
		// SQLite: metadata is plain text, LIKE works fine.
		var metadataExpr string
		if r.conn(ctx).Dialector.Name() == "postgres" {
			metadataExpr = "metadata @> '{\"type\":\"permission_request\"}'::jsonb"
		} else {
			metadataExpr = "metadata LIKE '%\"type\":\"permission_request\"%'"
		}
		q = q.Where("id IN (SELECT task_id FROM messages m1 WHERE created_at = (SELECT MAX(created_at) FROM messages m2 WHERE m2.task_id = m1.task_id) AND " + metadataExpr + ")")
	}

	orderBy := "created_at desc"
	if req.Filter == "pending_approval" {
		orderBy = "created_at asc"
	} else if len(req.Status) > 0 {
		status := req.Status[0]
		if status != "notstarted" && status != "pending" && status != "cron" {
			orderBy = "updated_at desc"
		}
	}

	if req.Limit > 0 {
		q = q.Limit(req.Limit)
	}
	if req.Offset > 0 {
		q = q.Offset(req.Offset)
	}

	err := q.Order(orderBy).Find(&tasks).Error
	return tasks, err
}

func (r *repository) UpdateTask(ctx context.Context, t model.Task) (model.Task, error) {
	if err := r.conn(ctx).Save(&t).Error; err != nil {
		return model.Task{}, err
	}
	return t, nil
}

func (r *repository) DeleteTask(ctx context.Context, workspaceID, taskID int64, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Delete all messages for this task
		if err := tx.Where("task_id = ?", taskID).Delete(&model.Message{}).Error; err != nil {
			return err
		}

		// 2. Delete the task
		res := tx.Where("id = ? AND workspace_id = ? AND user_id = ?", taskID, workspaceID, userID).
			Delete(&model.Task{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return nil
	})
}

func (r *repository) CreateMessage(ctx context.Context, m model.Message) error {
	return r.conn(ctx).Create(&m).Error
}

func (r *repository) ListMessages(ctx context.Context, taskID int64) ([]model.Message, error) {
	var msgs []model.Message
	err := r.conn(ctx).Where("task_id = ?", taskID).Order("created_at asc").Find(&msgs).Error
	return msgs, err
}

func (r *repository) UpdateMessageMetadata(ctx context.Context, messageID int64, metadata []byte) error {
	return r.conn(ctx).Model(&model.Message{}).Where("id = ?", messageID).Update("metadata", metadata).Error
}

func (r *repository) SystemGetWorkspace(ctx context.Context, id int64) (model.Workspace, error) {
	var p model.Workspace
	err := r.conn(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Workspace{}, ErrNotFound
	}
	return p, err
}

func (r *repository) SystemListTasksByStatus(ctx context.Context, status string) ([]model.Task, error) {
	var tasks []model.Task
	err := r.conn(ctx).Where("status = ?", status).Find(&tasks).Error
	return tasks, err
}

func (r *repository) SystemCheckTaskExists(ctx context.Context, workspaceID, parentID int64, status string) (bool, error) {
	var count int64
	err := r.conn(ctx).Model(&model.Task{}).
		Where("workspace_id = ? AND parent_id = ? AND status = ?", workspaceID, parentID, status).
		Count(&count).Error
	return count > 0, err
}

func (r *repository) GetDailyStats(ctx context.Context, workspaceID int64, days int) ([]entity.DailyStat, error) {
	var stats []entity.DailyStat
	since := time.Now().AddDate(0, 0, -days).Unix()

	query := r.conn(ctx).Model(&model.Telemetry{}).
		Where("workspace_id = ? AND occurred_at >= ?", workspaceID, since)

	// Dialect specific date formatting
	dialect := r.conn(ctx).Dialector.Name()
	var dateExpr string
	if dialect == "sqlite" {
		dateExpr = "strftime('%Y-%m-%d', datetime(occurred_at, 'unixepoch', 'localtime'))"
	} else {
		// Assume Postgres
		dateExpr = "TO_CHAR(TO_TIMESTAMP(occurred_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
	}

	err := query.Select(dateExpr + " as date, count(*) as count").
		Group("date").
		Order("date ASC").
		Scan(&stats).Error

	return stats, err
}

func (r *repository) GetWorkspaceTaskCounts(ctx context.Context, workspaceID int64) (int64, int64, error) {
	var total, active int64
	err := r.conn(ctx).Model(&model.Task{}).
		Where("workspace_id = ?", workspaceID).
		Count(&total).Error
	if err != nil {
		return 0, 0, err
	}

	err = r.conn(ctx).Model(&model.Task{}).
		Where("workspace_id = ? AND status NOT IN ?", workspaceID, []string{"completed", "archived"}).
		Count(&active).Error
	return active, total, err
}

// ── Users ─────────────────────────────────────────────────────────────────────

func (r *repository) FindUserByEmail(ctx context.Context, email string) (model.User, error) {
	var u model.User
	err := r.conn(ctx).Where("email = ?", email).First(&u).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	return u, err
}

func (r *repository) CreateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := r.conn(ctx).Create(&u).Error; err != nil {
		return model.User{}, err
	}
	return u, nil
}

func (r *repository) UpdateUser(ctx context.Context, u model.User) (model.User, error) {
	if err := r.conn(ctx).Save(&u).Error; err != nil {
		return model.User{}, err
	}
	return u, nil
}
