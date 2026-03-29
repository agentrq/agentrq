package scheduler

import (
	"context"
	"time"

	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/robfig/cron/v3"
)

type Service interface {
	Start(ctx context.Context)
}

type scheduler struct {
	repo   base.Repository
	idgen  idgen.Service
	bus    *eventbus.Bus
	pubsub pubsub.Service
}

func New(repo base.Repository, idgen idgen.Service, bus *eventbus.Bus, ps pubsub.Service) Service {
	return &scheduler{repo: repo, idgen: idgen, bus: bus, pubsub: ps}
}

func (s *scheduler) Start(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	go func() {
		zlog.Info().Msg("scheduler: background poller started (interval: 1m)")
		for {
			select {
			case <-ctx.Done():
				zlog.Info().Msg("scheduler: background poller stopped")
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
		zlog.Error().Err(err).Msg("scheduler: failed to list crons")
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
			zlog.Warn().Err(err).Int64("task_id", c.ID).Str("schedule", c.CronSchedule).Msg("scheduler: invalid cron schedule")
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
		zlog.Error().Err(err).Int64("task_id", parent.ID).Msg("scheduler: error checking existence")
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
		zlog.Error().Err(err).Int64("cron_id", parent.ID).Msg("scheduler: failed to spawn task")
		return
	}

	if s.pubsub != nil {
		_, _ = s.pubsub.Publish(ctx, pubsub.PublishRequest{
			PubSubID: entity.PubSubTopicCRUD,
			Event: entity.CRUDEvent{
				Action:       entity.ActionTaskFromScheduled,
				WorkspaceID:  parent.WorkspaceID,
				UserID:       parent.UserID,
				ResourceType: entity.ResourceTask,
				ResourceID:   created.ID,
				Actor:        entity.ActorHuman, // System acting on behalf of human
			},
		})
	}

	zlog.Info().Int64("task_id", created.ID).Int64("cron_id", parent.ID).Msg("scheduler: spawned task")

	s.bus.Publish(parent.WorkspaceID, eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(created),
	})
}
