// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"reflect"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
	"gorm.io/datatypes"
)

const taskRoute = "/workspaces/:id/tasks/:taskID"

var (
	taskPath   = "/workspaces/" + b62(20) + "/tasks/" + b62(101)
	noWsPath   = "/workspaces/0/tasks/" + b62(101)
	noTaskPath = "/workspaces/" + b62(20) + "/tasks/0"
)

func TestFromHTTPRequestToCreateTaskRequestEntity(t *testing.T) {
	tasks := "/workspaces/" + b62(20) + "/tasks"
	checkRequest(t, "/workspaces/:id/tasks", FromHTTPRequestToCreateTaskRequestEntity, []requestCase[entity.CreateTaskRequest]{
		{"every field", tasks,
			`{"task":{"createdBy":"human","assignee":"agent","title":"Ship","body":"b","status":"cron","cronSchedule":"@daily","sortOrder":1.5,` +
				`"allowAllCommands":true,"clearContext":true,"eventId":"` + b62(10) + `","workflowId":"` + b62(5) + `",` +
				`"attachments":[{"id":"ignored","filename":"a.png","mimeType":"image/png","data":"AA=="}]},"panoDocId":"` + b62(40) + `"}`,
			&entity.CreateTaskRequest{Task: entity.Task{WorkspaceID: 20, CreatedBy: "human", Assignee: "agent", Title: "Ship", Body: "b",
				Status: "cron", CronSchedule: "@daily", SortOrder: 1.5, AllowAllCommands: true, ClearContext: true, EventID: 10, WorkflowID: 5,
				Attachments: []entity.Attachment{{Filename: "a.png", MimeType: "image/png", Data: "AA=="}}}, PanoDocID: 40}},
		// No title is fine: the controller calls the task Untitled.
		{"who made it, and nothing else", tasks, `{"task":{"createdBy":"human"}}`,
			&entity.CreateTaskRequest{Task: entity.Task{WorkspaceID: 20, CreatedBy: "human"}}},
		{"no creator", tasks, `{"task":{"title":"Ship"}}`, nil},
		{"workspace 0", "/workspaces/0/tasks", `{"task":{"createdBy":"human"}}`, nil},
		{"not JSON", tasks, `{`, nil},
	})
}

