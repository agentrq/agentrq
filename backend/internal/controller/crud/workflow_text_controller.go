package crud

import (
	"context"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/mustafaturan/monoflake"
)

// WorkflowTextController exposes the workflow graph as an editable document.
//
// The text format and the canvas are two views of one graph, so both go through
// the same validation here rather than each enforcing its own rules — that is
// what keeps them from drifting into disagreeing about what is legal.
type WorkflowTextController interface {
	GetWorkflowText(ctx context.Context, req entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error)
	ReplaceWorkflowFromText(ctx context.Context, req entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error)
}

// defaultStepBody is used for steps created through text mode, which has no
// syntax for a task template. It forwards the publisher's payload so a
// text-authored workflow still produces a task the agent can act on.
const defaultStepBody = "{{EVENT_PAYLOAD}}"

func (c *controller) GetWorkflowText(ctx context.Context, req entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()

	workflow, err := c.repository.GetWorkflow(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}
	steps, err := c.repository.ListWorkflowStepsByWorkflow(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}

	eventNames, err := c.eventNamesByID(ctx, uid)
	if err != nil {
		return nil, err
	}
	workspaceNames, err := c.workspaceNamesByID(ctx, uid)
	if err != nil {
		return nil, err
	}

	textSteps := make([]TextStep, 0, len(steps))
	for _, s := range steps {
		// A step naming a deleted event or workspace can no longer be rendered
		// faithfully; skipping it keeps the document valid rather than emitting
		// a line that would fail to parse on the way back in.
		eventName, ok := eventNames[s.EventID]
		if !ok {
			continue
		}
		workspaceName, ok := workspaceNames[s.WorkspaceID]
		if !ok {
			continue
		}
		textSteps = append(textSteps, TextStep{
			EventName:     eventName,
			WorkspaceName: workspaceName,
			EmitEventName: eventNames[s.EmitEventID],
		})
	}

	text := RenderWorkflowText(workflow.Name, eventNames[workflow.StartEventID], textSteps)
	return &entity.GetWorkflowTextResponse{Text: text}, nil
}

func (c *controller) ReplaceWorkflowFromText(ctx context.Context, req entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}

	workflow, err := c.repository.GetWorkflow(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}

	parsed, err := ParseWorkflowText(req.Text)
	if err != nil {
		return nil, err
	}

	eventIDs, err := c.eventIDsByName(ctx, uid)
	if err != nil {
		return nil, err
	}
	workspaceIDs, err := c.workspaceIDsByName(ctx, uid)
	if err != nil {
		return nil, err
	}

	// Resolve every name before writing anything, so an unknown name on the
	// last line cannot leave the graph half-replaced.
	startEventID, ok := eventIDs[parsed.StartEvent]
	if !ok {
		return nil, textErr(0, "unknown event %q; create it first", parsed.StartEvent)
	}
	if err := validateTextNames(parsed.Roots, eventIDs, workspaceIDs); err != nil {
		return nil, err
	}

	textSteps := FlattenWorkflowText(parsed)

	// Re-check cycles here even though the parser produces a tree: the same
	// event may appear in several branches, and those re-joins can close a loop
	// that no single branch reveals.
	var accumulated []model.WorkflowStep
	for _, ts := range textSteps {
		eventID := eventIDs[ts.EventName]
		emitEventID := eventIDs[ts.EmitEventName]
		if wouldCreateCycle(accumulated, eventID, emitEventID) {
			return nil, textErr(0, "%s: %s -> %s closes a loop", ErrWorkflowCycle, ts.EventName, ts.EmitEventName)
		}
		accumulated = append(accumulated, model.WorkflowStep{EventID: eventID, EmitEventID: emitEventID})
	}

	now := time.Now()
	steps := make([]model.WorkflowStep, 0, len(textSteps))
	for _, ts := range textSteps {
		steps = append(steps, model.WorkflowStep{
			ID:          c.idgen.NextID(),
			CreatedAt:   now,
			UpdatedAt:   now,
			WorkflowID:  req.ID,
			UserID:      uid,
			EventID:     eventIDs[ts.EventName],
			WorkspaceID: workspaceIDs[ts.WorkspaceName],
			EmitEventID: eventIDs[ts.EmitEventName],
			// Text mode cannot express a per-step template, so the title names
			// the triggering event and the body forwards its payload.
			Title:    fmt.Sprintf("%s: %s", parsed.Name, ts.EventName),
			Body:     defaultStepBody,
			Assignee: "agent",
		})
	}

	if err := c.repository.ReplaceWorkflowSteps(ctx, req.ID, uid, steps); err != nil {
		return nil, err
	}

	// The document also carries the workflow's own name and start event, so a
	// rename in text mode takes effect too.
	workflow.Name = parsed.Name
	workflow.StartEventID = startEventID
	updated, err := c.repository.UpdateWorkflow(ctx, workflow)
	if err != nil {
		return nil, err
	}

	return &entity.ReplaceWorkflowFromTextResponse{
		Workflow:  mapper.FromModelWorkflowToEntity(updated),
		StepCount: len(steps),
	}, nil
}

// validateTextNames walks the tree reporting the first unknown name with the
// line that introduced it, so the editor can mark the exact row.
func validateTextNames(agents []*TextNode, eventIDs map[string]int64, workspaceIDs map[string]int64) error {
	for _, agent := range agents {
		if _, ok := workspaceIDs[agent.Name]; !ok {
			return textErr(agent.Line, "unknown workspace %q", agent.Name)
		}
		for _, emitted := range agent.Children {
			if _, ok := eventIDs[emitted.Name]; !ok {
				return textErr(emitted.Line, "unknown event %q; create it first", emitted.Name)
			}
			if err := validateTextNames(emitted.Children, eventIDs, workspaceIDs); err != nil {
				return err
			}
		}
	}
	return nil
}

// ── Name/ID lookup tables ─────────────────────────────────────────────────────

func (c *controller) eventNamesByID(ctx context.Context, userID int64) (map[int64]string, error) {
	events, err := c.repository.ListEventsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(events))
	for _, e := range events {
		names[e.ID] = e.Name
	}
	return names, nil
}

func (c *controller) eventIDsByName(ctx context.Context, userID int64) (map[string]int64, error) {
	events, err := c.repository.ListEventsByUser(ctx, userID)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]int64, len(events))
	for _, e := range events {
		ids[e.Name] = e.ID
	}
	return ids, nil
}

func (c *controller) workspaceNamesByID(ctx context.Context, userID int64) (map[int64]string, error) {
	workspaces, err := c.repository.ListWorkspaces(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	names := make(map[int64]string, len(workspaces))
	for _, w := range workspaces {
		names[w.ID] = w.Name
	}
	return names, nil
}

func (c *controller) workspaceIDsByName(ctx context.Context, userID int64) (map[string]int64, error) {
	workspaces, err := c.repository.ListWorkspaces(ctx, userID, false)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]int64, len(workspaces))
	for _, w := range workspaces {
		ids[w.Name] = w.ID
	}
	return ids, nil
}
