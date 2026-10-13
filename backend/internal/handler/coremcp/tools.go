// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

package coremcp

import (
	"context"

	mcpevent "github.com/agentrq/agentrq/backend/internal/controller/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The supervisor's tools: workspace, task, memory, skills, events, workflows,
// machines and pano, each an action and its payload, and help over all of
// them. Each action is run by the handler of the tool it replaced, which still
// records the call under that tool's name.

// The tools' inputs. The enum is each tool's actions, and help.
type (
	WorkspaceToolParams struct {
		Action  string         `json:"action" jsonschema:"enum: list, create, get, update, stats, fork, mergeFork, help"`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them."`
	}
	MemoryToolParams struct {
		Action  string         `json:"action" jsonschema:"enum: list, get, help"`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them."`
	}
	SkillsToolParams struct {
		Action  string         `json:"action" jsonschema:"enum: search, get, help"`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them."`
	}
	EventsToolParams struct {
		Action  string         `json:"action" jsonschema:"enum: list, create, get, update, delete, listTasks, listTriggers, createTrigger, getTrigger, updateTrigger, deleteTrigger, help"`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them. Leave it out when the action takes none."`
	}
	WorkflowsToolParams struct {
		Action  string         `json:"action" jsonschema:"enum: list, create, get, getText, update, delete, replaceFromText, listSteps, createStep, deleteStep, listTasks, help"`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them. Leave it out when the action takes none."`
	}
	MachinesToolParams struct {
		Action  string         `json:"action" jsonschema:"enum: list, createEnrolmentCode, launchAgent, stopAgent, help"`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The action's fields, as help prints them. Leave it out when the action takes none."`
	}
	// HelpToolParams is the help tool's input: what to explain.
	HelpToolParams struct {
		Action string `json:"action,omitempty" jsonschema:"A tool, like task, for its actions; tool.action, like task.create, for one action's payload and an example. Leave it out for every tool."`
	}
)

const (
	twoInputs = "Two inputs: action, and payload, the action's fields; help gives an action's payload and an example. "

	workspaceToolDescription = "The account's workspaces. " + twoInputs +
		"list, create, get and update do what they say; stats: a workspace's activity over 7 or 30 days; fork: a second workspace that shares the parent's settings, memory and skills; mergeFork: brings a fork's tasks back and removes it."
	memoryToolDescription = "A workspace's memory, read-only. " + twoInputs +
		"list: its memories' names and sizes; get: one in full. MEMORY.md is the index the others hang off."
	skillsToolDescription = "The account's skills, as a workspace sees them, read-only. " + twoInputs +
		"search: skills by name or description; get: one file of a skill by skill:// URI."
	eventsToolDescription = "Events, the named signals workspaces publish, and their triggers, which create a task somewhere else when one fires. " + twoInputs +
		"list, create, get, update and delete events; listTriggers, createTrigger, getTrigger, updateTrigger and deleteTrigger their triggers; listTasks: the tasks an event has spawned."
	workflowsToolDescription = "Workflows: graphs of events and the workspaces they hand off to. " + twoInputs +
		"list, create, get, update and delete workflows; listSteps, createStep and deleteStep their steps; getText and replaceFromText the whole graph as a document; listTasks: the tasks a workflow has spawned."
	machinesToolDescription = "The account's machines running agentrqd, and the agents on them. " + twoInputs +
		"list: the machines; createEnrolmentCode: a code to enrol a new one; launchAgent: starts an agent for a workspace; stopAgent: stops it."
	helpToolDescription = "How to call this server's tools. With no action, every tool and its actions; with action set to a tool, its actions; with tool.action, like task.create, that action's payload and an example."
)

func (s *WorkspaceServer) workspaceActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List the account's workspaces.", s.handleListWorkspaces).
			WithExample(`{"includeArchived": false}`),
		mcpevent.NewToolAction("create", "Create a workspace.", s.handleCreateWorkspace).
			WithDetails("Check list first for one with the same purpose. A human still has to connect an agent to it, or launch one with machines launchAgent.").
			WithExample(`{"name": "release-notes", "description": "Writes the release notes for every tag."}`),
		mcpevent.NewToolAction("get", "Get a workspace, with its mcpUrl.", s.handleGetWorkspace).
			WithExample(`{"id": "0ZzhYQG2qtl"}`),
		mcpevent.NewToolAction("update", "Update a workspace: only the fields sent change.", s.handleUpdateWorkspace).
			WithExample(`{"id": "0ZzhYQG2qtl", "description": "Writes and publishes the release notes."}`),
		mcpevent.NewToolAction("stats", "Get a workspace's statistics over 7 or 30 days.", s.handleGetWorkspaceStats).
			WithExample(`{"id": "0ZzhYQG2qtl", "range": "7d"}`),
		mcpevent.NewToolAction("fork", "Fork a workspace: its own queue and agent, sharing the parent's settings, memory and skills.", s.handleForkWorkspace).
			WithDetails("Move tasks into the fork to have a second agent work on them; merge it back with mergeFork when they are done. A fork, the supervisor workspace and an archived workspace cannot be forked.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "name": "release-notes-fork"}`),
		mcpevent.NewToolAction("mergeFork", "Merge a fork back into its parent and remove it.", s.handleMergeFork).
			WithDetails("The fork's agent is stopped and every task moves to the parent with its thread. Its folder on the machine is left unless deleteFolder is true. Refused while any task in the fork is not completed or rejected.").
			WithExample(`{"workspaceId": "0kGpezGeuxt"}`),
	}
}

