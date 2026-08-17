package event

import (
	"context"
	"fmt"
	"strings"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/agentrq/agentrq/backend/internal/repository/base"
	"github.com/agentrq/agentrq/backend/internal/service/eventbus"
	"github.com/agentrq/agentrq/backend/internal/service/idgen"
	"github.com/agentrq/agentrq/backend/internal/service/pubsub"
	"github.com/mustafaturan/monoflake"
	zlog "github.com/rs/zerolog/log"
)

type (
	Params struct {
		Repository base.Repository
		PubSub     pubsub.Service
		IDGen      idgen.Service
		Bus        *eventbus.Bus
	}

	Controller interface {
		Start(ctx context.Context) error
	}

	controller struct {
		repo   base.Repository
		pubsub pubsub.Service
		ids    idgen.Service
		bus    *eventbus.Bus
	}
)

func New(p Params) Controller {
	return &controller{
		repo:   p.Repository,
		pubsub: p.PubSub,
		ids:    p.IDGen,
		bus:    p.Bus,
	}
}

func (c *controller) Start(ctx context.Context) error {
	res, err := c.pubsub.Subscribe(ctx, pubsub.SubscribeRequest{PubSubID: entity.PubSubTopicEvents})
	if err != nil {
		return fmt.Errorf("failed to subscribe to events topic: %w", err)
	}

	zlog.Info().Msg("[event-consumer] started controller")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-res.Events:
				if !ok {
					zlog.Warn().Msg("[event-consumer] pubsub channel closed")
					return
				}
				ev, ok := msg.(entity.EventPublishedPayload)
				if !ok {
					continue
				}
				go c.processEvent(context.Background(), ev)
			}
		}
	}()

	return nil
}

// maxWorkflowDepth bounds how many hops one workflow run may take.
//
// Cycles are rejected when a step is created, so a well-formed graph can never
// reach this. It is a backstop for the cases setup validation cannot see: a
// graph edited mid-run, or rows written before that validation existed. Without
// it, one bad edge spawns tasks forever.
const maxWorkflowDepth = 32

func (c *controller) processEvent(ctx context.Context, ev entity.EventPublishedPayload) {
	faqText := renderFAQ(ev.FAQ)

	// A publish carrying a workflow stays inside that workflow: its steps
	// decide the fan-out, not the global triggers. This is what makes a
	// workflow an isolated graph rather than an edit to global event behavior.
	if ev.WorkflowID != 0 {
		c.processWorkflowEvent(ctx, ev, faqText)
		return
	}

	triggers, err := c.repo.SystemListEventTriggersByEventID(ctx, ev.EventID)
	if err != nil || len(triggers) == 0 {
		return
	}

	for _, trigger := range triggers {
		c.createTriggeredTask(ctx, trigger, ev.Payload, faqText)
	}
}

// processWorkflowEvent advances one hop of a workflow run.
func (c *controller) processWorkflowEvent(ctx context.Context, ev entity.EventPublishedPayload, faqText string) {
	if ev.Depth >= maxWorkflowDepth {
		zlog.Warn().
			Int64("workflowID", ev.WorkflowID).
			Int64("eventID", ev.EventID).
			Int("depth", ev.Depth).
			Msg("[event-consumer] workflow hop limit reached, stopping run")
		return
	}

	steps, err := c.repo.SystemListWorkflowStepsByEvent(ctx, ev.WorkflowID, ev.EventID)
	if err != nil || len(steps) == 0 {
		return
	}

	for _, step := range steps {
		c.createWorkflowTask(ctx, step, ev, faqText)
	}
}

func (c *controller) createTriggeredTask(ctx context.Context, trigger model.EventTrigger, payload string, faqText string) {
	title := strings.TrimSpace(trigger.Title)
	if title == "" {
		zlog.Warn().Int64("triggerID", trigger.ID).Msg("[event-consumer] rendered title is empty, skipping")
		return
	}
	body := renderTemplate(trigger.Body, payload, faqText)

	ws, err := c.repo.SystemGetWorkspace(ctx, trigger.WorkspaceID)
	if err != nil {
		zlog.Warn().Err(err).Int64("workspaceID", trigger.WorkspaceID).Msg("[event-consumer] workspace not found")
		return
	}

	// If the trigger chains to another event, append publish instruction to the body.
	if trigger.EmitEventID != 0 {
		if emitEv, evErr := c.repo.GetEvent(ctx, trigger.EmitEventID, ws.UserID); evErr == nil {
			instruction := fmt.Sprintf("\n\n[On completion: call publishEvent(\"%s\", \"<your output payload>\")]", emitEv.Name)
			if emitEv.PayloadGuidelines != "" {
				instruction += fmt.Sprintf("\nPayload guidelines: %s", emitEv.PayloadGuidelines)
			}
			body += instruction
		}
	}

	now := time.Now()
	task := model.Task{
		ID:               c.ids.NextID(),
		CreatedAt:        now,
		UpdatedAt:        now,
		WorkspaceID:      trigger.WorkspaceID,
		UserID:           ws.UserID,
		CreatedBy:        "agent",
		Assignee:         trigger.Assignee,
		Status:           "notstarted",
		Title:            title,
		Body:             body,
		AllowAllCommands: trigger.AllowAllCommands,
		TriggerID:        trigger.EventID,
		EventID:          trigger.EmitEventID,
	}

	created, err := c.repo.CreateTask(ctx, task)
	if err != nil {
		zlog.Warn().Err(err).Int64("triggerID", trigger.ID).Msg("[event-consumer] failed to create task")
		return
	}

	ownerID := monoflake.ID(ws.UserID).String()

	c.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event: entity.CRUDEvent{
			Action:       entity.ActionTaskCreate,
			WorkspaceID:  trigger.WorkspaceID,
			UserID:       ws.UserID,
			ResourceType: entity.ResourceTask,
			ResourceID:   created.ID,
			Actor:        entity.ActorAgent,
			Origin:       entity.OriginAPI,
		},
	})

	c.bus.Publish(trigger.WorkspaceID, ownerID, eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(created),
	})
}

