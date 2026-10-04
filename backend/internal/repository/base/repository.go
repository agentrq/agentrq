// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package base

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/tasklatency"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var ErrNotFound = errors.New("not found")

type Repository interface {
	// Workspace
	CreateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error)
	GetWorkspace(ctx context.Context, id int64, userID int64) (model.Workspace, error)
	CheckWorkspaceAccess(ctx context.Context, id int64, userID int64) (bool, error)
	ListWorkspaces(ctx context.Context, userID int64, includeArchived bool) ([]model.Workspace, error)
	DeleteWorkspace(ctx context.Context, id int64, userID int64) error
	UpdateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error)
	// SetWorkingDirectory writes a workspace's folder and nothing else.
	SetWorkingDirectory(ctx context.Context, id, userID int64, dir string) error
	// ArchiveWorkspace saves p, which carries its ArchivedAt, and deletes the
	// triggers and workflow steps that create tasks in it, in one transaction.
	ArchiveWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error)
	// ListForks lists a parent's forks; CountForks counts them.
	ListForks(ctx context.Context, parentID, userID int64) ([]model.Workspace, error)
	CountForks(ctx context.Context, parentID, userID int64) (int64, error)
	// CountUnfinishedTasks counts, per workspace, the tasks that would keep
	// a fork from merging. Workspaces with none are absent from the map.
	CountUnfinishedTasks(ctx context.Context, workspaceIDs []int64) (map[int64]int64, error)
	// MergeForkIntoParent moves every task of a fork, with everything that
	// follows a task, into its parent and deletes the fork, in one
	// transaction. It refuses, moving nothing, while any task is unfinished.
	MergeForkIntoParent(ctx context.Context, forkID, parentID int64) ([]int64, error)

	// Task
	CreateTask(ctx context.Context, t model.Task) (model.Task, error)
	// CreateTaskWithMessages writes a task and its messages together, or
	// neither.
	CreateTaskWithMessages(ctx context.Context, t model.Task, msgs []model.Message) (model.Task, error)
	GetTask(ctx context.Context, workspaceID, taskID int64, userID int64) (model.Task, error)
	ListTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error)
	CountTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) (int64, error)
	UpdateTask(ctx context.Context, t model.Task) (model.Task, error)
	DeleteTask(ctx context.Context, workspaceID, taskID int64, userID int64) error

	// Message
	CreateMessage(ctx context.Context, m model.Message) error
	ListMessages(ctx context.Context, taskID int64) ([]model.Message, error)
	UpdateMessageMetadata(ctx context.Context, taskID int64, messageID int64, metadata []byte) error
	GetWorkspaceAttachments(ctx context.Context, workspaceID int64) ([]entity.TaskAttachment, error)

	// Memory — the workspace's own notes, written by agents through the MCP
	// memory tools. Keyed per (owner, workspace, name); see model.Memory.
	GetMemory(ctx context.Context, userID, workspaceID int64, name string) (model.Memory, error)
	UpsertMemory(ctx context.Context, m model.Memory) (model.Memory, error)
	ListMemoriesByWorkspace(ctx context.Context, userID, workspaceID int64) ([]model.Memory, error)
	DeleteMemory(ctx context.Context, userID, workspaceID int64, name string) error

	// Skill — a workspace's playbooks. Metadata only: file content is in the
	// storage service, and the methods that drop files return the storage ids
	// the caller must purge once the transaction has committed.
	GetSkill(ctx context.Context, userID, workspaceID int64, name string) (model.Skill, error)
	ListSkillsByWorkspace(ctx context.Context, userID, workspaceID int64) ([]model.Skill, error)
	ListSkillsSharedInto(ctx context.Context, userID, workspaceID int64) ([]model.Skill, error)
	SearchSkills(ctx context.Context, userID, workspaceID int64, q string, limit, offset int) ([]model.Skill, int64, error)
	ReplaceSkill(ctx context.Context, s model.Skill, files []model.SkillFile) (model.Skill, []string, error)
	UpsertSkillFile(ctx context.Context, s model.Skill, f model.SkillFile) (model.Skill, string, error)
	DeleteSkillFile(ctx context.Context, s model.Skill, path string) (model.Skill, string, error)
	DeleteSkill(ctx context.Context, skillID int64) ([]string, error)
	GetSkillFile(ctx context.Context, skillID int64, path string) (model.SkillFile, error)
	ListSkillFiles(ctx context.Context, skillID int64) ([]model.SkillFile, error)
	CreateSkillShare(ctx context.Context, sh model.SkillShare) error
	DeleteSkillShare(ctx context.Context, skillID, targetWorkspaceID int64) error
	ListSkillShares(ctx context.Context, skillID int64) ([]model.SkillShare, error)
	GetWorkspaceSkillStorageIDs(ctx context.Context, workspaceID int64) ([]string, error)

	// SiteShare — a website shared from the Chrome extension into a
	// workspace. Unique on (user, origin): one site, one workspace.
	UpsertSiteShare(ctx context.Context, s model.SiteShare) (model.SiteShare, error)
	DeleteSiteShare(ctx context.Context, userID int64, origin string) (bool, error)
	ListSiteSharesForWorkspace(ctx context.Context, workspaceID, userID int64) ([]model.SiteShare, error)
	ListSiteSharesForUser(ctx context.Context, userID int64) ([]model.SiteShare, error)
	GetSiteShare(ctx context.Context, workspaceID, userID int64, origin string) (model.SiteShare, error)
	SetSiteShareAlwaysAllow(ctx context.Context, id int64, names []string) error

	// Machine — an enrolled computer running agentrqd, and the short-lived
	// codes used to enrol one. See internal/controller/machine for the rules.
	CreateEnrolmentCode(ctx context.Context, c model.EnrolmentCode) (model.EnrolmentCode, error)
	GetEnrolmentCode(ctx context.Context, codeHash string) (model.EnrolmentCode, error)
	ConsumeEnrolmentCode(ctx context.Context, id, machineID int64, at time.Time) (bool, error)
	CreateMachine(ctx context.Context, m model.Machine) (model.Machine, error)
	GetMachineByTokenHash(ctx context.Context, tokenHash string) (model.Machine, error)
	GetMachine(ctx context.Context, id, userID int64) (model.Machine, error)
	ListMachines(ctx context.Context, userID int64) ([]model.Machine, error)
	UpdateMachine(ctx context.Context, m model.Machine) (model.Machine, error)
	DeleteMachine(ctx context.Context, id, userID int64) error
	TouchMachine(ctx context.Context, id int64, at time.Time, instanceID string) error
	ReleaseMachine(ctx context.Context, id int64, instanceID string) error
	CreateSession(ctx context.Context, s model.Session) (model.Session, error)
	GetSession(ctx context.Context, id, userID int64) (model.Session, error)
	RecordMachineMetrics(ctx context.Context, m model.Machine) error
	RecordAvailableVersion(ctx context.Context, id int64, version string) error
	RecordMachineVersion(ctx context.Context, id int64, version string) error
	CountLiveSessionsByUser(ctx context.Context, userID int64) (map[int64]int, error)
	ReconcileSessions(ctx context.Context, machineID int64, running []int64, at time.Time) error
	DeleteFinishedSession(ctx context.Context, id int64) error
	ListSessionsByMachine(ctx context.Context, machineID, userID int64) ([]model.Session, error)
	ActiveSessionForWorkspace(ctx context.Context, workspaceID, userID int64) (model.Session, error)
	RecordForkFolder(ctx context.Context, workspaceID, machineID, userID int64) error
	ForkFolderMachines(ctx context.Context, workspaceID, userID int64) ([]int64, error)
	WorkspaceNamesByID(ctx context.Context, ids []int64, userID int64) (map[int64]string, error)
	UpdateSessionState(ctx context.Context, id int64, status string, exitCode *int, endedAt *time.Time, restored bool) error

	// ToolCall
	CreateToolCall(ctx context.Context, tc model.ToolCall) (model.ToolCall, error)
	ListToolCalls(ctx context.Context, taskID int64) ([]model.ToolCall, error)
	UpdateToolCallStatus(ctx context.Context, id int64, status string) (model.ToolCall, error)
	UpdateToolCallsWorkspaceID(ctx context.Context, taskID int64, workspaceID int64) error
	ListTaskStateTransitions(ctx context.Context, taskID int64) ([]model.TaskStateTransition, error)
	CreateAgents(ctx context.Context, agents []model.Agent) error
	CreateAgentModels(ctx context.Context, models []model.AgentModel) error
	ListAgents(ctx context.Context, ids []int64) ([]model.Agent, error)
	ListAgentModels(ctx context.Context, ids []int64) ([]model.AgentModel, error)

	SystemGetWorkspace(ctx context.Context, id int64) (model.Workspace, error)
	SystemGetTask(ctx context.Context, id int64) (model.Task, error)
	SystemGetMessage(ctx context.Context, id int64) (model.Message, error)
	SystemGetUser(ctx context.Context, id int64) (model.User, error)
	SystemListTasksByStatus(ctx context.Context, status string) ([]model.Task, error)
	SystemCheckTaskExists(ctx context.Context, workspaceID, parentID int64, status string) (bool, error)
	SystemCreateCronRun(ctx context.Context, t model.Task) (bool, error)
	SystemStartOneTimeTask(ctx context.Context, t model.Task) (bool, error)
	GetDetailedWorkspaceStats(ctx context.Context, workspaceID int64, startTime, endTime int64) (entity.GetDetailedWorkspaceStatsResponse, error)
	GetDetailedUserStats(ctx context.Context, userID int64, startTime, endTime int64) (entity.GetDetailedUserStatsRows, error)
	GetWorkspaceTaskCounts(ctx context.Context, workspaceID int64) (int64, int64, error)
	GetWorkspaceTaskCountsByCategory(ctx context.Context, workspaceID int64, userID int64) (map[string]int64, error)
	GetTelemetryActionCounts(ctx context.Context) (map[uint8]int64, error)

	// Telemetry aggregation — hourly/daily/monthly rollups (see
	// model.HourlyTelemetry) and the claim row that stops two backend
	// instances aggregating the same period twice.
	ClaimTelemetryAggregation(ctx context.Context, aggregationType, periodKey string) (bool, error)
	AggregateHourlyTelemetry(ctx context.Context, periodStart, periodEnd int64) error
	AggregateDailyTelemetry(ctx context.Context, periodStart, periodEnd int64) error
	AggregateMonthlyTelemetry(ctx context.Context, periodStart, periodEnd int64) error
	LatestTelemetryAggregation(ctx context.Context, aggregationType string) (string, bool, error)

	// Task latency — one row per closed task (model.TaskLatency) and its
	// hourly/daily/monthly rollups, claimed like the telemetry ones.
	AggregateHourlyTaskLatency(ctx context.Context, periodStart, periodEnd int64) error
	AggregateDailyTaskLatency(ctx context.Context, periodStart, periodEnd int64) error
	AggregateMonthlyTaskLatency(ctx context.Context, periodStart, periodEnd int64) error
	ListTaskLatencies(ctx context.Context, workspaceID, userID, start, end int64) ([]model.TaskLatency, error)
	ListTaskLatencyRollups(ctx context.Context, g tasklatency.Granularity, workspaceID, userID, start, end int64) ([]entity.TaskLatencyRollup, error)
	BackfillTaskLatency(ctx context.Context, hourCut, dayCut int64) (int, error)
	FindUserByEmail(ctx context.Context, email string) (model.User, error)
	CreateUser(ctx context.Context, u model.User) (model.User, error)
	UpdateUser(ctx context.Context, u model.User) (model.User, error)
	GetNextTask(ctx context.Context, workspaceID int64, userID int64) (model.Task, error)
	GetGlobalTaskStats(ctx context.Context, userID int64) (entity.GlobalTaskStatsResponse, error)

	// Slack integration
	UpsertSlackWorkspaceLink(ctx context.Context, link model.SlackWorkspaceLink) error
	GetSlackWorkspaceLink(ctx context.Context, workspaceID int64) (model.SlackWorkspaceLink, error)
	GetSlackWorkspaceLinkByChannel(ctx context.Context, channelID string) (model.SlackWorkspaceLink, error)
	DeleteSlackWorkspaceLink(ctx context.Context, workspaceID int64) error
	UpsertSlackTaskThread(ctx context.Context, thread model.SlackTaskThread) error
	UpdateSlackTaskThreadWorkspaceID(ctx context.Context, taskID int64, workspaceID int64) error
	GetSlackTaskThreadByTask(ctx context.Context, taskID int64) (model.SlackTaskThread, error)
	GetSlackTaskThreadByChannel(ctx context.Context, channelID, threadTS string) (model.SlackTaskThread, error)

	// Push subscriptions
	SavePushSubscription(ctx context.Context, sub model.PushSubscription) error
	DeletePushSubscription(ctx context.Context, userID int64, endpoint string) error
	DeletePushSubscriptionByWorkspace(ctx context.Context, userID int64, workspaceID int64, endpoint string) error
	GetPushSubscriptionForWorkspace(ctx context.Context, userID int64, workspaceID int64, endpoint string) (bool, error)
	ListPushSubscriptionsByUserAndWorkspace(ctx context.Context, userID int64, workspaceID int64) ([]model.PushSubscription, error)

	// Events
	CreateEvent(ctx context.Context, e model.Event) (model.Event, error)
	GetEvent(ctx context.Context, id int64, userID int64) (model.Event, error)
	GetEventByName(ctx context.Context, name string, userID int64) (model.Event, error)
	ListEventsByUser(ctx context.Context, userID int64) ([]model.Event, error)
	UpdateEvent(ctx context.Context, id int64, userID int64, payloadGuidelines string) (model.Event, error)
	DeleteEvent(ctx context.Context, id int64, userID int64) error

	// EventTriggers
	CreateEventTrigger(ctx context.Context, t model.EventTrigger) (model.EventTrigger, error)
	GetEventTrigger(ctx context.Context, id int64, userID int64) (model.EventTrigger, error)
	ListEventTriggersByEvent(ctx context.Context, eventID int64, userID int64) ([]model.EventTrigger, error)
	SystemListEventTriggersByEventID(ctx context.Context, eventID int64) ([]model.EventTrigger, error)
	UpdateEventTrigger(ctx context.Context, id int64, userID int64, t model.EventTrigger) (model.EventTrigger, error)
	DeleteEventTrigger(ctx context.Context, id int64, userID int64) error
	// SystemDeleteOrphanedEventRouting deletes the triggers and workflow steps
	// whose workspace is deleted or archived, returning how many rows went.
	SystemDeleteOrphanedEventRouting(ctx context.Context) (int64, error)
	ListTasksByTriggerID(ctx context.Context, triggerID int64, userID int64) ([]model.Task, error)

	// Workflows
	CreateWorkflow(ctx context.Context, w model.Workflow) (model.Workflow, error)
	GetWorkflow(ctx context.Context, id int64, userID int64) (model.Workflow, error)
	ListWorkflowsByUser(ctx context.Context, userID int64) ([]model.Workflow, error)
	UpdateWorkflow(ctx context.Context, w model.Workflow) (model.Workflow, error)
	DeleteWorkflow(ctx context.Context, id int64, userID int64) error

	// WorkflowSteps
	CreateWorkflowStep(ctx context.Context, s model.WorkflowStep) (model.WorkflowStep, error)
	ListWorkflowStepsByWorkflow(ctx context.Context, workflowID int64, userID int64) ([]model.WorkflowStep, error)
	DeleteWorkflowStep(ctx context.Context, id int64, userID int64) error
	// SystemListWorkflowStepsByEvent resolves the fan-out for one hop of a
	// workflow run. Deliberately userID-free: the consumer runs outside any
	// request and keys off the workflow the originating task already carried.
	SystemListWorkflowStepsByEvent(ctx context.Context, workflowID int64, eventID int64) ([]model.WorkflowStep, error)
	ListTasksByWorkflowID(ctx context.Context, workflowID int64, userID int64) ([]model.Task, error)
	ReplaceWorkflowSteps(ctx context.Context, workflowID int64, userID int64, steps []model.WorkflowStep) error
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

