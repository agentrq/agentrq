package api

import (
	"encoding/json"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"github.com/gofiber/fiber/v2"
	"github.com/mustafaturan/monoflake"
)

// ── Workflow HTTP mappers ─────────────────────────────────────────────────────

func FromHTTPRequestToCreateWorkflowRequestEntity(c *fiber.Ctx) *entity.CreateWorkflowRequest {
	var payload view.CreateWorkflowRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	if payload.Name == "" {
		return nil
	}
	return &entity.CreateWorkflowRequest{
		Name:         payload.Name,
		Description:  payload.Description,
		StartEventID: monoflake.IDFromBase62(payload.StartEventID).Int64(),
	}
}

func FromCreateWorkflowResponseEntityToHTTPResponse(rs *entity.CreateWorkflowResponse) []byte {
	payload, _ := json.Marshal(view.CreateWorkflowResponse{Workflow: fromEntityWorkflowToView(rs.Workflow)})
	return payload
}

func FromHTTPRequestToGetWorkflowRequestEntity(c *fiber.Ctx) *entity.GetWorkflowRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.GetWorkflowRequest{ID: id}
}

func FromGetWorkflowResponseEntityToHTTPResponse(rs *entity.GetWorkflowResponse) []byte {
	payload, _ := json.Marshal(view.GetWorkflowResponse{Workflow: fromEntityWorkflowToView(rs.Workflow)})
	return payload
}

func FromListWorkflowsResponseEntityToHTTPResponse(rs *entity.ListWorkflowsResponse) []byte {
	workflows := make([]view.Workflow, len(rs.Workflows))
	for i, w := range rs.Workflows {
		workflows[i] = fromEntityWorkflowToView(w)
	}
	payload, _ := json.Marshal(view.ListWorkflowsResponse{Workflows: workflows})
	return payload
}

// FromHTTPRequestToUpdateWorkflowRequestEntity carries each field through as a
// pointer so the controller can tell "omitted" from "cleared": the graph editor
// PATCHes only `layout` on every node drag, and that must not wipe the name.
func FromHTTPRequestToUpdateWorkflowRequestEntity(c *fiber.Ctx) *entity.UpdateWorkflowRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	var payload view.UpdateWorkflowRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	rq := &entity.UpdateWorkflowRequest{
		ID:          id,
		Name:        payload.Name,
		Description: payload.Description,
	}
	if payload.StartEventID != nil {
		startEventID := monoflake.IDFromBase62(*payload.StartEventID).Int64()
		rq.StartEventID = &startEventID
	}
	if payload.Layout != nil {
		layout := string(*payload.Layout)
		rq.Layout = &layout
	}
	return rq
}

func FromUpdateWorkflowResponseEntityToHTTPResponse(rs *entity.UpdateWorkflowResponse) []byte {
	payload, _ := json.Marshal(view.UpdateWorkflowResponse{Workflow: fromEntityWorkflowToView(rs.Workflow)})
	return payload
}

func FromHTTPRequestToDeleteWorkflowRequestEntity(c *fiber.Ctx) *entity.DeleteWorkflowRequest {
	id := monoflake.IDFromBase62(c.Params("id")).Int64()
	if id == 0 {
		return nil
	}
	return &entity.DeleteWorkflowRequest{ID: id}
}

// ── WorkflowStep HTTP mappers ─────────────────────────────────────────────────

func FromHTTPRequestToCreateWorkflowStepRequestEntity(c *fiber.Ctx) *entity.CreateWorkflowStepRequest {
	workflowID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workflowID == 0 {
		return nil
	}
	var payload view.CreateWorkflowStepRequest
	if err := json.Unmarshal(c.BodyRaw(), &payload); err != nil {
		return nil
	}
	eventID := monoflake.IDFromBase62(payload.EventID).Int64()
	workspaceID := monoflake.IDFromBase62(payload.WorkspaceID).Int64()
	if payload.Title == "" || eventID == 0 || workspaceID == 0 {
		return nil
	}
	assignee := payload.Assignee
	if assignee == "" {
		assignee = "agent"
	}
	return &entity.CreateWorkflowStepRequest{
		WorkflowID:       workflowID,
		EventID:          eventID,
		WorkspaceID:      workspaceID,
		EmitEventID:      monoflake.IDFromBase62(payload.EmitEventID).Int64(),
		Title:            payload.Title,
		Body:             payload.Body,
		Assignee:         assignee,
		AllowAllCommands: payload.AllowAllCommands,
	}
}

func FromCreateWorkflowStepResponseEntityToHTTPResponse(rs *entity.CreateWorkflowStepResponse) []byte {
	payload, _ := json.Marshal(view.CreateWorkflowStepResponse{WorkflowStep: fromEntityWorkflowStepToView(rs.WorkflowStep)})
	return payload
}

