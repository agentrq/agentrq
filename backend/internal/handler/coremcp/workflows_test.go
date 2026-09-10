package coremcp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/crud"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
)

// ── mock crud controller ──────────────────────────────────────────────────────
//
// Same shape as the events mock: the interface is embedded, so a method these
// tools do not call panics rather than quietly answering with a zero value.

type mockWorkflowCrud struct {
	crud.Controller

	createWorkflow        func(ctx context.Context, req entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error)
	getWorkflow           func(ctx context.Context, req entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error)
	listWorkflows         func(ctx context.Context, req entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error)
	updateWorkflow        func(ctx context.Context, req entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error)
	deleteWorkflow        func(ctx context.Context, req entity.DeleteWorkflowRequest) error
	createStep            func(ctx context.Context, req entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error)
	listSteps             func(ctx context.Context, req entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error)
	deleteStep            func(ctx context.Context, req entity.DeleteWorkflowStepRequest) error
	listTasksFromWorkflow func(ctx context.Context, req entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error)
	getWorkflowText       func(ctx context.Context, req entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error)
	replaceFromText       func(ctx context.Context, req entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error)
	deleteTask            func(ctx context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error)
}

func (m *mockWorkflowCrud) CreateWorkflow(ctx context.Context, req entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error) {
	return m.createWorkflow(ctx, req)
}
func (m *mockWorkflowCrud) GetWorkflow(ctx context.Context, req entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error) {
	return m.getWorkflow(ctx, req)
}
func (m *mockWorkflowCrud) ListWorkflows(ctx context.Context, req entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error) {
	return m.listWorkflows(ctx, req)
}
func (m *mockWorkflowCrud) UpdateWorkflow(ctx context.Context, req entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error) {
	return m.updateWorkflow(ctx, req)
}
func (m *mockWorkflowCrud) DeleteWorkflow(ctx context.Context, req entity.DeleteWorkflowRequest) error {
	return m.deleteWorkflow(ctx, req)
}
func (m *mockWorkflowCrud) CreateWorkflowStep(ctx context.Context, req entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error) {
	return m.createStep(ctx, req)
}
func (m *mockWorkflowCrud) ListWorkflowSteps(ctx context.Context, req entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error) {
	return m.listSteps(ctx, req)
}
func (m *mockWorkflowCrud) DeleteWorkflowStep(ctx context.Context, req entity.DeleteWorkflowStepRequest) error {
	return m.deleteStep(ctx, req)
}
func (m *mockWorkflowCrud) ListTasksFromWorkflow(ctx context.Context, req entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error) {
	return m.listTasksFromWorkflow(ctx, req)
}
func (m *mockWorkflowCrud) GetWorkflowText(ctx context.Context, req entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error) {
	return m.getWorkflowText(ctx, req)
}
func (m *mockWorkflowCrud) ReplaceWorkflowFromText(ctx context.Context, req entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error) {
	return m.replaceFromText(ctx, req)
}
func (m *mockWorkflowCrud) DeleteTask(ctx context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error) {
	return m.deleteTask(ctx, req)
}

const (
	testWorkflowID = int64(500)
	testStepID     = int64(600)
	testTaskID     = int64(700)
)

func workflowServer(ctrl *mockWorkflowCrud) *WorkspaceServer {
	return &WorkspaceServer{crud: ctrl}
}

// ── workflows ─────────────────────────────────────────────────────────────────

func TestListWorkflows_ScopesToTheAuthenticatedUser(t *testing.T) {
	var got entity.ListWorkflowsRequest
	ctrl := &mockWorkflowCrud{listWorkflows: func(_ context.Context, req entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error) {
		got = req
		return &entity.ListWorkflowsResponse{Workflows: []entity.Workflow{{ID: testWorkflowID, Name: "release"}}}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleListWorkflows(authedContext(), nil, struct{}{})))

	if got.UserID != testUserID {
		t.Errorf("UserID = %q, want %q", got.UserID, testUserID)
	}
	var payload struct {
		Workflows []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"workflows"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(payload.Workflows) != 1 || payload.Workflows[0].Name != "release" {
		t.Fatalf("workflows = %+v", payload.Workflows)
	}
	// Base62 on the wire, like every other id these tools answer with.
	if payload.Workflows[0].ID != base62(testWorkflowID) {
		t.Errorf("id = %q, want %q", payload.Workflows[0].ID, base62(testWorkflowID))
	}
}

func TestListWorkflows_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{listWorkflows: func(context.Context, entity.ListWorkflowsRequest) (*entity.ListWorkflowsResponse, error) {
		return nil, errors.New("database unavailable")
	}}

	result := toolResult(workflowServer(ctrl).handleListWorkflows(authedContext(), nil, struct{}{}))

	if !result.isError || result.text != "database unavailable" {
		t.Fatalf("result = %+v", result)
	}
}

