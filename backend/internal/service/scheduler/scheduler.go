package scheduler

import (
	"context"
	"fmt"
	"time"

	"github.com/hasmcp/agentrq/backend/internal/data/model"
	"github.com/hasmcp/agentrq/backend/internal/repository/base"
	"github.com/hasmcp/agentrq/backend/internal/service/idgen"
	"github.com/hasmcp/agentrq/backend/internal/service/eventbus"
    mapper "github.com/hasmcp/agentrq/backend/internal/mapper/api"
	"github.com/robfig/cron/v3"
)

type Service interface {
	Start(ctx context.Context)
}

type scheduler struct {
	repo  base.Repository
	idgen idgen.Service
	bus   *eventbus.Bus
}

func New(repo base.Repository, idgen idgen.Service, bus *eventbus.Bus) Service {
	return &scheduler{repo: repo, idgen: idgen, bus: bus}
}

func (s *scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		fmt.Println("scheduler: background poller started (interval: 1m)")
		for {
			select {
			case <-ctx.Done():
				fmt.Println("scheduler: background poller stopped")
				return
			case <-ticker.C:
				s.tick(ctx)
			}
		}
	}()
}

func (s *scheduler) tick(ctx context.Context) {
	crons, err := s.repo.SystemListTasksByStatus(ctx, "cron")
	if err != nil {
		fmt.Printf("scheduler: failed to list crons: %v\n", err)
		return
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	now := time.Now().Truncate(time.Minute)

	for _, c := range crons {
		if c.CronSchedule == "" {
			continue
		}
		
		sched, err := parser.Parse(c.CronSchedule)
		if err != nil {
			fmt.Printf("scheduler: invalid cron schedule for task %d: %s\n", c.ID, c.CronSchedule)
			continue
		}

		// Calculate the next run time from the last minute
		// If the next calculated run time is EXACTLY this minute, we spawn.
		next := sched.Next(now.Add(-1 * time.Second))
		
		if next.Equal(now) {
			s.spawn(ctx, c)
		}
	}
}

func (s *scheduler) spawn(ctx context.Context, parent model.Task) {
	// Check if ANY active task with ParentID exists (notstarted OR ongoing)
	// This prevents double-spawning if the first one was already picked up by an agent.
	exists, err := s.repo.SystemCheckTaskExists(ctx, parent.WorkspaceID, parent.ID, "notstarted")
	if err == nil && !exists {
		exists, err = s.repo.SystemCheckTaskExists(ctx, parent.WorkspaceID, parent.ID, "ongoing")
	}
	
	if err != nil {
		fmt.Printf("scheduler: error checking existence for task %d: %v\n", parent.ID, err)
		return
	}
	if exists {
		return
	}

	now := time.Now()
	child := model.Task{
		ID:          s.idgen.NextID(),
		CreatedAt:   now,
		UpdatedAt:   now,
		UserID:      parent.UserID,
		WorkspaceID: parent.WorkspaceID,
		CreatedBy:   parent.CreatedBy,
		Assignee:    parent.Assignee,
		Status:      "notstarted",
		Title:       parent.Title,
		Body:        parent.Body,
		Attachments: parent.Attachments,
		ParentID:    parent.ID,
	}

	created, err := s.repo.CreateTask(ctx, child)
	if err != nil {
		fmt.Printf("scheduler: failed to spawn task from cron %d: %v\n", parent.ID, err)
		return
	}

	fmt.Printf("scheduler: spawned task %d from cron %d\n", created.ID, parent.ID)

	s.bus.Publish(parent.WorkspaceID, eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(created),
	})
}
