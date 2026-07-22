package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

func TestController_CreateSwarm(t *testing.T) {
	env := newTestController(t)
	c := env.controller

	userID := monoflake.ID(1).String()
	uidInt := int64(1)

	t.Run("Success", func(t *testing.T) {
		env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(100), uidInt).Return(model.Workspace{ID: 100, UserID: uidInt}, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(200), uidInt).Return(model.Workspace{ID: 200, UserID: uidInt}, nil)
		env.idgen.EXPECT().NextID().Return(int64(9))
		env.repo.EXPECT().CreateSwarm(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, s model.Swarm) (model.Swarm, error) {
			if s.ID != 9 || s.LeaderWorkspaceID != 100 || s.WorkspaceID != 100 {
				t.Errorf("unexpected swarm passed to repository: %+v", s)
			}
			return s, nil
		})

		res, err := c.CreateSwarm(context.Background(), entity.CreateSwarmRequest{
			UserID:             userID,
			Name:               "my-swarm",
			LeaderWorkspaceID:  100,
			MemberWorkspaceIDs: []int64{100, 200},
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Swarm.ID != 9 || len(res.Swarm.MemberWorkspaceIDs) != 2 {
			t.Errorf("unexpected response: %+v", res.Swarm)
		}
	})

	t.Run("LeaderNotInMembers", func(t *testing.T) {
		env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(200), uidInt).Return(model.Workspace{ID: 200, UserID: uidInt}, nil)

		_, err := c.CreateSwarm(context.Background(), entity.CreateSwarmRequest{
			UserID:             userID,
			Name:               "my-swarm",
			LeaderWorkspaceID:  100,
			MemberWorkspaceIDs: []int64{200},
		})
		if err == nil {
			t.Fatal("expected error when leader is not in memberWorkspaceIds")
		}
	})

	t.Run("MissingName", func(t *testing.T) {
		_, err := c.CreateSwarm(context.Background(), entity.CreateSwarmRequest{
			UserID:             userID,
			LeaderWorkspaceID:  100,
			MemberWorkspaceIDs: []int64{100},
		})
		if err == nil {
			t.Fatal("expected error for missing name")
		}
	})

	t.Run("DefaultsWorkspaceIDToLeader", func(t *testing.T) {
		env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(100), uidInt).Return(model.Workspace{ID: 100, UserID: uidInt}, nil)
		env.idgen.EXPECT().NextID().Return(int64(10))
		env.repo.EXPECT().CreateSwarm(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, s model.Swarm) (model.Swarm, error) {
			if s.WorkspaceID != 100 {
				t.Errorf("expected WorkspaceID to default to LeaderWorkspaceID (100), got %d", s.WorkspaceID)
			}
			return s, nil
		})

		res, err := c.CreateSwarm(context.Background(), entity.CreateSwarmRequest{
			UserID:             userID,
			Name:               "swarm-default-workspace",
			LeaderWorkspaceID:  100,
			MemberWorkspaceIDs: []int64{100},
			WorkspaceID:        0, // Explicitly zero; should default to LeaderWorkspaceID
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Swarm.WorkspaceID != 100 {
			t.Errorf("expected WorkspaceID to be 100, got %d", res.Swarm.WorkspaceID)
		}
	})

	t.Run("RejectsUnownedWorkspaceID", func(t *testing.T) {
		env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(100), uidInt).Return(model.Workspace{ID: 100, UserID: uidInt}, nil)
		env.repo.EXPECT().GetWorkspace(gomock.Any(), int64(999), uidInt).Return(model.Workspace{}, gomock.Any()).Times(1)

		_, err := c.CreateSwarm(context.Background(), entity.CreateSwarmRequest{
			UserID:             userID,
			Name:               "swarm-unowned",
			LeaderWorkspaceID:  100,
			MemberWorkspaceIDs: []int64{100},
			WorkspaceID:        999, // Unowned workspace
		})
		if err == nil {
			t.Fatal("expected error when WorkspaceID is not owned by caller")
		}
	})
}