// CreateWorkspace stores a new workspace as given. GORM leaves a zero value
// out of the INSERT when its column has a default, and reads the default back
// into p, so a false ClearContextDefault would become the column's true; it
// is written back in the same transaction.
func (r *repository) CreateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error) {
	clearContext := p.ClearContextDefault
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&p).Error; err != nil {
			return err
		}
		if clearContext {
			return nil
		}
		p.ClearContextDefault = false
		return tx.Model(&model.Workspace{}).Where("id = ?", p.ID).Update("clear_context_default", false).Error
	})
	if err != nil {
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

func (r *repository) CheckWorkspaceAccess(ctx context.Context, id int64, userID int64) (bool, error) {
	var count int64
	err := r.conn(ctx).Model(&model.Workspace{}).Where("id = ? AND user_id = ?", id, userID).Count(&count).Error
	return count > 0, err
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

// UpdateWorkspace saves p and, when p has forks, writes the settings they
// inherit to every one of them in the same transaction, so a fork never runs
// with settings its parent no longer has.
func (r *repository) UpdateWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error) {
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&p).Error; err != nil {
			return err
		}
		if p.ForkOfID != 0 {
			return nil
		}
		return tx.Model(&model.Workspace{}).
			Where("fork_of_id = ? AND user_id = ?", p.ID, p.UserID).
			Updates(p.ForkSettings()).Error
	})
	if err != nil {
		return model.Workspace{}, err
	}
	return p, nil
}

// SetWorkingDirectory is a narrow update rather than a full save: a fork's
// folder is recorded while its parent may be saving the settings it copies to
// the fork, and writing every column would put the old ones back.
func (r *repository) SetWorkingDirectory(ctx context.Context, id, userID int64, dir string) error {
	return r.conn(ctx).Model(&model.Workspace{}).
		Where("id = ? AND user_id = ?", id, userID).
		Updates(map[string]any{"working_directory": dir, "updated_at": time.Now()}).Error
}

func (r *repository) ListForks(ctx context.Context, parentID, userID int64) ([]model.Workspace, error) {
	var forks []model.Workspace
	err := r.conn(ctx).Where("fork_of_id = ? AND user_id = ?", parentID, userID).
		Order("created_at desc").Find(&forks).Error
	return forks, err
}

