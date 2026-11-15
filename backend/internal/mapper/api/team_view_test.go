// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strings"
	"testing"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

// links is the url of every file of t, its own then its messages'.
func links(t view.Task) string {
	var out []string
	for _, a := range t.Attachments {
		out = append(out, a.URL)
	}
	for _, m := range t.Messages {
		for _, a := range m.Attachments {
			out = append(out, a.URL)
		}
	}
	return strings.Join(out, " ")
}

// In team mode every file of a task links to the team's route, never to its
// public link, and the task it was made from keeps its own links.
func TestTeamTaskFilesLinkThroughTheTeam(t *testing.T) {
	task := view.Task{ID: "T",
		Attachments: []view.Attachment{{ID: "a", URL: "/storage/artifacts/a"}, {ID: "b"}},
		Messages: []view.Message{
			{ID: "m", Attachments: []view.Attachment{{ID: "c", URL: "/storage/artifacts/c"}}},
			{ID: "n"},
		},
	}
	before := links(task)
	base := "/api/v1/teams/" + id62(10) + "/tasks/T/attachments/"
	want := base + "a " + base + "b " + base + "c"

	if got := links(FromTaskViewToTeamTaskView(task, entity.TeamWorkspace{}, 10).Task); got != want {
		t.Errorf("the team's task links to %q, want %q", got, want)
	}
	if got := links(task); got != before {
		t.Errorf("the task it was made from now links to %q, want %q", got, before)
	}

	var detail struct {
		Task view.Task `json:"task"`
	}
	if err := json.Unmarshal(FromTeamTaskDetailEntityToHTTPResponse(entity.TeamTaskDetail{Detail: entity.GetTaskResponse{Task: entity.Task{ID: 1,
		Attachments: []entity.Attachment{{ID: "a", URL: "/storage/artifacts/a"}}}}}, 10), &detail); err != nil {
		t.Fatal(err)
	}
	if got, want := links(detail.Task), "/api/v1/teams/"+id62(10)+"/tasks/"+id62(1)+"/attachments/a"; got != want {
		t.Errorf("the task's page links to %q, want %q", got, want)
	}

	var list view.ListTeamTasksResponse
	if err := json.Unmarshal(FromTeamTaskEntitiesToHTTPResponse([]entity.TeamTask{{Task: entity.Task{ID: 1,
		Attachments: []entity.Attachment{{ID: "a"}}}}}, 10), &list); err != nil || len(list.Tasks) != 1 {
		t.Fatalf("the list: %v %+v", err, list)
	}
	if got, want := links(list.Tasks[0].Task), "/api/v1/teams/"+id62(10)+"/tasks/"+id62(1)+"/attachments/a"; got != want {
		t.Errorf("the list links to %q, want %q", got, want)
	}
}

// A team's workspace, task and task page carry the shared name the team
// workspace is matched by, and never a group.
func TestTeamWorkspaceCarriesItsSharedName(t *testing.T) {
	w := entity.TeamWorkspace{ID: 2, Name: "api fork", ForkOfID: 1, SharedName: "api", OwnerID: 3, OwnerName: "Bob", OwnerPicture: "b"}
	b := string(FromTeamWorkspaceEntitiesToHTTPResponse([]entity.TeamWorkspace{w, {ID: 1, Name: "api", SharedName: "api"}}))
	if want := `{"workspaces":[{"id":"00000000002","name":"api fork","sharedName":"api","owner":{"userId":"00000000003","name":"Bob","picture":"b"},"forkOfId":"00000000001"},` +
		`{"id":"00000000001","name":"api","sharedName":"api","owner":{"userId":"00000000000","name":"","picture":""}}]}`; b != want {
		t.Errorf("the workspaces render\n %s\nwant\n %s", b, want)
	}
	if got := FromTaskViewToTeamTaskView(view.Task{ID: "T"}, w, 10); got.SharedName != "api" || got.Owner.Name != "Bob" {
		t.Errorf("the task is %+v", got)
	}
	var detail view.GetTeamTaskResponse
	if err := json.Unmarshal(FromTeamTaskDetailEntityToHTTPResponse(entity.TeamTaskDetail{Workspace: w}, 10), &detail); err != nil ||
		detail.Workspace.SharedName != "api" || detail.Owner.Name != "Bob" {
		t.Errorf("the task page is %+v (%v)", detail, err)
	}
}