func TestCreateWorkflow_ParsesTheStartEventAndCarriesTheUser(t *testing.T) {
	var got entity.CreateWorkflowRequest
	ctrl := &mockWorkflowCrud{createWorkflow: func(_ context.Context, req entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error) {
		got = req
		return &entity.CreateWorkflowResponse{Workflow: entity.Workflow{ID: testWorkflowID, Name: req.Name}}, nil
	}}

	textOf(t, toolResult(workflowServer(ctrl).handleCreateWorkflow(authedContext(), nil, CreateWorkflowParams{
		Name:         "release",
		Description:  "ship it",
		StartEventID: base62(testEventID),
	})))

	if got.UserID != testUserID || got.Name != "release" || got.Description != "ship it" {
		t.Fatalf("request = %+v", got)
	}
	if got.StartEventID != testEventID {
		t.Errorf("StartEventID = %d, want %d", got.StartEventID, testEventID)
	}
}

func TestCreateWorkflow_ReportsTheControllersRefusal(t *testing.T) {
	ctrl := &mockWorkflowCrud{createWorkflow: func(context.Context, entity.CreateWorkflowRequest) (*entity.CreateWorkflowResponse, error) {
		return nil, errors.New("event not found")
	}}

	result := toolResult(workflowServer(ctrl).handleCreateWorkflow(authedContext(), nil, CreateWorkflowParams{Name: "release"}))

	if !result.isError || result.text != "event not found" {
		t.Fatalf("result = %+v", result)
	}
}

func TestGetWorkflow_PassesTheParsedID(t *testing.T) {
	var got entity.GetWorkflowRequest
	ctrl := &mockWorkflowCrud{getWorkflow: func(_ context.Context, req entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error) {
		got = req
		return &entity.GetWorkflowResponse{Workflow: entity.Workflow{ID: testWorkflowID, Name: "release"}}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleGetWorkflow(authedContext(), nil, GetWorkflowParams{
		WorkflowID: base62(testWorkflowID),
	})))

	if got.ID != testWorkflowID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "release") {
		t.Errorf("body = %q", body)
	}
}