func (s *WorkspaceServer) memoryActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List a workspace's memories: name, size and when each last changed, without content.", s.handleListMemories).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl"}`),
		mcpevent.NewToolAction("get", "Get one of a workspace's memories in full, by name.", s.handleGetMemory).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "name": "memory.md"}`),
	}
}

func (s *WorkspaceServer) skillsActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("search", "Find the account's skills as a workspace sees them: name, description, source and size, and the total matched.", s.handleSearchSkills).
			WithDetails("enabled false means turned off for every workspace; workspaceEnabled false means not on in that workspace. Its agent sees only skills with both on. Content is not included: get a file with get.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "q": "release"}`),
		mcpevent.NewToolAction("get", "Get one file of a skill; skill://<name> alone gets its SKILL.md and the list of its other files.", s.handleGetSkill).
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "uri": "skill://release-notes"}`),
	}
}

func (s *WorkspaceServer) eventsActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List the account's events.", s.handleListEvents),
		mcpevent.NewToolAction("create", "Define a named signal that workspaces publish and triggers react to.", s.handleCreateEvent).
			WithExample(`{"name": "release_tagged", "payloadGuidelines": "The tag, and the PRs it includes."}`),
		mcpevent.NewToolAction("get", "Get an event.", s.handleGetEvent).
			WithExample(`{"eventId": "0kEvt7hQx2M"}`),
		mcpevent.NewToolAction("update", "Revise an event's payload guidelines. Its name is fixed once created.", s.handleUpdateEvent).
			WithExample(`{"eventId": "0kEvt7hQx2M", "payloadGuidelines": "The tag, its PRs, and the changelog link."}`),
		mcpevent.NewToolAction("delete", "Delete an event. Its triggers stop firing.", s.handleDeleteEvent).
			WithExample(`{"eventId": "0kEvt7hQx2M"}`),
		mcpevent.NewToolAction("listTasks", "List the tasks an event has spawned, to see whether what was wired up is running.", s.handleListEventTasks).
			WithExample(`{"eventId": "0kEvt7hQx2M"}`),
		mcpevent.NewToolAction("listTriggers", "List an event's triggers: everything that happens when it fires.", s.handleListEventTriggers).
			WithExample(`{"eventId": "0kEvt7hQx2M"}`),
		mcpevent.NewToolAction("createTrigger", "React to an event: when it fires, create a task in a workspace.", s.handleCreateEventTrigger).
			WithDetails("The body may carry {{EVENT_PAYLOAD}} and {{EVENT_FAQ}}; the title is used as written. emitEventId chains a second event to the task's completion. A cron schedule is at most hourly.").
			WithExample(`{"eventId": "0kEvt7hQx2M", "workspaceId": "0ZzhYQG2qtl", "title": "Write the release notes", "body": "{{EVENT_PAYLOAD}}", "emitEventId": "0kEvt9notes"}`),
		mcpevent.NewToolAction("getTrigger", "Get an event trigger.", s.handleGetEventTrigger).
			WithExample(`{"triggerId": "0kTrg3aB9cD"}`),
		mcpevent.NewToolAction("updateTrigger", "Rewrite an event trigger.", s.handleUpdateEventTrigger).
			WithDetails("Every field is written as given, so send the ones to keep as well.").
			WithExample(`{"triggerId": "0kTrg3aB9cD", "workspaceId": "0ZzhYQG2qtl", "title": "Write the release notes", "body": "{{EVENT_PAYLOAD}}"}`),
		mcpevent.NewToolAction("deleteTrigger", "Delete an event trigger, leaving its event in place.", s.handleDeleteEventTrigger).
			WithExample(`{"triggerId": "0kTrg3aB9cD"}`),
	}
}

func (s *WorkspaceServer) workflowsActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List the account's workflows.", s.handleListWorkflows),
		mcpevent.NewToolAction("create", "Create a workflow, optionally starting from an event.", s.handleCreateWorkflow).
			WithExample(`{"name": "Release", "startEventId": "0kEvt7hQx2M"}`),
		mcpevent.NewToolAction("get", "Get a workflow.", s.handleGetWorkflow).
			WithExample(`{"workflowId": "0kWfl5mN2pQ"}`),
		mcpevent.NewToolAction("getText", "Read a workflow's whole graph as the indented document the UI's text mode edits.", s.handleGetWorkflowText).
			WithDetails("Steps naming a deleted event or workspace are left out, so the document always parses.").
			WithExample(`{"workflowId": "0kWfl5mN2pQ"}`),
		mcpevent.NewToolAction("update", "Update a workflow: only the fields sent change.", s.handleUpdateWorkflow).
			WithExample(`{"workflowId": "0kWfl5mN2pQ", "name": "Release train"}`),
		mcpevent.NewToolAction("delete", "Delete a workflow and its steps.", s.handleDeleteWorkflow).
			WithExample(`{"workflowId": "0kWfl5mN2pQ"}`),
		mcpevent.NewToolAction("replaceFromText", "Replace a workflow's entire graph with a document.", s.handleReplaceWorkflowFromText).
			WithDetails("Every name is resolved before anything is written, so an unknown one on the last line leaves the workflow untouched.").
			WithExample(`{"workflowId": "0kWfl5mN2pQ", "text": "- event:release_tagged\n  - agent:release-notes\n    - event:notes_ready"}`),
		mcpevent.NewToolAction("listSteps", "List a workflow's steps.", s.handleListWorkflowSteps).
			WithExample(`{"workflowId": "0kWfl5mN2pQ"}`),
		mcpevent.NewToolAction("createStep", "Add a step: when its event fires, create a task in a workspace.", s.handleCreateWorkflowStep).
			WithDetails("The body may carry {{EVENT_PAYLOAD}} and {{EVENT_FAQ}}; emitEventId hands off to the next step when the task completes.").
			WithExample(`{"workflowId": "0kWfl5mN2pQ", "eventId": "0kEvt7hQx2M", "workspaceId": "0ZzhYQG2qtl", "title": "Write the release notes", "emitEventId": "0kEvt9notes"}`),
		mcpevent.NewToolAction("deleteStep", "Delete a workflow step.", s.handleDeleteWorkflowStep).
			WithExample(`{"workflowId": "0kWfl5mN2pQ", "stepId": "0kStp1xY2zA"}`),
		mcpevent.NewToolAction("listTasks", "List the tasks a workflow has spawned.", s.handleListWorkflowTasks).
			WithExample(`{"workflowId": "0kWfl5mN2pQ"}`),
	}
}

func (s *WorkspaceServer) machinesActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List the account's machines: name, OS, agentrqd version, whether each is enabled and online, and its running agents.", s.handleListMachines),
		mcpevent.NewToolAction("createEnrolmentCode", "Mint a one-time code for enrolling a new machine with agentrqd.", s.handleCreateEnrolmentCode).
			WithDetails("It is shown once and expires shortly, so hand it to the human right away; the agentrq://guides/agentrqd-setup resource has the rest of the setup."),
		mcpevent.NewToolAction("launchAgent", "Start an agent for a workspace on one of the account's machines.", s.handleLaunchAgent).
			WithDetails("It runs in the workspace's working directory (a fork's folder is made from its parent's). Refused while the workspace already has an agent, when it has no working directory, or when the machine is disabled, offline or runs an agentrqd too old for what was asked. The agent connects a few seconds later and picks up its tasks.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl", "machineId": "0kMch4rT8uV", "kind": "claude-code", "model": "opus"}`),
		mcpevent.NewToolAction("stopAgent", "Stop the agent a workspace is running on a machine, ending whatever it is in the middle of.", s.handleStopAgent).
			WithDetails("stopped is false when none was running on a machine.").
			WithExample(`{"workspaceId": "0ZzhYQG2qtl"}`),
	}
}

