package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	zlog "github.com/rs/zerolog/log"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	"github.com/mustafaturan/monoflake"
)

// NotifyFunc delivers a best-effort live notification to a workspace's connected
// agent session. It must never block for long; the swarm dispatch queue calls it
// from a worker goroutine.
type NotifyFunc func(ctx context.Context, workspaceID int64, taskID int64, message string)

// SubtaskInput is one independent sub-task the Leader's agent wants delegated.
type SubtaskInput struct {
	Title string
	Body  string
}

// StatusResult is the swarm membership plus a parent task's sub-task statuses.
type StatusResult struct {
	Swarm    model.Swarm
	Children []model.Task
}

type Orchestrator interface {
	ValidateSwarmTask(ctx context.Context, task model.Task) error
	DelegateSubtasks(ctx context.Context, callerWorkspaceID, swarmID, parentTaskID int64, subtasks []SubtaskInput) ([]model.Task, error)
	OnTaskCompleted(ctx context.Context, task model.Task) error
	GetStatus(ctx context.Context, callerWorkspaceID, swarmID, parentTaskID int64) (StatusResult, error)
}

type orchestrator struct {
	repo   base.Repository
	idgen  idgen.Service
	mq     memq.Service
	mqID   uint32
	bus    *eventbus.Bus
	notify NotifyFunc

	// synthesizePublished de-dupes the swarm_ready_to_synthesize event within
	// this process instance: it tracks which parent task IDs have already had
	// their aggregation event published, so that two sub-tasks completing at
	// nearly the same moment cannot both observe zero incomplete children and
	// both publish the event.
	synthesizePublished sync.Map // map[int64]bool, keyed by parentTaskID
}

// New creates the swarm dispatch queue (4 workers) and returns the Orchestrator.
func New(ctx context.Context, repo base.Repository, ids idgen.Service, mq memq.Service, bus *eventbus.Bus, notify NotifyFunc) (Orchestrator, error) {
	res, err := mq.Create(ctx, memq.CreateRequest{Name: "swarm-dispatch", Size: 256})
	if err != nil {
		return nil, fmt.Errorf("create swarm dispatch queue: %w", err)
	}
	o := &orchestrator{repo: repo, idgen: ids, mq: mq, mqID: res.ID, bus: bus, notify: notify}
	if err := mq.AddWorkers(ctx, memq.AddWorkersRequest{QueueID: res.ID, Count: 4, Handle: o.handleDispatch}); err != nil {
		return nil, fmt.Errorf("add swarm dispatch workers: %w", err)
	}
	return o, nil
}

func (o *orchestrator) handleDispatch(ctx context.Context, t memq.Task) error {
	task, ok := t.Val.(model.Task)
	if !ok {
		return fmt.Errorf("unexpected dispatch payload type %T", t.Val)
	}
	if o.notify != nil {
		o.notify(ctx, task.WorkspaceID, task.ID, fmt.Sprintf("New swarm sub-task assigned:\nTitle: %s\nDetails: %s", task.Title, task.Body))
	}
	return nil
}

func (o *orchestrator) ValidateSwarmTask(ctx context.Context, task model.Task) error {
	if !task.IsSwarmEnabled {
		return nil
	}
	if task.SwarmID == 0 {
		return fmt.Errorf("swarmId is required for a swarm-enabled task")
	}
	s, err := o.repo.SystemGetSwarm(ctx, task.SwarmID)
	if err != nil {
		return fmt.Errorf("swarm %d: %w", task.SwarmID, err)
	}
	if s.LeaderWorkspaceID != task.WorkspaceID {
		return fmt.Errorf("swarm-enabled tasks must be created in the swarm's leader workspace")
	}
	return nil
}

