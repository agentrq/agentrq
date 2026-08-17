package crud

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	mapper "github.com/agentrq/agentrq/backend/internal/mapper/api"
	"github.com/mustafaturan/monoflake"
	"gorm.io/datatypes"
)

// WorkflowController defines workflow operations (experimental).
type WorkflowController interface {
	CreateWorkflow(ctx context.Context, req entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error)
	GetWorkflow(ctx context.Context, req entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error)
	ListWorkflows(ctx context.Context, req entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error)
	UpdateWorkflow(ctx context.Context, req entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error)
	DeleteWorkflow(ctx context.Context, req entity.DeleteWorkflowRequest) error
}

// WorkflowStepController defines workflow step (graph edge) operations.
type WorkflowStepController interface {
	CreateWorkflowStep(ctx context.Context, req entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error)
	ListWorkflowSteps(ctx context.Context, req entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error)
	DeleteWorkflowStep(ctx context.Context, req entity.DeleteWorkflowStepRequest) error
	ListTasksFromWorkflow(ctx context.Context, req entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error)
}

// ErrWorkflowCycle is returned when a step would close a loop in the workflow
// graph. A cycle is rejected at setup rather than guarded at runtime because a
// looping graph spawns tasks forever, and the editor can show the problem while
// the user is still looking at the canvas.
var ErrWorkflowCycle = fmt.Errorf("this step would create a cycle in the workflow")

// ── Workflows ─────────────────────────────────────────────────────────────────

func (c *controller) CreateWorkflow(ctx context.Context, req entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}
	if req.Name == "" {
		return nil, fmt.Errorf("name is required")
	}

	// A start event is optional at create time so the editor can open an empty
	// canvas, but a supplied one must exist and belong to this user.
	if req.StartEventID != 0 {
		if _, err := c.repository.GetEvent(ctx, req.StartEventID, uid); err != nil {
			return nil, fmt.Errorf("start event not found")
		}
	}

	now := time.Now()
	m := model.Workflow{
		ID:           c.idgen.NextID(),
		CreatedAt:    now,
		UpdatedAt:    now,
		UserID:       uid,
		Name:         req.Name,
		Description:  req.Description,
		StartEventID: req.StartEventID,
	}

	created, err := c.repository.CreateWorkflow(ctx, m)
	if err != nil {
		return nil, err
	}
	return &entity.CreateWorkflowResponse{Workflow: mapper.FromModelWorkflowToEntity(created)}, nil
}

func (c *controller) GetWorkflow(ctx context.Context, req entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	w, err := c.repository.GetWorkflow(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}
	return &entity.GetWorkflowResponse{Workflow: mapper.FromModelWorkflowToEntity(w)}, nil
}

func (c *controller) ListWorkflows(ctx context.Context, req entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	models, err := c.repository.ListWorkflowsByUser(ctx, uid)
	if err != nil {
		return nil, err
	}
	workflows := make([]entity.Workflow, len(models))
	for i, m := range models {
		workflows[i] = mapper.FromModelWorkflowToEntity(m)
	}
	return &entity.ListWorkflowsResponse{Workflows: workflows}, nil
}

func (c *controller) UpdateWorkflow(ctx context.Context, req entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}

	existing, err := c.repository.GetWorkflow(ctx, req.ID, uid)
	if err != nil {
		return nil, err
	}

	if req.Name != nil {
		if *req.Name == "" {
			return nil, fmt.Errorf("name cannot be empty")
		}
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.StartEventID != nil {
		if *req.StartEventID != 0 {
			if _, err := c.repository.GetEvent(ctx, *req.StartEventID, uid); err != nil {
				return nil, fmt.Errorf("start event not found")
			}
		}
		existing.StartEventID = *req.StartEventID
	}
	if req.Layout != nil {
		// Layout is opaque to the backend, but it is stored in a JSON column
		// and echoed back inside a JSON response — so it has to parse.
		if *req.Layout != "" && !json.Valid([]byte(*req.Layout)) {
			return nil, fmt.Errorf("layout must be valid JSON")
		}
		existing.Layout = datatypes.JSON(*req.Layout)
	}

	updated, err := c.repository.UpdateWorkflow(ctx, existing)
	if err != nil {
		return nil, err
	}
	return &entity.UpdateWorkflowResponse{Workflow: mapper.FromModelWorkflowToEntity(updated)}, nil
}

func (c *controller) DeleteWorkflow(ctx context.Context, req entity.DeleteWorkflowRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	return c.repository.DeleteWorkflow(ctx, req.ID, uid)
}

// ── WorkflowSteps ─────────────────────────────────────────────────────────────

