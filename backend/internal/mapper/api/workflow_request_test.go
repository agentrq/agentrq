// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func ptr[T any](v T) *T { return &v }

func TestFromHTTPRequestToCreateWorkflowRequestEntity(t *testing.T) {
	checkRequest(t, "/workflows", FromHTTPRequestToCreateWorkflowRequestEntity, []requestCase[entity.CreateWorkflowRequest]{
		{"every field", "/workflows", `{"name":"release","description":"d","startEventId":"` + b62(10) + `"}`,
			&entity.CreateWorkflowRequest{Name: "release", Description: "d", StartEventID: 10}},
		{"a name alone", "/workflows", `{"name":"release"}`, &entity.CreateWorkflowRequest{Name: "release"}},
		{"no name", "/workflows", `{"description":"d"}`, nil},
		{"not JSON", "/workflows", `{`, nil},
	})
}

func TestFromHTTPRequestToWorkflowIDRequestEntities(t *testing.T) {
	wf := "/workflows/" + b62(5)
	checkRequest(t, "/workflows/:id", FromHTTPRequestToGetWorkflowRequestEntity, []requestCase[entity.GetWorkflowRequest]{
		{"a workflow", wf, ``, &entity.GetWorkflowRequest{ID: 5}},
		{"workflow 0", "/workflows/0", ``, nil},
	})
	checkRequest(t, "/workflows/:id", FromHTTPRequestToDeleteWorkflowRequestEntity, []requestCase[entity.DeleteWorkflowRequest]{
		{"a workflow", wf, ``, &entity.DeleteWorkflowRequest{ID: 5}},
		{"workflow 0", "/workflows/0", ``, nil},
	})
	checkRequest(t, "/workflows/:id/steps", FromHTTPRequestToListWorkflowStepsRequestEntity, []requestCase[entity.ListWorkflowStepsRequest]{
		{"a workflow", wf + "/steps", ``, &entity.ListWorkflowStepsRequest{WorkflowID: 5}},
		{"workflow 0", "/workflows/0/steps", ``, nil},
	})
	checkRequest(t, "/workflows/:id/tasks", FromHTTPRequestToListTasksFromWorkflowRequestEntity, []requestCase[entity.ListTasksFromWorkflowRequest]{
		{"a workflow", wf + "/tasks", ``, &entity.ListTasksFromWorkflowRequest{WorkflowID: 5}},
		{"workflow 0", "/workflows/0/tasks", ``, nil},
	})
	checkRequest(t, "/workflows/:id/text", FromHTTPRequestToGetWorkflowTextRequestEntity, []requestCase[entity.GetWorkflowTextRequest]{
		{"a workflow", wf + "/text", ``, &entity.GetWorkflowTextRequest{ID: 5}},
		{"workflow 0", "/workflows/0/text", ``, nil},
	})
	checkRequest(t, "/workflows/:id/text", FromHTTPRequestToReplaceWorkflowFromTextRequestEntity, []requestCase[entity.ReplaceWorkflowFromTextRequest]{
		{"new text", wf + "/text", `{"text":"on a -> b"}`, &entity.ReplaceWorkflowFromTextRequest{ID: 5, Text: "on a -> b"}},
		{"workflow 0", "/workflows/0/text", `{"text":"x"}`, nil},
		{"not JSON", wf + "/text", `{`, nil},
	})
	checkRequest(t, "/workflows/:id/steps/:stepID", FromHTTPRequestToDeleteWorkflowStepRequestEntity, []requestCase[entity.DeleteWorkflowStepRequest]{
		{"a step", wf + "/steps/" + b62(300), ``, &entity.DeleteWorkflowStepRequest{ID: 300, WorkflowID: 5}},
		{"workflow 0", "/workflows/0/steps/" + b62(300), ``, nil},
		{"step 0", wf + "/steps/0", ``, nil},
	})
}

// Only what is sent changes: a layout-only PATCH must leave the name alone.
func TestFromHTTPRequestToUpdateWorkflowRequestEntity(t *testing.T) {
	wf := "/workflows/" + b62(5)
	checkRequest(t, "/workflows/:id", FromHTTPRequestToUpdateWorkflowRequestEntity, []requestCase[entity.UpdateWorkflowRequest]{
		{"every field", wf, `{"name":"r","description":"d","startEventId":"` + b62(10) + `","layout":{"nodes":[1]}}`,
			&entity.UpdateWorkflowRequest{ID: 5, Name: ptr("r"), Description: ptr("d"), StartEventID: ptr(int64(10)), Layout: ptr(`{"nodes":[1]}`)}},
		{"the layout alone", wf, `{"layout":{"x":1}}`, &entity.UpdateWorkflowRequest{ID: 5, Layout: ptr(`{"x":1}`)}},
		{"a cleared start event", wf, `{"startEventId":""}`, &entity.UpdateWorkflowRequest{ID: 5, StartEventID: ptr(int64(0))}},
		{"nothing", wf, `{}`, &entity.UpdateWorkflowRequest{ID: 5}},
		{"workflow 0", "/workflows/0", `{}`, nil},
		{"not JSON", wf, `{`, nil},
	})
}

