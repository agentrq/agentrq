package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mock_idgen "github.com/agentrq/agentrq/backend/internal/service/mocks/idgen"
	mock_repo "github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

func TestController_CreateSwarm(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockRepo := mock_repo.NewMockRepository(ctrl)
	mockIdgen := mock_idgen.NewMockService(ctrl)
	c := New(Params{IDGen: mockIdgen, Repository: mockRepo})

	userID := monoflake.ID(1).String()
	uidInt := int64(1)

	t.Run("Success", func(t *testing.T) {
		mockRepo.EXPECT().GetWorkspace(gomock.Any(), int64(100), uidInt).Return(model.Workspace{ID: 100, UserID: uidInt}, nil)
		mockRepo.EXPECT().GetWorkspace(gomock.Any(), int64(200), uidInt).Return(model.Workspace{ID: 200, UserID: uidInt}, nil)
		mockIdgen.EXPECT().NextID().Return(int64(9))
		mockRepo.EXPECT().CreateSwarm(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, s model.Swarm) (model.Swarm, error) {
			if s.ID != 9 || s.LeaderWorkspaceID != 100 {
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
		mockRepo.EXPECT().GetWorkspace(gomock.Any(), int64(200), uidInt).Return(model.Workspace{ID: 200, UserID: uidInt}, nil)

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
}
