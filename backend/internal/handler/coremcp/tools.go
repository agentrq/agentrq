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

// The tools' inputs. Their schemas offer each tool's actions, and help, as
// the enum of action: see ActionInputSchema.
type (
	WorkspaceToolParams struct {
		Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
	}
	MemoryToolParams struct {
		Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
	}
	SkillsToolParams struct {
		Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
	}
	EventsToolParams struct {
		Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
	}
	WorkflowsToolParams struct {
		Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
	}
	MachinesToolParams struct {
		Action  string         `json:"action" jsonschema:"What to do: one of the actions this tool's description lists, or help for each action's fields and an example."`
		Payload map[string]any `json:"payload,omitempty" jsonschema:"The fields the chosen action works on, such as the ids it reads or the text it writes; help with payload {\"topic\": \"<action>\"} names them and gives an example. Leave it out for an action that takes none."`
	}
	// HelpToolParams is the help tool's input: what to explain.
	HelpToolParams struct {
		Action string `json:"action,omitempty" jsonschema:"A tool, like task, for its actions; tool.action, like task.create, for one action's payload and an example. Leave it out for every tool."`
	}
)

const (
	twoInputs = "Two inputs: action, one of the enum, and payload, an object with that action's fields; leave payload out when the action takes none. " +
		"Run action help for every action, or help with payload {\"topic\": \"<action>\"} for one action's fields and an example. "

	workspaceToolDescription = "Find, create and change the account's workspaces. A workspace is one agent's queue of tasks, with its mission, memory and skills. " +
		"Use it to find the workspaceId every other tool here takes, and to see whether a workspace's agent is free before you hand it work. " + twoInputs +
		"list: every workspace's id, name, forkOfId, workingDirectory and agent state (running, idle, paused or offline); includeDetails adds descriptions and settings, includeArchived adds archived ones. " +
		"get (id): one workspace in full, with its mcpUrl and agent state. " +
		"create (name in kebab-case, description as its mission): a new workspace; check list first for one with the same purpose, and an agent still has to be started in it, by a human or machines launchAgent. " +
		"update (id): changes only the fields sent. stats (id, range 7d or 30d): its activity over that time. " +
		"fork (workspaceId): a second workspace with its own queue and agent that shares the parent's settings, memory and skills, to work on tasks in parallel. " +
		"mergeFork (workspaceId of the fork): stops its agent, moves every task back to the parent and removes the fork; refused while any of its tasks is not completed or rejected. " +
		"Workspaces cannot be deleted here."
	memoryToolDescription = "Read what a workspace's agent remembers: the notes it keeps from one task to the next, such as its project's conventions and lessons learned. " +
		"Read-only: to change a note, create a task asking that workspace's agent to. " +
		"Use it to check what a workspace knows before giving it work, or to carry a lesson to another workspace in a task's body. " + twoInputs +
		"list (workspaceId): every memory's name, size and when it last changed, without content. " +
		"get (workspaceId, name as list reports it): one memory in full. Start with memory.md, the index that links the others. " +
		"workspaceId comes from workspace list."
	skillsToolDescription = "Read the skills a workspace's agent can load: reusable instructions, a SKILL.md and its files, shared across the account and turned on or off per workspace. " +
		"Read-only. Use it to see what a workspace can already do before giving it work, or to name a skill in a task by its skill:// URI. " + twoInputs +
		"search (workspaceId, optional q, limit and offset): skills by name or description, each with its description, source, size, and whether it is on for the account (enabled) and in that workspace (workspaceEnabled); the agent sees only skills with both on. Content is not included. " +
		"get (workspaceId, uri as skill://<name>/<path>): one file; skill://<name> alone returns its SKILL.md and the list of its other files. " +
		"workspaceId comes from workspace list."
	eventsToolDescription = "Make one workspace's work start another's. An event is a named signal an agent publishes when it finishes something; a trigger on it creates a task in a workspace each time it fires, and the task's body can carry what the publisher sent. " +
		"Agents publish events from their own workspace's server, not from here. For a whole chain drawn as one graph, use workflows. " + twoInputs +
		"list, get (eventId), create (name, lowercase and unique, and payloadGuidelines telling the publisher what to send), update (eventId: its payloadGuidelines only; the name is fixed) and delete (eventId; its triggers stop firing). " +
		"listTriggers (eventId), getTrigger, updateTrigger and deleteTrigger (triggerId) manage what happens when it fires. " +
		"createTrigger (eventId, workspaceId, title, and a body that may carry {{EVENT_PAYLOAD}} and {{EVENT_FAQ}}): emitEventId publishes a second event when the spawned task completes, and cronSchedule is at most hourly. " +
		"updateTrigger writes every field as given, so send the ones to keep as well. " +
		"listTasks (eventId): the tasks it has spawned, to check that what was wired up is running."
	workflowsToolDescription = "Design multi-step pipelines across workspaces. A workflow is a graph of events and the workspaces that react to them: each step creates a task in a workspace when its event fires, and can publish the next event when that task completes. " +
		"Use it to build or read a whole chain at once; events holds the events themselves and single triggers. " + twoInputs +
		"list, get (workflowId), create (name, description, and startEventId, the event that starts it), update (workflowId: only the fields sent change) and delete (workflowId, with its steps). " +
		"listSteps (workflowId), createStep (workflowId, eventId, workspaceId, title; the body may carry {{EVENT_PAYLOAD}} and {{EVENT_FAQ}}, and emitEventId hands off to the next step) and deleteStep (workflowId, stepId) change one step at a time. " +
		"getText (workflowId) reads the whole graph as an indented document, and replaceFromText (workflowId, text) rewrites the whole graph from one; the events and workspaces it names must already exist, and an unknown name leaves the workflow untouched. " +
		"listTasks (workflowId): the tasks it has spawned."
	machinesToolDescription = "Run agents on the account's machines. Each machine runs agentrqd, which starts and stops coding agents (claude-code, or another agent through acp-gateway) in a workspace's working directory. " +
		"Use it to see where agents run, to start an agent for a workspace that has none, or to stop one. " + twoInputs +
		"list: every machine's id, name, OS, agentrqd version, whether it is enabled and online, and the agents on it with their workspaces and state. " +
		"createEnrolmentCode: a one-time code a human uses to enrol a new machine; it is shown once and expires shortly, so hand it on at once (the agentrq://guides/agentrqd-setup resource has the steps). " +
		"launchAgent (workspaceId, machineId from list, kind claude-code or acp-gateway, optional model and effort): starts an agent, which connects a few seconds later and picks up its tasks; refused while the workspace already has an agent or has no working directory, or when the machine is disabled, offline or runs an agentrqd too old for what was asked. " +
		"stopAgent (workspaceId): stops that workspace's agent at once, ending whatever it is in the middle of."
	helpToolDescription = "How to call this server's tools. Every tool here takes an action and its payload: call help when you do not know an action's fields. Read-only. " +
		"With no action, every tool and its actions; with action set to a tool, like task, its actions; with tool.action, like task.create, that action's payload and an example."
)

func (s *WorkspaceServer) workspaceActions() []mcpevent.ToolAction {
	return []mcpevent.ToolAction{
		mcpevent.NewToolAction("list", "List the account's workspaces, and what each one's agent is doing.", s.handleListWorkspaces).
			WithDetails("Each comes with only its id, name, forkOfId, workingDirectory and agent unless includeDetails is true. " + agentStateHelp).
			WithExample(`{"includeArchived": false}`),
		mcpevent.NewToolAction("create", "Create a workspace.", s.handleCreateWorkspace).
			WithDetails("Check list first for one with the same purpose. A human still has to connect an agent to it, or launch one with machines launchAgent.").
			WithExample(`{"name": "release-notes", "description": "Writes the release notes for every tag."}`),
		mcpevent.NewToolAction("get", "Get a workspace, with its mcpUrl and what its agent is doing.", s.handleGetWorkspace).
			WithDetails(agentStateHelp).
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
		mcpevent.NewToolAction("list", "List the account's machines: name, OS, agentrqd version, whether each is enabled and online, and its agents.", s.handleListMachines).
			WithDetails("agents names each agent's workspace and what it is doing. " + agentStateHelp),
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