func (o *orchestrator) DelegateSubtasks(ctx context.Context, callerWorkspaceID, swarmID, parentTaskID int64, subtasks []SubtaskInput) ([]model.Task, error) {
	if len(subtasks) == 0 {
		return nil, fmt.Errorf("at least one subtask is required")
	}

	s, err := o.repo.SystemGetSwarm(ctx, swarmID)
	if err != nil {
		return nil, fmt.Errorf("swarm %d: %w", swarmID, err)
	}
	if s.LeaderWorkspaceID != callerWorkspaceID {
		return nil, fmt.Errorf("only the swarm's leader workspace can delegate subtasks")
	}

	parent, err := o.repo.SystemGetTask(ctx, parentTaskID)
	if err != nil {
		return nil, fmt.Errorf("parent task %d: %w", parentTaskID, err)
	}
	if parent.SwarmID != swarmID || parent.WorkspaceID != callerWorkspaceID {
		return nil, fmt.Errorf("parent task does not belong to this swarm/leader workspace")
	}

	var memberIDs []int64
	if len(s.MemberWorkspaceIDs) > 0 {
		if err := json.Unmarshal(s.MemberWorkspaceIDs, &memberIDs); err != nil {
			return nil, fmt.Errorf("corrupt swarm membership: %w", err)
		}
	}

	var workers []int64
	for _, id := range memberIDs {
		if id == s.LeaderWorkspaceID {
			continue
		}
		if _, err := o.repo.SystemGetWorkspace(ctx, id); err != nil {
			return nil, fmt.Errorf("member workspace %d no longer exists: %w", id, err)
		}
		workers = append(workers, id)
	}
	if len(workers) == 0 {
		workers = []int64{s.LeaderWorkspaceID}
	}

	// Validate every subtask up front, before any CreateTask call, so a bad
	// input never leaves earlier subtasks persisted as orphaned rows.
	for i, st := range subtasks {
		if st.Title == "" {
			return nil, fmt.Errorf("subtask %d: title is required", i)
		}
	}

	now := time.Now()
	created := make([]model.Task, 0, len(subtasks))
	for i, st := range subtasks {
		workerID := workers[i%len(workers)]
		t := model.Task{
			ID:          o.idgen.NextID(),
			CreatedAt:   now,
			UpdatedAt:   now,
			WorkspaceID: workerID,
			UserID:      parent.UserID,
			CreatedBy:   "agent",
			Assignee:    "agent",
			Status:      "notstarted",
			Title:       st.Title,
			Body:        st.Body,
			ParentID:    parentTaskID,
			SwarmID:     swarmID,
			SortOrder:   float64(now.UnixMilli())/1000.0 + float64(i)*0.001,
		}
		c, err := o.repo.CreateTask(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("create subtask %d: %w", i, err)
		}
		created = append(created, c)

		if err := o.mq.AddTask(ctx, memq.AddTaskRequest{QueueID: o.mqID, Task: memq.Task{ID: c.ID, Val: c}}); err != nil {
			zlog.Warn().Err(err).Int64("task_id", c.ID).Msg("swarm: failed to enqueue dispatch notification")
		}
	}

	return created, nil
}

func (o *orchestrator) OnTaskCompleted(ctx context.Context, task model.Task) error {
	if task.ParentID == 0 {
		return nil
	}
	remaining, err := o.repo.CountIncompleteChildren(ctx, task.ParentID)
	if err != nil {
		return fmt.Errorf("count incomplete children of %d: %w", task.ParentID, err)
	}
	if remaining > 0 {
		return nil
	}

	// Guard against double-firing the aggregation event: two sibling
	// sub-tasks completing near-simultaneously can both observe remaining==0
	// above. LoadOrStore atomically checks-and-sets so only the first caller
	// for this parentTaskID proceeds to publish; later callers return nil.
	if _, alreadyPublished := o.synthesizePublished.LoadOrStore(task.ParentID, true); alreadyPublished {
		return nil
	}

	parent, err := o.repo.SystemGetTask(ctx, task.ParentID)
	if err != nil {
		return fmt.Errorf("parent task %d: %w", task.ParentID, err)
	}
	if parent.SwarmID == 0 {
		return nil
	}
	s, err := o.repo.SystemGetSwarm(ctx, parent.SwarmID)
	if err != nil {
		return fmt.Errorf("swarm %d: %w", parent.SwarmID, err)
	}

	owner, err := o.repo.SystemGetWorkspace(ctx, s.LeaderWorkspaceID)
	if err != nil {
		return fmt.Errorf("leader workspace %d: %w", s.LeaderWorkspaceID, err)
	}

	o.bus.Publish(s.LeaderWorkspaceID, monoflake.ID(owner.UserID).String(), eventbus.Event{
		Type: "swarm_ready_to_synthesize",
		Payload: map[string]any{
			"swarmId":      monoflake.ID(s.ID).String(),
			"parentTaskId": monoflake.ID(parent.ID).String(),
		},
	})
	return nil
}

func (o *orchestrator) GetStatus(ctx context.Context, callerWorkspaceID, swarmID, parentTaskID int64) (StatusResult, error) {
	s, err := o.repo.SystemGetSwarm(ctx, swarmID)
	if err != nil {
		return StatusResult{}, fmt.Errorf("swarm %d: %w", swarmID, err)
	}

	var memberIDs []int64
	if len(s.MemberWorkspaceIDs) > 0 {
		_ = json.Unmarshal(s.MemberWorkspaceIDs, &memberIDs)
	}
	isMember := false
	for _, id := range memberIDs {
		if id == callerWorkspaceID {
			isMember = true
			break
		}
	}
	if !isMember {
		return StatusResult{}, fmt.Errorf("workspace %d is not a member of swarm %d", callerWorkspaceID, swarmID)
	}

	children, err := o.repo.ListChildTasks(ctx, parentTaskID)
	if err != nil {
		return StatusResult{}, fmt.Errorf("list child tasks of %d: %w", parentTaskID, err)
	}

	return StatusResult{Swarm: s, Children: children}, nil
}