func (r *repository) CountForks(ctx context.Context, parentID, userID int64) (int64, error) {
	var n int64
	err := r.conn(ctx).Model(&model.Workspace{}).
		Where("fork_of_id = ? AND user_id = ?", parentID, userID).Count(&n).Error
	return n, err
}

// forkFinishedStatuses are the statuses a merge takes back. A cron template
// is not a piece of work, it is a schedule, so it goes back and keeps running.
var forkFinishedStatuses = []string{"completed", "rejected", "cron"}

func (r *repository) CountUnfinishedTasks(ctx context.Context, workspaceIDs []int64) (map[int64]int64, error) {
	out := make(map[int64]int64)
	if len(workspaceIDs) == 0 {
		return out, nil
	}
	var rows []struct {
		WorkspaceID int64
		N           int64
	}
	err := r.conn(ctx).Model(&model.Task{}).
		Select("workspace_id, COUNT(*) AS n").
		Where("workspace_id IN ? AND status NOT IN ?", workspaceIDs, forkFinishedStatuses).
		Group("workspace_id").Scan(&rows).Error
	for _, row := range rows {
		out[row.WorkspaceID] = row.N
	}
	return out, err
}

func (r *repository) MergeForkIntoParent(ctx context.Context, forkID, parentID int64) ([]int64, error) {
	var moved []int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var fork model.Workspace
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND fork_of_id = ?", forkID, parentID).
			Take(&fork).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrNotFound
			}
			return err
		}

		// Move the finished tasks, then refuse if anything is left. Checking
		// first and moving after would take along a task reopened, or
		// created by the fork's agent, in between.
		if err := tx.Model(&model.Task{}).
			Where("workspace_id = ? AND status IN ?", forkID, forkFinishedStatuses).
			Order("id").Pluck("id", &moved).Error; err != nil {
			return err
		}
		if len(moved) > 0 {
			if err := tx.Model(&model.Task{}).Where("id IN ?", moved).
				Update("workspace_id", parentID).Error; err != nil {
				return err
			}
		}
		var left int64
		if err := tx.Model(&model.Task{}).Where("workspace_id = ?", forkID).Count(&left).Error; err != nil {
			return err
		}
		if left > 0 {
			return entity.NewForkError(entity.ErrForkUnfinished, UnfinishedForkMessage(left))
		}

		// The columns a task move keeps in step (MoveTask, UpdateTask).
		for _, m := range []any{&model.ToolCall{}, &model.SlackTaskThread{}, &model.TaskStateTransition{}, &model.TaskLatency{}} {
			if err := tx.Model(m).Where("workspace_id = ?", forkID).
				Update("workspace_id", parentID).Error; err != nil {
				return err
			}
		}
		return deleteWorkspaceRows(tx, forkID, fork.UserID)
	})
	if err != nil {
		return nil, err
	}
	return moved, nil
}

// UnfinishedForkMessage is why a merge was refused, in words a person reads.
func UnfinishedForkMessage(n int64) string {
	if n == 1 {
		return "1 task in this fork is not finished"
	}
	return fmt.Sprintf("%d tasks in this fork are not finished", n)
}

func (r *repository) ArchiveWorkspace(ctx context.Context, p model.Workspace) (model.Workspace, error) {
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Save(&p).Error; err != nil {
			return err
		}
		return deleteEventRouting(tx, p.ID)
	})
	if err != nil {
		return model.Workspace{}, err
	}
	return p, nil
}

// deleteEventRouting deletes the triggers and workflow steps that create tasks
// in a workspace being deleted or archived. Either way nobody would see those
// tasks, and a left-behind row is drawn and fanned out to forever.
func deleteEventRouting(tx *gorm.DB, workspaceID int64) error {
	if err := tx.Where("workspace_id = ?", workspaceID).Delete(&model.EventTrigger{}).Error; err != nil {
		return err
	}
	return tx.Where("workspace_id = ?", workspaceID).Delete(&model.WorkflowStep{}).Error
}

func (r *repository) DeleteWorkspace(ctx context.Context, id int64, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		return deleteWorkspaceRows(tx, id, userID)
	})
}

// deleteWorkspaceRows deletes a workspace and everything in it, inside tx.
func deleteWorkspaceRows(tx *gorm.DB, id int64, userID int64) error {
	// 1. Delete everything that references any task in this workspace.
	//    Tool calls matter as much as messages here: they carry the same
	//    foreign key to tasks.id, so leaving them would refuse the delete
	//    in step 2 exactly as it did for a single task.
	taskIDs := tx.Model(&model.Task{}).Select("id").Where("workspace_id = ?", id)
	if err := tx.Where("task_id IN (?)", taskIDs).Delete(&model.Message{}).Error; err != nil {
		return err
	}
	if err := tx.Where("task_id IN (?)", taskIDs).Delete(&model.ToolCall{}).Error; err != nil {
		return err
	}
	if err := tx.Where("workspace_id = ?", id).Delete(&model.SlackTaskThread{}).Error; err != nil {
		return err
	}
	if err := tx.Where("task_id IN (?)", taskIDs).Delete(&model.TaskStateTransition{}).Error; err != nil {
		return err
	}
	// Skills go with their workspace, and so does every share of them and
	// every share into it. The files' content is purged by the caller.
	skillIDs := tx.Model(&model.Skill{}).Select("id").Where("workspace_id = ?", id)
	if err := tx.Where("skill_id IN (?) OR target_workspace_id = ?", skillIDs, id).Delete(&model.SkillShare{}).Error; err != nil {
		return err
	}
	if err := tx.Where("skill_id IN (?)", skillIDs).Delete(&model.SkillFile{}).Error; err != nil {
		return err
	}
	if err := tx.Where("workspace_id = ?", id).Delete(&model.Skill{}).Error; err != nil {
		return err
	}
	if err := tx.Where("workspace_id = ?", id).Delete(&model.SiteShare{}).Error; err != nil {
		return err
	}
	if err := tx.Where("workspace_id = ?", id).Delete(&model.ForkFolder{}).Error; err != nil {
		return err
	}
	if err := deleteEventRouting(tx, id); err != nil {
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
}

// ── Tasks ─────────────────────────────────────────────────────────────────────

func (r *repository) CreateTask(ctx context.Context, t model.Task) (model.Task, error) {
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		return recordTaskStateTransition(tx, t, model.TaskStateNone, model.TaskStateFromStatus(t.Status))
	})
	if err != nil {
		return model.Task{}, err
	}
	return t, nil
}

func (r *repository) CreateTaskWithMessages(ctx context.Context, t model.Task, msgs []model.Message) (model.Task, error) {
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&t).Error; err != nil {
			return err
		}
		if err := recordTaskStateTransition(tx, t, model.TaskStateNone, model.TaskStateFromStatus(t.Status)); err != nil {
			return err
		}
		if len(msgs) == 0 {
			return nil
		}
		return tx.Create(&msgs).Error
	})
	if err != nil {
		return model.Task{}, err
	}
	return t, nil
}

func (r *repository) GetTask(ctx context.Context, workspaceID, taskID int64, userID int64) (model.Task, error) {
	var t model.Task
	err := r.conn(ctx).
		Preload("Messages").
		Preload("ToolCalls").
		Where("id = ? AND workspace_id = ? AND user_id = ?", taskID, workspaceID, userID).
		First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Task{}, ErrNotFound
	}
	return t, err
}

