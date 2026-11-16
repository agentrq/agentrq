// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package telemetry

import (
	"testing"

	"github.com/agentrq/agentrq/backend/internal/controller/mcp"
	entity "github.com/agentrq/agentrq/backend/internal/data/entity/crud"
	"github.com/agentrq/agentrq/backend/internal/data/model"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func promController(t *testing.T) (*controller, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	return New(Params{Registerer: reg}).(*controller), reg
}

func countOf(c *controller, action, subAction string) float64 {
	return testutil.ToFloat64(c.actions.WithLabelValues(action, subAction))
}

// Without a registerer nothing is counted, and recording still queues.
func TestPrometheus_OffCountsNothing(t *testing.T) {
	c := New(Params{}).(*controller)
	if c.actions != nil {
		t.Fatal("actions counter exists with prometheus off")
	}
	c.recordCRUD(entity.CRUDEvent{Action: entity.ActionTaskCreate})
	if got := len(queued(c)); got != 1 {
		t.Fatalf("queued %d rows, want 1", got)
	}
}

// Every CRUD action that is stored is counted under its real name, never
// "unknown", so a new action left out of Action.String() fails here.
func TestPrometheus_CRUDActionsUseTheirNames(t *testing.T) {
	c, reg := promController(t)
	for a := 0; a < 256; a++ {
		action := entity.Action(a)
		before := len(c.queue)
		c.recordCRUD(entity.CRUDEvent{Action: action})
		rows := len(c.queue) - before
		if rows == 0 {
			continue
		}
		if action.String() == "unknown" {
			t.Errorf("action %d is stored but has no name", a)
		}
		if got := countOf(c, action.String(), ""); got != 1 {
			t.Errorf("%s counted %v, want 1", action, got)
		}
	}
	// A scheduled task is also a task create.
	if got := countOf(c, "task_create", ""); got != 2 {
		t.Errorf("task_create counted %v, want 2 (its own and the scheduled one's)", got)
	}
	if got := countOf(c, "unknown", ""); got != 0 {
		t.Errorf("unknown counted %v", got)
	}
	if n, err := testutil.GatherAndCount(reg, "agentrq_telemetry_actions_total"); err != nil || n == 0 {
		t.Fatalf("registry gathered %d series, err %v", n, err)
	}
}

// A team skill an agent changed is not stored, so it is not counted either.
func TestPrometheus_SkippedCRUDIsNotCounted(t *testing.T) {
	c, _ := promController(t)
	c.recordCRUD(entity.CRUDEvent{Action: entity.ActionTeamSkillAdd, ResourceType: entity.ResourceTeamSkill, Origin: entity.OriginMCP})
	if got := testutil.CollectAndCount(c.actions); got != 0 {
		t.Fatalf("counted %d series, want 0", got)
	}
}

func TestPrometheus_MCPActionsUseTheirNames(t *testing.T) {
	c, _ := promController(t)
	for _, tc := range []struct {
		event     mcp.MCPEvent
		action    string
		subAction string
	}{
		{mcp.MCPEvent{Action: mcp.ActionMCPToolCall, ToolName: "getTask"}, "mcp_tool_call", "getTask"},
		{mcp.MCPEvent{Action: mcp.ActionMCPToolCall, ToolName: "noSuchTool"}, "mcp_tool_call", "unknown"},
		{mcp.MCPEvent{Action: mcp.ActionMCPMethodCall, ToolName: "resource:task"}, "mcp_method_call", "resource:task"},
		{mcp.MCPEvent{Action: mcp.ActionMCPMethodCall, ToolName: "resource:made-up"}, "mcp_method_call", "unknown"},
		{mcp.MCPEvent{Action: mcp.ActionMCPConnect}, "mcp_connect", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPClearContext}, "mcp_clear_context", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPPlanUpdate}, "mcp_plan_update", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_manual_allow"}, "mcp_permission_manual", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_auto_allow"}, "mcp_permission_auto", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_manual_deny"}, "mcp_permission_deny", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_extension_allow"}, "mcp_permission_extension_allow", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_extension_deny"}, "mcp_permission_extension_deny", ""},
	} {
		c.recordMCP(tc.event)
		if got := countOf(c, tc.action, tc.subAction); got != 1 {
			t.Errorf("%s/%s counted %v, want 1", tc.action, tc.subAction, got)
		}
	}
	// Events that store nothing count nothing.
	c.recordMCP(mcp.MCPEvent{Action: mcp.ActionMCPNotification, Method: "permission_request"})
	c.recordMCP(mcp.MCPEvent{Action: mcp.Action(255)})
	if got := testutil.CollectAndCount(c.actions); got != 12 {
		t.Fatalf("counted %d series, want 12", got)
	}
}

// Only MCP actions have an MCP name; an empty tool is no tool at all.
func TestPrometheus_StringersFallBack(t *testing.T) {
	if got := mcpAction(model.ActionIDTaskCreate).String(); got != "unknown" {
		t.Errorf("mcpAction(task create) = %q, want unknown", got)
	}
	if got := mcpTool("").String(); got != "" {
		t.Errorf("mcpTool(\"\") = %q, want empty", got)
	}
}
