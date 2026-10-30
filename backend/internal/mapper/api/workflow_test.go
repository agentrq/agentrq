// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package api

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/mustafaturan/monoflake"
)

// A stored layout that is not JSON must be dropped, not echoed: it lands in a
// json.RawMessage, and a malformed one would fail the whole response.
func TestFromGetWorkflowResponseEntityToHTTPResponse_Layout(t *testing.T) {
	for _, tc := range []struct {
		name, layout string
		wantLayout   bool
	}{
		{"valid layout is echoed verbatim", `{"nodes":[1]}`, true},
		{"malformed layout is dropped", `{"nodes":`, false},
		{"empty layout is omitted", ``, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := FromGetWorkflowResponseEntityToHTTPResponse(&entity.GetWorkflowResponse{
				Workflow: entity.Workflow{ID: 5, Name: "release", Layout: tc.layout},
			})
			if len(raw) == 0 || !json.Valid(raw) {
				t.Fatalf("response is not valid JSON: %q", raw)
			}
			has := strings.Contains(string(raw), `"layout":`)
			if has != tc.wantLayout {
				t.Errorf("layout present = %v, want %v: %s", has, tc.wantLayout, raw)
			}
			if tc.wantLayout && !strings.Contains(string(raw), `"layout":`+tc.layout) {
				t.Errorf("layout not echoed verbatim: %s", raw)
			}
			// No start event: the key is omitted rather than sent as "0".
			if strings.Contains(string(raw), "startEventId") {
				t.Errorf("unset start event should be omitted: %s", raw)
			}
		})
	}
}

func TestFromModelWorkflowToEntity(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := FromModelWorkflowToEntity(model.Workflow{
		ID: 5, CreatedAt: at, UpdatedAt: at, UserID: 9, Name: "release",
		Description: "d", StartEventID: 10, Layout: []byte(`{"x":1}`),
	})
	want := entity.Workflow{
		ID: 5, CreatedAt: at, UpdatedAt: at, UserID: 9, Name: "release",
		Description: "d", StartEventID: 10, Layout: `{"x":1}`,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

func TestFromModelWorkflowStepToEntity(t *testing.T) {
	at := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	got := FromModelWorkflowStepToEntity(model.WorkflowStep{
		ID: 300, CreatedAt: at, UpdatedAt: at, WorkflowID: 5, UserID: 9, EventID: 10,
		WorkspaceID: 20, EmitEventID: 11, Title: "t", Body: "b", Assignee: "agent", AllowAllCommands: true,
	})
	want := entity.WorkflowStep{
		ID: 300, CreatedAt: at, UpdatedAt: at, WorkflowID: 5, UserID: 9, EventID: 10,
		WorkspaceID: 20, EmitEventID: 11, Title: "t", Body: "b", Assignee: "agent", AllowAllCommands: true,
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// A leaf step has no emit event; the key is omitted rather than sent as "0".
func TestFromListWorkflowStepsResponseEntityToHTTPResponse_LeafStep(t *testing.T) {
	raw := string(FromListWorkflowStepsResponseEntityToHTTPResponse(&entity.ListWorkflowStepsResponse{
		WorkflowSteps: []entity.WorkflowStep{
			{ID: 300, WorkflowID: 5, EventID: 10, WorkspaceID: 20},
			{ID: 301, WorkflowID: 5, EventID: 10, WorkspaceID: 20, EmitEventID: 11},
		},
	}))
	if n := strings.Count(raw, `"emitEventId"`); n != 1 {
		t.Errorf("expected exactly one emitEventId, got %d: %s", n, raw)
	}
	if !strings.Contains(raw, `"emitEventId":"`+monoflake.ID(11).String()+`"`) {
		t.Errorf("emit event not encoded: %s", raw)
	}
}

func TestFromWorkflowTextErrorToHTTPResponse(t *testing.T) {
	if got := string(FromWorkflowTextErrorToHTTPResponse("bad indent", 4)); got != `{"error":{"message":"bad indent","line":4}}` {
		t.Errorf("got %s", got)
	}
	// A document-level error has no line, and none is sent.
	if got := string(FromWorkflowTextErrorToHTTPResponse("missing workflow", 0)); got != `{"error":{"message":"missing workflow"}}` {
		t.Errorf("got %s", got)
	}
}