func (r *repository) ListTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) ([]model.Task, error) {
	var tasks []model.Task
	q := r.conn(ctx).Where("user_id = ?", userID)

	if req.WorkspaceID != 0 {
		q = q.Where("workspace_id = ?", req.WorkspaceID)
	}
	if req.CreatedBy != "" {
		q = q.Where("created_by = ?", req.CreatedBy)
	}
	if req.Assignee != "" {
		q = q.Where("assignee = ?", req.Assignee)
	}
	if len(req.Status) == 1 {
		q = q.Where("status = ?", req.Status[0])
	} else if len(req.Status) > 0 {
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
	} else if len(req.Status) > 1 {
		// Mixed statuses, likely "active" view (ongoing, blocked, notstarted, cron)
		// We prioritize status: ongoing (0) > blocked (1) > cron (2) > notstarted (3)
		orderBy = "CASE WHEN status = 'ongoing' THEN 0 WHEN status = 'blocked' THEN 1 WHEN status = 'cron' THEN 2 ELSE 3 END, updated_at DESC"
	} else if len(req.Status) == 1 {
		status := req.Status[0]
		if status == "notstarted" {
			dialect := r.conn(ctx).Dialector.Name()
			var sortExpr string
			if dialect == "sqlite" {
				sortExpr = "(CASE WHEN sort_order > 0 THEN sort_order ELSE CAST(strftime('%s', created_at) AS REAL) END)"
			} else {
				sortExpr = "(CASE WHEN sort_order > 0 THEN sort_order ELSE EXTRACT(EPOCH FROM created_at) END)"
			}
			orderBy = fmt.Sprintf("%s ASC, id ASC", sortExpr)
		} else if status != "cron" {
			orderBy = "updated_at desc"
		}
	}

	limit := req.Limit
	if limit <= 0 {
		limit = 100 // Enforce default safety limit to prevent OOM
	}
	if limit > 500 {
		limit = 500 // Hard cap to prevent PostgreSQL / backend overload
	}
	q = q.Limit(limit)

	if req.Offset > 0 {
		q = q.Offset(req.Offset)
	}

	err := q.Order(orderBy).Find(&tasks).Error
	if err != nil {
		return nil, err
	}

	if req.PreloadMessages && len(tasks) > 0 {
		var metadataExpr string
		if r.conn(ctx).Dialector.Name() == "postgres" {
			metadataExpr = "metadata @> '{\"type\":\"permission_request\",\"status\":\"pending\"}'::jsonb"
		} else {
			metadataExpr = "metadata LIKE '%\"type\":\"permission_request\"%' AND metadata LIKE '%\"status\":\"pending\"%'"
		}

		taskIDs := make([]int64, len(tasks))
		taskMap := make(map[int64]*model.Task, len(tasks))
		for i := range tasks {
			taskIDs[i] = tasks[i].ID
			taskMap[tasks[i].ID] = &tasks[i]
		}

		const chunkSize = 500
		for i := 0; i < len(taskIDs); i += chunkSize {
			end := i + chunkSize
			if end > len(taskIDs) {
				end = len(taskIDs)
			}
			batch := taskIDs[i:end]

			var batchMessages []model.Message
			err := r.conn(ctx).
				Where("task_id IN ?", batch).
				Where("id = (SELECT MAX(id) FROM messages m2 WHERE m2.task_id = messages.task_id) OR (" + metadataExpr + ")").
				Order("created_at asc").
				Find(&batchMessages).Error
			if err != nil {
				return nil, err
			}

			for _, msg := range batchMessages {
				if t, ok := taskMap[msg.TaskID]; ok {
					t.Messages = append(t.Messages, msg)
				}
			}
		}
	}

	return tasks, nil
}

// CountTasks answers a filtered count without hydrating a single row — for a
// caller that only needs to compare against a threshold (is the agent full?)
// and would otherwise pay to unmarshal bodies, responses and attachments it
// never looks at.
func (r *repository) CountTasks(ctx context.Context, req entity.ListTasksRequest, userID int64) (int64, error) {
	q := r.conn(ctx).Model(&model.Task{}).Where("user_id = ?", userID)

	if req.WorkspaceID != 0 {
		q = q.Where("workspace_id = ?", req.WorkspaceID)
	}
	if req.CreatedBy != "" {
		q = q.Where("created_by = ?", req.CreatedBy)
	}
	if req.Assignee != "" {
		q = q.Where("assignee = ?", req.Assignee)
	}
	if len(req.Status) == 1 {
		q = q.Where("status = ?", req.Status[0])
	} else if len(req.Status) > 0 {
		q = q.Where("status IN ?", req.Status)
	}

	var count int64
	if err := q.Count(&count).Error; err != nil {
		return 0, err
	}
	return count, nil
}

func (r *repository) GetNextTask(ctx context.Context, workspaceID int64, userID int64) (model.Task, error) {
	var t model.Task
	dialect := r.conn(ctx).Dialector.Name()
	var sortExpr string
	if dialect == "sqlite" {
		sortExpr = "(CASE WHEN sort_order > 0 THEN sort_order ELSE CAST(strftime('%s', created_at) AS REAL) END)"
	} else {
		// Assume Postgres
		sortExpr = "(CASE WHEN sort_order > 0 THEN sort_order ELSE EXTRACT(EPOCH FROM created_at) END)"
	}

	err := r.conn(ctx).
		Where("workspace_id = ? AND user_id = ? AND status = ? AND assignee = ?", workspaceID, userID, "notstarted", "agent").
		Order(fmt.Sprintf("%s ASC, id ASC", sortExpr)).
		First(&t).Error

	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Task{}, ErrNotFound
	}
	return t, err
}

// UpdateTask saves t and, when that changes its status, records the
// transition in the same transaction. Every status change in the backend is a
// save through here, which is why the history is written here and not by the
// callers.
func (r *repository) UpdateTask(ctx context.Context, t model.Task) (model.Task, error) {
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		var prev struct {
			Status      string
			WorkspaceID int64
		}
		err := tx.Model(&model.Task{}).
			Clauses(clause.Locking{Strength: "UPDATE"}).
			Select("status, workspace_id").
			Where("id = ?", t.ID).
			Take(&prev).Error
		inserted := errors.Is(err, gorm.ErrRecordNotFound)
		if err != nil && !inserted {
			return err
		}
		if err := tx.Save(&t).Error; err != nil {
			return err
		}
		if inserted {
			// Save inserted it: this is the task's first state.
			return recordTaskStateTransition(tx, t, model.TaskStateNone, model.TaskStateFromStatus(t.Status))
		}
		if prev.WorkspaceID != t.WorkspaceID {
			// A moved task takes its history with it.
			if err := tx.Model(&model.TaskStateTransition{}).
				Where("task_id = ?", t.ID).
				Update("workspace_id", t.WorkspaceID).Error; err != nil {
				return err
			}
			if err := tx.Model(&model.TaskLatency{}).
				Where("task_id = ?", t.ID).
				Update("workspace_id", t.WorkspaceID).Error; err != nil {
				return err
			}
		}
		if prev.Status == t.Status {
			return nil
		}
		from, err := currentTaskState(tx, t.ID, prev.Status)
		if err != nil {
			return err
		}
		return recordTaskStateTransition(tx, t, from, model.TaskStateFromStatus(t.Status))
	})
	if err != nil {
		return model.Task{}, err
	}
	return t, nil
}

// DeleteTask removes a task and everything that points at it.
//
// Every child has to go first, and every child means every child: Task declares
// both Messages and ToolCalls as associations, so AutoMigrate gives each a real
// foreign key back to tasks.id, and a row left in either one refuses the delete
// outright with "violates foreign key constraint". Clearing only the messages
// is what made deleting any task that had run a tool fail.
//
// The Slack thread mapping carries no such constraint, so it cannot block the
// delete — it is cleared anyway, because a row keyed by a task id that no
// longer exists is never going to be read again.
func (r *repository) DeleteTask(ctx context.Context, workspaceID, taskID int64, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		// 1. Delete everything that references this task
		if err := tx.Where("task_id = ?", taskID).Delete(&model.Message{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&model.ToolCall{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&model.SlackTaskThread{}).Error; err != nil {
			return err
		}
		if err := tx.Where("task_id = ?", taskID).Delete(&model.TaskStateTransition{}).Error; err != nil {
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
	if !hasRequestStatus(m.Metadata) {
		return r.conn(ctx).Create(&m).Error
	}
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&m).Error; err != nil {
			return err
		}
		return syncNeedsInput(tx, m.TaskID)
	})
}

func (r *repository) ListMessages(ctx context.Context, taskID int64) ([]model.Message, error) {
	var msgs []model.Message
	err := r.conn(ctx).Where("task_id = ?", taskID).Order("created_at asc").Find(&msgs).Error
	return msgs, err
}

func (r *repository) UpdateMessageMetadata(ctx context.Context, taskID int64, messageID int64, metadata []byte) error {
	update := func(tx *gorm.DB) error {
		return tx.Model(&model.Message{}).Where("id = ? AND task_id = ?", messageID, taskID).Update("metadata", metadata).Error
	}
	if !hasRequestStatus(metadata) {
		return update(r.conn(ctx))
	}
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := update(tx); err != nil {
			return err
		}
		return syncNeedsInput(tx, taskID)
	})
}

func (r *repository) CreateToolCall(ctx context.Context, tc model.ToolCall) (model.ToolCall, error) {
	if err := r.conn(ctx).Create(&tc).Error; err != nil {
		return model.ToolCall{}, err
	}
	return tc, nil
}

// ListToolCalls returns a task's tool calls in the order they were made.
//
// The task view carries them alongside the messages, so anything rebuilding a
// task outside GetTask — which preloads both — has to load them itself, or it
// publishes a task that looks like it never called a tool.
func (r *repository) ListToolCalls(ctx context.Context, taskID int64) ([]model.ToolCall, error) {
	var tcs []model.ToolCall
	err := r.conn(ctx).Where("task_id = ?", taskID).Order("created_at asc").Find(&tcs).Error
	return tcs, err
}

func (r *repository) UpdateToolCallStatus(ctx context.Context, id int64, status string) (model.ToolCall, error) {
	var tc model.ToolCall
	if err := r.conn(ctx).Where("id = ?", id).First(&tc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.ToolCall{}, ErrNotFound
		}
		return model.ToolCall{}, err
	}
	tc.Status = status
	if err := r.conn(ctx).Save(&tc).Error; err != nil {
		return model.ToolCall{}, err
	}
	return tc, nil
}

// UpdateToolCallsWorkspaceID re-scopes every ToolCall belonging to taskID to
// workspaceID. ToolCall.WorkspaceID is denormalized from the parent Task at
// creation time and is otherwise never touched again, so moving a task to a
// different workspace must reconcile it here or existing tool-call rows keep
// pointing at the task's old workspace.
func (r *repository) UpdateToolCallsWorkspaceID(ctx context.Context, taskID int64, workspaceID int64) error {
	return r.conn(ctx).Model(&model.ToolCall{}).Where("task_id = ?", taskID).Update("workspace_id", workspaceID).Error
}