// tools are the server's tools, as the help tool lists them.
func (s *WorkspaceServer) tools() []mcpevent.ActionTool {
	return []mcpevent.ActionTool{
		{Name: "workspace", Summary: "The account's workspaces, and their forks.", Actions: s.workspaceActions()},
		{Name: "task", Summary: "The tasks of every workspace.", Actions: s.taskActions()},
		{Name: "memory", Summary: "A workspace's memory, read-only.", Actions: s.memoryActions()},
		{Name: "skills", Summary: "The account's skills as a workspace sees them, read-only.", Actions: s.skillsActions()},
		{Name: "events", Summary: "Named signals, and the triggers that turn them into tasks.", Actions: s.eventsActions()},
		{Name: "workflows", Summary: "Graphs of events and the workspaces they hand off to.", Actions: s.workflowsActions()},
		{Name: "machines", Summary: "Machines running agentrqd, and the agents on them.", Actions: s.machinesActions()},
		{Name: "pano", Summary: "Markdown docs every workspace of the account shares.", Actions: mcpevent.PanoActions(nil, "", 0)},
	}
}

// runTool runs one call of the action tool tool. An action records itself;
// help, or an action the tool does not have, is recorded as the tool.
func (s *WorkspaceServer) runTool(ctx context.Context, req *mcp.CallToolRequest, tool string, actions []mcpevent.ToolAction, action string, payload map[string]any) (*mcp.CallToolResult, any, error) {
	if !mcpevent.IsAction(actions, action) {
		s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, tool, 0)
	}
	return mcpevent.RunAction(ctx, req, tool, actions, action, payload)
}