// A workflow belonging to somebody else is a not-found from the controller, and
// the tool must pass that on rather than translating it into something softer.
func TestGetWorkflow_ReportsAnotherUsersWorkflowAsNotFound(t *testing.T) {
	ctrl := &mockWorkflowCrud{getWorkflow: func(_ context.Context, req entity.GetWorkflowRequest) (*entity.GetWorkflowResponse, error) {
		if req.UserID != testUserID {
			t.Fatalf("UserID = %q", req.UserID)
		}
		return nil, errors.New("workflow not found")
	}}

	result := toolResult(workflowServer(ctrl).handleGetWorkflow(authedContext(), nil, GetWorkflowParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "workflow not found" {
		t.Fatalf("result = %+v", result)
	}
}

// The PATCH semantics are the whole point of the pointer fields: a caller
// renaming a workflow must not blank the canvas layout somebody else is editing.
func TestUpdateWorkflow_LeavesOutTheFieldsThatWereNotSent(t *testing.T) {
	var got entity.UpdateWorkflowRequest
	ctrl := &mockWorkflowCrud{updateWorkflow: func(_ context.Context, req entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error) {
		got = req
		return &entity.UpdateWorkflowResponse{Workflow: entity.Workflow{ID: testWorkflowID}}, nil
	}}
	name := "renamed"

	textOf(t, toolResult(workflowServer(ctrl).handleUpdateWorkflow(authedContext(), nil, UpdateWorkflowParams{
		WorkflowID: base62(testWorkflowID),
		Name:       &name,
	})))

	if got.ID != testWorkflowID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if got.Name == nil || *got.Name != "renamed" {
		t.Errorf("Name = %v, want a pointer to %q", got.Name, "renamed")
	}
	if got.Description != nil || got.Layout != nil || got.StartEventID != nil {
		t.Errorf("unsent fields came through as set: %+v", got)
	}
}

func TestUpdateWorkflow_ParsesAStartEventThatWasSent(t *testing.T) {
	var got entity.UpdateWorkflowRequest
	ctrl := &mockWorkflowCrud{updateWorkflow: func(_ context.Context, req entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error) {
		got = req
		return &entity.UpdateWorkflowResponse{Workflow: entity.Workflow{ID: testWorkflowID}}, nil
	}}
	start := base62(testEventID)
	layout := `{"nodes":[]}`

	textOf(t, toolResult(workflowServer(ctrl).handleUpdateWorkflow(authedContext(), nil, UpdateWorkflowParams{
		WorkflowID:   base62(testWorkflowID),
		StartEventID: &start,
		Layout:       &layout,
	})))

	if got.StartEventID == nil || *got.StartEventID != testEventID {
		t.Fatalf("StartEventID = %v, want a pointer to %d", got.StartEventID, testEventID)
	}
	if got.Layout == nil || *got.Layout != layout {
		t.Errorf("Layout = %v", got.Layout)
	}
}

func TestUpdateWorkflow_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{updateWorkflow: func(context.Context, entity.UpdateWorkflowRequest) (*entity.UpdateWorkflowResponse, error) {
		return nil, errors.New("workflow not found")
	}}

	result := toolResult(workflowServer(ctrl).handleUpdateWorkflow(authedContext(), nil, UpdateWorkflowParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "workflow not found" {
		t.Fatalf("result = %+v", result)
	}
}

func TestDeleteWorkflow_ConfirmsInPlainWords(t *testing.T) {
	var got entity.DeleteWorkflowRequest
	ctrl := &mockWorkflowCrud{deleteWorkflow: func(_ context.Context, req entity.DeleteWorkflowRequest) error {
		got = req
		return nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleDeleteWorkflow(authedContext(), nil, DeleteWorkflowParams{
		WorkflowID: base62(testWorkflowID),
	})))

	if got.ID != testWorkflowID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if body != "workflow deleted" {
		t.Errorf("body = %q", body)
	}
}

func TestDeleteWorkflow_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{deleteWorkflow: func(context.Context, entity.DeleteWorkflowRequest) error {
		return errors.New("workflow not found")
	}}

	result := toolResult(workflowServer(ctrl).handleDeleteWorkflow(authedContext(), nil, DeleteWorkflowParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "workflow not found" {
		t.Fatalf("result = %+v", result)
	}
}

// ── steps ─────────────────────────────────────────────────────────────────────

func TestCreateWorkflowStep_ParsesEveryIDAndDefaultsTheAssignee(t *testing.T) {
	var got entity.CreateWorkflowStepRequest
	ctrl := &mockWorkflowCrud{createStep: func(_ context.Context, req entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error) {
		got = req
		return &entity.CreateWorkflowStepResponse{WorkflowStep: entity.WorkflowStep{ID: testStepID}}, nil
	}}

	textOf(t, toolResult(workflowServer(ctrl).handleCreateWorkflowStep(authedContext(), nil, CreateWorkflowStepParams{
		WorkflowID:       base62(testWorkflowID),
		EventID:          base62(testEventID),
		WorkspaceID:      base62(testWorkspace),
		EmitEventID:      base62(testEmitEvent),
		Title:            "write the notes",
		Body:             "{{EVENT_PAYLOAD}}",
		AllowAllCommands: true,
	})))

	if got.WorkflowID != testWorkflowID || got.EventID != testEventID ||
		got.WorkspaceID != testWorkspace || got.EmitEventID != testEmitEvent {
		t.Fatalf("ids = %+v", got)
	}
	// Left empty by the caller, filled in here: a step with no assignee at all
	// would be created and then never picked up by anything.
	if got.Assignee != "agent" {
		t.Errorf("Assignee = %q, want %q", got.Assignee, "agent")
	}
	if got.Title != "write the notes" || got.Body != "{{EVENT_PAYLOAD}}" || !got.AllowAllCommands {
		t.Errorf("request = %+v", got)
	}
}

func TestCreateWorkflowStep_KeepsAnAssigneeThatWasGiven(t *testing.T) {
	var got entity.CreateWorkflowStepRequest
	ctrl := &mockWorkflowCrud{createStep: func(_ context.Context, req entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error) {
		got = req
		return &entity.CreateWorkflowStepResponse{WorkflowStep: entity.WorkflowStep{ID: testStepID}}, nil
	}}

	textOf(t, toolResult(workflowServer(ctrl).handleCreateWorkflowStep(authedContext(), nil, CreateWorkflowStepParams{
		WorkflowID: base62(testWorkflowID),
		Assignee:   "human",
	})))

	if got.Assignee != "human" {
		t.Errorf("Assignee = %q, want %q", got.Assignee, "human")
	}
}

// The cycle check lives in the controller, and the tool's job is to report it
// rather than to have its own opinion about which graphs are legal.
func TestCreateWorkflowStep_ReportsACycle(t *testing.T) {
	ctrl := &mockWorkflowCrud{createStep: func(context.Context, entity.CreateWorkflowStepRequest) (*entity.CreateWorkflowStepResponse, error) {
		return nil, errors.New("step would create a cycle")
	}}

	result := toolResult(workflowServer(ctrl).handleCreateWorkflowStep(authedContext(), nil, CreateWorkflowStepParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "step would create a cycle" {
		t.Fatalf("result = %+v", result)
	}
}

func TestListWorkflowSteps_PassesTheWorkflowAndUser(t *testing.T) {
	var got entity.ListWorkflowStepsRequest
	ctrl := &mockWorkflowCrud{listSteps: func(_ context.Context, req entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error) {
		got = req
		return &entity.ListWorkflowStepsResponse{WorkflowSteps: []entity.WorkflowStep{{ID: testStepID, Title: "write the notes"}}}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleListWorkflowSteps(authedContext(), nil, ListWorkflowStepsParams{
		WorkflowID: base62(testWorkflowID),
	})))

	if got.WorkflowID != testWorkflowID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "write the notes") {
		t.Errorf("body = %q", body)
	}
}

func TestListWorkflowSteps_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{listSteps: func(context.Context, entity.ListWorkflowStepsRequest) (*entity.ListWorkflowStepsResponse, error) {
		return nil, errors.New("workflow not found")
	}}

	result := toolResult(workflowServer(ctrl).handleListWorkflowSteps(authedContext(), nil, ListWorkflowStepsParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "workflow not found" {
		t.Fatalf("result = %+v", result)
	}
}

// Both ids are sent: the step alone would be enough to find the row, and
// requiring the workflow too is what stops a step being removed from a graph the
// caller was not looking at.
func TestDeleteWorkflowStep_SendsBothIDs(t *testing.T) {
	var got entity.DeleteWorkflowStepRequest
	ctrl := &mockWorkflowCrud{deleteStep: func(_ context.Context, req entity.DeleteWorkflowStepRequest) error {
		got = req
		return nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleDeleteWorkflowStep(authedContext(), nil, DeleteWorkflowStepParams{
		WorkflowID: base62(testWorkflowID),
		StepID:     base62(testStepID),
	})))

	if got.WorkflowID != testWorkflowID || got.ID != testStepID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if body != "workflow step deleted" {
		t.Errorf("body = %q", body)
	}
}

func TestDeleteWorkflowStep_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{deleteStep: func(context.Context, entity.DeleteWorkflowStepRequest) error {
		return errors.New("step not found")
	}}

	result := toolResult(workflowServer(ctrl).handleDeleteWorkflowStep(authedContext(), nil, DeleteWorkflowStepParams{
		WorkflowID: base62(testWorkflowID),
		StepID:     base62(testStepID),
	}))

	if !result.isError || result.text != "step not found" {
		t.Fatalf("result = %+v", result)
	}
}

func TestListWorkflowTasks_PassesTheWorkflowAndUser(t *testing.T) {
	var got entity.ListTasksFromWorkflowRequest
	ctrl := &mockWorkflowCrud{listTasksFromWorkflow: func(_ context.Context, req entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error) {
		got = req
		return &entity.ListTasksFromWorkflowResponse{Tasks: []entity.Task{{ID: testTaskID, Title: "write the notes"}}}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleListWorkflowTasks(authedContext(), nil, ListWorkflowTasksParams{
		WorkflowID: base62(testWorkflowID),
	})))

	if got.WorkflowID != testWorkflowID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "write the notes") {
		t.Errorf("body = %q", body)
	}
}

func TestListWorkflowTasks_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{listTasksFromWorkflow: func(context.Context, entity.ListTasksFromWorkflowRequest) (*entity.ListTasksFromWorkflowResponse, error) {
		return nil, errors.New("workflow not found")
	}}

	result := toolResult(workflowServer(ctrl).handleListWorkflowTasks(authedContext(), nil, ListWorkflowTasksParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "workflow not found" {
		t.Fatalf("result = %+v", result)
	}
}

// ── text mode ─────────────────────────────────────────────────────────────────

// The document itself, not a JSON envelope around it. A caller reading text mode
// is about to edit these lines and hand them straight back.
func TestGetWorkflowText_AnswersWithTheDocumentItself(t *testing.T) {
	var got entity.GetWorkflowTextRequest
	document := "workflow: release\nevent: code_changed\n- agent:doc\n"
	ctrl := &mockWorkflowCrud{getWorkflowText: func(_ context.Context, req entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error) {
		got = req
		return &entity.GetWorkflowTextResponse{Text: document}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleGetWorkflowText(authedContext(), nil, GetWorkflowTextParams{
		WorkflowID: base62(testWorkflowID),
	})))

	if got.ID != testWorkflowID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if body != document {
		t.Errorf("body = %q, want the document verbatim", body)
	}
}

func TestGetWorkflowText_ReportsAFailure(t *testing.T) {
	ctrl := &mockWorkflowCrud{getWorkflowText: func(context.Context, entity.GetWorkflowTextRequest) (*entity.GetWorkflowTextResponse, error) {
		return nil, errors.New("workflow not found")
	}}

	result := toolResult(workflowServer(ctrl).handleGetWorkflowText(authedContext(), nil, GetWorkflowTextParams{
		WorkflowID: base62(testWorkflowID),
	}))

	if !result.isError || result.text != "workflow not found" {
		t.Fatalf("result = %+v", result)
	}
}

func TestReplaceWorkflowFromText_PassesTheDocumentThrough(t *testing.T) {
	var got entity.ReplaceWorkflowFromTextRequest
	document := "workflow: release\nevent: code_changed\n- agent:doc\n"
	ctrl := &mockWorkflowCrud{replaceFromText: func(_ context.Context, req entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error) {
		got = req
		return &entity.ReplaceWorkflowFromTextResponse{
			Workflow:  entity.Workflow{ID: testWorkflowID, Name: "release"},
			StepCount: 1,
		}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleReplaceWorkflowFromText(authedContext(), nil, ReplaceWorkflowFromTextParams{
		WorkflowID: base62(testWorkflowID),
		Text:       document,
	})))

	if got.ID != testWorkflowID || got.UserID != testUserID || got.Text != document {
		t.Fatalf("request = %+v", got)
	}
	if !strings.Contains(body, "release") {
		t.Errorf("body = %q", body)
	}
}

// A name the document mentions that does not exist is reported with the line
// the controller pointed at — that is the only thing an author can act on.
func TestReplaceWorkflowFromText_ReportsTheParseError(t *testing.T) {
	ctrl := &mockWorkflowCrud{replaceFromText: func(context.Context, entity.ReplaceWorkflowFromTextRequest) (*entity.ReplaceWorkflowFromTextResponse, error) {
		return nil, errors.New(`line 3: unknown event "doc_updated"; create it first`)
	}}

	result := toolResult(workflowServer(ctrl).handleReplaceWorkflowFromText(authedContext(), nil, ReplaceWorkflowFromTextParams{
		WorkflowID: base62(testWorkflowID),
		Text:       "workflow: release\n",
	}))

	if !result.isError || !strings.Contains(result.text, `unknown event "doc_updated"`) {
		t.Fatalf("result = %+v", result)
	}
}

// ── deleteTask ────────────────────────────────────────────────────────────────

func TestDeleteTask_SendsTheWorkspaceTaskAndUser(t *testing.T) {
	var got entity.DeleteTaskRequest
	ctrl := &mockWorkflowCrud{deleteTask: func(_ context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error) {
		got = req
		return &entity.DeleteTaskResponse{}, nil
	}}

	body := textOf(t, toolResult(workflowServer(ctrl).handleDeleteTask(authedContext(), nil, DeleteTaskParams{
		WorkspaceID: base62(testWorkspace),
		TaskID:      base62(testTaskID),
	})))

	if got.WorkspaceID != testWorkspace || got.TaskID != testTaskID || got.UserID != testUserID {
		t.Fatalf("request = %+v", got)
	}
	if body != "task deleted" {
		t.Errorf("body = %q", body)
	}
}

// The ownership check is `ensureActiveWorkspace` in the controller, and it is
// the only thing standing between this tool and another account's tasks. The
// tool must carry the user through and report the refusal as it came.
func TestDeleteTask_RefusesAWorkspaceTheUserDoesNotOwn(t *testing.T) {
	ctrl := &mockWorkflowCrud{deleteTask: func(_ context.Context, req entity.DeleteTaskRequest) (*entity.DeleteTaskResponse, error) {
		if req.UserID != testUserID {
			t.Fatalf("UserID = %q, want %q", req.UserID, testUserID)
		}
		return nil, errors.New("workspace not found")
	}}

	result := toolResult(workflowServer(ctrl).handleDeleteTask(authedContext(), nil, DeleteTaskParams{
		WorkspaceID: base62(testWorkspace),
		TaskID:      base62(testTaskID),
	}))

	if !result.isError || result.text != "workspace not found" {
		t.Fatalf("result = %+v", result)
	}
}