// GetWorkspaceAttachments lists the attachments of every task in a workspace
// and of its messages, each with the task it belongs to.
func (r *repository) GetWorkspaceAttachments(ctx context.Context, workspaceID int64) ([]entity.TaskAttachment, error) {
	type row struct {
		TaskID      int64
		Attachments string
	}
	var rows []row
	if err := r.conn(ctx).Model(&model.Task{}).Select("id AS task_id, attachments").
		Where("workspace_id = ?", workspaceID).Scan(&rows).Error; err != nil {
		return nil, err
	}
	var msgRows []row
	if err := r.conn(ctx).Model(&model.Message{}).Select("messages.task_id, messages.attachments").
		Joins("JOIN tasks ON tasks.id = messages.task_id").
		Where("tasks.workspace_id = ?", workspaceID).Scan(&msgRows).Error; err != nil {
		return nil, err
	}
	var out []entity.TaskAttachment
	for _, rw := range append(rows, msgRows...) {
		if rw.Attachments == "" {
			continue
		}
		var atts []entity.Attachment
		if json.Unmarshal([]byte(rw.Attachments), &atts) != nil {
			continue
		}
		for _, a := range atts {
			if a.ID != "" {
				out = append(out, entity.TaskAttachment{TaskID: rw.TaskID, ID: a.ID})
			}
		}
	}
	return out, nil
}

func (r *repository) SystemGetWorkspace(ctx context.Context, id int64) (model.Workspace, error) {
	var p model.Workspace
	err := r.conn(ctx).First(&p, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Workspace{}, ErrNotFound
	}
	return p, err
}

func (r *repository) SystemGetTask(ctx context.Context, id int64) (model.Task, error) {
	var t model.Task
	err := r.conn(ctx).First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Task{}, ErrNotFound
	}
	return t, err
}

func (r *repository) SystemGetMessage(ctx context.Context, id int64) (model.Message, error) {
	var m model.Message
	err := r.conn(ctx).First(&m, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Message{}, ErrNotFound
	}
	return m, err
}

func (r *repository) SystemGetUser(ctx context.Context, id int64) (model.User, error) {
	var u model.User
	err := r.conn(ctx).First(&u, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, ErrNotFound
	}
	return u, err
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

// SystemCreateCronRun creates t, a scheduled run of the cron t.ParentID, unless
// that run already exists: idx_tasks_cron_run allows one task per (parent_id,
// created_at), and every backend instance ticking a minute writes the same
// pair, so exactly one of them gets true. Losing is not an error, so the
// conflict is skipped rather than raised and logged.
func (r *repository) SystemCreateCronRun(ctx context.Context, t model.Task) (bool, error) {
	created := false
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&t)
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		created = true
		return recordTaskStateTransition(tx, t, model.TaskStateNone, model.TaskStateFromStatus(t.Status))
	})
	return created && err == nil, err
}

// SystemStartOneTimeTask turns a one-time cron template into its own run: it
// becomes notstarted with no schedule, keeping its ID, and takes t's body and
// timestamps. Only a task still in cron is changed, so when several backend
// instances tick the same minute exactly one of them gets true.
func (r *repository) SystemStartOneTimeTask(ctx context.Context, t model.Task) (bool, error) {
	started := false
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.Task{}).
			Where("id = ? AND status = ?", t.ID, "cron").
			Updates(map[string]any{
				"status":        "notstarted",
				"cron_schedule": "",
				"body":          t.Body,
				"created_at":    t.CreatedAt,
				"updated_at":    t.UpdatedAt,
			})
		if res.Error != nil || res.RowsAffected == 0 {
			return res.Error
		}
		started = true
		from, err := currentTaskState(tx, t.ID, "cron")
		if err != nil {
			return err
		}
		return recordTaskStateTransition(tx, t, from, model.TaskStateFromStatus("notstarted"))
	})
	return started && err == nil, err
}

func (r *repository) GetDetailedWorkspaceStats(ctx context.Context, workspaceID int64, startTime, endTime int64) (entity.GetDetailedWorkspaceStatsResponse, error) {
	return r.detailedStats(ctx, "workspace_id = ?", workspaceID, startTime, endTime)
}

// GetDetailedUserStats is the same five aggregations summed over every
// workspace the user owns, plus the per-workspace breakdown.
//
// It scopes on telemetries.user_id, which every writer sets and which carries
// its own index, rather than joining through the workspace list. The one
// difference that follows from scoping this way is that rows belonging to no
// workspace (user creation, action 18) are in scope — none of them are actions
// the summary buckets, so the account totals still add up to exactly the sum of
// the per-workspace views.
func (r *repository) GetDetailedUserStats(ctx context.Context, userID int64, startTime, endTime int64) (entity.GetDetailedUserStatsRows, error) {
	var res entity.GetDetailedUserStatsRows

	base, err := r.detailedStats(ctx, "user_id = ?", userID, startTime, endTime)
	if err != nil {
		return res, err
	}
	res.Summary = base.Summary
	res.Timeseries = base.Timeseries
	res.Heatmap = base.Heatmap

	// Conditional sums keep this to one pass over the window instead of one
	// query per metric. The LEFT JOIN is what supplies the name; a workspace
	// that has since been deleted still has telemetry rows, and those come back
	// with an empty name rather than being dropped — dropping them would make
	// the breakdown quietly fail to add up to the summary above.
	//
	// workspace_id = 0 is excluded because it is not a workspace: those are the
	// account-level rows described above, and they have no row to join to.
	res.Workspaces = make([]entity.WorkspaceStatsBreakdownRow, 0)
	err = r.conn(ctx).Model(&model.Telemetry{}).
		Select(
			"telemetries.workspace_id as workspace_id,"+
				" COALESCE(workspaces.name, '') as name,"+
				" SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) as tasks_completed,"+
				" SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) as messages",
			model.ActionIDTaskComplete, model.ActionIDMessageCreate).
		Joins("LEFT JOIN workspaces ON workspaces.id = telemetries.workspace_id").
		Where("telemetries.user_id = ?", userID).
		Where("telemetries.occurred_at >= ? AND telemetries.occurred_at <= ?", startTime, endTime).
		Where("telemetries.workspace_id <> 0").
		// Postgres requires every non-aggregated column in the GROUP BY, so the
		// name is grouped alongside the ID rather than merely selected.
		Group("telemetries.workspace_id, workspaces.name").
		// A workspace with neither a completion nor a message in the window has
		// nothing to say on this panel — it would still be listed, because
		// other action types (a rename, a permission prompt) put a row in
		// range. The expressions are repeated rather than referenced by alias:
		// Postgres allows a select alias in ORDER BY but not in HAVING.
		Having(
			"SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) > 0"+
				" OR SUM(CASE WHEN telemetries.action = ? THEN 1 ELSE 0 END) > 0",
			model.ActionIDTaskComplete, model.ActionIDMessageCreate).
		Order("tasks_completed DESC, messages DESC, telemetries.workspace_id ASC").
		Scan(&res.Workspaces).Error

	return res, err
}

