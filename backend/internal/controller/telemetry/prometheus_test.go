// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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

// total sums every series of the actions counter.
func total(t *testing.T, reg *prometheus.Registry) float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var sum float64
	for _, f := range families {
		if f.GetName() != "agentrq_telemetry_actions_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			sum += m.GetCounter().GetValue()
		}
	}
	return sum
}

// series reads every action/sub_action series the registry holds, with its value.
func series(t *testing.T, reg *prometheus.Registry) map[[2]string]float64 {
	t.Helper()
	families, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	out := map[[2]string]float64{}
	for _, f := range families {
		if f.GetName() != "agentrq_telemetry_actions_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			var key [2]string
			for _, l := range m.GetLabel() {
				switch l.GetName() {
				case "action":
					key[0] = l.GetValue()
				case "sub_action":
					key[1] = l.GetValue()
				}
			}
			out[key] = m.GetCounter().GetValue()
		}
	}
	return out
}

// Every series exists at 0 before its first event, so increase() and rate()
// see that event rather than a series born at 1.
func TestPrometheus_SeriesStartAtZero(t *testing.T) {
	_, reg := promController(t)
	got := series(t, reg)
	for _, key := range [][2]string{
		{"team_create", ""},
		{"team_join", ""},
		{"user_signout", ""},
		{"pano_share", ""},
		{"team_skill_opt_in", ""},
		{"task_from_scheduled", ""},
		{"mcp_permission_deny", ""},
		{"mcp_connect", ""},
		{"agent_tool_call_fail", ""},
		{"mcp_tool_call", "getTask"},
		{"mcp_tool_call_error", "pano"},
		{"mcp_method_call", "resource:task"},
		{"mcp_method_call", "prompt:new-workspace"},
		{"mcp_method_call", "skills/list"},
	} {
		v, ok := got[key]
		if !ok {
			t.Errorf("%s/%q has no series", key[0], key[1])
		} else if v != 0 {
			t.Errorf("%s/%q starts at %v, want 0", key[0], key[1], v)
		}
	}
	// Tools are only tool calls, resources and prompts only method calls, and
	// a call by tool is never primed without one.
	for _, key := range [][2]string{
		{"mcp_tool_call", "resource:task"},
		{"mcp_tool_call_error", "prompt:new-workspace"},
		{"mcp_method_call", "getTask"},
		{"mcp_tool_call", ""},
		{"mcp_method_call", ""},
		{"unknown", ""},
		{"hook_receive", ""},
	} {
		if _, ok := got[key]; ok {
			t.Errorf("%s/%q is primed, want no series", key[0], key[1])
		}
	}
	// Every action recordCRUD counts, and every tool and method, is primed.
	for a := 0; a < 256; a++ {
		if _, ok := crudActionID(entity.Action(a)); ok {
			subs, ok := crudSubActions[entity.Action(a)]
			if !ok {
				subs = []string{""}
			}
			for _, sub := range subs {
				if _, ok := got[[2]string{entity.Action(a).String(), sub}]; !ok {
					t.Errorf("%s/%q has no series", entity.Action(a), sub)
				}
			}
		}
	}
	for name := range subActionIDByToolName {
		_, asTool := got[[2]string{"mcp_tool_call", name}]
		_, asMethod := got[[2]string{"mcp_method_call", name}]
		if asTool == asMethod {
			t.Errorf("%s primed as tool %v, as method %v; want exactly one", name, asTool, asMethod)
		}
	}
	if sum := total(t, reg); sum != 0 {
		t.Fatalf("primed series sum to %v, want 0", sum)
	}
}

// A team skill an agent changed is not stored, so it is not counted either.
func TestPrometheus_SkippedCRUDIsNotCounted(t *testing.T) {
	c, reg := promController(t)
	c.recordCRUD(entity.CRUDEvent{Action: entity.ActionTeamSkillAdd, ResourceType: entity.ResourceTeamSkill, Origin: entity.OriginMCP})
	if got := total(t, reg); got != 0 {
		t.Fatalf("counted %v, want 0", got)
	}
}

func TestPrometheus_MCPActionsUseTheirNames(t *testing.T) {
	c, reg := promController(t)
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
		{mcp.MCPEvent{Action: mcp.ActionMCPInitialize}, "mcp_initialize", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPReconnect}, "mcp_reconnect", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPClearContext}, "mcp_clear_context", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPPlanUpdate}, "mcp_plan_update", ""},
		{mcp.MCPEvent{Action: mcp.ActionMCPToolCallError, ToolName: "getTask"}, "mcp_tool_call_error", "getTask"},
		{mcp.MCPEvent{Action: mcp.ActionMCPToolCallError, ToolName: "noSuchTool"}, "mcp_tool_call_error", "unknown"},
		{mcp.MCPEvent{Action: mcp.ActionMCPAgentToolCallFail}, "agent_tool_call_fail", ""},
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
	if got := total(t, reg); got != 17 {
		t.Fatalf("counted %v, want 17", got)
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

// With storage off every action is still counted, and nothing is queued or
// written: the controller has no database to write to here.
func TestPrometheus_SkipStorageStillCounts(t *testing.T) {
	reg := prometheus.NewRegistry()
	c := New(Params{Registerer: reg, SkipStorage: true}).(*controller)
	c.recordCRUD(entity.CRUDEvent{Action: entity.ActionTaskCreate})
	c.recordMCP(mcp.MCPEvent{Action: mcp.ActionMCPToolCall, ToolName: "getTask", ClientID: 7, ClientName: "claude"})
	if got := countOf(c, "task_create", ""); got != 1 {
		t.Errorf("task_create counted %v, want 1", got)
	}
	if got := countOf(c, "mcp_tool_call", "getTask"); got != 1 {
		t.Errorf("mcp_tool_call counted %v, want 1", got)
	}
	if got := len(queued(c)); got != 0 {
		t.Fatalf("queued %d rows, want 0", got)
	}
}

// hook_receive is counted under its outcome, starting at 0, and never
// stored; an outcome not in the list is "unknown".
func TestPrometheus_HookReceiveOutcomes(t *testing.T) {
	c, _ := promController(t)
	for _, o := range entity.HookReceiveOutcomes {
		if got := countOf(c, "hook_receive", o); got != 0 {
			t.Fatalf("%s primed at %v, want 0", o, got)
		}
	}
	c.recordCRUD(entity.CRUDEvent{Action: entity.ActionHookReceive, SubAction: "ignored"})
	c.recordCRUD(entity.CRUDEvent{Action: entity.ActionHookReceive, SubAction: "made-up"})
	if got := countOf(c, "hook_receive", "ignored"); got != 1 {
		t.Errorf("ignored counted %v, want 1", got)
	}
	if got := countOf(c, "hook_receive", "unknown"); got != 1 {
		t.Errorf("unknown counted %v, want 1", got)
	}
	if got := len(queued(c)); got != 0 {
		t.Fatalf("queued %d rows, want 0", got)
	}
}
