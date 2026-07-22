package coremcp

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	mock_crud "github.com/agentrq/agentrq/backend/internal/service/mocks/crud"
	"github.com/golang/mock/gomock"
	"github.com/mustafaturan/monoflake"
)

func TestWorkspaceServer_HandleCreateSwarm(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockCrud := mock_crud.NewMockController(ctrl)

	ws := &WorkspaceServer{crud: mockCrud}

	mockCrud.EXPECT().CreateSwarm(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, req entity.CreateSwarmRequest) (*entity.CreateSwarmResponse, error) {
		if req.Name != "my-swarm" || req.LeaderWorkspaceID != 100 || len(req.MemberWorkspaceIDs) != 2 {
			t.Errorf("unexpected request: %+v", req)
		}
		return &entity.CreateSwarmResponse{Swarm: entity.Swarm{ID: 9, LeaderWorkspaceID: 100}}, nil
	})

	res, _, err := ws.handleCreateSwarm(context.Background(), nil, CreateSwarmParams{
		Name:               "my-swarm",
		LeaderWorkspaceID:  monoflake.ID(100).String(),
		MemberWorkspaceIDs: []string{monoflake.ID(100).String(), monoflake.ID(200).String()},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Errorf("expected success, got error result: %v", res.Content)
	}
}
