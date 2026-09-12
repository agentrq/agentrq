// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The WebMCP tool catalogue: everything the AgentRQ interface can do, offered
 * to an agent running in the browser.
 *
 * The rule this file follows is that the catalogue mirrors `src/api.js`. That
 * module *is* the interface's capability surface — every button in the app
 * ends up calling one of its functions — so mirroring it is what makes
 * "anything you can do in the UI" a checkable claim rather than an aspiration.
 * A new API function without a tool here is a gap; that is the invariant to
 * keep.
 *
 * Two tools have no API function behind them, and they are the reason this is
 * WebMCP rather than a published HTTP API: `getCurrentPage` and `navigate` let
 * an agent see and change what the person is actually looking at. "Reply to the
 * task I have open" is answerable here and nowhere else.
 *
 * Everything is injected — the API client and the router — so the catalogue is
 * a pure function of its dependencies and can be tested without a browser, a
 * network or a Vue app.
 *
 * Naming follows the MCP server in `backend/internal/controller/mcp`: an agent
 * that can see both surfaces sees one vocabulary, and `createTask` means the
 * same thing on each. Field names are camelCase, as every other AgentRQ API
 * surface is.
 */

/** JSON Schema fragments, named once so the catalogue reads as intent. */
const str = (description) => ({ type: 'string', description })
const bool = (description) => ({ type: 'boolean', description })
const int = (description) => ({ type: 'integer', description })

const WORKSPACE_ID = str('The workspace ID (base62), as it appears in the URL.')
const TASK_ID = str('The task ID (base62), as it appears in the URL.')

/**
 * Build a WebMCP tool descriptor.
 *
 * `readOnly` and `destructive` become the annotations an agent uses to decide
 * what it may do unattended. They are set from the operation itself rather than
 * left to each entry to remember: a tool that only reads is annotated as such,
 * and anything that removes data says so.
 */
function tool({ name, description, properties = {}, required = [], readOnly = false, destructive = false, run }) {
  return {
    name,
    description,
    inputSchema: { type: 'object', properties, required },
    annotations: {
      readOnlyHint: readOnly,
      // Both spellings: MCP annotates removal with `destructiveHint`, while the
      // WebMCP draft asks whether an action is consequential. Unknown members
      // are ignored, and being understood by both is worth more than guessing.
      destructiveHint: destructive,
      consequentialHint: !readOnly,
    },
    execute: run,
  }
}

/**
 * Everything the interface can do.
 *
 * @param {object} deps
 * @param {object} deps.api the `src/api.js` module, or a stand-in
 * @param {(path: string) => Promise<unknown>} deps.navigate router push
 * @param {() => { path: string, params: object, query: object }} deps.currentPage
 *        where the person is right now
 * @returns {Array<object>} descriptors ready for `registerTools`
 */