func FromHTTPRequestToListWorkflowStepsRequestEntity(c *fiber.Ctx) *entity.ListWorkflowStepsRequest {
	workflowID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workflowID == 0 {
		return nil
	}
	return &entity.ListWorkflowStepsRequest{WorkflowID: workflowID}
}

func FromListWorkflowStepsResponseEntityToHTTPResponse(rs *entity.ListWorkflowStepsResponse) []byte {
	steps := make([]view.WorkflowStep, len(rs.WorkflowSteps))
	for i, s := range rs.WorkflowSteps {
		steps[i] = fromEntityWorkflowStepToView(s)
	}
	payload, _ := json.Marshal(view.ListWorkflowStepsResponse{WorkflowSteps: steps})
	return payload
}

func FromHTTPRequestToDeleteWorkflowStepRequestEntity(c *fiber.Ctx) *entity.DeleteWorkflowStepRequest {
	workflowID := monoflake.IDFromBase62(c.Params("id")).Int64()
	id := monoflake.IDFromBase62(c.Params("stepID")).Int64()
	if workflowID == 0 || id == 0 {
		return nil
	}
	return &entity.DeleteWorkflowStepRequest{ID: id, WorkflowID: workflowID}
}

func FromHTTPRequestToListTasksFromWorkflowRequestEntity(c *fiber.Ctx) *entity.ListTasksFromWorkflowRequest {
	workflowID := monoflake.IDFromBase62(c.Params("id")).Int64()
	if workflowID == 0 {
		return nil
	}
	return &entity.ListTasksFromWorkflowRequest{WorkflowID: workflowID}
}

func FromListTasksFromWorkflowResponseEntityToHTTPResponse(rs *entity.ListTasksFromWorkflowResponse) []byte {
	tasks := make([]view.Task, len(rs.Tasks))
	for i, t := range rs.Tasks {
		tasks[i] = FromEntityTaskToView(t)
	}
	payload, _ := json.Marshal(view.ListTasksResponse{Tasks: tasks})
	return payload
}

// ── Internal model↔entity mappers ─────────────────────────────────────────────

func FromModelWorkflowToEntity(m model.Workflow) entity.Workflow {
	return entity.Workflow{
		ID:           m.ID,
		CreatedAt:    m.CreatedAt,
		UpdatedAt:    m.UpdatedAt,
		UserID:       m.UserID,
		Name:         m.Name,
		Description:  m.Description,
		StartEventID: m.StartEventID,
		Layout:       string(m.Layout),
	}
}

func FromModelWorkflowStepToEntity(m model.WorkflowStep) entity.WorkflowStep {
	return entity.WorkflowStep{
		ID:               m.ID,
		CreatedAt:        m.CreatedAt,
		UpdatedAt:        m.UpdatedAt,
		WorkflowID:       m.WorkflowID,
		UserID:           m.UserID,
		EventID:          m.EventID,
		WorkspaceID:      m.WorkspaceID,
		EmitEventID:      m.EmitEventID,
		Title:            m.Title,
		Body:             m.Body,
		Assignee:         m.Assignee,
		AllowAllCommands: m.AllowAllCommands,
	}
}

// ── Internal entity↔view mappers ──────────────────────────────────────────────

func fromEntityWorkflowToView(w entity.Workflow) view.Workflow {
	v := view.Workflow{
		ID:          monoflake.ID(w.ID).String(),
		CreatedAt:   w.CreatedAt,
		UpdatedAt:   w.UpdatedAt,
		Name:        w.Name,
		Description: w.Description,
	}
	if w.StartEventID != 0 {
		v.StartEventID = monoflake.ID(w.StartEventID).String()
	}
	// Emit the stored layout only when it is valid JSON: it lands in a
	// json.RawMessage, so a malformed value would corrupt the whole response
	// body rather than just this field.
	if json.Valid([]byte(w.Layout)) {
		v.Layout = json.RawMessage(w.Layout)
	}
	return v
}

func fromEntityWorkflowStepToView(s entity.WorkflowStep) view.WorkflowStep {
	v := view.WorkflowStep{
		ID:               monoflake.ID(s.ID).String(),
		CreatedAt:        s.CreatedAt,
		WorkflowID:       monoflake.ID(s.WorkflowID).String(),
		EventID:          monoflake.ID(s.EventID).String(),
		WorkspaceID:      monoflake.ID(s.WorkspaceID).String(),
		Title:            s.Title,
		Body:             s.Body,
		Assignee:         s.Assignee,
		AllowAllCommands: s.AllowAllCommands,
	}
	if s.EmitEventID != 0 {
		v.EmitEventID = monoflake.ID(s.EmitEventID).String()
	}
	return v
}