// detailedStats runs the five aggregations behind both statistics endpoints.
//
// scopeClause is a parameterised predicate ("workspace_id = ?" or
// "user_id = ?") rather than a column name pasted into a string, so widening
// the scope cannot become a way to inject SQL.
func (r *repository) detailedStats(ctx context.Context, scopeClause string, scopeValue int64, startTime, endTime int64) (entity.GetDetailedWorkspaceStatsResponse, error) {
	var res entity.GetDetailedWorkspaceStatsResponse

	// Dialect specific date formatting
	dialect := r.conn(ctx).Dialector.Name()
	var dateExpr, hourExpr string
	if dialect == "sqlite" {
		dateExpr = "strftime('%Y-%m-%d', datetime(occurred_at, 'unixepoch', 'localtime'))"
		hourExpr = "strftime('%Y-%m-%d %H:00', datetime(occurred_at, 'unixepoch', 'localtime'))"
	} else {
		// Assume Postgres
		dateExpr = "TO_CHAR(TO_TIMESTAMP(occurred_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD')"
		hourExpr = "TO_CHAR(TO_TIMESTAMP(occurred_at) AT TIME ZONE 'UTC', 'YYYY-MM-DD HH24:00')"
	}

	// 1. Get Summary Stats
	type countResult struct {
		Action uint8
		Count  int64
	}
	var summaryResults []countResult
	err := r.conn(ctx).Model(&model.Telemetry{}).
		Select("action, count(*) as count").
		Where(scopeClause, scopeValue).
		Where("occurred_at >= ? AND occurred_at <= ?", startTime, endTime).
		Group("action").
		Scan(&summaryResults).Error
	if err != nil {
		return res, err
	}

	for _, row := range summaryResults {
		switch row.Action {
		case model.ActionIDTaskComplete:
			res.Summary.TasksCompleted = row.Count
		case model.ActionIDTaskFromScheduled:
			res.Summary.TasksScheduled = row.Count
		case model.ActionIDMessageCreate:
			res.Summary.Messages = row.Count
		case model.ActionIDTaskApproveManual, model.ActionIDMCPPermissionManual:
			res.Summary.ManualApprovals += row.Count
		case model.ActionIDMCPPermissionAuto:
			res.Summary.AutoApprovals += row.Count
		case model.ActionIDTaskRejectManual, model.ActionIDMCPPermissionDeny:
			res.Summary.Denies += row.Count
		}
	}

	// 2. Get Timeseries for Tasks Completed
	err = r.conn(ctx).Model(&model.Telemetry{}).
		Select(dateExpr+" as date, count(*) as count").
		Where(scopeClause, scopeValue).
		Where("occurred_at >= ? AND occurred_at <= ? AND action = ?", startTime, endTime, model.ActionIDTaskComplete).
		Group("date").
		Order("date ASC").
		Scan(&res.Timeseries.TasksCompleted).Error
	if err != nil {
		return res, err
	}

	// 3. Get Timeseries for Messages
	err = r.conn(ctx).Model(&model.Telemetry{}).
		Select(dateExpr+" as date, count(*) as count").
		Where(scopeClause, scopeValue).
		Where("occurred_at >= ? AND occurred_at <= ? AND action = ?", startTime, endTime, model.ActionIDMessageCreate).
		Group("date").
		Order("date ASC").
		Scan(&res.Timeseries.Messages).Error
	if err != nil {
		return res, err
	}

	// 4. Heatmap: hourly buckets for windows of a week or less, daily buckets otherwise
	const heatmapHourlyThresholdSeconds = 7 * 24 * 3600
	bucketExpr := dateExpr
	res.Heatmap.Granularity = "day"
	if endTime > startTime && endTime-startTime <= heatmapHourlyThresholdSeconds {
		bucketExpr = hourExpr
		res.Heatmap.Granularity = "hour"
	}
	res.Heatmap.RangeStart = startTime
	res.Heatmap.RangeEnd = endTime

	err = r.conn(ctx).Model(&model.Telemetry{}).
		Select(bucketExpr+" as bucket, count(*) as count").
		Where(scopeClause, scopeValue).
		Where("occurred_at >= ? AND occurred_at <= ? AND action = ?", startTime, endTime, model.ActionIDTaskComplete).
		Group("bucket").
		Order("bucket ASC").
		Scan(&res.Heatmap.TasksCompleted).Error
	if err != nil {
		return res, err
	}

	err = r.conn(ctx).Model(&model.Telemetry{}).
		Select(bucketExpr+" as bucket, count(*) as count").
		Where(scopeClause, scopeValue).
		Where("occurred_at >= ? AND occurred_at <= ? AND action = ?", startTime, endTime, model.ActionIDMessageCreate).
		Group("bucket").
		Order("bucket ASC").
		Scan(&res.Heatmap.Messages).Error

	return res, err
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

func (r *repository) GetTelemetryActionCounts(ctx context.Context) (map[uint8]int64, error) {
	type countResult struct {
		Action uint8
		Count  int64
	}
	var results []countResult
	err := r.conn(ctx).Model(&model.Telemetry{}).
		Select("action, count(*) as count").
		Group("action").
		Scan(&results).Error

	m := make(map[uint8]int64)
	for _, rr := range results {
		m[rr.Action] = rr.Count
	}
	return m, err
}

// ClaimTelemetryAggregation is the distributed lock for the telemetry
// aggregator: it inserts the claim row and reports whether *this* call
// created it. With several backend instances polling the same schedule, only
// the one whose INSERT wins the unique index gets true; every other sees
// RowsAffected == 0 from the ON CONFLICT DO NOTHING and skips the run.
func (r *repository) ClaimTelemetryAggregation(ctx context.Context, aggregationType, periodKey string) (bool, error) {
	result := r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model.TelemetryAggregation{
		CreatedAt:       time.Now(),
		AggregationType: aggregationType,
		PeriodKey:       periodKey,
	})
	return result.RowsAffected > 0, result.Error
}

// telemetryCounts groups source rows in [periodStart, periodEnd) by the
// dimensions every rollup shares. table names the model to scan ("telemetries"
// for the hourly source, "hourly_telemetries" for the daily source, and so
// on), which is safe to interpolate because it is always one of our own
// constant table names, never caller input.
func (r *repository) telemetryCounts(ctx context.Context, table string, periodStart, periodEnd int64) ([]entity.TelemetryCount, error) {
	var counts []entity.TelemetryCount
	err := r.conn(ctx).Table(table).
		Select("user_id, workspace_id, action, sub_action_id, actor, sum(count) as count").
		Where("period_start >= ? AND period_start < ?", periodStart, periodEnd).
		Group("user_id, workspace_id, action, sub_action_id, actor").
		Scan(&counts).Error
	return counts, err
}

// AggregateHourlyTelemetry rolls the raw telemetries table up into one
// hourly_telemetries row per (user, workspace, action, sub-action, actor) for
// [periodStart, periodEnd) — normally one complete hour. OnConflict DoNothing
// makes a re-run over the same period a no-op rather than doubling counts;
// ClaimTelemetryAggregation is what stops that re-run from happening at all
// under normal operation.
func (r *repository) AggregateHourlyTelemetry(ctx context.Context, periodStart, periodEnd int64) error {
	var counts []entity.TelemetryCount
	err := r.conn(ctx).Model(&model.Telemetry{}).
		Select("user_id, workspace_id, action, sub_action_id, actor, count(*) as count").
		Where("occurred_at >= ? AND occurred_at < ?", periodStart, periodEnd).
		Group("user_id, workspace_id, action, sub_action_id, actor").
		Scan(&counts).Error
	if err != nil || len(counts) == 0 {
		return err
	}

	rows := make([]model.HourlyTelemetry, len(counts))
	for i, c := range counts {
		rows[i] = model.HourlyTelemetry{
			PeriodStart: periodStart,
			UserID:      c.UserID,
			WorkspaceID: c.WorkspaceID,
			Action:      c.Action,
			SubActionID: c.SubActionID,
			Actor:       c.Actor,
			Count:       c.Count,
		}
	}
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// AggregateDailyTelemetry rolls hourly_telemetries up into one
// daily_telemetries row per dimension set for [periodStart, periodEnd) —
// normally one complete day — so the raw event log is never rescanned for a
// day-or-wider report.
func (r *repository) AggregateDailyTelemetry(ctx context.Context, periodStart, periodEnd int64) error {
	counts, err := r.telemetryCounts(ctx, "hourly_telemetries", periodStart, periodEnd)
	if err != nil || len(counts) == 0 {
		return err
	}

	rows := make([]model.DailyTelemetry, len(counts))
	for i, c := range counts {
		rows[i] = model.DailyTelemetry{
			PeriodStart: periodStart,
			UserID:      c.UserID,
			WorkspaceID: c.WorkspaceID,
			Action:      c.Action,
			SubActionID: c.SubActionID,
			Actor:       c.Actor,
			Count:       c.Count,
		}
	}
	return r.conn(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&rows).Error
}

// AggregateMonthlyTelemetry rolls daily_telemetries up into
// monthly_telemetries for [periodStart, periodEnd) — periodStart is the
// month's first instant and periodEnd is "today", so the sum only ever covers
// complete days. Unlike the hourly/daily rollups this one runs once a day for
// as long as the month is open, so the row for a given dimension set is
// upserted (its count replaced with the freshly summed total) rather than
// inserted once.
func (r *repository) AggregateMonthlyTelemetry(ctx context.Context, periodStart, periodEnd int64) error {
	counts, err := r.telemetryCounts(ctx, "daily_telemetries", periodStart, periodEnd)
	if err != nil || len(counts) == 0 {
		return err
	}

	rows := make([]model.MonthlyTelemetry, len(counts))
	for i, c := range counts {
		rows[i] = model.MonthlyTelemetry{
			PeriodStart: periodStart,
			UserID:      c.UserID,
			WorkspaceID: c.WorkspaceID,
			Action:      c.Action,
			SubActionID: c.SubActionID,
			Actor:       c.Actor,
			Count:       c.Count,
		}
	}
	return r.conn(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{
			{Name: "period_start"}, {Name: "user_id"}, {Name: "workspace_id"},
			{Name: "action"}, {Name: "sub_action_id"}, {Name: "actor"},
		},
		DoUpdates: clause.AssignmentColumns([]string{"count"}),
	}).Create(&rows).Error
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

// ── Slack Integration ─────────────────────────────────────────────────────────

func (r *repository) UpsertSlackWorkspaceLink(ctx context.Context, link model.SlackWorkspaceLink) error {
	return r.conn(ctx).Save(&link).Error
}

func (r *repository) GetSlackWorkspaceLink(ctx context.Context, workspaceID int64) (model.SlackWorkspaceLink, error) {
	var l model.SlackWorkspaceLink
	err := r.conn(ctx).First(&l, "workspace_id = ?", workspaceID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SlackWorkspaceLink{}, ErrNotFound
	}
	return l, err
}

func (r *repository) GetSlackWorkspaceLinkByChannel(ctx context.Context, channelID string) (model.SlackWorkspaceLink, error) {
	var l model.SlackWorkspaceLink
	err := r.conn(ctx).First(&l, "slack_channel_id = ?", channelID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SlackWorkspaceLink{}, ErrNotFound
	}
	return l, err
}

func (r *repository) DeleteSlackWorkspaceLink(ctx context.Context, workspaceID int64) error {
	return r.conn(ctx).Delete(&model.SlackWorkspaceLink{}, "workspace_id = ?", workspaceID).Error
}

func (r *repository) UpsertSlackTaskThread(ctx context.Context, thread model.SlackTaskThread) error {
	return r.conn(ctx).Save(&thread).Error
}

// UpdateSlackTaskThreadWorkspaceID re-scopes the task's Slack thread (if any)
// to workspaceID. SlackTaskThread.WorkspaceID is denormalized from the task
// at creation time; inbound Slack replies are routed using this stale value
// (see HandleSlackEvent), so it must be reconciled when a task moves or a
// reply on the old thread will look up the wrong workspace's bot token and
// then 404 trying to find the task in a workspace it no longer belongs to.
func (r *repository) UpdateSlackTaskThreadWorkspaceID(ctx context.Context, taskID int64, workspaceID int64) error {
	return r.conn(ctx).Model(&model.SlackTaskThread{}).Where("task_id = ?", taskID).Update("workspace_id", workspaceID).Error
}

func (r *repository) GetSlackTaskThreadByTask(ctx context.Context, taskID int64) (model.SlackTaskThread, error) {
	var t model.SlackTaskThread
	err := r.conn(ctx).First(&t, "task_id = ?", taskID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SlackTaskThread{}, ErrNotFound
	}
	return t, err
}

func (r *repository) GetSlackTaskThreadByChannel(ctx context.Context, channelID, threadTS string) (model.SlackTaskThread, error) {
	var t model.SlackTaskThread
	err := r.conn(ctx).Where("slack_channel_id = ? AND thread_ts = ?", channelID, threadTS).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.SlackTaskThread{}, ErrNotFound
	}
	return t, err
}