export function createToolCatalogue({ api, navigate, currentPage }) {
  return [
    // ---- Where the person is, and taking them elsewhere --------------------
    tool({
      name: 'getCurrentPage',
      description:
        'Where the user is right now in the AgentRQ interface: the route path and its parameters. ' +
        'Call this first to resolve phrases like "this task" or "the workspace I am looking at" ' +
        'into the IDs the other tools need.',
      readOnly: true,
      run: () => currentPage(),
    }),
    tool({
      name: 'navigate',
      description:
        'Open a page in the AgentRQ interface, moving the user there. Paths are the ones in the ' +
        'address bar, for example "/", "/tasks/ongoing", "/workspaces/<workspaceId>/board", ' +
        '"/workspaces/<workspaceId>/tasks/<taskId>", "/events", "/workflows".',
      properties: { path: str('An in-app path beginning with "/".') },
      required: ['path'],
      run: async ({ path }) => {
        await navigate(path)
        return { navigatedTo: path }
      },
    }),

    tool({
      name: 'getCurrentUser',
      description: 'Who is signed in. Everything else these tools do is done as this user.',
      readOnly: true,
      run: () => api.fetchUser(),
    }),

    // ---- Workspaces --------------------------------------------------------
    tool({
      name: 'listWorkspaces',
      description: 'Every workspace the signed-in user can see.',
      properties: { includeArchived: bool('Include archived workspaces. Defaults to false.') },
      readOnly: true,
      run: ({ includeArchived = false } = {}) => api.fetchWorkspaces(includeArchived),
    }),
    tool({
      name: 'getWorkspace',
      description: 'One workspace, including its mission description and settings.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId }) => api.getWorkspace(workspaceId),
    }),
    tool({
      name: 'createWorkspace',
      description: 'Create a workspace. The description is the mission agents are given.',
      properties: {
        name: str('Display name.'),
        description: str('The mission: what agents in this workspace are for.'),
        icon: str('Optional emoji shown beside the name.'),
        selfLearningLoopNote: str('Optional standing note appended to every task in this workspace.'),
        workingDirectory: str('Optional absolute path agents run in.'),
      },
      required: ['name', 'description'],
      run: ({ name, description, icon = '', selfLearningLoopNote = '', workingDirectory = '' }) =>
        api.createWorkspace(name, description, icon, selfLearningLoopNote, workingDirectory),
    }),
    tool({
      name: 'updateWorkspace',
      description: 'Change a workspace. Only the fields given are altered.',
      properties: {
        workspaceId: WORKSPACE_ID,
        name: str('New display name.'),
        description: str('New mission description.'),
        icon: str('New emoji.'),
        selfLearningLoopNote: str('New standing note.'),
        workingDirectory: str('New working directory.'),
      },
      required: ['workspaceId'],
      run: ({ workspaceId, ...fields }) => api.updateWorkspace(workspaceId, fields),
    }),
    tool({
      name: 'archiveWorkspace',
      description: 'Archive a workspace, hiding it from the default list without deleting anything.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      run: ({ workspaceId }) => api.archiveWorkspace(workspaceId),
    }),
    tool({
      name: 'unarchiveWorkspace',
      description: 'Restore an archived workspace to the default list.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      run: ({ workspaceId }) => api.unarchiveWorkspace(workspaceId),
    }),
    tool({
      name: 'deleteWorkspace',
      description:
        'Permanently delete a workspace and everything in it. This cannot be undone — prefer ' +
        'archiveWorkspace unless the user has asked for deletion in so many words.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      destructive: true,
      run: ({ workspaceId }) => api.deleteWorkspace(workspaceId),
    }),
    tool({
      name: 'getWorkspaceToken',
      description:
        'The workspace token an agent uses to connect to this workspace over MCP. ' +
        'This is a credential: show it to the user, never paste it into anything else.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId }) => api.getWorkspaceToken(workspaceId),
    }),
    tool({
      name: 'getWorkspaceStats',
      description: 'Activity statistics for a workspace over a time range.',
      properties: {
        workspaceId: WORKSPACE_ID,
        range: str('Range such as "7d" or "30d". Defaults to "7d".'),
        from: int('Optional start as a Unix timestamp.'),
        to: int('Optional end as a Unix timestamp.'),
      },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId, range = '7d', from = 0, to = 0 }) =>
        api.fetchWorkspaceStats(workspaceId, range, from, to),
    }),
    tool({
      name: 'getAccountStats',
      description:
        'Activity statistics across every workspace the user owns, over a time range, with a ' +
        'per-workspace breakdown of what drove the totals.',
      properties: {
        range: str('Range such as "7d" or "30d". Defaults to "7d".'),
        from: int('Optional start as a Unix timestamp.'),
        to: int('Optional end as a Unix timestamp.'),
      },
      readOnly: true,
      run: ({ range = '7d', from = 0, to = 0 }) => api.fetchUserStats(range, from, to),
    }),
    tool({
      name: 'listWorkspaceMemories',
      description:
        'What the agents working in a workspace have written down for each other: name, size and when ' +
        'each was last changed. The content is not included — read one by name for that. MEMORY.md is ' +
        'the index the others hang off.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId }) => api.fetchWorkspaceMemories(workspaceId),
    }),
    tool({
      name: 'getWorkspaceMemory',
      description:
        'One of a workspace\'s memories, in full. Start with MEMORY.md, which indexes the rest.',
      properties: { workspaceId: WORKSPACE_ID, name: str('The memory\'s name, as listWorkspaceMemories reports it.') },
      required: ['workspaceId', 'name'],
      readOnly: true,
      run: ({ workspaceId, name }) => api.getWorkspaceMemory(workspaceId, name),
    }),
    tool({
      name: 'setWorkspaceSlackChannel',
      description: 'Connect a workspace to a Slack channel so its activity is posted there.',
      properties: {
        workspaceId: WORKSPACE_ID,
        channelId: str('Slack channel ID.'),
        channelName: str('Slack channel name, for display.'),
      },
      required: ['workspaceId', 'channelId', 'channelName'],
      run: ({ workspaceId, channelId, channelName }) =>
        api.setWorkspaceSlackChannel(workspaceId, channelId, channelName),
    }),
    tool({
      name: 'removeWorkspaceSlackChannel',
      description: 'Disconnect a workspace from its Slack channel.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      run: ({ workspaceId }) => api.removeWorkspaceSlackChannel(workspaceId),
    }),

    // ---- Tasks -------------------------------------------------------------
    tool({
      name: 'listTasks',
      description:
        'Tasks in one workspace, or across every workspace when workspaceId is omitted. ' +
        'Statuses are notstarted, ongoing, completed, rejected, cron and blocked.',
      properties: {
        workspaceId: str('Restrict to one workspace. Omit for all workspaces.'),
        status: str('Filter by status.'),
        filter: str('Named filter, as used by the /tasks/<filter> pages.'),
        limit: int('How many to return. Defaults to 10.'),
        offset: int('How many to skip, for paging.'),
      },
      readOnly: true,
      run: ({ workspaceId, ...options } = {}) =>
        workspaceId ? api.fetchTasks(workspaceId, options) : api.fetchGlobalTasks(options),
    }),
    tool({
      name: 'getTask',
      description: 'One task in full, including its conversation.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID },
      required: ['workspaceId', 'taskId'],
      readOnly: true,
      run: ({ workspaceId, taskId }) => api.getTask(workspaceId, taskId),
    }),
    tool({
      name: 'createTask',
      description:
        'Create a task in a workspace. Assign it to an agent to have it worked on, or to a human ' +
        'to make it a to-do. A cronSchedule makes it recurring, and must be hourly at coarsest: ' +
        'the minute field is a single fixed number.',
      properties: {
        workspaceId: WORKSPACE_ID,
        title: str('Short title.'),
        body: str('The task itself, in markdown.'),
        assignee: str('"agent" or "human". Defaults to "agent".'),
        status: str('Initial status. Defaults to "notstarted".'),
        cronSchedule: str('Optional cron expression, making this a recurring task.'),
        allowAllCommands: bool('Let the agent run commands without asking each time.'),
        eventId: str('Optional event this task publishes when it completes.'),
        workflowId: str('Optional workflow this task belongs to.'),
      },
      required: ['workspaceId', 'title', 'body'],
      run: ({
        workspaceId,
        title,
        body,
        assignee = 'agent',
        status = 'notstarted',
        cronSchedule = '',
        allowAllCommands = false,
        eventId = '',
        workflowId = '',
      }) =>
        api.createTask(
          workspaceId,
          title,
          body,
          assignee,
          [],
          status,
          cronSchedule,
          allowAllCommands,
          eventId,
          workflowId
        ),
    }),
    tool({
      name: 'replyToTask',
      description: 'Send a message in a task conversation, as the signed-in user.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, text: str('The message.') },
      required: ['workspaceId', 'taskId', 'text'],
      run: ({ workspaceId, taskId, text }) => api.replyToTask(workspaceId, taskId, text),
    }),
    tool({
      name: 'respondToTask',
      description:
        'Answer a task that is waiting on the user — accepting or rejecting what was proposed.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        action: str('The response action, such as "accept" or "reject".'),
        text: str('Optional message to send with it.'),
      },
      required: ['workspaceId', 'taskId', 'action'],
      run: ({ workspaceId, taskId, action, text = '' }) =>
        api.respondToTask(workspaceId, taskId, action, text),
    }),
    tool({
      name: 'updateTaskStatus',
      description:
        'Set a task\'s status: notstarted, ongoing, completed, rejected, cron or blocked.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, status: str('The new status.') },
      required: ['workspaceId', 'taskId', 'status'],
      run: ({ workspaceId, taskId, status }) => api.updateTaskStatus(workspaceId, taskId, status),
    }),
    tool({
      name: 'updateTaskAssignee',
      description: 'Hand a task to the agent or back to a human.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, assignee: str('"agent" or "human".') },
      required: ['workspaceId', 'taskId', 'assignee'],
      run: ({ workspaceId, taskId, assignee }) => api.updateTaskAssignee(workspaceId, taskId, assignee),
    }),
    tool({
      name: 'updateTaskOrder',
      description: 'Reorder a task on the board.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, order: int('The new position.') },
      required: ['workspaceId', 'taskId', 'order'],
      run: ({ workspaceId, taskId, order }) => api.updateTaskOrder(workspaceId, taskId, order),
    }),
    tool({
      name: 'moveTask',
      description: 'Move a task to a different workspace.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        destinationWorkspaceId: str('The workspace to move it into.'),
      },
      required: ['workspaceId', 'taskId', 'destinationWorkspaceId'],
      run: ({ workspaceId, taskId, destinationWorkspaceId }) =>
        api.moveTask(workspaceId, taskId, destinationWorkspaceId),
    }),
    tool({
      name: 'updateTaskAllowAllCommands',
      description:
        'Turn off (or back on) the per-command permission prompts for one task. Turning it on ' +
        'lets the agent run anything in that task without asking, so confirm with the user first.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        allowAllCommands: bool('Whether the agent may run commands unprompted.'),
      },
      required: ['workspaceId', 'taskId', 'allowAllCommands'],
      run: ({ workspaceId, taskId, allowAllCommands }) =>
        api.updateTaskAllowAllCommands(workspaceId, taskId, allowAllCommands),
    }),
    tool({
      name: 'stopTask',
      description: 'Interrupt a running task. Refuses when whatever is connected has no stop.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID },
      required: ['workspaceId', 'taskId'],
      run: ({ workspaceId, taskId }) => api.stopTask(workspaceId, taskId),
    }),
    tool({
      name: 'setAgentModel',
      description:
        'Ask the workspace\'s connected agent to switch to a different model. Only offered where ' +
        'the agent reported models and said it can change them; asking is not the same as it ' +
        'having changed, which the workspace reports separately.',
      properties: {
        workspaceId: WORKSPACE_ID,
        modelId: str('The id of a model the agent listed as available.'),
      },
      required: ['workspaceId', 'modelId'],
      run: ({ workspaceId, modelId }) => api.setAgentModel(workspaceId, modelId),
    }),
    tool({
      name: 'deleteTask',
      description: 'Permanently delete a task and its conversation. This cannot be undone.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID },
      required: ['workspaceId', 'taskId'],
      destructive: true,
      run: ({ workspaceId, taskId }) => api.deleteTask(workspaceId, taskId),
    }),
    tool({
      name: 'sendPermissionVerdict',
      description:
        'Answer an agent asking permission to run something. This is the prompt the user sees in ' +
        'the task; only answer it on their instruction.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        requestId: str('The permission request being answered.'),
        behavior: str('The verdict, such as "allow" or "deny".'),
      },
      required: ['workspaceId', 'taskId', 'requestId', 'behavior'],
      run: ({ workspaceId, taskId, requestId, behavior }) =>
        api.sendPermissionVerdict(workspaceId, taskId, requestId, behavior),
    }),
    tool({
      name: 'respondToElicitation',
      description: 'Answer a question an agent asked the user inside a task.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        requestId: str('The question being answered.'),
        action: str('"accept", "decline" or "cancel".'),
        content: { type: 'object', description: 'The answer, shaped by the question that was asked.' },
      },
      required: ['workspaceId', 'taskId', 'requestId', 'action'],
      run: ({ workspaceId, taskId, requestId, action, content = {} }) =>
        api.respondToElicitation(workspaceId, taskId, requestId, action, content),
    }),
    tool({
      name: 'updateScheduledTask',
      description:
        'Change the template of a recurring task — what each future run will be given. ' +
        'The cron schedule must stay hourly at coarsest.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        title: str('New title.'),
        body: str('New body.'),
        assignee: str('"agent" or "human".'),
        cronSchedule: str('New cron expression.'),
        allowAllCommands: bool('Whether runs may execute commands unprompted.'),
      },
      required: ['workspaceId', 'taskId', 'title', 'body', 'assignee', 'cronSchedule'],
      run: ({ workspaceId, taskId, title, body, assignee, cronSchedule, allowAllCommands = false }) =>
        api.updateScheduledTask(workspaceId, taskId, title, body, assignee, cronSchedule, allowAllCommands),
    }),
    tool({
      name: 'getTaskCounts',
      description: 'How many tasks a workspace has in each status.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId }) => api.fetchTaskCounts(workspaceId),
    }),
    tool({
      name: 'getGlobalTaskStats',
      description: 'Task statistics across every workspace the user can see.',
      readOnly: true,
      run: () => api.fetchGlobalTaskStats(),
    }),

    // ---- Events ------------------------------------------------------------
    tool({
      name: 'listEvents',
      description:
        'Named signals that let a task in one workspace start tasks in another.',
      readOnly: true,
      run: () => api.fetchEvents(),
    }),
    tool({
      name: 'getEvent',
      description: 'One event, with its payload guidelines.',
      properties: { eventId: str('The event ID.') },
      required: ['eventId'],
      readOnly: true,
      run: ({ eventId }) => api.getEvent(eventId),
    }),
    tool({
      name: 'createEvent',
      description: 'Create a named event that tasks can publish.',
      properties: {
        name: str('Event name, as agents will publish it.'),
        payloadGuidelines: str('What a publisher should put in the payload.'),
      },
      required: ['name'],
      run: ({ name, payloadGuidelines = '' }) => api.createEvent(name, payloadGuidelines),
    }),
    tool({
      name: 'updateEvent',
      description: 'Change an event\'s payload guidelines.',
      properties: { eventId: str('The event ID.'), payloadGuidelines: str('New guidelines.') },
      required: ['eventId', 'payloadGuidelines'],
      run: ({ eventId, payloadGuidelines }) => api.updateEvent(eventId, payloadGuidelines),
    }),
    tool({
      name: 'deleteEvent',
      description: 'Permanently delete an event and its triggers.',
      properties: { eventId: str('The event ID.') },
      required: ['eventId'],
      destructive: true,
      run: ({ eventId }) => api.deleteEvent(eventId),
    }),
    tool({
      name: 'listEventTriggers',
      description: 'The triggers on an event: what each publish creates, and where.',
      properties: { eventId: str('The event ID.') },
      required: ['eventId'],
      readOnly: true,
      run: ({ eventId }) => api.fetchEventTriggers(eventId),
    }),
    tool({
      name: 'createEventTrigger',
      description:
        'Add a trigger, so publishing this event creates a task. In the body, {{EVENT_PAYLOAD}} ' +
        'and {{EVENT_FAQ}} are replaced with what the publisher sent; the title is always literal.',
      properties: {
        eventId: str('The event to trigger on.'),
        workspaceId: str('Where the task is created.'),
        title: str('Task title. Substitutions are not applied here.'),
        body: str('Task body, which may use {{EVENT_PAYLOAD}} and {{EVENT_FAQ}}.'),
        assignee: str('"agent" or "human". Defaults to "agent".'),
        cronSchedule: str('Optional cron expression.'),
        allowAllCommands: bool('Let the created task run commands unprompted.'),
        emitEventId: str('Optional second event published when the created task completes.'),
      },
      required: ['eventId', 'workspaceId', 'title'],
      run: ({ eventId, ...trigger }) => api.createEventTrigger(eventId, trigger),
    }),
    tool({
      name: 'updateEventTrigger',
      description: 'Change what a trigger creates.',
      properties: {
        eventId: str('The event the trigger belongs to.'),
        triggerId: str('The trigger being changed.'),
        workspaceId: str('Where the task is created.'),
        title: str('Task title.'),
        body: str('Task body.'),
        assignee: str('"agent" or "human".'),
        cronSchedule: str('Optional cron expression.'),
        allowAllCommands: bool('Let the created task run commands unprompted.'),
        emitEventId: str('Optional second event published on completion.'),
      },
      required: ['eventId', 'triggerId', 'workspaceId', 'title'],
      run: ({ eventId, triggerId, ...trigger }) => api.updateEventTrigger(eventId, triggerId, trigger),
    }),
    tool({
      name: 'deleteEventTrigger',
      description: 'Remove a trigger, so this event stops creating that task.',
      properties: { eventId: str('The event ID.'), triggerId: str('The trigger ID.') },
      required: ['eventId', 'triggerId'],
      destructive: true,
      run: ({ eventId, triggerId }) => api.deleteEventTrigger(eventId, triggerId),
    }),
    tool({
      name: 'listEventTasks',
      description: 'Tasks that were spawned by an event.',
      properties: { eventId: str('The event ID.') },
      required: ['eventId'],
      readOnly: true,
      run: ({ eventId }) => api.fetchEventTasks(eventId),
    }),

    // ---- Workflows ---------------------------------------------------------
    tool({
      name: 'listWorkflows',
      description: 'Workflows: chains of events and the tasks they create.',
      readOnly: true,
      run: () => api.fetchWorkflows(),
    }),
    tool({
      name: 'getWorkflow',
      description: 'One workflow, with its layout and starting event.',
      properties: { workflowId: str('The workflow ID.') },
      required: ['workflowId'],
      readOnly: true,
      run: ({ workflowId }) => api.getWorkflow(workflowId),
    }),
    tool({
      name: 'createWorkflow',
      description:
        'Create a workflow: a named chain in which one event\'s task publishes the next event. ' +
        'Add its steps with createWorkflowStep, or write the whole thing with replaceWorkflowFromText.',
      properties: {
        name: str('Workflow name.'),
        description: str('What the workflow is for.'),
        startEventId: str('Optional event that starts it.'),
      },
      required: ['name'],
      run: ({ name, description = '', startEventId = '' }) =>
        api.createWorkflow({ name, description, startEventId }),
    }),
    tool({
      name: 'updateWorkflow',
      description: 'Change a workflow. Only the fields given are altered.',
      properties: {
        workflowId: str('The workflow ID.'),
        name: str('New name.'),
        description: str('New description.'),
        startEventId: str('New starting event.'),
      },
      required: ['workflowId'],
      run: ({ workflowId, ...fields }) => api.updateWorkflow(workflowId, fields),
    }),
    tool({
      name: 'deleteWorkflow',
      description: 'Permanently delete a workflow and its steps.',
      properties: { workflowId: str('The workflow ID.') },
      required: ['workflowId'],
      destructive: true,
      run: ({ workflowId }) => api.deleteWorkflow(workflowId),
    }),
    tool({
      name: 'listWorkflowSteps',
      description: 'The steps of a workflow: which event creates which task, where.',
      properties: { workflowId: str('The workflow ID.') },
      required: ['workflowId'],
      readOnly: true,
      run: ({ workflowId }) => api.fetchWorkflowSteps(workflowId),
    }),
    tool({
      name: 'createWorkflowStep',
      description:
        'Add a step: when the given event fires, create this task. Refused when it would ' +
        'introduce a cycle, and the refusal says which connection was rejected.',
      properties: {
        workflowId: str('The workflow ID.'),
        eventId: str('The event this step listens for.'),
        workspaceId: str('Where the task is created.'),
        title: str('Task title.'),
        body: str('Task body.'),
        assignee: str('"agent" or "human". Defaults to "agent".'),
        allowAllCommands: bool('Let the created task run commands unprompted.'),
        emitEventId: str('Optional event published when the created task completes.'),
      },
      required: ['workflowId', 'eventId', 'workspaceId', 'title'],
      run: ({ workflowId, ...step }) => api.createWorkflowStep(workflowId, step),
    }),
    tool({
      name: 'deleteWorkflowStep',
      description: 'Remove one step from a workflow.',
      properties: { workflowId: str('The workflow ID.'), stepId: str('The step ID.') },
      required: ['workflowId', 'stepId'],
      destructive: true,
      run: ({ workflowId, stepId }) => api.deleteWorkflowStep(workflowId, stepId),
    }),
    tool({
      name: 'listWorkflowTasks',
      description: 'Tasks a workflow has created.',
      properties: { workflowId: str('The workflow ID.') },
      required: ['workflowId'],
      readOnly: true,
      run: ({ workflowId }) => api.fetchWorkflowTasks(workflowId),
    }),
    tool({
      name: 'getWorkflowText',
      description:
        'A workflow as editable text — the whole thing in one document, which is usually easier ' +
        'to reason about than the steps one at a time.',
      properties: { workflowId: str('The workflow ID.') },
      required: ['workflowId'],
      readOnly: true,
      run: ({ workflowId }) => api.fetchWorkflowText(workflowId),
    }),
    tool({
      name: 'replaceWorkflowFromText',
      description:
        'Replace a workflow with the one described by this text. Everything not in the text is ' +
        'removed, so read it with getWorkflowText and edit that rather than composing from scratch.',
      properties: { workflowId: str('The workflow ID.'), text: str('The full workflow document.') },
      required: ['workflowId', 'text'],
      destructive: true,
      run: ({ workflowId, text }) => api.replaceWorkflowFromText(workflowId, text),
    }),
  ]
}
