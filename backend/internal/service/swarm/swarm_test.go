package swarm

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/memq"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

func newTestOrchestrator(t *testing.T, mockRepo *mock_repo.MockRepository, mockIdgen *mock_idgen.MockService) (Orchestrator, *eventbus.Bus) {
	t.Helper()
	mq, err := memq.New(memq.Params{})
	if err != nil {
		t.Fatalf("memq.New: %v", err)
	}
	bus := eventbus.New()
	o, err := New(context.Background(), mockRepo, mockIdgen, mq, bus, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return o, bus
}

func membersJSON(ids ...int64) []byte {
	b, _ := json.Marshal(ids)
	return b
}

func TestOrchestrator_ValidateSwarmTask(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	o, _ := newTestOrchestrator(t, mockRepo, mockIdgen)

	t.Run("NotSwarmEnabled", func(t *testing.T) {
		if err := o.ValidateSwarmTask(context.Background(), model.Task{}); err != nil {
			t.Errorf("expected nil error for non-swarm task, got %v", err)
		}
	})

	t.Run("MissingSwarmID", func(t *testing.T) {
		if err := o.ValidateSwarmTask(context.Background(), model.Task{IsSwarmEnabled: true}); err == nil {
			t.Error("expected error for missing swarmId")
		}
	})

	t.Run("NotLeaderWorkspace", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(model.Swarm{ID: 1, LeaderWorkspaceID: 100}, nil)
		err := o.ValidateSwarmTask(context.Background(), model.Task{IsSwarmEnabled: true, SwarmID: 1, WorkspaceID: 200})
		if err == nil {
			t.Error("expected error when task workspace is not the swarm leader")
		}
	})

	t.Run("Valid", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(model.Swarm{ID: 1, LeaderWorkspaceID: 100}, nil)
		err := o.ValidateSwarmTask(context.Background(), model.Task{IsSwarmEnabled: true, SwarmID: 1, WorkspaceID: 100})
		if err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})
}

func TestOrchestrator_DelegateSubtasks(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	o, _ := newTestOrchestrator(t, mockRepo, mockIdgen)

	swarmModel := model.Swarm{ID: 1, LeaderWorkspaceID: 100, MemberWorkspaceIDs: membersJSON(100, 200, 300)}
	parent := model.Task{ID: 5, WorkspaceID: 100, SwarmID: 1, UserID: 1}

	t.Run("RoundRobinAcrossWorkers", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(parent, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(200)).Return(model.Workspace{ID: 200}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(300)).Return(model.Workspace{ID: 300}, nil)
		mockIdgen.EXPECT().NextID().Return(int64(10))
		mockIdgen.EXPECT().NextID().Return(int64(11))
		mockIdgen.EXPECT().NextID().Return(int64(12))

		var created []model.Task
		mockRepo.EXPECT().CreateTask(gomock.Any(), gomock.Any()).Times(3).DoAndReturn(func(_ context.Context, t model.Task) (model.Task, error) {
			created = append(created, t)
			return t, nil
		})

		result, err := o.DelegateSubtasks(context.Background(), 100, 1, 5, []SubtaskInput{
			{Title: "sub-1", Body: "do a"},
			{Title: "sub-2", Body: "do b"},
			{Title: "sub-3", Body: "do c"},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result) != 3 {
			t.Fatalf("expected 3 subtasks, got %d", len(result))
		}
		// Workers are [200, 300] (leader excluded); round-robin: 200, 300, 200.
		wantWorkers := []int64{200, 300, 200}
		for i, tsk := range created {
			if tsk.WorkspaceID != wantWorkers[i] {
				t.Errorf("subtask %d: expected workspace %d, got %d", i, wantWorkers[i], tsk.WorkspaceID)
			}
			if tsk.ParentID != 5 || tsk.SwarmID != 1 {
				t.Errorf("subtask %d: expected ParentID=5 SwarmID=1, got ParentID=%d SwarmID=%d", i, tsk.ParentID, tsk.SwarmID)
			}
		}
	})

	t.Run("RejectsNonLeaderCaller", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		_, err := o.DelegateSubtasks(context.Background(), 200, 1, 5, []SubtaskInput{{Title: "x"}})
		if err == nil {
			t.Error("expected error when caller is not the leader workspace")
		}
	})

	t.Run("RejectsDeletedMemberWorkspace", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(parent, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(200)).Return(model.Workspace{}, base.ErrNotFound)

		_, err := o.DelegateSubtasks(context.Background(), 100, 1, 5, []SubtaskInput{{Title: "x"}})
		if err == nil {
			t.Error("expected error when a member workspace no longer exists")
		}
	})

	t.Run("EmptyTitleAmongSubtasksCreatesNothing", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(parent, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(200)).Return(model.Workspace{ID: 200}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(300)).Return(model.Workspace{ID: 300}, nil)
		// No CreateTask expectation set: gomock's strict mock will fail the
		// test if CreateTask is called at all, which is what we want to
		// verify — subtask 2's empty title must be caught by the pre-pass
		// validation before subtasks 0 and 1 are ever persisted.

		_, err := o.DelegateSubtasks(context.Background(), 100, 1, 5, []SubtaskInput{
			{Title: "sub-1", Body: "do a"},
			{Title: "sub-2", Body: "do b"},
			{Title: "", Body: "do c"},
		})
		if err == nil {
			t.Fatal("expected error for empty subtask title")
		}
	})
}