// createWorkflowTask creates one task for a workflow step and, when the step
// chains onward, arms that task to publish the next event.
//
// Mirrors createTriggeredTask, but stamps the workflow and its hop count onto
// the task so the run keeps its identity across the task boundary.
func (c *controller) createWorkflowTask(ctx context.Context, step model.WorkflowStep, ev entity.EventPublishedPayload, faqText string) {
	title := strings.TrimSpace(step.Title)
	if title == "" {
		zlog.Warn().Int64("stepID", step.ID).Msg("[event-consumer] rendered title is empty, skipping")
		return
	}
	body := renderTemplate(step.Body, ev.Payload, faqText)

	ws, err := c.repo.SystemGetWorkspace(ctx, step.WorkspaceID)
	if err != nil {
		zlog.Warn().Err(err).Int64("workspaceID", step.WorkspaceID).Msg("[event-consumer] workspace not found")
		return
	}

	if step.EmitEventID != 0 {
		if emitEv, evErr := c.repo.GetEvent(ctx, step.EmitEventID, ws.UserID); evErr == nil {
			instruction := fmt.Sprintf("\n\n[On completion: call publishEvent(\"%s\", \"<your output payload>\")]", emitEv.Name)
			if emitEv.PayloadGuidelines != "" {
				instruction += fmt.Sprintf("\nPayload guidelines: %s", emitEv.PayloadGuidelines)
			}
			body += instruction
		}
	}

	now := time.Now()
	task := model.Task{
		ID:               c.ids.NextID(),
		CreatedAt:        now,
		UpdatedAt:        now,
		WorkspaceID:      step.WorkspaceID,
		UserID:           ws.UserID,
		CreatedBy:        "agent",
		Assignee:         step.Assignee,
		Status:           "notstarted",
		Title:            title,
		Body:             body,
		AllowAllCommands: step.AllowAllCommands,
		TriggerID:        step.EventID,
		EventID:          step.EmitEventID,
		WorkflowID:       step.WorkflowID,
		WorkflowDepth:    ev.Depth + 1,
	}

	created, err := c.repo.CreateTask(ctx, task)
	if err != nil {
		zlog.Warn().Err(err).Int64("stepID", step.ID).Msg("[event-consumer] failed to create task")
		return
	}

	ownerID := monoflake.ID(ws.UserID).String()

	c.pubsub.Publish(ctx, pubsub.PublishRequest{
		PubSubID: entity.PubSubTopicCRUD,
		Event: entity.CRUDEvent{
			Action:       entity.ActionTaskCreate,
			WorkspaceID:  step.WorkspaceID,
			UserID:       ws.UserID,
			ResourceType: entity.ResourceTask,
			ResourceID:   created.ID,
			Actor:        entity.ActorAgent,
			Origin:       entity.OriginAPI,
		},
	})

	c.bus.Publish(step.WorkspaceID, ownerID, eventbus.Event{
		Type:    "task.created",
		Payload: mapper.FromModelTaskToView(created),
	})
}

// renderTemplate substitutes {{EVENT_PAYLOAD}} and {{EVENT_FAQ}} in s.
func renderTemplate(s string, payload string, faqText string) string {
	s = strings.ReplaceAll(s, "{{EVENT_PAYLOAD}}", payload)
	s = strings.ReplaceAll(s, "{{EVENT_FAQ}}", faqText)
	return strings.TrimSpace(s)
}

// renderFAQ formats FAQ items as human-readable Q/A text.
func renderFAQ(faq []entity.EventFAQ) string {
	if len(faq) == 0 {
		return ""
	}
	var sb strings.Builder
	for i, f := range faq {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("Q: ")
		sb.WriteString(f.Q)
		sb.WriteString("\nA: ")
		sb.WriteString(f.A)
	}
	return sb.String()
}