func (c *controller) CreateWorkflowStep(ctx context.Context, req entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	if uid == 0 {
		return nil, fmt.Errorf("invalid userID")
	}

	if _, err := c.repository.GetWorkflow(ctx, req.WorkflowID, uid); err != nil {
		return nil, fmt.Errorf("workflow not found")
	}
	if _, err := c.repository.GetEvent(ctx, req.EventID, uid); err != nil {
		return nil, fmt.Errorf("event not found")
	}

	ok, err := c.repository.CheckWorkspaceAccess(ctx, req.WorkspaceID, uid)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("workspace not found")
	}

	if req.Title == "" {
		return nil, fmt.Errorf("title is required")
	}

	if req.EmitEventID != 0 {
		if _, err := c.repository.GetEvent(ctx, req.EmitEventID, uid); err != nil {
			return nil, fmt.Errorf("emit event not found")
		}
	}

	existing, err := c.repository.ListWorkflowStepsByWorkflow(ctx, req.WorkflowID, uid)
	if err != nil {
		return nil, err
	}
	if wouldCreateCycle(existing, req.EventID, req.EmitEventID) {
		return nil, ErrWorkflowCycle
	}

	now := time.Now()
	m := model.WorkflowStep{
		ID:               c.idgen.NextID(),
		CreatedAt:        now,
		UpdatedAt:        now,
		WorkflowID:       req.WorkflowID,
		UserID:           uid,
		EventID:          req.EventID,
		WorkspaceID:      req.WorkspaceID,
		EmitEventID:      req.EmitEventID,
		Title:            req.Title,
		Body:             req.Body,
		Assignee:         req.Assignee,
		AllowAllCommands: req.AllowAllCommands,
	}

	created, err := c.repository.CreateWorkflowStep(ctx, m)
	if err != nil {
		return nil, err
	}
	return &entity.CreateWorkflowStepResponse{WorkflowStep: mapper.FromModelWorkflowStepToEntity(created)}, nil
}

func (c *controller) ListWorkflowSteps(ctx context.Context, req entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	models, err := c.repository.ListWorkflowStepsByWorkflow(ctx, req.WorkflowID, uid)
	if err != nil {
		return nil, err
	}
	steps := make([]entity.WorkflowStep, len(models))
	for i, m := range models {
		steps[i] = mapper.FromModelWorkflowStepToEntity(m)
	}
	return &entity.ListWorkflowStepsResponse{WorkflowSteps: steps}, nil
}

func (c *controller) DeleteWorkflowStep(ctx context.Context, req entity.DeleteWorkflowStepRequest) error {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	return c.repository.DeleteWorkflowStep(ctx, req.ID, uid)
}

func (c *controller) ListTasksFromWorkflow(ctx context.Context, req entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error) {
	uid := monoflake.IDFromBase62(req.UserID).Int64()
	models, err := c.repository.ListTasksByWorkflowID(ctx, req.WorkflowID, uid)
	if err != nil {
		return nil, err
	}
	tasks := make([]entity.Task, len(models))
	for i, m := range models {
		tasks[i] = c.fromModelTaskToEntity(m)
	}
	return &entity.ListTasksFromWorkflowResponse{Tasks: tasks}, nil
}

// ── Cycle detection ───────────────────────────────────────────────────────────

// wouldCreateCycle reports whether adding an edge from fromEventID to
// emitEventID closes a loop in the workflow's event graph.
//
// Only the event-to-event shape matters: a step is an edge
// event → workspace → event, and the workspace is a pass-through, so a run
// loops exactly when the events do. A step with no emit event is a leaf and can
// never close a loop.
//
// Two steps may legitimately share a source event (fan-out) or a target event
// (fan-in); neither is a cycle. What is rejected is emitEventID being able to
// reach fromEventID through edges that already exist.
func wouldCreateCycle(steps []model.WorkflowStep, fromEventID int64, emitEventID int64) bool {
	if emitEventID == 0 {
		return false
	}
	if emitEventID == fromEventID {
		return true
	}

	// Adjacency over existing edges only; the candidate edge is represented by
	// starting the walk at its target and looking for its source.
	adjacency := make(map[int64][]int64, len(steps))
	for _, s := range steps {
		if s.EmitEventID != 0 {
			adjacency[s.EventID] = append(adjacency[s.EventID], s.EmitEventID)
		}
	}

	visited := make(map[int64]bool, len(adjacency))
	stack := []int64{emitEventID}
	for len(stack) > 0 {
		current := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if current == fromEventID {
			return true
		}
		if visited[current] {
			continue
		}
		visited[current] = true
		stack = append(stack, adjacency[current]...)
	}
	return false
}
