// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * What the two MCP servers offer, so an extension can be told before it installs.
 *
 * `checkCompatibility` needs these lists, and until now the app passed none —
 * so every tool an extension asked for was "not offered" and anything wanting
 * MCP at all was refused with three lines of nonsense:
 *
 *     The workspace server does not offer "getWorkspace".
 *     The workspace server does not offer "loadMemory".
 *     The workspace server does not offer "getTask".
 *
 * There is even a test asserting that behaviour — *"judges nothing compatible
 * when it was told nothing about the servers"* — which is correct as a
 * statement about the function and describes exactly the state the real app was
 * in. A unit test can only check the answer to the question it was asked.
 *
 * ## Why these are written down rather than fetched
 *
 * Asking a server what it offers means an MCP session: initialize, `tools/list`,
 * and a transport this app does not yet have. Writing the lists down is what
 * makes the install screen work today, and a test keeps them honest, checked
 * against the Go source so a tool added on one side fails the build on the
 * other.
 *
 * ## What that costs, stated rather than hidden
 *
 * These describe **this app's idea of the servers**, not the server it happens
 * to be connected to. A self-hosted backend older than the desktop app may not
 * have a tool listed here, and an extension needing it will install and then be
 * refused at call time — by the server, with the server's own message, which is
 * at least an honest failure rather than a silent one. Fetching the real lists
 * is the right fix and belongs with the transport work.
 *
 * ## The two are not the same, deliberately
 *
 * The workspace server has **no task listing**. An agent connected to a
 * workspace acts on the task it was given and reads what the workspace
 * remembers; it does not enumerate the board. The supervisor is account-wide and
 * does list things — which is why reaching it is the top rung of the grant
 * ladder rather than a checkbox.
 */

/**
 * `backend/internal/controller/mcp/server.go`, in registration order.
 *
 * Every tool but `help` takes an action and a payload; `help` only explains
 * them. Deliberately absent: any action that lists tasks. See above.
 */
export const WORKSPACE_TOOLS = Object.freeze(['task', 'memory', 'skills', 'workspace', 'siteTools', 'pano', 'help'])

/** `backend/internal/handler/coremcp/`, across every file that registers a tool. */
export const SUPERVISOR_TOOLS = Object.freeze([
  'workspace',
  'task',
  'memory',
  'skills',
  'events',
  'workflows',
  'machines',
  'help',
  'pano',
])

const actions = (...names) => Object.freeze([...names, 'help'])
const PANO_ACTIONS = actions('save', 'edit', 'read', 'comment', 'attach', 'delete')

/**
 * Each tool's actions, per surface: the enums of the Go params, which a test
 * compares. A manifest asks for a tool bare (`"task"`, every action) or for one
 * action (`"task.create"`), and the broker holds a call to the action it names.
 */
export const WORKSPACE_ACTIONS = Object.freeze({
  task: actions('create', 'get', 'updateStatus', 'reply', 'elicit', 'getAttachment'),
  memory: actions('load', 'save', 'delete'),
  skills: actions('search', 'load', 'save', 'delete'),
  workspace: actions('get', 'publishEvent'),
  siteTools: actions('search', 'getDefinition', 'call'),
  pano: PANO_ACTIONS,
})

export const SUPERVISOR_ACTIONS = Object.freeze({
  workspace: actions('list', 'create', 'get', 'update', 'stats', 'fork', 'mergeFork'),
  task: actions(
    'list',
    'listAll',
    'create',
    'get',
    'reply',
    'respond',
    'updateStatus',
    'updateSortingOrder',
    'updateAssignee',
    'updateAllowAll',
    'updateSchedule',
    'delete',
    'getAttachment',
  ),
  memory: actions('list', 'get'),
  skills: actions('search', 'get'),
  events: actions(
    'list',
    'create',
    'get',
    'update',
    'delete',
    'listTasks',
    'listTriggers',
    'createTrigger',
    'getTrigger',
    'updateTrigger',
    'deleteTrigger',
  ),
  workflows: actions(
    'list',
    'create',
    'get',
    'getText',
    'update',
    'delete',
    'replaceFromText',
    'listSteps',
    'createStep',
    'deleteStep',
    'listTasks',
  ),
  machines: actions('list', 'createEnrolmentCode', 'launchAgent', 'stopAgent'),
  pano: PANO_ACTIONS,
})

/**
 * The tools that became actions, and the `tool.action` each became: the
 * `legacyTools` maps in each server's `tools.go`, which a test compares.
 *
 * So an extension installed before the merge keeps both its manifest and its
 * calls: a retired name is read as its action wherever it appears. The servers
 * answer these names too, but a grant has to be checked against what a call
 * really does, and that is the action.
 */
