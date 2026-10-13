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
 * Deliberately absent: anything that lists tasks. See above.
 */
export const WORKSPACE_TOOLS = Object.freeze([
  'task',
  'getWorkspace',
  'publishEvent',
  'loadMemory',
  'saveMemory',
  'deleteMemory',
  'searchSkills',
  'loadSkill',
  'saveSkill',
  'deleteSkill',
  'pano',
  'listSiteTools',
  'getSiteToolDefinition',
  'callSiteTool',
])

/** `backend/internal/handler/coremcp/`, across every file that registers a tool. */
export const SUPERVISOR_TOOLS = Object.freeze([
  'listWorkspaces',
  'createWorkspace',
  'forkWorkspace',
  'mergeFork',
  'getWorkspace',
  'updateWorkspace',
  'getWorkspaceStats',
  'task',
  'listMemories',
  'getMemory',
  'searchSkills',
  'getSkill',
  'listEvents',
  'createEvent',
  'getEvent',
  'updateEvent',
  'deleteEvent',
  'createEventTrigger',
  'listEventTriggers',
  'getEventTrigger',
  'updateEventTrigger',
  'deleteEventTrigger',
  'listEventTasks',
  'listWorkflows',
  'createWorkflow',
  'getWorkflow',
  'updateWorkflow',
  'deleteWorkflow',
  'createWorkflowStep',
  'listWorkflowSteps',
  'deleteWorkflowStep',
  'listWorkflowTasks',
  'getWorkflowText',
  'replaceWorkflowFromText',
  'createEnrolmentCode',
  'listMachines',
  'launchAgent',
  'stopAgent',
  'pano',
])

/**
 * The tools that take an action and a payload, and their actions, per surface.
 *
 * The enums of the Go `TaskParams` and `PanoParams`, which a test compares. A
 * manifest asks for one of these bare (`"task"`, every action) or for one action
 * (`"task.create"`), and the broker holds a call to the action it names.
 */
const PANO_ACTIONS = Object.freeze(['save', 'read', 'comment', 'attach', 'delete', 'help'])

export const WORKSPACE_ACTIONS = Object.freeze({
  task: Object.freeze(['create', 'get', 'updateStatus', 'reply', 'elicit', 'getAttachment', 'help']),
  pano: PANO_ACTIONS,
})

export const SUPERVISOR_ACTIONS = Object.freeze({
  task: Object.freeze([
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
    'help',
  ]),
  pano: PANO_ACTIONS,
})

/**
 * The tools that became `task` actions, and the action each became.
 *
 * So an extension installed before the merge keeps both its manifest and its
 * calls: a retired name is read as `task.<action>` wherever it appears. The
 * servers answer these names too, but a grant has to be checked against what a
 * call really does, and that is the action.
 */
export const RETIRED_TOOLS = Object.freeze({
  workspace: Object.freeze({
    createTask: 'create',
    getTask: 'get',
    updateTaskStatus: 'updateStatus',
    reply: 'reply',
    elicit: 'elicit',
    getAttachment: 'getAttachment',
  }),
  supervisor: Object.freeze({
    listTasks: 'list',
    listAllTasks: 'listAll',
    createTask: 'create',
    getTask: 'get',
    respondToTask: 'respond',
    replyToTask: 'reply',
    updateTaskStatus: 'updateStatus',
    updateTaskOrder: 'updateSortingOrder',
    updateTaskAssignee: 'updateAssignee',
    updateTaskAllowAll: 'updateAllowAll',
    updateScheduledTask: 'updateSchedule',
    deleteTask: 'delete',
    getAttachment: 'getAttachment',
  }),
})

/** A manifest or grant entry as it is now spelled: a retired name becomes `task.<action>`. */
export function currentToolName(surface, tool) {
  const action = RETIRED_TOOLS[surface]?.[tool]
  return action ? `task.${action}` : tool
}

/**
 * A call that names a retired tool, as the `task` call it now is.
 *
 * The old arguments are the payload, apart from the workspace reply's `chatId`,
 * which the task tool calls `taskId`. Anything else comes back as it was.
 */
export function currentCall(surface, tool, args = {}) {
  const action = RETIRED_TOOLS[surface]?.[tool]
  if (!action) return { tool, args }
  const { workspaceId, ...payload } = args
  if (surface === 'workspace' && action === 'reply' && 'chatId' in payload) {
    payload.taskId = payload.chatId
    delete payload.chatId
  }
  return { tool: 'task', args: { ...(workspaceId ? { workspaceId } : {}), action, payload } }
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
