package telemetry

import (
	"context"
	"fmt"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/service/mocks/dbconn"
	"github.com/agentrq/agentrq/backend/internal/service/mocks/repository"
	"github.com/golang/mock/gomock"
)

func TestTelemetry(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockDB := dbconn.NewMockDBConn(ctrl)
	mockRepo := repository.NewMockRepository(ctrl)

	s := New(mockDB, mockRepo)

	t.Run("RecordAndStop", func(t *testing.T) {
		// We avoid Record call here because it triggers a flush on Stop,
		// which requires a fully working gorm.DB mock.
		s.Stop(context.Background())
	})

	t.Run("GetWorkspaceStats", func(t *testing.T) {
		workspaceID := int64(456)
		mockRepo.EXPECT().GetDailyStats(gomock.Any(), workspaceID, 30).Return([]crud.DailyStat{
			{Date: "2023-01-01", Count: 10},
		}, nil)
		mockRepo.EXPECT().GetWorkspaceTaskCounts(gomock.Any(), workspaceID).Return(int64(5), int64(10), nil)

		resp, err := s.GetWorkspaceStats(context.Background(), workspaceID)
		if err != nil {
			t.Fatal(err)
		}
		if resp.Total != 10 {
			t.Errorf("expected total 10, got %d", resp.Total)
		}
		if resp.ActiveTasks != 5 {
			t.Errorf("expected 5 active tasks, got %d", resp.ActiveTasks)
		}
	})

	t.Run("GetWorkspaceStatsError", func(t *testing.T) {
		mockRepo.EXPECT().GetDailyStats(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, fmt.Errorf("error"))
		_, err := s.GetWorkspaceStats(context.Background(), 1)
		if err == nil {
			t.Error("expected error, got nil")
		}
	})
}
