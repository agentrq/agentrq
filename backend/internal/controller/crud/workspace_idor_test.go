package crud

import (
	"context"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/golang/mock/gomock"
)

func TestGetDetailedWorkspaceStats_IDOR(t *testing.T) {
	e := newTestController(t)

	// Workspace 2 belongs to a different user, so GetWorkspace will return ErrNotFound for current user
	e.repo.EXPECT().GetWorkspace(gomock.Any(), int64(2), testUserID).Return(model.Workspace{}, base.ErrNotFound)

	resp, err := e.controller.GetDetailedWorkspaceStats(context.Background(), entity.GetWorkspaceStatsRequest{
		ID:     2,
		UserID: testUserIDStr,
		Range:  "7d",
	})

	if err == nil {
		t.Errorf("expected error due to IDOR, but got nil")
	}
	if resp != nil {
		t.Errorf("expected nil response, but got %v", resp)
	}
}
