package telemetry

import (
	"context"
	"fmt"
	"sync"
	"time"

	entity "github.com/hasmcp/agentrq/backend/internal/data/entity/crud"
	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/repository/base"
	"github.com/hasmcp/agentrq/backend/internal/repository/dbconn"
)

type Service interface {
	Record(ctx context.Context, userID int64, workspaceID int64, action uint8)
	Stop(ctx context.Context)
	GetWorkspaceStats(ctx context.Context, workspaceID int64) (*entity.GetWorkspaceStatsResponse, error)
}

type service struct {
	db        dbconn.DBConn
	queue     chan model.Telemetry
	stop      chan struct{}
	wg        sync.WaitGroup
	batchSize int
	interval  time.Duration
	repo      base.Repository
}

func New(db dbconn.DBConn, repo base.Repository) Service {
	s := &service{
		db:        db,
		repo:      repo,
		queue:     make(chan model.Telemetry, 10000),
		stop:      make(chan struct{}),
		batchSize: 1000,
		interval:  5 * time.Second,
	}
	s.wg.Add(1)
	go s.worker()
	return s
}

func (s *service) Record(ctx context.Context, userID int64, workspaceID int64, action uint8) {
	s.queue <- model.Telemetry{
		UserID:      userID,
		WorkspaceID: workspaceID,
		OccurredAt:  time.Now().Unix(),
		Action:      action,
	}
}

func (s *service) Stop(ctx context.Context) {
	close(s.stop)
	s.wg.Wait()
}

func (s *service) worker() {
	defer s.wg.Done()

	buffer := make([]model.Telemetry, 0, s.batchSize)
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	flush := func() {
		if len(buffer) == 0 {
			return
		}
		if err := s.db.Conn(context.Background()).Create(&buffer).Error; err != nil {
			fmt.Printf("Telemetry flush error: %v\n", err)
		}
		buffer = buffer[:0]
	}

	for {
		select {
		case record := <-s.queue:
			buffer = append(buffer, record)
			if len(buffer) >= s.batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-s.stop:
			flush()
			return
		}
	}
}

func (s *service) GetWorkspaceStats(ctx context.Context, workspaceID int64) (*entity.GetWorkspaceStatsResponse, error) {
	stats, err := s.repo.GetDailyStats(ctx, workspaceID, 30)
	if err != nil {
		return nil, err
	}

	var totalOps int64
	for _, st := range stats {
		totalOps += st.Count
	}

	activeTasks, totalTasks, err := s.repo.GetWorkspaceTaskCounts(ctx, workspaceID)
	if err != nil {
		return nil, err
	}

	return &entity.GetWorkspaceStatsResponse{
		Stats:       stats,
		Total:       totalOps,
		ActiveTasks: activeTasks,
		TotalTasks:  totalTasks,
	}, nil
}