func (s *WorkspaceServer) handleWorkspaceTool(ctx context.Context, req *mcp.CallToolRequest, p WorkspaceToolParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "workspace", s.workspaceActions(), p.Action, p.Payload)
}

func (s *WorkspaceServer) handleMemoryTool(ctx context.Context, req *mcp.CallToolRequest, p MemoryToolParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "memory", s.memoryActions(), p.Action, p.Payload)
}

func (s *WorkspaceServer) handleSkillsTool(ctx context.Context, req *mcp.CallToolRequest, p SkillsToolParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "skills", s.skillsActions(), p.Action, p.Payload)
}

func (s *WorkspaceServer) handleEventsTool(ctx context.Context, req *mcp.CallToolRequest, p EventsToolParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "events", s.eventsActions(), p.Action, p.Payload)
}

func (s *WorkspaceServer) handleWorkflowsTool(ctx context.Context, req *mcp.CallToolRequest, p WorkflowsToolParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "workflows", s.workflowsActions(), p.Action, p.Payload)
}

func (s *WorkspaceServer) handleMachinesTool(ctx context.Context, req *mcp.CallToolRequest, p MachinesToolParams) (*mcp.CallToolResult, any, error) {
	return s.runTool(ctx, req, "machines", s.machinesActions(), p.Action, p.Payload)
}

func (s *WorkspaceServer) handleHelpTool(ctx context.Context, req *mcp.CallToolRequest, p HelpToolParams) (*mcp.CallToolResult, any, error) {
	s.emitTelemetry(ctx, mcpevent.ActionMCPToolCall, "help", 0)
	return mcpevent.ToolsHelp(s.tools(), p.Action), nil, nil
}

