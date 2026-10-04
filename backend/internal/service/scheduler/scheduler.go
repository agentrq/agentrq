// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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
	"github.com/agentrq/agentrq/backend/internal/service/eventinstruction"
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
	now := time.Now().UTC().Truncate(time.Minute)

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
			s.spawn(ctx, c, now)
		}
	}
}

// spawn starts the run of parent scheduled for the minute at. Every backend
// instance ticks that minute, so both ways of starting it let exactly one
// instance through and leave the others with nothing to do or announce.
func (s *scheduler) spawn(ctx context.Context, parent model.Task, at time.Time) {
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

	// A schedule is one-time only when BOTH day-of-month and month are fixed (e.g.
	// "0 9 1 1 *"), matching the frontend contract (useCron.js and the task-form
	// generators). A fixed month with a wildcard day-of-month (e.g. "0 9 * 6 *" —
	// every day in June) is recurring and must keep its template.
	parts := strings.Fields(parent.CronSchedule)
	if len(parts) == 5 && parts[2] != "*" && parts[3] != "*" {
		s.startOneTime(ctx, parent, at)
		return
	}

	// The ID is claimed before the body is built so the instruction can tell the
	// agent which task it is publishing for.
	taskID := s.idgen.NextID()

	now := time.Now()
	child := model.Task{
		ID: taskID,
		// The scheduled minute, not the clock: it is what every instance
		// writes for this run, so idx_tasks_cron_run lets only one in.
		CreatedAt:        at,
		UpdatedAt:        now,
		UserID:           parent.UserID,
		WorkspaceID:      parent.WorkspaceID,
		CreatedBy:        parent.CreatedBy,
		Assignee:         parent.Assignee,
		Status:           "notstarted",
		Title:            parent.Title,
		Body:             s.runBody(ctx, parent, taskID),
		Attachments:      parent.Attachments,
		ParentID:         parent.ID,
		AllowAllCommands: parent.AllowAllCommands,
		EventID:          parent.EventID,
		// The template carries the whole completion choice, not just the event:
		// WorkflowID is what routes the publish through that workflow's steps
		// instead of the global triggers, and CompletionTriggerType is what lets
		// the UI say "workflow X" rather than mislabelling it as a bare event.
		// Copying EventID alone leaves a scheduled workflow run fanning out to
		// the wrong subscribers.
		//
		// WorkflowDepth stays at zero deliberately: each scheduled run begins a
		// fresh run of the workflow, so it is hop zero, not a continuation of
		// whatever spawned the template.
		WorkflowID:            parent.WorkflowID,
		CompletionTriggerType: parent.CompletionTriggerType,
	}

	created, err := s.repo.SystemCreateCronRun(ctx, child)
	if err != nil {
		zlog.Error().Err(err).Int64("cron_id", parent.ID).Msg("scheduler: failed to spawn task")
		return
	}
	if !created {
		// Another instance created this run first.
		return
	}

	s.publishRun(ctx, child)
	zlog.Info().Int64("task_id", child.ID).Int64("cron_id", parent.ID).Msg("scheduler: spawned task")
	s.bus.Publish(parent.WorkspaceID, monoflake.ID(parent.UserID).String(), eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(child),
	})
}

// startOneTime runs a one-time schedule as the template itself, rather than
// as a child of a template that is then deleted: the task keeps its ID, and
// only the instance whose update finds it still in cron announces it.
func (s *scheduler) startOneTime(ctx context.Context, t model.Task, at time.Time) {
	t.Status = "notstarted"
	t.CronSchedule = ""
	t.Body = s.runBody(ctx, t, t.ID)
	t.CreatedAt = at
	t.UpdatedAt = time.Now()

	started, err := s.repo.SystemStartOneTimeTask(ctx, t)
	if err != nil {
		zlog.Error().Err(err).Int64("cron_id", t.ID).Msg("scheduler: failed to start one-time task")
		return
	}
	if !started {
		// Another instance started it first.
		return
	}

	s.publishRun(ctx, t)
	zlog.Info().Int64("task_id", t.ID).Msg("scheduler: started one-time task")
	s.bus.Publish(t.WorkspaceID, monoflake.ID(t.UserID).String(), eventbus.Event{
		Type:    "task.updated",
		Payload: mapper.FromModelTaskToView(t),
	})
}

// runBody is the template's body plus, when it is linked to an event, the
// instruction to publish it naming runID. The publish instruction must live
// in the body because scheduled runs reach the agent via getTask/poller
// notifications, which have no event-aware path of their own.
func (s *scheduler) runBody(ctx context.Context, parent model.Task, runID int64) string {
	if parent.EventID == 0 {
		return parent.Body
	}
	ev, err := s.repo.GetEvent(ctx, parent.EventID, parent.UserID)
	if err != nil {
		zlog.Warn().Err(err).Int64("cron_id", parent.ID).Int64("event_id", parent.EventID).
			Msg("scheduler: linked event not found, on-completion publishEvent instruction omitted")
		return parent.Body
	}
	return parent.Body + eventinstruction.Build(eventinstruction.Params{
		EventName:         ev.Name,
		TaskID:            monoflake.ID(runID).String(),
		PayloadGuidelines: ev.PayloadGuidelines,
	})
}

func (s *scheduler) publishRun(ctx context.Context, run model.Task) {
	if s.pubsub == nil {
		return
	}
	_, _ = s.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event: entity.CRUDEvent{
			Action:       entity.ActionTaskFromScheduled,
			WorkspaceID:  run.WorkspaceID,
			UserID:       run.UserID,
			ResourceType: entity.ResourceTask,
			ResourceID:   run.ID,
			Actor:        entity.ActorHuman, // System acting on behalf of human
			Origin:       entity.OriginScheduler,
		},
	})
}