func TestOrchestrator_OnTaskCompleted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	o, bus := newTestOrchestrator(t, mockRepo, mockIdgen)

	t.Run("NoParentIsNoOp", func(t *testing.T) {
		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 1}); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("SiblingsStillIncompleteIsNoOp", func(t *testing.T) {
		mockRepo.EXPECT().CountIncompleteChildren(gomock.Any(), int64(5)).Return(int64(1), nil)
		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 2, ParentID: 5}); err != nil {
			t.Errorf("expected nil error, got %v", err)
		}
	})

	t.Run("LastSiblingPublishesEvent", func(t *testing.T) {
		ch := bus.Subscribe(100, "")
		defer bus.Unsubscribe(100, "", ch)

		mockRepo.EXPECT().CountIncompleteChildren(gomock.Any(), int64(5)).Return(int64(0), nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(model.Task{ID: 5, SwarmID: 1}, nil)
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(model.Swarm{ID: 1, LeaderWorkspaceID: 100}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(100)).Return(model.Workspace{ID: 100, UserID: 1}, nil)

		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 2, ParentID: 5}); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		select {
		case line := <-ch:
			if len(line) == 0 {
				t.Error("expected non-empty SSE line")
			}
		default:
			t.Error("expected swarm_ready_to_synthesize event to be published")
		}
	})

	t.Run("ConcurrentCompletionPublishesOnce", func(t *testing.T) {
		ch := bus.Subscribe(100, "")
		defer bus.Unsubscribe(100, "", ch)

		// Both "concurrent" callers observe zero incomplete children for the
		// same parent task; only the first should actually publish.
		mockRepo.EXPECT().CountIncompleteChildren(gomock.Any(), int64(7)).Return(int64(0), nil).Times(2)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(7)).Return(model.Task{ID: 7, SwarmID: 1}, nil)
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(model.Swarm{ID: 1, LeaderWorkspaceID: 100}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(100)).Return(model.Workspace{ID: 100, UserID: 1}, nil)

		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 3, ParentID: 7}); err != nil {
			t.Fatalf("unexpected error on first call: %v", err)
		}
		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 4, ParentID: 7}); err != nil {
			t.Fatalf("unexpected error on second call: %v", err)
		}

		select {
		case <-ch:
		default:
			t.Fatal("expected exactly one swarm_ready_to_synthesize event")
		}
		select {
		case line := <-ch:
			t.Fatalf("expected no second event, got %s", line)
		default:
		}
	})

	t.Run("GuardIsClearedAfterDownstreamErrorSoRetryCanPublish", func(t *testing.T) {
		ch := bus.Subscribe(100, "")
		defer bus.Unsubscribe(100, "", ch)

		// First call: remaining==0, guard gets set via LoadOrStore, then
		// SystemGetTask fails. The guard must be deleted so a later retry
		// for the same parent isn't silently swallowed forever.
		mockRepo.EXPECT().CountIncompleteChildren(gomock.Any(), int64(9)).Return(int64(0), nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{}, errors.New("boom"))

		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 8, ParentID: 9}); err == nil {
			t.Fatal("expected error from first call, got nil")
		}

		select {
		case line := <-ch:
			t.Fatalf("expected no event to be published on the failing call, got %s", line)
		default:
		}

		// Second call for the same parent: all downstream calls succeed
		// this time, and the event must actually be published, proving the
		// guard was cleaned up after the first call's error.
		mockRepo.EXPECT().CountIncompleteChildren(gomock.Any(), int64(9)).Return(int64(0), nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(9)).Return(model.Task{ID: 9, SwarmID: 1}, nil)
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(model.Swarm{ID: 1, LeaderWorkspaceID: 100}, nil)
		mockRepo.EXPECT().SystemGetWorkspace(gomock.Any(), int64(100)).Return(model.Workspace{ID: 100, UserID: 1}, nil)

		if err := o.OnTaskCompleted(context.Background(), model.Task{ID: 10, ParentID: 9}); err != nil {
			t.Fatalf("unexpected error on retry: %v", err)
		}

		select {
		case line := <-ch:
			if len(line) == 0 {
				t.Error("expected non-empty SSE line")
			}
		default:
			t.Error("expected swarm_ready_to_synthesize event to be published on retry")
		}
	})
}

func TestOrchestrator_GetStatus(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	o, _ := newTestOrchestrator(t, mockRepo, mockIdgen)

	swarmModel := model.Swarm{ID: 1, LeaderWorkspaceID: 100, MemberWorkspaceIDs: membersJSON(100, 200)}

	t.Run("Success", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(model.Task{ID: 5, SwarmID: 1}, nil)
		mockRepo.EXPECT().ListChildTasks(gomock.Any(), int64(5)).Return([]model.Task{{ID: 6, Status: "ongoing"}}, nil)

		res, err := o.GetStatus(context.Background(), 200, 1, 5)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(res.Children) != 1 {
			t.Errorf("expected 1 child task, got %d", len(res.Children))
		}
	})

	t.Run("RejectsNonMember", func(t *testing.T) {
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		_, err := o.GetStatus(context.Background(), 999, 1, 5)
		if err == nil {
			t.Error("expected error for non-member workspace")
		}
	})

	t.Run("RejectsParentFromDifferentSwarm", func(t *testing.T) {
		// Caller (200) IS a member of swarm 1, but the requested parentTaskID
		// (5) actually belongs to a different swarm (2). GetStatus must reject
		// this before ever calling ListChildTasks - no ListChildTasks
		// expectation is set below, so gomock's strict-mock default fails the
		// test if the IDOR guard is missing and ListChildTasks gets called.
		mockRepo.EXPECT().SystemGetSwarm(gomock.Any(), int64(1)).Return(swarmModel, nil)
		mockRepo.EXPECT().SystemGetTask(gomock.Any(), int64(5)).Return(model.Task{ID: 5, SwarmID: 2}, nil)

		_, err := o.GetStatus(context.Background(), 200, 1, 5)
		if err == nil {
			t.Error("expected error when parent task belongs to a different swarm")
		}
	})
}