func TestFromHTTPRequestToCreateWorkflowStepRequestEntity(t *testing.T) {
	steps := "/workflows/" + b62(5) + "/steps"
	ids := `"eventId":"` + b62(10) + `","workspaceId":"` + b62(20) + `"`
	checkRequest(t, "/workflows/:id/steps", FromHTTPRequestToCreateWorkflowStepRequestEntity, []requestCase[entity.CreateWorkflowStepRequest]{
		{"every field", steps, `{` + ids + `,"emitEventId":"` + b62(11) + `","title":"Ship","body":"b","assignee":"human","allowAllCommands":true}`,
			&entity.CreateWorkflowStepRequest{WorkflowID: 5, EventID: 10, WorkspaceID: 20, EmitEventID: 11, Title: "Ship", Body: "b", Assignee: "human", AllowAllCommands: true}},
		{"the agent is the default assignee", steps, `{` + ids + `,"title":"Ship"}`,
			&entity.CreateWorkflowStepRequest{WorkflowID: 5, EventID: 10, WorkspaceID: 20, Title: "Ship", Assignee: "agent"}},
		{"no title", steps, `{` + ids + `}`, nil},
		{"no event", steps, `{"workspaceId":"` + b62(20) + `","title":"Ship"}`, nil},
		{"no workspace", steps, `{"eventId":"` + b62(10) + `","title":"Ship"}`, nil},
		{"workflow 0", "/workflows/0/steps", `{` + ids + `,"title":"Ship"}`, nil},
		{"not JSON", steps, `{`, nil},
	})
}

func TestWorkflowResponses(t *testing.T) {
	wf := entity.Workflow{ID: 5, CreatedAt: at, UpdatedAt: at.Add(1), UserID: 9, Name: "release", Description: "d", StartEventID: 10, Layout: `{"x":1}`}
	want := view.Workflow{ID: b62(5), CreatedAt: at, UpdatedAt: at.Add(1), Name: "release", Description: "d", StartEventID: b62(10), Layout: json.RawMessage(`{"x":1}`)}
	bare := entity.Workflow{ID: 6, CreatedAt: at, UpdatedAt: at, Name: "empty"}
	wantBare := view.Workflow{ID: b62(6), CreatedAt: at, UpdatedAt: at, Name: "empty"}

	checkResponse(t, FromCreateWorkflowResponseEntityToHTTPResponse(&entity.CreateWorkflowResponse{Workflow: wf}), view.CreateWorkflowResponse{Workflow: want})
	checkResponse(t, FromGetWorkflowResponseEntityToHTTPResponse(&entity.GetWorkflowResponse{Workflow: wf}), view.GetWorkflowResponse{Workflow: want})
	checkResponse(t, FromUpdateWorkflowResponseEntityToHTTPResponse(&entity.UpdateWorkflowResponse{Workflow: bare}), view.UpdateWorkflowResponse{Workflow: wantBare})
	checkResponse(t, FromListWorkflowsResponseEntityToHTTPResponse(&entity.ListWorkflowsResponse{Workflows: []entity.Workflow{wf, bare}}),
		view.ListWorkflowsResponse{Workflows: []view.Workflow{want, wantBare}})
	if got := string(FromListWorkflowsResponseEntityToHTTPResponse(&entity.ListWorkflowsResponse{})); got != `{"workflows":[]}` {
		t.Errorf("no workflows = %s", got)
	}
	checkResponse(t, FromReplaceWorkflowFromTextResponseEntityToHTTPResponse(&entity.ReplaceWorkflowFromTextResponse{Workflow: wf, StepCount: 3}),
		view.ReplaceWorkflowFromTextResponse{Workflow: want, StepCount: 3})

	if got := string(FromGetWorkflowTextResponseEntityToHTTPResponse(&entity.GetWorkflowTextResponse{Text: "on a"})); got != `{"text":"on a"}` {
		t.Errorf("text = %s", got)
	}
	for _, tc := range []struct {
		line int
		want string
	}{
		{4, `{"error":{"message":"unknown event","line":4}}`},
		// A failure that is about no one line names none.
		{0, `{"error":{"message":"unknown event"}}`},
	} {
		if got := string(FromWorkflowTextErrorToHTTPResponse("unknown event", tc.line)); got != tc.want {
			t.Errorf("line %d: got %s, want %s", tc.line, got, tc.want)
		}
	}
}

func TestWorkflowStepAndTaskResponses(t *testing.T) {
	step := entity.WorkflowStep{ID: 300, CreatedAt: at, UpdatedAt: at.Add(1), WorkflowID: 5, UserID: 9, EventID: 10, WorkspaceID: 20,
		EmitEventID: 11, Title: "Ship", Body: "b", Assignee: "agent", AllowAllCommands: true}
	checkResponse(t, FromCreateWorkflowStepResponseEntityToHTTPResponse(&entity.CreateWorkflowStepResponse{WorkflowStep: step}),
		view.CreateWorkflowStepResponse{WorkflowStep: view.WorkflowStep{ID: b62(300), CreatedAt: at, WorkflowID: b62(5), EventID: b62(10),
			WorkspaceID: b62(20), EmitEventID: b62(11), Title: "Ship", Body: "b", Assignee: "agent", AllowAllCommands: true}})

	checkResponse(t, FromListTasksFromWorkflowResponseEntityToHTTPResponse(&entity.ListTasksFromWorkflowResponse{
		Tasks: []entity.Task{{ID: 1, WorkspaceID: 20, WorkflowID: 5, EventID: 10, Title: "Ship", CompletionTriggerType: 2}},
	}), view.ListTasksResponse{Tasks: []view.Task{{ID: b62(1), WorkspaceID: b62(20), WorkflowID: b62(5), EventID: b62(10), Title: "Ship", CompletionTriggerType: 2}}})
	if got := string(FromListTasksFromWorkflowResponseEntityToHTTPResponse(&entity.ListTasksFromWorkflowResponse{})); got != `{"tasks":[]}` {
		t.Errorf("no tasks = %s", got)
	}
}
