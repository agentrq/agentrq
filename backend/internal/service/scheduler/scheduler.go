package scheduler

import (
	"context"
	"strings"
	"time"

	zlog "github.com/rs/zerolog/log"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
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

	// lastProcessed is the most recent wall-clock minute the poller has evaluated. It is
	// touched only by the single ticker goroutine (and directly in tests), so it needs no
	// lock. It lets tick catch up any minutes skipped by ticker drift or scheduling latency
	// so a scheduled minute is never silently missed.
	lastProcessed time.Time
}

// maxCatchUpMinutes bounds how far back tick will catch up in one pass, so a long pause
// (suspend, GC stall, clock jump) cannot spawn an unbounded backlog of tasks.
const maxCatchUpMinutes = 120

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
		return // leave lastProcessed unchanged so the next tick catches up these minutes
	}

	now := time.Now().UTC().Truncate(time.Minute)
	minutes := minutesToProcess(s.lastProcessed, now)
	if len(minutes) == 0 {
		return
	}

	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	for _, c := range crons {
		if c.CronSchedule == "" {
			continue
		}
		sched, err := parser.Parse(c.CronSchedule)
		if err != nil {
			zlog.Warn().Err(err).Int64("task_id", c.ID).Str("schedule", c.CronSchedule).Msg("scheduler: invalid cron schedule")
			continue
		}
		// Fire the schedule for every minute in the window that matches. Evaluating each
		// elapsed minute (not just "now") means ticker drift or latency cannot skip a
		// scheduled minute; lastProcessed advancing monotonically means none fires twice.
		for _, minute := range minutes {
			if sched.Next(minute.Add(-1 * time.Second)).Equal(minute) {
				s.spawn(ctx, c)
			}
		}
	}

	s.lastProcessed = now
}

// minutesToProcess returns the minutes in (last, now] that tick should evaluate, each
// truncated to the minute. On the first run (last is zero) it returns only the current
// minute. It returns nil when no new full minute has elapsed (or the clock moved
// backward), and caps the window at maxCatchUpMinutes ending at now.
func minutesToProcess(last, now time.Time) []time.Time {
	now = now.Truncate(time.Minute)
	if now.IsZero() {
		return nil
	}

	var start time.Time
	if last.IsZero() {
		start = now // first run: only the current minute
	} else {
		start = last.Truncate(time.Minute).Add(time.Minute)
	}
	if start.After(now) {
		return nil // no new minute elapsed, or the clock went backward
	}
	if earliest := now.Add(-time.Duration(maxCatchUpMinutes-1) * time.Minute); start.Before(earliest) {
		start = earliest
	}

	var minutes []time.Time
	for m := start; !m.After(now); m = m.Add(time.Minute) {
		minutes = append(minutes, m)
	}
	return minutes
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
		ID:               s.idgen.NextID(),
		CreatedAt:        now,
		UpdatedAt:        now,
		UserID:           parent.UserID,
		WorkspaceID:      parent.WorkspaceID,
		CreatedBy:        parent.CreatedBy,
		Assignee:         parent.Assignee,
		Status:           "notstarted",
		Title:            parent.Title,
		Body:             parent.Body,
		Attachments:      parent.Attachments,
		ParentID:         parent.ID,
		AllowAllCommands: parent.AllowAllCommands,
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
				Origin:       entity.OriginScheduler,
			},
		})
	}

	zlog.Info().Int64("task_id", created.ID).Int64("cron_id", parent.ID).Msg("scheduler: spawned task")

	s.bus.Publish(parent.WorkspaceID, monoflake.ID(parent.UserID).String(), eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(created),
	})

	// If this is a one-time schedule, delete the parent template now that we've spawned it.
	// A schedule is one-time only when BOTH day-of-month and month are fixed (e.g.
	// "0 9 1 1 *"), matching the frontend contract (useCron.js and the task-form
	// generators). A fixed month with a wildcard day-of-month (e.g. "0 9 * 6 *" —
	// every day in June) is recurring and must keep its parent template.
	parts := strings.Fields(parent.CronSchedule)
	if len(parts) == 5 && parts[2] != "*" && parts[3] != "*" {
		err := s.repo.DeleteTask(ctx, parent.WorkspaceID, parent.ID, parent.UserID)
		if err != nil {
			zlog.Error().Err(err).Int64("cron_id", parent.ID).Msg("scheduler: failed to delete one-time parent task")
		} else {
			zlog.Info().Int64("cron_id", parent.ID).Msg("scheduler: deleted one-time parent task")
			if s.pubsub != nil {
				_, _ = s.pubsub.Publish(ctx, pubsub.PublishRequest{
					PubSubID: entity.PubSubTopicCRUD,
					Event: entity.CRUDEvent{
						Action:       entity.ActionTaskDelete,
						WorkspaceID:  parent.WorkspaceID,
						UserID:       parent.UserID,
						ResourceType: entity.ResourceTask,
						ResourceID:   parent.ID,
						Actor:        entity.ActorHuman,
						Origin:       entity.OriginScheduler,
					},
				})
			}
		}
	}
}
