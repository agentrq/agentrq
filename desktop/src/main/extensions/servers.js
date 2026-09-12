// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

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
 * makes the install screen work today, and a test keeps them honest — the same
 * arrangement `frontend/src/composables/useWorkspaceSettings.js` already uses
 * for the settings snippet, checked against the Go source so a tool added on one
 * side fails the build on the other.
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
  'createTask',
  'updateTaskStatus',
  'reply',
  'downloadAttachment',
  'getWorkspace',
  'getTask',
  'publishEvent',
  'loadMemory',
  'saveMemory',
  'deleteMemory',
  'elicit',
])

/** `backend/internal/handler/coremcp/`, across every file that registers a tool. */
export const SUPERVISOR_TOOLS = Object.freeze([
  'listWorkspaces',
  'createWorkspace',
  'getWorkspace',
  'updateWorkspace',
  'getWorkspaceStats',
  'listTasks',
  'listAllTasks',
  'createTask',
  'getTask',
  'respondToTask',
  'replyToTask',
  'updateTaskStatus',
  'updateTaskOrder',
  'updateTaskAssignee',
  'updateTaskAllowAll',
  'updateScheduledTask',
  'deleteTask',
  'getAttachment',
  'listMemories',
  'getMemory',
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
])

/** What `checkCompatibility` takes, with the running version. */
export function serverTools(appVersion) {
  return {
    appVersion,
    workspaceTools: [...WORKSPACE_TOOLS],
    supervisorTools: [...SUPERVISOR_TOOLS],
  }
}