// legacyTools are the supervisor tools the action tools replaced.
var legacyTools = map[string]mcpevent.LegacyTool{
	"listWorkspaces":          {Tool: "workspace", Action: "list"},
	"createWorkspace":         {Tool: "workspace", Action: "create"},
	"getWorkspace":            {Tool: "workspace", Action: "get"},
	"updateWorkspace":         {Tool: "workspace", Action: "update"},
	"getWorkspaceStats":       {Tool: "workspace", Action: "stats"},
	"forkWorkspace":           {Tool: "workspace", Action: "fork"},
	"mergeFork":               {Tool: "workspace", Action: "mergeFork"},
	"listTasks":               {Tool: "task", Action: "list"},
	"listAllTasks":            {Tool: "task", Action: "listAll"},
	"createTask":              {Tool: "task", Action: "create"},
	"getTask":                 {Tool: "task", Action: "get"},
	"respondToTask":           {Tool: "task", Action: "respond"},
	"replyToTask":             {Tool: "task", Action: "reply"},
	"updateTaskStatus":        {Tool: "task", Action: "updateStatus"},
	"updateTaskOrder":         {Tool: "task", Action: "updateSortingOrder"},
	"updateTaskAssignee":      {Tool: "task", Action: "updateAssignee"},
	"updateTaskAllowAll":      {Tool: "task", Action: "updateAllowAll"},
	"updateScheduledTask":     {Tool: "task", Action: "updateSchedule"},
	"deleteTask":              {Tool: "task", Action: "delete"},
	"getAttachment":           {Tool: "task", Action: "getAttachment"},
	"listMemories":            {Tool: "memory", Action: "list"},
	"getMemory":               {Tool: "memory", Action: "get"},
	"searchSkills":            {Tool: "skills", Action: "search"},
	"getSkill":                {Tool: "skills", Action: "get"},
	"listEvents":              {Tool: "events", Action: "list"},
	"createEvent":             {Tool: "events", Action: "create"},
	"getEvent":                {Tool: "events", Action: "get"},
	"updateEvent":             {Tool: "events", Action: "update"},
	"deleteEvent":             {Tool: "events", Action: "delete"},
	"listEventTasks":          {Tool: "events", Action: "listTasks"},
	"listEventTriggers":       {Tool: "events", Action: "listTriggers"},
	"createEventTrigger":      {Tool: "events", Action: "createTrigger"},
	"getEventTrigger":         {Tool: "events", Action: "getTrigger"},
	"updateEventTrigger":      {Tool: "events", Action: "updateTrigger"},
	"deleteEventTrigger":      {Tool: "events", Action: "deleteTrigger"},
	"listWorkflows":           {Tool: "workflows", Action: "list"},
	"createWorkflow":          {Tool: "workflows", Action: "create"},
	"getWorkflow":             {Tool: "workflows", Action: "get"},
	"getWorkflowText":         {Tool: "workflows", Action: "getText"},
	"updateWorkflow":          {Tool: "workflows", Action: "update"},
	"deleteWorkflow":          {Tool: "workflows", Action: "delete"},
	"replaceWorkflowFromText": {Tool: "workflows", Action: "replaceFromText"},
	"listWorkflowSteps":       {Tool: "workflows", Action: "listSteps"},
	"createWorkflowStep":      {Tool: "workflows", Action: "createStep"},
	"deleteWorkflowStep":      {Tool: "workflows", Action: "deleteStep"},
	"listWorkflowTasks":       {Tool: "workflows", Action: "listTasks"},
	"listMachines":            {Tool: "machines", Action: "list"},
	"createEnrolmentCode":     {Tool: "machines", Action: "createEnrolmentCode"},
	"launchAgent":             {Tool: "machines", Action: "launchAgent"},
	"stopAgent":               {Tool: "machines", Action: "stopAgent"},
}

// actionToolNames are the tools a flat call to is folded into a payload.
var actionToolNames = []string{"workspace", "task", "memory", "skills", "events", "workflows", "machines", "pano"}