func (r *repository) GetGlobalTaskStats(ctx context.Context, userID int64) (entity.GlobalTaskStatsResponse, error) {
	var res entity.GlobalTaskStatsResponse
	var pending, scheduled int64

	err := r.conn(ctx).Model(&model.Task{}).
		Where("user_id = ? AND status IN ?", userID, []string{"notstarted", "ongoing", "blocked"}).
		Count(&pending).Error
	if err != nil {
		return res, err
	}

	err = r.conn(ctx).Model(&model.Task{}).
		Where("user_id = ? AND status = ?", userID, "cron").
		Count(&scheduled).Error
	if err != nil {
		return res, err
	}

	res.PendingTasks = pending
	res.ScheduledTasks = scheduled
	return res, nil
}

func (r *repository) GetWorkspaceTaskCountsByCategory(ctx context.Context, workspaceID int64, userID int64) (map[string]int64, error) {
	counts := map[string]int64{
		"ongoing":    0,
		"notstarted": 0,
		"scheduled":  0,
		"completed":  0,
		"pending":    0,
	}

	type statusCount struct {
		Status string
		Count  int64
	}
	var results []statusCount
	if err := r.conn(ctx).Model(&model.Task{}).
		Select("status, count(*) as count").
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Group("status").
		Scan(&results).Error; err != nil {
		return nil, err
	}

	for _, res := range results {
		switch res.Status {
		case "ongoing", "blocked":
			counts["ongoing"] += res.Count
		case "notstarted":
			counts["notstarted"] += res.Count
		case "cron":
			counts["scheduled"] += res.Count
		case "completed", "rejected":
			counts["completed"] += res.Count
		}
	}

	// 5. Pending (Action Required)
	var pending int64
	var metadataExpr string
	if r.conn(ctx).Dialector.Name() == "postgres" {
		metadataExpr = "metadata @> '{\"type\":\"permission_request\"}'::jsonb"
	} else {
		metadataExpr = "metadata LIKE '%\"type\":\"permission_request\"%'"
	}
	err := r.conn(ctx).Model(&model.Task{}).
		Where("workspace_id = ? AND user_id = ?", workspaceID, userID).
		Where("id IN (SELECT task_id FROM messages m1 WHERE created_at = (SELECT MAX(created_at) FROM messages m2 WHERE m2.task_id = m1.task_id) AND " + metadataExpr + ")").
		Count(&pending).Error
	if err != nil {
		return nil, err
	}
	counts["pending"] = pending

	return counts, nil
}

// ── Push Subscriptions ──────────────────────────────────────────────────────────

func (r *repository) SavePushSubscription(ctx context.Context, sub model.PushSubscription) error {
	return r.conn(ctx).
		Where(model.PushSubscription{Endpoint: sub.Endpoint, WorkspaceID: sub.WorkspaceID}).
		Assign(sub).
		FirstOrCreate(&sub).Error
}

func (r *repository) DeletePushSubscription(ctx context.Context, userID int64, endpoint string) error {
	return r.conn(ctx).
		Where("user_id = ? AND endpoint = ?", userID, endpoint).
		Delete(&model.PushSubscription{}).Error
}

func (r *repository) DeletePushSubscriptionByWorkspace(ctx context.Context, userID int64, workspaceID int64, endpoint string) error {
	return r.conn(ctx).
		Where("user_id = ? AND workspace_id = ? AND endpoint = ?", userID, workspaceID, endpoint).
		Delete(&model.PushSubscription{}).Error
}

func (r *repository) GetPushSubscriptionForWorkspace(ctx context.Context, userID int64, workspaceID int64, endpoint string) (bool, error) {
	var count int64
	err := r.conn(ctx).Model(&model.PushSubscription{}).
		Where("user_id = ? AND workspace_id = ? AND endpoint = ?", userID, workspaceID, endpoint).
		Count(&count).Error
	return count > 0, err
}

func (r *repository) ListPushSubscriptionsByUserAndWorkspace(ctx context.Context, userID int64, workspaceID int64) ([]model.PushSubscription, error) {
	var subs []model.PushSubscription
	err := r.conn(ctx).Where("user_id = ? AND workspace_id = ?", userID, workspaceID).Find(&subs).Error
	return subs, err
}

// ── Events ─────────────────────────────────────────────────────────────────────

// ── Memories ──────────────────────────────────────────────────────────────────

func (r *repository) GetMemory(ctx context.Context, userID, workspaceID int64, name string) (model.Memory, error) {
	var m model.Memory
	err := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ? AND name = ?", userID, workspaceID, name).
		First(&m).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Memory{}, ErrNotFound
	}
	return m, err
}

// UpsertMemory writes a memory, replacing whatever was stored under that name.
//
// Overwriting is the whole semantic of saveMemory, so this is one statement
// rather than a read followed by a write: two agents saving the same memory at
// once would otherwise race, and the loser's insert would fail against the
// unique key rather than simply being overwritten.
//
// The caller supplies a fresh ID for the insert case. On conflict the stored
// row keeps the ID it already had — only the content and the timestamp move —
// so a memory's identity survives being rewritten.
func (r *repository) UpsertMemory(ctx context.Context, m model.Memory) (model.Memory, error) {
	err := r.conn(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "workspace_id"}, {Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"}),
	}).Create(&m).Error
	if err != nil {
		return model.Memory{}, err
	}
	// Read back rather than returning the struct that went in: on the update
	// path the row's ID and creation time are the ones it already had, not the
	// ones this call generated.
	return r.GetMemory(ctx, m.UserID, m.WorkspaceID, m.Name)
}

// ListMemoriesByWorkspace returns every memory in a workspace, ordered by name.
//
// Content comes back with them: the rows are capped at 16 KiB and a workspace
// holds a handful, so a second query per memory would cost more than it saves.
// Callers that only need the index — the settings list — drop it themselves.
func (r *repository) ListMemoriesByWorkspace(ctx context.Context, userID, workspaceID int64) ([]model.Memory, error) {
	var memories []model.Memory
	err := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ?", userID, workspaceID).
		Order("name asc").
		Find(&memories).Error
	return memories, err
}

// DeleteMemory removes one named memory. Deleting a name nobody wrote under
// is reported as ErrNotFound rather than silently succeeding, so a caller can
// tell "removed" apart from "was never there" the same way GetMemory does.
func (r *repository) DeleteMemory(ctx context.Context, userID, workspaceID int64, name string) error {
	res := r.conn(ctx).
		Where("user_id = ? AND workspace_id = ? AND name = ?", userID, workspaceID, name).
		Delete(&model.Memory{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) CreateEvent(ctx context.Context, e model.Event) (model.Event, error) {
	if err := r.conn(ctx).Create(&e).Error; err != nil {
		return model.Event{}, err
	}
	return e, nil
}

func (r *repository) GetEvent(ctx context.Context, id int64, userID int64) (model.Event, error) {
	var e model.Event
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Event{}, ErrNotFound
	}
	return e, err
}

func (r *repository) GetEventByName(ctx context.Context, name string, userID int64) (model.Event, error) {
	var e model.Event
	err := r.conn(ctx).Where("name = ? AND user_id = ?", name, userID).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Event{}, ErrNotFound
	}
	return e, err
}

func (r *repository) ListEventsByUser(ctx context.Context, userID int64) ([]model.Event, error) {
	var events []model.Event
	err := r.conn(ctx).Where("user_id = ?", userID).Order("created_at desc").Find(&events).Error
	return events, err
}