func TestFromHTTPRequestToTaskIDRequestEntities(t *testing.T) {
	checkRequest(t, taskRoute, FromHTTPRequestToGetTaskRequestEntity, []requestCase[entity.GetTaskRequest]{
		{"a task", taskPath, ``, &entity.GetTaskRequest{WorkspaceID: 20, TaskID: 101}},
		{"workspace 0", noWsPath, ``, nil},
		{"task 0", noTaskPath, ``, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToGetTaskPlanRequestEntity, []requestCase[entity.GetTaskPlanRequest]{
		{"a task", taskPath, ``, &entity.GetTaskPlanRequest{WorkspaceID: 20, TaskID: 101}},
		{"workspace 0", noWsPath, ``, nil},
		{"task 0", noTaskPath, ``, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToDeleteTaskRequestEntity, []requestCase[entity.DeleteTaskRequest]{
		{"a task", taskPath, ``, &entity.DeleteTaskRequest{WorkspaceID: 20, TaskID: 101}},
		{"workspace 0", noWsPath, ``, nil},
		{"task 0", noTaskPath, ``, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToListTaskToolCallsRequestEntity, []requestCase[entity.ListTaskToolCallsRequest]{
		{"the latest", taskPath, ``, &entity.ListTaskToolCallsRequest{WorkspaceID: 20, TaskID: 101}},
		{"a page before a call", taskPath + "?before=" + b62(7) + "&limit=25", ``,
			&entity.ListTaskToolCallsRequest{WorkspaceID: 20, TaskID: 101, Before: 7, Limit: 25}},
		{"before call 0", taskPath + "?before=0", ``, nil},
		{"workspace 0", noWsPath, ``, nil},
		{"task 0", noTaskPath, ``, nil},
	})
}

func TestFromHTTPRequestToListTasksRequestEntity(t *testing.T) {
	ws := "/workspaces/" + b62(20) + "/tasks"
	checkRequest(t, "/workspaces/:id/tasks", FromHTTPRequestToListTasksRequestEntity, []requestCase[entity.ListTasksRequest]{
		{"ten by default", ws, ``, &entity.ListTasksRequest{WorkspaceID: 20, Limit: 10, PreloadMessages: true}},
		{"every filter", ws + "?status=ongoing,blocked&limit=20&offset=40&filter=pending_approval&created_by=agent", ``,
			&entity.ListTasksRequest{WorkspaceID: 20, Status: []string{"ongoing", "blocked"}, Limit: 20, Offset: 40,
				Filter: "pending_approval", CreatedBy: "agent", PreloadMessages: true}},
		{"a zero limit is the default", ws + "?limit=0", ``, &entity.ListTasksRequest{WorkspaceID: 20, Limit: 10, PreloadMessages: true}},
		{"a limit that is not a number is the default", ws + "?limit=many", ``, &entity.ListTasksRequest{WorkspaceID: 20, Limit: 10, PreloadMessages: true}},
		{"at most fifty", ws + "?limit=500", ``, &entity.ListTasksRequest{WorkspaceID: 20, Limit: 50, PreloadMessages: true}},
		{"workspace 0", "/workspaces/0/tasks", ``, nil},
	})
	// Without a workspace in the route, it lists the account's.
	checkRequest(t, "/tasks", FromHTTPRequestToListTasksRequestEntity, []requestCase[entity.ListTasksRequest]{
		{"every workspace", "/tasks?status=cron", ``, &entity.ListTasksRequest{Status: []string{"cron"}, Limit: 10, PreloadMessages: true}},
	})
}

func TestFromHTTPRequestToRespondToTaskRequestEntity(t *testing.T) {
	checkRequest(t, taskRoute, FromHTTPRequestToRespondToTaskRequestEntity, []requestCase[entity.RespondToTaskRequest]{
		{"text with a file", taskPath, `{"response":{"action":"text","text":"Yes","attachments":[{"id":"a1","filename":"a.png","mimeType":"image/png","data":"AA=="}]}}`,
			&entity.RespondToTaskRequest{WorkspaceID: 20, TaskID: 101, Action: "text", Text: "Yes",
				Attachments: []entity.Attachment{{ID: "a1", Filename: "a.png", MimeType: "image/png", Data: "AA=="}}}},
		{"an allow", taskPath, `{"response":{"action":"allow"}}`, &entity.RespondToTaskRequest{WorkspaceID: 20, TaskID: 101, Action: "allow"}},
		{"no action", taskPath, `{"response":{"text":"Yes"}}`, nil},
		{"workspace 0", noWsPath, `{"response":{"action":"allow"}}`, nil},
		{"task 0", noTaskPath, `{"response":{"action":"allow"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
}

func TestFromHTTPRequestToReplyToTaskRequestEntity(t *testing.T) {
	checkRequest(t, taskRoute, FromHTTPRequestToReplyToTaskRequestEntity, []requestCase[entity.ReplyToTaskRequest]{
		{"text with a file", taskPath, `{"reply":{"text":"Done","attachments":[{"id":"a1","filename":"a.png","mimeType":"image/png","data":"AA=="}]}}`,
			&entity.ReplyToTaskRequest{WorkspaceID: 20, TaskID: 101, Text: "Done",
				Attachments: []entity.Attachment{{ID: "a1", Filename: "a.png", MimeType: "image/png", Data: "AA=="}}}},
		{"text alone", taskPath, `{"reply":{"text":"Done"}}`, &entity.ReplyToTaskRequest{WorkspaceID: 20, TaskID: 101, Text: "Done"}},
		{"no text", taskPath, `{"reply":{}}`, nil},
		{"workspace 0", noWsPath, `{"reply":{"text":"Done"}}`, nil},
		{"task 0", noTaskPath, `{"reply":{"text":"Done"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
}

func TestFromHTTPRequestToForkTaskRequestEntity(t *testing.T) {
	msg := `{"messageId":"` + b62(77) + `"}`
	checkRequest(t, taskRoute, FromHTTPRequestToForkTaskRequestEntity, []requestCase[entity.ForkTaskRequest]{
		{"up to a message", taskPath, msg, &entity.ForkTaskRequest{WorkspaceID: 20, TaskID: 101, MessageID: 77}},
		{"no message", taskPath, `{}`, nil},
		{"workspace 0", noWsPath, msg, nil},
		{"task 0", noTaskPath, msg, nil},
		{"not JSON", taskPath, `{`, nil},
	})
}

func TestFromHTTPRequestToUpdateTaskFieldRequestEntities(t *testing.T) {
	checkRequest(t, taskRoute, FromHTTPRequestToUpdateTaskStatusRequestEntity, []requestCase[entity.UpdateTaskStatusRequest]{
		{"a status", taskPath, `{"status":{"value":"completed"}}`, &entity.UpdateTaskStatusRequest{WorkspaceID: 20, TaskID: 101, Status: "completed"}},
		{"no status", taskPath, `{"status":{}}`, nil},
		{"workspace 0", noWsPath, `{"status":{"value":"completed"}}`, nil},
		{"task 0", noTaskPath, `{"status":{"value":"completed"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToUpdateTaskOrderRequestEntity, []requestCase[entity.UpdateTaskOrderRequest]{
		{"an order", taskPath, `{"order":{"value":2.25}}`, &entity.UpdateTaskOrderRequest{WorkspaceID: 20, TaskID: 101, SortOrder: 2.25}},
		{"order 0 is an order", taskPath, `{}`, &entity.UpdateTaskOrderRequest{WorkspaceID: 20, TaskID: 101}},
		{"workspace 0", noWsPath, `{}`, nil},
		{"task 0", noTaskPath, `{}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToUpdateTaskAssigneeRequestEntity, []requestCase[entity.UpdateTaskAssigneeRequest]{
		{"an assignee", taskPath, `{"assignee":{"value":"human"}}`, &entity.UpdateTaskAssigneeRequest{WorkspaceID: 20, TaskID: 101, Assignee: "human"}},
		{"no assignee", taskPath, `{"assignee":{}}`, nil},
		{"workspace 0", noWsPath, `{"assignee":{"value":"human"}}`, nil},
		{"task 0", noTaskPath, `{"assignee":{"value":"human"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToUpdateTaskTitleRequestEntity, []requestCase[entity.UpdateTaskTitleRequest]{
		{"a title, trimmed", taskPath, `{"title":{"value":"  Ship it \n"}}`, &entity.UpdateTaskTitleRequest{WorkspaceID: 20, TaskID: 101, Title: "Ship it"}},
		{"a blank title", taskPath, `{"title":{"value":"   "}}`, nil},
		{"workspace 0", noWsPath, `{"title":{"value":"Ship"}}`, nil},
		{"task 0", noTaskPath, `{"title":{"value":"Ship"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToMoveTaskRequestEntity, []requestCase[entity.MoveTaskRequest]{
		{"to a workspace", taskPath, `{"workspace":{"value":"` + b62(21) + `"}}`, &entity.MoveTaskRequest{WorkspaceID: 20, TaskID: 101, DestinationWorkspaceID: 21}},
		{"nowhere", taskPath, `{"workspace":{}}`, nil},
		{"workspace 0", noWsPath, `{"workspace":{"value":"` + b62(21) + `"}}`, nil},
		{"task 0", noTaskPath, `{"workspace":{"value":"` + b62(21) + `"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToUpdateTaskAllowAllCommandsRequestEntity, []requestCase[entity.UpdateTaskAllowAllCommandsRequest]{
		{"on", taskPath, `{"allowAll":{"value":true}}`, &entity.UpdateTaskAllowAllCommandsRequest{WorkspaceID: 20, TaskID: 101, AllowAllCommands: true}},
		{"off", taskPath, `{"allowAll":{"value":false}}`, &entity.UpdateTaskAllowAllCommandsRequest{WorkspaceID: 20, TaskID: 101}},
		{"workspace 0", noWsPath, `{}`, nil},
		{"task 0", noTaskPath, `{}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
	checkRequest(t, taskRoute, FromHTTPRequestToUpdateScheduledTaskRequestEntity, []requestCase[entity.UpdateScheduledTaskRequest]{
		{"every field", taskPath, `{"task":{"title":"Daily","body":"b","assignee":"agent","cronSchedule":"@daily","allowAllCommands":true,"clearContext":true}}`,
			&entity.UpdateScheduledTaskRequest{WorkspaceID: 20, TaskID: 101, Title: "Daily", Body: "b", Assignee: "agent", CronSchedule: "@daily",
				AllowAllCommands: true, ClearContext: true}},
		{"no title", taskPath, `{"task":{"cronSchedule":"@daily"}}`, nil},
		{"workspace 0", noWsPath, `{"task":{"title":"Daily"}}`, nil},
		{"task 0", noTaskPath, `{"task":{"title":"Daily"}}`, nil},
		{"not JSON", taskPath, `{`, nil},
	})
}

// fullTask has every field set, and fullTaskView is how the API writes it.
var (
	fullTask = entity.Task{ID: 101, CreatedAt: at, UpdatedAt: at.Add(1), UserID: 9, WorkspaceID: 20, CreatedBy: "human", Assignee: "agent",
		Status: "ongoing", Title: "Ship", Body: "b", Response: "allow", ReplyText: "ok",
		Attachments: []entity.Attachment{{ID: "a1", Filename: "a.png", MimeType: "image/png", URL: "https://cdn/a1"}},
		Messages: []entity.Message{{ID: 77, CreatedAt: at, TaskID: 101, UserID: 9, Sender: "agent", Text: "On it",
			Attachments: []entity.Attachment{{ID: "a2", Filename: "b.txt", MimeType: "text/plain", Data: "AA=="}},
			Metadata:    map[string]any{"kind": "permission"}}},
		ToolCalls:     []entity.ToolCall{{ID: 5, CreatedAt: at, TaskID: 101, WorkspaceID: 20, ToolName: "Bash", Description: "list", InputPreview: "ls", Status: "allowed"}},
		ToolCallCount: 3, CronSchedule: "@daily", ParentID: 100, SortOrder: 1.5, AllowAllCommands: true, ClearContext: true,
		EventID: 10, WorkflowID: 5, WorkflowDepth: 2, CompletionTriggerType: 2, CreatedByWorkspaceID: 30}
	fullTaskView = view.Task{ID: b62(101), CreatedAt: at, UpdatedAt: at.Add(1), WorkspaceID: b62(20), CreatedBy: "human", Assignee: "agent",
		Status: "ongoing", Title: "Ship", Body: "b", Response: "allow", ReplyText: "ok",
		Attachments: []view.Attachment{{ID: "a1", Filename: "a.png", MimeType: "image/png", URL: "https://cdn/a1"}},
		Messages: []view.Message{{ID: b62(77), CreatedAt: at, TaskID: b62(101), UserID: b62(9), Sender: "agent", Text: "On it",
			Attachments: []view.Attachment{{ID: "a2", Filename: "b.txt", MimeType: "text/plain", Data: "AA=="}},
			Metadata:    map[string]any{"kind": "permission"}}},
		ToolCalls:     []view.ToolCall{{ID: b62(5), CreatedAt: at, TaskID: b62(101), ToolName: "Bash", Description: "list", InputPreview: "ls", Status: "allowed"}},
		ToolCallCount: 3, CronSchedule: "@daily", ParentID: b62(100), SortOrder: 1.5, AllowAllCommands: true, ClearContext: true,
		EventID: b62(10), WorkflowID: b62(5), CompletionTriggerType: 2, CreatedByWorkspaceID: b62(30)}
	// bareTask has only an id; the view leaves out every optional field.
	bareTask     = entity.Task{ID: 102}
	bareTaskView = view.Task{ID: b62(102)}
)

func TestFromEntityTaskToView(t *testing.T) {
	if got := FromEntityTaskToView(fullTask); !reflect.DeepEqual(got, fullTaskView) {
		t.Errorf("got  %+v\nwant %+v", got, fullTaskView)
	}
	// No workspace is a blank one, not "00000000000"; the lists are empty, not nil.
	got := FromEntityTaskToView(bareTask)
	want := bareTaskView
	want.Attachments, want.Messages, want.ToolCalls = []view.Attachment{}, []view.Message{}, []view.ToolCall{}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}
}

func TestTaskResponses(t *testing.T) {
	for name, raw := range map[string][]byte{
		"create":    FromCreateTaskResponseEntityToHTTPResponse(&entity.CreateTaskResponse{Task: fullTask}),
		"fork":      FromForkTaskResponseEntityToHTTPResponse(&entity.ForkTaskResponse{Task: fullTask}),
		"respond":   FromRespondToTaskResponseEntityToHTTPResponse(&entity.RespondToTaskResponse{Task: fullTask}),
		"status":    FromUpdateTaskStatusResponseEntityToHTTPResponse(&entity.UpdateTaskStatusResponse{Task: fullTask}),
		"order":     FromUpdateTaskOrderResponseEntityToHTTPResponse(&entity.UpdateTaskOrderResponse{Task: fullTask}),
		"assignee":  FromUpdateTaskAssigneeResponseEntityToHTTPResponse(&entity.UpdateTaskAssigneeResponse{Task: fullTask}),
		"title":     FromUpdateTaskTitleResponseEntityToHTTPResponse(&entity.UpdateTaskTitleResponse{Task: fullTask}),
		"move":      FromMoveTaskResponseEntityToHTTPResponse(&entity.MoveTaskResponse{Task: fullTask}),
		"allowAll":  FromUpdateTaskAllowAllCommandsResponseEntityToHTTPResponse(&entity.UpdateTaskAllowAllCommandsResponse{Task: fullTask}),
		"reply":     FromReplyToTaskResponseEntityToHTTPResponse(&entity.ReplyToTaskResponse{Task: fullTask}),
		"scheduled": FromUpdateScheduledTaskResponseEntityToHTTPResponse(&entity.UpdateScheduledTaskResponse{Task: fullTask}),
	} {
		t.Run(name, func(t *testing.T) {
			checkResponse(t, raw, struct {
				Task view.Task `json:"task"`
			}{fullTaskView})
		})
	}

	checkResponse(t, FromListTasksResponseEntityToHTTPResponse(&entity.ListTasksResponse{Tasks: []entity.Task{fullTask, bareTask}}),
		view.ListTasksResponse{Tasks: []view.Task{fullTaskView, bareTaskView}})
	if got := string(FromListTasksResponseEntityToHTTPResponse(&entity.ListTasksResponse{})); got != `{"tasks":[]}` {
		t.Errorf("no tasks = %s", got)
	}
}

func TestFromGetTaskPlanResponseEntityToHTTPResponse(t *testing.T) {
	checkResponse(t, FromGetTaskPlanResponseEntityToHTTPResponse(&entity.GetTaskPlanResponse{
		Plan: entity.TaskPlan{ID: 101, Content: "- [ ] Read", Version: 2, CreatedAt: at, UpdatedAt: at.Add(1)},
	}), view.TaskPlan{ID: b62(101), Content: "- [ ] Read", Version: 2, CreatedAt: at, UpdatedAt: at.Add(1)})
}

func TestFromListTaskToolCallsResponseEntityToHTTPResponse(t *testing.T) {
	checkResponse(t, FromListTaskToolCallsResponseEntityToHTTPResponse(&entity.ListTaskToolCallsResponse{ToolCalls: fullTask.ToolCalls}),
		view.ListTaskToolCallsResponse{ToolCalls: fullTaskView.ToolCalls})
	// No calls is an empty list the client can append to.
	if got := string(FromListTaskToolCallsResponseEntityToHTTPResponse(&entity.ListTaskToolCallsResponse{})); got != `{"toolCalls":[]}` {
		t.Errorf("no calls = %s", got)
	}
}

func TestSupervisorTaskResponses(t *testing.T) {
	spoke := entity.TaskReplyState{AwaitingReply: true, LastMessageAt: at, LastMessageBy: "agent"}
	got := FromGetTaskResponseEntityToMCPResponse(&entity.GetTaskResponse{Task: fullTask}, spoke)
	checkResponse(t, got, view.SupervisorGetTaskResponse{
		GetTaskResponse: view.GetTaskResponse{StateTransitions: []view.TaskStateTransition{}},
		Task:            view.SupervisorTask{Task: fullTaskView, AwaitingReply: true, LastMessageAt: &at, LastMessageBy: "agent"},
	})

	// A task with no conversation names no time for its last message.
	raw := FromListTasksResponseEntityToMCPResponse(&entity.ListTasksResponse{Tasks: []entity.Task{fullTask, bareTask}},
		[]entity.TaskReplyState{spoke, {}})
	checkResponse(t, raw, view.SupervisorListTasksResponse{Tasks: []view.SupervisorTask{
		{Task: fullTaskView, AwaitingReply: true, LastMessageAt: &at, LastMessageBy: "agent"},
		{Task: bareTaskView},
	}})
	if got := decodeResponse[map[string][]map[string]any](t, raw)["tasks"][1]; got["lastMessageAt"] != nil {
		t.Errorf("a silent task carries lastMessageAt %v", got["lastMessageAt"])
	}
	if got := string(FromListTasksResponseEntityToMCPResponse(&entity.ListTasksResponse{}, nil)); got != `{"tasks":[]}` {
		t.Errorf("no tasks = %s", got)
	}
}

func TestTaskConversationMCPResponses(t *testing.T) {
	msgs := []entity.Message{
		{ID: 77, CreatedAt: at, TaskID: 101, Sender: "human", Text: "See these",
			Attachments: []entity.Attachment{{ID: "a1", Filename: "a.png"}, {ID: "a2", Filename: "b.txt"}}},
		{ID: 78, CreatedAt: at.Add(1), TaskID: 101, Sender: "agent", Text: "On it"},
	}
	// A message names its files and carries none of them.
	want := []view.TaskMessage{
		{ID: b62(77), CreatedAt: at, Sender: "human", Text: "See these", Attachments: []string{"a.png", "b.txt"}},
		{ID: b62(78), CreatedAt: at.Add(1), Sender: "agent", Text: "On it"},
	}

	checkResponse(t, FromTaskEntityToTaskMessagesMCPResponse(fullTask, msgs, 12),
		view.TaskMessagesResponse{Status: "ongoing", UpdatedAt: at.Add(1), Total: 12, Messages: want})
	if got := string(FromTaskEntityToTaskMessagesMCPResponse(entity.Task{Status: "notstarted", UpdatedAt: at}, nil, 0)); got !=
		`{"status":"notstarted","updatedAt":"2026-10-30T09:00:00Z","total":0,"messages":[]}` {
		t.Errorf("no messages = %s", got)
	}

	checkResponse(t, FromTaskEntityToTaskResourceMCPResponse(fullTask, msgs, true), view.TaskResource{
		ID: b62(101), WorkspaceID: b62(20), Title: "Ship", Status: "ongoing", UpdatedAt: at.Add(1), AwaitingReply: true, Messages: want,
	})
}

func TestFromModelTaskToView(t *testing.T) {
	m := model.Task{ID: 101, CreatedAt: at, UpdatedAt: at.Add(1), WorkspaceID: 20, CreatedBy: "human", Assignee: "agent",
		Status: "ongoing", Title: "Ship", Body: "b", Response: "allow", ReplyText: "ok",
		Attachments: []byte(`[{"id":"a1","filename":"a.png","mimeType":"image/png","data":"","url":"https://cdn/a1"}]`),
		Messages: []model.Message{
			{ID: 77, CreatedAt: at, TaskID: 101, UserID: 9, Sender: "agent", Text: "On it",
				Attachments: []byte(`[{"id":"a2","filename":"b.txt","mimeType":"text/plain","data":"AA=="}]`),
				Metadata:    datatypes.JSON(`{"kind":"permission"}`)},
			{ID: 78, CreatedAt: at, TaskID: 101, UserID: 9, Sender: "human", Text: "Thanks"},
		},
		ToolCalls:     []model.ToolCall{{ID: 5, CreatedAt: at, TaskID: 101, WorkspaceID: 20, ToolName: "Bash", Description: "list", InputPreview: "ls", Status: "allowed"}},
		ToolCallCount: 3, CronSchedule: "@daily", ParentID: 100, SortOrder: 1.5, AllowAllCommands: true, ClearContext: true,
		EventID: 10, WorkflowID: 5, CompletionTriggerType: 2, CreatedByWorkspaceID: 30}
	want := fullTaskView
	want.Messages = []view.Message{
		{ID: b62(77), CreatedAt: at, TaskID: b62(101), UserID: b62(9), Sender: "agent", Text: "On it",
			Attachments: []view.Attachment{{ID: "a2", Filename: "b.txt", MimeType: "text/plain", Data: "AA=="}},
			Metadata:    datatypes.JSON(`{"kind":"permission"}`)},
		// The stored metadata is passed on as it is, even when there is none.
		{ID: b62(78), CreatedAt: at, TaskID: b62(101), UserID: b62(9), Sender: "human", Text: "Thanks", Metadata: datatypes.JSON(nil)},
	}
	if got := FromModelTaskToView(m); !reflect.DeepEqual(got, want) {
		t.Errorf("got  %+v\nwant %+v", got, want)
	}

	// Stored attachments that are not JSON are left out rather than failing
	// the event; the lists are empty, not nil.
	got := FromModelTaskToView(model.Task{ID: 102, Attachments: []byte(`{`)})
	wantBare := view.Task{ID: b62(102), Messages: []view.Message{}, ToolCalls: []view.ToolCall{}}
	if !reflect.DeepEqual(got, wantBare) {
		t.Errorf("got  %+v\nwant %+v", got, wantBare)
	}
}
