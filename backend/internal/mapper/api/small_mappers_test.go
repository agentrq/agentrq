// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	view "github.com/agentrq/agentrq/backend/internal/data/view/api"
)

func TestFromHTTPRequestToRecordTelemetryRequestEntity(t *testing.T) {
	checkRequest(t, "/telemetry", FromHTTPRequestToRecordTelemetryRequestEntity, []requestCase[entity.RecordTelemetryRequest]{
		{"an allowed action", "/telemetry", `{"action":"ui_search","workspaceId":"` + b62(20) + `"}`,
			&entity.RecordTelemetryRequest{Action: entity.ActionUISearch, WorkspaceID: 20}},
		// Only the allowlist reaches the stack: a server-side action is refused.
		{"an action the client may not report", "/telemetry", `{"action":"task_create","workspaceId":"` + b62(20) + `"}`, nil},
		{"no workspace", "/telemetry", `{"action":"ui_search"}`, nil},
		{"not JSON", "/telemetry", `{`, nil},
	})
}

func TestFromHTTPRequestToEnrolMachineRequestEntity(t *testing.T) {
	checkRequest(t, "/machines/enrol", FromHTTPRequestToEnrolMachineRequestEntity, []requestCase[entity.EnrolMachineRequest]{
		{"every field", "/machines/enrol", `{"code":"ABCD","name":"laptop","hostname":"h","os":"linux","arch":"arm64","version":"0.9.3"}`,
			&entity.EnrolMachineRequest{Code: "ABCD", Name: "laptop", Hostname: "h", OS: "linux", Arch: "arm64", Version: "0.9.3"}},
		// An account in the body is ignored: the code decides whose machine it is.
		{"no account is read", "/machines/enrol", `{"code":"ABCD","userId":"someone"}`, &entity.EnrolMachineRequest{Code: "ABCD"}},
		{"not JSON", "/machines/enrol", `{`, nil},
	})
}

func TestFromBrowserTicketToHTTPResponse(t *testing.T) {
	if got := string(FromBrowserTicketToHTTPResponse("tk", 30)); got != `{"ticket":"tk","expiresIn":30}` {
		t.Errorf("got %s", got)
	}
}

func TestFromAcpRegistryAgentsEntityToHTTPResponse(t *testing.T) {
	checkResponse(t, FromAcpRegistryAgentsEntityToHTTPResponse([]entity.AcpRegistryAgent{
		{ID: "gemini", Name: "Gemini CLI", Version: "1.2.3", Description: "d", Runtimes: []string{"binary", "npx"}},
		{ID: "bare", Name: "Bare"},
	}), view.AcpRegistryAgents{Agents: []view.AcpRegistryAgent{
		{ID: "gemini", Name: "Gemini CLI", Version: "1.2.3", Description: "d", Runtimes: []string{"binary", "npx"}},
		{ID: "bare", Name: "Bare"},
	}})
	if got := string(FromAcpRegistryAgentsEntityToHTTPResponse(nil)); got != `{"agents":[]}` {
		t.Errorf("no agents = %s, want an empty list", got)
	}
}

func TestFromHTTPRequestToMemoryRequestEntities(t *testing.T) {
	ws := "/workspaces/" + b62(20)
	checkRequest(t, "/workspaces/:id/memories", FromHTTPRequestToListMemoriesRequestEntity, []requestCase[entity.ListMemoriesRequest]{
		{"a workspace", ws + "/memories", ``, &entity.ListMemoriesRequest{WorkspaceID: 20}},
		{"workspace 0", "/workspaces/0/memories", ``, nil},
	})
	checkRequest(t, "/workspaces/:id/memories/:name", FromHTTPRequestToGetMemoryRequestEntity, []requestCase[entity.GetMemoryRequest]{
		{"a name is decoded", ws + "/memories/release%20notes.md", ``, &entity.GetMemoryRequest{WorkspaceID: 20, Name: "release notes.md"}},
		{"workspace 0", "/workspaces/0/memories/x.md", ``, nil},
		{"a broken escape", ws + "/memories/%zz", ``, nil},
	})
}

func TestMemoryResponses(t *testing.T) {
	m := entity.Memory{ID: 5, CreatedAt: at, UpdatedAt: at.Add(1), WorkspaceID: 20, Name: "memory.md", Content: "# Index", SizeBytes: 7}
	want := view.Memory{ID: b62(5), CreatedAt: at, UpdatedAt: at.Add(1), WorkspaceID: b62(20), Name: "memory.md", SizeBytes: 7, Content: "# Index"}
	checkResponse(t, FromGetMemoryResponseEntityToHTTPResponse(&entity.GetMemoryResponse{Memory: m}), view.GetMemoryResponse{Memory: want})

	// The list carries no content when the controller left it out.
	listed := m
	listed.Content = ""
	wantListed := want
	wantListed.Content = ""
	raw := FromListMemoriesResponseEntityToHTTPResponse(&entity.ListMemoriesResponse{Memories: []entity.Memory{listed}})
	checkResponse(t, raw, view.ListMemoriesResponse{Memories: []view.Memory{wantListed}})
	if got := decodeResponse[map[string][]map[string]any](t, raw)["memories"][0]; got["content"] != nil {
		t.Errorf("a listed memory carries content %v", got["content"])
	}
	if got := string(FromListMemoriesResponseEntityToHTTPResponse(&entity.ListMemoriesResponse{})); got != `{"memories":[]}` {
		t.Errorf("no memories = %s, want an empty list", got)
	}
}

// A preview carries only what a row shows, with every id in base62, and a
// workspace with no tasks still answers an empty list rather than null.
func TestFromGetTaskOverviewResponseEntityToHTTPResponse(t *testing.T) {
	at := time.Date(2026, 10, 31, 1, 0, 0, 0, time.UTC)
	got := string(FromGetTaskOverviewResponseEntityToHTTPResponse(&entity.GetTaskOverviewResponse{Workspaces: []entity.WorkspaceTaskOverview{
		{WorkspaceID: 20, Counts: map[string]int64{"ongoing": 1}, Tasks: []entity.TaskPreview{
			{ID: 30, WorkspaceID: 20, Title: "t", Status: "ongoing", Assignee: "agent", CreatedAt: at, UpdatedAt: at},
		}},
		{WorkspaceID: 21, Counts: map[string]int64{"completed": 2}},
	}}))
	want := `{"workspaces":[{"workspaceId":"` + b62(20) + `","counts":{"ongoing":1},"tasks":[{"id":"` + b62(30) + `","workspaceId":"` + b62(20) +
		`","title":"t","status":"ongoing","assignee":"agent","createdAt":"2026-10-31T01:00:00Z","updatedAt":"2026-10-31T01:00:00Z"}]},` +
		`{"workspaceId":"` + b62(21) + `","counts":{"completed":2},"tasks":[]}]}`
	if got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
