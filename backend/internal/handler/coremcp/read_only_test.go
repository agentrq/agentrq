// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// What a read-only connection may run, tool by tool. Every other action
// writes. A new action fails this test until it is put on one side or the
// other here, so a write never reaches a read-only connection by default.
var readActions = map[string][]string{
	"workspace": {"list", "get", "stats"},
	"task":      {"list", "listAll", "get", "messages", "changes", "getAttachment"},
	"memory":    {"list", "get"},
	"skills":    {"search", "get"},
	"events":    {"list", "get", "listTasks", "listTriggers", "getTrigger"},
	"workflows": {"list", "get", "getText", "listSteps", "listTasks"},
	"machines":  {"list"},
	"pano":      {"read"},
}

var writeActions = map[string][]string{
	"workspace": {"create", "update", "fork", "mergeFork"},
	"task":      {"create", "reply", "respond", "updateStatus", "updateSortingOrder", "updateAssignee", "updateAllowAll", "updateSchedule", "delete"},
	"memory":    {},
	"skills":    {},
	"events":    {"create", "update", "delete", "createTrigger", "updateTrigger", "deleteTrigger"},
	"workflows": {"create", "update", "delete", "replaceFromText", "createStep", "deleteStep"},
	"machines":  {"createEnrolmentCode", "launchAgent", "stopAgent"},
	"pano":      {"save", "edit", "comment", "attach", "delete"},
}

func TestReadOnly_EveryActionIsClassified(t *testing.T) {
	s := &WorkspaceServer{}
	for _, tool := range s.tools() {
		var reads, writes []string
		for _, a := range tool.Actions {
			if a.Reads {
				reads = append(reads, a.Name)
			} else {
				writes = append(writes, a.Name)
			}
		}
		if !slices.Equal(reads, readActions[tool.Name]) {
			t.Errorf("%s reads %v, want %v", tool.Name, reads, readActions[tool.Name])
		}
		if !slices.Equal(writes, writeActions[tool.Name]) {
			t.Errorf("%s writes %v, want %v: put a new action in readActions or writeActions", tool.Name, writes, writeActions[tool.Name])
		}
	}
}

// Each write, called the way its tool calls it, is refused on a read-only
// connection before its handler runs. The fake answers nothing, so a handler
// that ran would panic.
func TestReadOnly_RefusesEveryWrite(t *testing.T) {
	s := &WorkspaceServer{crud: &mockPanoCrud{workspaces: []entity.Workspace{{ID: testWorkspace, Name: "supervisor"}}}}
	ctx := mcpevent.WithReadOnly(authedContext())
	call := map[string]func(action string, payload map[string]any) (*mcp.CallToolResult, any, error){
		"workspace": func(a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return s.handleWorkspaceTool(ctx, nil, WorkspaceToolParams{Action: a, Payload: p})
		},
		"task": func(a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return s.handleTask(ctx, nil, TaskParams{Action: a, Payload: p})
		},
		"events": func(a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return s.handleEventsTool(ctx, nil, EventsToolParams{Action: a, Payload: p})
		},
		"workflows": func(a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return s.handleWorkflowsTool(ctx, nil, WorkflowsToolParams{Action: a, Payload: p})
		},
		"machines": func(a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return s.handleMachinesTool(ctx, nil, MachinesToolParams{Action: a, Payload: p})
		},
		"pano": func(a string, p map[string]any) (*mcp.CallToolResult, any, error) {
			return s.handlePano(ctx, nil, mcpevent.PanoParams{Action: a, Payload: p})
		},
	}
	for _, tool := range s.tools() {
		for _, a := range tool.Actions {
			if a.Reads {
				continue
			}
			t.Run(tool.Name+"."+a.Name, func(t *testing.T) {
				var payload map[string]any
				if a.Example != "" {
					if err := json.Unmarshal([]byte(a.Example), &payload); err != nil {
						t.Fatalf("example: %v", err)
					}
				}
				res := func() (r callResult) {
					defer func() {
						if p := recover(); p != nil {
							r = callResult{text: fmt.Sprint("the handler ran: ", p)}
						}
					}()
					return toolResult(call[tool.Name](a.Name, payload))
				}()
				if !res.isError || !strings.Contains(res.text, "read-only") {
					t.Errorf("not refused: %s", res.text)
				}
			})
		}
	}
}