func (r *repository) UpdateEvent(ctx context.Context, id int64, userID int64, payloadGuidelines string) (model.Event, error) {
	var e model.Event
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&e).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Event{}, ErrNotFound
	}
	if err != nil {
		return model.Event{}, err
	}
	e.PayloadGuidelines = payloadGuidelines
	e.UpdatedAt = time.Now()
	if err := r.conn(ctx).Save(&e).Error; err != nil {
		return model.Event{}, err
	}
	return e, nil
}

func (r *repository) DeleteEvent(ctx context.Context, id int64, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&model.Event{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}

		if err := tx.Where("event_id = ? AND user_id = ?", id, userID).Delete(&model.EventTrigger{}).Error; err != nil {
			return err
		}

		if err := tx.Model(&model.EventTrigger{}).
			Where("emit_event_id = ? AND user_id = ?", id, userID).
			Update("emit_event_id", 0).Error; err != nil {
			return err
		}

		// Workflow steps need the same cleanup as triggers. A step whose source
		// event no longer exists can never fire, and one still naming the
		// deleted event as its emit target would arm a task to publish
		// something gone — both would sit invisibly in a workflow that looks
		// intact.
		if err := tx.Where("event_id = ? AND user_id = ?", id, userID).Delete(&model.WorkflowStep{}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.WorkflowStep{}).
			Where("emit_event_id = ? AND user_id = ?", id, userID).
			Update("emit_event_id", 0).Error; err != nil {
			return err
		}

		// A workflow that started on this event has no entry point left.
		// Clearing it surfaces the "Pick a start event" prompt instead of
		// rendering an empty canvas with no explanation.
		return tx.Model(&model.Workflow{}).
			Where("start_event_id = ? AND user_id = ?", id, userID).
			Update("start_event_id", 0).Error
	})
}

func (r *repository) SystemDeleteOrphanedEventRouting(ctx context.Context) (int64, error) {
	var deleted int64
	err := r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		workspaceIDs := tx.Model(&model.Workspace{}).Select("id").Where("archived_at IS NULL")
		for _, m := range []any{&model.EventTrigger{}, &model.WorkflowStep{}} {
			res := tx.Where("workspace_id NOT IN (?)", workspaceIDs).Delete(m)
			if res.Error != nil {
				return res.Error
			}
			deleted += res.RowsAffected
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	return deleted, nil
}

// ── EventTriggers ──────────────────────────────────────────────────────────────

func (r *repository) CreateEventTrigger(ctx context.Context, t model.EventTrigger) (model.EventTrigger, error) {
	if err := r.conn(ctx).Create(&t).Error; err != nil {
		return model.EventTrigger{}, err
	}
	return t, nil
}

func (r *repository) GetEventTrigger(ctx context.Context, id int64, userID int64) (model.EventTrigger, error) {
	var t model.EventTrigger
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&t).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.EventTrigger{}, ErrNotFound
	}
	return t, err
}

func (r *repository) ListEventTriggersByEvent(ctx context.Context, eventID int64, userID int64) ([]model.EventTrigger, error) {
	var triggers []model.EventTrigger
	err := r.conn(ctx).Where("event_id = ? AND user_id = ?", eventID, userID).Order("created_at desc").Find(&triggers).Error
	return triggers, err
}

func (r *repository) SystemListEventTriggersByEventID(ctx context.Context, eventID int64) ([]model.EventTrigger, error) {
	var triggers []model.EventTrigger
	err := r.conn(ctx).Where("event_id = ?", eventID).Find(&triggers).Error
	return triggers, err
}

func (r *repository) UpdateEventTrigger(ctx context.Context, id int64, userID int64, t model.EventTrigger) (model.EventTrigger, error) {
	var existing model.EventTrigger
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.EventTrigger{}, ErrNotFound
	}
	if err != nil {
		return model.EventTrigger{}, err
	}
	existing.WorkspaceID = t.WorkspaceID
	existing.Title = t.Title
	existing.Body = t.Body
	existing.Assignee = t.Assignee
	existing.CronSchedule = t.CronSchedule
	existing.AllowAllCommands = t.AllowAllCommands
	existing.EmitEventID = t.EmitEventID
	existing.UpdatedAt = time.Now()
	if err := r.conn(ctx).Save(&existing).Error; err != nil {
		return model.EventTrigger{}, err
	}
	return existing, nil
}

func (r *repository) DeleteEventTrigger(ctx context.Context, id int64, userID int64) error {
	res := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.EventTrigger{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) ListTasksByTriggerID(ctx context.Context, triggerID int64, userID int64) ([]model.Task, error) {
	var tasks []model.Task
	err := r.conn(ctx).Where("trigger_id = ? AND user_id = ?", triggerID, userID).Order("created_at desc").Find(&tasks).Error
	return tasks, err
}

// ── Workflows ──────────────────────────────────────────────────────────────────

func (r *repository) CreateWorkflow(ctx context.Context, w model.Workflow) (model.Workflow, error) {
	if err := r.conn(ctx).Create(&w).Error; err != nil {
		return model.Workflow{}, err
	}
	return w, nil
}

func (r *repository) GetWorkflow(ctx context.Context, id int64, userID int64) (model.Workflow, error) {
	var w model.Workflow
	err := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).First(&w).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Workflow{}, ErrNotFound
	}
	return w, err
}

func (r *repository) ListWorkflowsByUser(ctx context.Context, userID int64) ([]model.Workflow, error) {
	var workflows []model.Workflow
	err := r.conn(ctx).Where("user_id = ?", userID).Order("created_at desc").Find(&workflows).Error
	return workflows, err
}

// UpdateWorkflow persists the mutable fields of an existing workflow. The
// caller supplies a whole model, but only name/description/startEventId/layout
// are written — ownership and creation time are never reassigned.
func (r *repository) UpdateWorkflow(ctx context.Context, w model.Workflow) (model.Workflow, error) {
	var existing model.Workflow
	err := r.conn(ctx).Where("id = ? AND user_id = ?", w.ID, w.UserID).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.Workflow{}, ErrNotFound
	}
	if err != nil {
		return model.Workflow{}, err
	}
	existing.Name = w.Name
	existing.Description = w.Description
	existing.StartEventID = w.StartEventID
	existing.Layout = w.Layout
	existing.UpdatedAt = time.Now()
	if err := r.conn(ctx).Save(&existing).Error; err != nil {
		return model.Workflow{}, err
	}
	return existing, nil
}

// DeleteWorkflow removes a workflow and its steps together. Tasks already
// spawned by past runs keep their workflow_id: they are historical records, and
// blanking them would lose the only trace of why they exist.
func (r *repository) DeleteWorkflow(ctx context.Context, id int64, userID int64) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Where("id = ? AND user_id = ?", id, userID).Delete(&model.Workflow{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrNotFound
		}
		return tx.Where("workflow_id = ? AND user_id = ?", id, userID).Delete(&model.WorkflowStep{}).Error
	})
}

// ── WorkflowSteps ──────────────────────────────────────────────────────────────

func (r *repository) CreateWorkflowStep(ctx context.Context, s model.WorkflowStep) (model.WorkflowStep, error) {
	if err := r.conn(ctx).Create(&s).Error; err != nil {
		return model.WorkflowStep{}, err
	}
	return s, nil
}

func (r *repository) ListWorkflowStepsByWorkflow(ctx context.Context, workflowID int64, userID int64) ([]model.WorkflowStep, error) {
	var steps []model.WorkflowStep
	err := r.conn(ctx).Where("workflow_id = ? AND user_id = ?", workflowID, userID).Order("created_at asc").Find(&steps).Error
	return steps, err
}

func (r *repository) DeleteWorkflowStep(ctx context.Context, id int64, userID int64) error {
	res := r.conn(ctx).Where("id = ? AND user_id = ?", id, userID).Delete(&model.WorkflowStep{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *repository) SystemListWorkflowStepsByEvent(ctx context.Context, workflowID int64, eventID int64) ([]model.WorkflowStep, error) {
	var steps []model.WorkflowStep
	err := r.conn(ctx).Where("workflow_id = ? AND event_id = ?", workflowID, eventID).Find(&steps).Error
	return steps, err
}

func (r *repository) ListTasksByWorkflowID(ctx context.Context, workflowID int64, userID int64) ([]model.Task, error) {
	var tasks []model.Task
	err := r.conn(ctx).Where("workflow_id = ? AND user_id = ?", workflowID, userID).Order("created_at desc").Find(&tasks).Error
	return tasks, err
}

// ReplaceWorkflowSteps swaps a workflow's whole edge set in one transaction.
//
// Text mode edits the graph as a document, so the save is a replace rather than
// a diff. Doing it transactionally matters: a partial apply would leave a
// half-rewired graph live, and the consumer reads these rows the moment an
// event fires.
func (r *repository) ReplaceWorkflowSteps(ctx context.Context, workflowID int64, userID int64, steps []model.WorkflowStep) error {
	return r.conn(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("workflow_id = ? AND user_id = ?", workflowID, userID).Delete(&model.WorkflowStep{}).Error; err != nil {
			return err
		}
		if len(steps) == 0 {
			return nil
		}
		return tx.Create(&steps).Error
	})
}