export const RETIRED_TOOLS = Object.freeze({
  workspace: Object.freeze({
    createTask: 'task.create',
    getTask: 'task.get',
    updateTaskStatus: 'task.updateStatus',
    reply: 'task.reply',
    elicit: 'task.elicit',
    getAttachment: 'task.getAttachment',
    loadMemory: 'memory.load',
    saveMemory: 'memory.save',
    deleteMemory: 'memory.delete',
    searchSkills: 'skills.search',
    loadSkill: 'skills.load',
    saveSkill: 'skills.save',
    deleteSkill: 'skills.delete',
    getWorkspace: 'workspace.get',
    publishEvent: 'workspace.publishEvent',
    listSiteTools: 'siteTools.search',
    getSiteToolDefinition: 'siteTools.getDefinition',
    callSiteTool: 'siteTools.call',
  }),
  supervisor: Object.freeze({
    listWorkspaces: 'workspace.list',
    createWorkspace: 'workspace.create',
    getWorkspace: 'workspace.get',
    updateWorkspace: 'workspace.update',
    getWorkspaceStats: 'workspace.stats',
    forkWorkspace: 'workspace.fork',
    mergeFork: 'workspace.mergeFork',
    listTasks: 'task.list',
    listAllTasks: 'task.listAll',
    createTask: 'task.create',
    getTask: 'task.get',
    respondToTask: 'task.respond',
    replyToTask: 'task.reply',
    updateTaskStatus: 'task.updateStatus',
    updateTaskOrder: 'task.updateSortingOrder',
    updateTaskAssignee: 'task.updateAssignee',
    updateTaskAllowAll: 'task.updateAllowAll',
    updateScheduledTask: 'task.updateSchedule',
    deleteTask: 'task.delete',
    getAttachment: 'task.getAttachment',
    listMemories: 'memory.list',
    getMemory: 'memory.get',
    searchSkills: 'skills.search',
    getSkill: 'skills.get',
    listEvents: 'events.list',
    createEvent: 'events.create',
    getEvent: 'events.get',
    updateEvent: 'events.update',
    deleteEvent: 'events.delete',
    listEventTasks: 'events.listTasks',
    listEventTriggers: 'events.listTriggers',
    createEventTrigger: 'events.createTrigger',
    getEventTrigger: 'events.getTrigger',
    updateEventTrigger: 'events.updateTrigger',
    deleteEventTrigger: 'events.deleteTrigger',
    listWorkflows: 'workflows.list',
    createWorkflow: 'workflows.create',
    getWorkflow: 'workflows.get',
    getWorkflowText: 'workflows.getText',
    updateWorkflow: 'workflows.update',
    deleteWorkflow: 'workflows.delete',
    replaceWorkflowFromText: 'workflows.replaceFromText',
    listWorkflowSteps: 'workflows.listSteps',
    createWorkflowStep: 'workflows.createStep',
    deleteWorkflowStep: 'workflows.deleteStep',
    listWorkflowTasks: 'workflows.listTasks',
    listMachines: 'machines.list',
    createEnrolmentCode: 'machines.createEnrolmentCode',
    launchAgent: 'machines.launchAgent',
    stopAgent: 'machines.stopAgent',
  }),
})

/** A manifest or grant entry as it is now spelled: a retired name becomes `tool.action`. */
export function currentToolName(surface, tool) {
  return RETIRED_TOOLS[surface]?.[tool] ?? tool
}

/**
 * A call that names a retired tool, as the action call it now is.
 *
 * The old arguments are the payload, apart from the workspace reply's `chatId`,
 * which the task tool calls `taskId`. Anything else comes back as it was.
 */
export function currentCall(surface, tool, args = {}) {
  const current = RETIRED_TOOLS[surface]?.[tool]
  if (!current) return { tool, args }
  const [name, action] = current.split('.')
  const { workspaceId, ...payload } = args
  if (surface === 'workspace' && current === 'task.reply' && 'chatId' in payload) {
    payload.taskId = payload.chatId
    delete payload.chatId
  }
  return { tool: name, args: { ...(workspaceId ? { workspaceId } : {}), action, payload } }
}

/** What `checkCompatibility` takes, with the running version. */
export function serverTools(appVersion) {
  return {
    appVersion,
    workspaceTools: [...WORKSPACE_TOOLS],
    supervisorTools: [...SUPERVISOR_TOOLS],
    workspaceActions: WORKSPACE_ACTIONS,
    supervisorActions: SUPERVISOR_ACTIONS,
  }
}
