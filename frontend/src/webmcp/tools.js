// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

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

import { MIN_FORK_VERSION } from '../composables/useMachineFormat.js'

/** JSON Schema fragments, named once so the catalogue reads as intent. */
const str = (description) => ({ type: 'string', description })
const bool = (description) => ({ type: 'boolean', description })
const int = (description) => ({ type: 'integer', description })

const WORKSPACE_ID = str('The workspace ID (base62), as it appears in the URL.')
const TASK_ID = str('The task ID (base62), as it appears in the URL.')
const MACHINE_ID = str('The machine ID (base62), as it appears in the URL.')
// What the machines pages show in red next to an old version.
const DAEMON_VERSION_NOTE = ` An agentrqd older than ${MIN_FORK_VERSION} should be updated to use new features such as workspace forks.`

/**
 * Build a WebMCP tool descriptor.
 *
 * `readOnly` and `destructive` become the annotations an agent uses to decide
 * what it may do unattended. They are set from the operation itself rather than
 * left to each entry to remember: a tool that only reads is annotated as such,
 * and anything that removes data says so.
 */
function tool({ name, description, properties = {}, required = [], readOnly = false, destructive = false, screen, run }) {
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
    // Not part of what an agent sees: `withScreen` reads it and strips it.
    ...(screen && { screen }),
  }
}

// An ID from an agent goes into a path, so it is encoded: "../x" must not
// become a different page.
const seg = (id) => encodeURIComponent(id ?? '')
const workspacePage = ({ workspaceId }) => `/workspaces/${seg(workspaceId)}`
const settingsPage = (tab) => ({ workspaceId }) => `/workspaces/${seg(workspaceId)}/settings${tab ? `?tab=${tab}` : ''}`
const taskPage = ({ workspaceId, taskId }) => `/workspaces/${seg(workspaceId)}/tasks/${seg(taskId)}`
const machinePage = ({ machineId }) => `/machines/${seg(machineId)}`
const eventPage = ({ eventId }) => `/events/${seg(eventId)}`
const workflowPage = ({ workflowId }) => `/workflows/${seg(workflowId)}`

/**
 * The page a write tool acts on, so the person sees what the agent did.
 *
 * `before` is a function of the arguments and is followed before the tool runs;
 * `after` also sees the result, for a page that exists only once the tool has
 * made it. Every tool that is not read-only declares one — a test fails when
 * one does not — except `navigate`, which is the move itself.
 */
const before = (page) => ({ before: page })
const after = (page) => ({ after: page })

/**
 * Everything the interface can do.
 *
 * @param {object} deps
 * @param {object} deps.api the `src/api.js` module, or a stand-in
 * @param {(path: string) => Promise<unknown>} deps.navigate router push; rejects a
 *        path that matches no route
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
        'Take the user to an AgentRQ page. Pass the in-app path, ' +
        'optionally ?query; IDs are base62, from getCurrentPage or list tools. Pages:\n' +
        '- "/" every workspace, "/workspaces/new" creates one.\n' +
        '- "/kanban" or "/tasks/<filter>" tasks in all workspaces; <filter> is active, notstarted, pending ' +
        '(on the user), ongoing, completed or scheduled. Add "/<workspaceId>/<taskId>" to ' +
        'open one beside it, then "/instances" for a scheduled task\'s runs.\n' +
        '- "/workspaces/<workspaceId>" its tasks ("?filter=" as above); under it "/board", ' +
        '"/analytics", "/settings", "/settings/skills/<name>", "/tasks/new", "/tasks/<taskId>", and that plus "/instances" or ' +
        '"/edit". "/settings" takes "?tab=" general, setup, automations, notifications, memories, ' +
        'skills, slack, input, storage or danger.\n' +
        '- "/events", "/events/<eventId>", "/workflows", "/workflows/<workflowId>".\n' +
        '- "/machines", "/machines/<machineId>", "/sessions/<sessionId>" (a terminal).\n' +
        '- "/extensions", "/extensions/<name>/<pageId>" (desktop only).\n' +
        'Others are refused. Returns the page reached.',
      properties: { path: str('An in-app path beginning with "/", e.g. "/workspaces/<workspaceId>/board".') },
      required: ['path'],
      run: async ({ path }) => {
        // In-app only: "//host" would read as another origin, and a full URL is
        // not a route. Moving the user off the app is not this tool's to do.
        if (typeof path !== 'string' || !path.startsWith('/') || path.startsWith('//')) {
          throw new Error(`navigate takes an in-app path beginning with "/", not ${JSON.stringify(path)}`)
        }
        await navigate(path)
        // Where they landed, not what was asked: a guard may have redirected.
        return { navigatedTo: path, page: currentPage() }
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
      screen: after((_, r) => (r?.workspace?.id ? `/workspaces/${seg(r.workspace.id)}` : '/')),
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
      screen: before(settingsPage()),
      run: ({ workspaceId, ...fields }) => api.updateWorkspace(workspaceId, fields),
    }),
    tool({
      name: 'archiveWorkspace',
      description:
        'Archive a workspace, hiding it from the default list. Its tasks are kept, but the event ' +
        'triggers and workflow steps that create tasks in it are deleted, and unarchiving does not ' +
        'restore them.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      destructive: true,
      screen: before(workspacePage),
      run: ({ workspaceId }) => api.archiveWorkspace(workspaceId),
    }),
    tool({
      name: 'unarchiveWorkspace',
      description: 'Restore an archived workspace to the default list.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      screen: before(workspacePage),
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
      screen: before(() => '/'),
      run: ({ workspaceId }) => api.deleteWorkspace(workspaceId),
    }),
    tool({
      name: 'forkWorkspace',
      description:
        'Fork a workspace: a workspace of its own, with its own queue and agent, that shares the ' +
        "parent's settings, memory and skills and is merged back when its tasks are done. A fork, " +
        'the supervisor workspace and an archived workspace cannot be forked.',
      properties: {
        workspaceId: WORKSPACE_ID,
        name: str('Name for the fork, in kebab-case like other workspace names. Defaults to "<parent>-fork".'),
      },
      required: ['workspaceId'],
      screen: after((a, r) => (r?.workspace?.id ? `/workspaces/${seg(r.workspace.id)}` : workspacePage(a))),
      run: ({ workspaceId, name = '' }) => api.forkWorkspace(workspaceId, { name }),
    }),
    tool({
      name: 'mergeFork',
      description:
        "Merge a fork back into its parent: the fork's agent is stopped, every task moves to the " +
        "parent with its thread, and the fork is removed. Its folder on the machine is left unless " +
        "deleteFolder is true, which deletes it and keeps its git branch. " +
        'Refused while any task in the fork is not completed or rejected.',
      properties: {
        workspaceId: str('The fork to merge.'),
        deleteFolder: bool("Also delete the fork's folder on its machine. Defaults to false."),
      },
      required: ['workspaceId'],
      destructive: true,
      screen: after((a, r) => (r?.parentId ? `/workspaces/${seg(r.parentId)}` : workspacePage(a))),
      run: ({ workspaceId, deleteFolder = false }) => api.mergeFork(workspaceId, { deleteFolder }),
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
      name: 'getWorkspaceTaskLatency',
      description:
        'How long a workspace\'s tasks closed in a time range took, in seconds: start to close, worked, ' +
        'blocked and needing input, per hour, day or month and in total, as the p50, min or max. ' +
        'A bucket with no closed task has seconds null.',
      properties: {
        workspaceId: WORKSPACE_ID,
        range: str('Range such as "7d" or "30d". Defaults to "7d".'),
        from: int('Optional start as a Unix timestamp.'),
        to: int('Optional end as a Unix timestamp.'),
        aggregate: str('"p50" (the default), "min" or "max".'),
      },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId, range = '7d', from = 0, to = 0, aggregate = 'p50' }) =>
        api.fetchWorkspaceTaskLatency(workspaceId, range, from, to, aggregate),
    }),
    tool({
      name: 'getAccountTaskLatency',
      description:
        'getWorkspaceTaskLatency across every workspace the user owns.',
      properties: {
        range: str('Range such as "7d" or "30d". Defaults to "7d".'),
        from: int('Optional start as a Unix timestamp.'),
        to: int('Optional end as a Unix timestamp.'),
        aggregate: str('"p50" (the default), "min" or "max".'),
      },
      readOnly: true,
      run: ({ range = '7d', from = 0, to = 0, aggregate = 'p50' }) =>
        api.fetchUserTaskLatency(range, from, to, aggregate),
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
      name: 'searchWorkspaceSkills',
      description:
        'Find the skills a workspace\'s agents can load: its own and those other workspaces of the account ' +
        'share into it, with description, size and source, plus how many match in all. A shared-in skill ' +
        'carries sharedFromWorkspaceId and is read-only there. One with enabled false is turned off: it is ' +
        'kept, but agents do not see it. File contents are not included.',
      properties: {
        workspaceId: WORKSPACE_ID,
        q: str('Text to find in a skill\'s name or description, ignoring case; at least 3 characters. Leave it out to list every skill.'),
        limit: int('How many skills to return, at most 100. Leave it out to return every match.'),
        offset: int('How many matches to skip, for the next page.'),
      },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId, q, limit, offset }) => api.searchWorkspaceSkills(workspaceId, { q, limit, offset }),
    }),
    tool({
      name: 'getWorkspaceSkill',
      description: 'One skill of a workspace, with the paths and sizes of its files but not their content.',
      properties: { workspaceId: WORKSPACE_ID, name: str('The skill\'s name, as searchWorkspaceSkills reports it.') },
      required: ['workspaceId', 'name'],
      readOnly: true,
      run: ({ workspaceId, name }) => api.getWorkspaceSkill(workspaceId, name),
    }),
    tool({
      name: 'getWorkspaceSkillFile',
      description: 'One file of a skill, in full. Start with SKILL.md, which says which other files matter.',
      properties: {
        workspaceId: WORKSPACE_ID,
        name: str('The skill\'s name.'),
        path: str('The file\'s path within the skill, like SKILL.md or references/guide.md.'),
      },
      required: ['workspaceId', 'name', 'path'],
      readOnly: true,
      run: ({ workspaceId, name, path }) => api.getWorkspaceSkillFile(workspaceId, name, path),
    }),
    tool({
      name: 'importWorkspaceSkills',
      description:
        'Import skills from a public GitHub repository into a workspace, e.g. https://github.com/obra/superpowers ' +
        'or a /tree/<ref>/<path> link to part of one. Returns what was imported and what was skipped, with why. ' +
        'A repository too large to import whole imports nothing and returns `candidates`; import again naming the wanted ones in `skills`.',
      properties: {
        workspaceId: WORKSPACE_ID,
        url: str('The GitHub link.'),
        overwrite: bool('Replace skills this workspace already has under the same name.'),
        skills: { type: 'array', items: { type: 'string' }, description: 'Import only these skills, by the `path` a candidate gives.' },
      },
      required: ['workspaceId', 'url'],
      // With overwrite, it replaces skills the workspace already had.
      destructive: true,
      screen: before(settingsPage('skills')),
      run: ({ workspaceId, url, overwrite, skills }) => api.importWorkspaceSkills(workspaceId, url, overwrite, skills),
    }),
    tool({
      name: 'deleteWorkspaceSkill',
      description: 'Delete one of a workspace\'s own skills, with all its files and shares. Cannot be undone.',
      properties: { workspaceId: WORKSPACE_ID, name: str('The skill\'s name.') },
      required: ['workspaceId', 'name'],
      destructive: true,
      screen: before(settingsPage('skills')),
      run: ({ workspaceId, name }) => api.deleteWorkspaceSkill(workspaceId, name),
    }),
    tool({
      name: 'setWorkspaceSkillEnabled',
      description:
        'Turn one of a workspace\'s own skills on or off for agents. Off, the skill is kept and still listed here, but agents ' +
        'no longer find or load it, in this workspace or any it is shared into. Skills are on when created.',
      properties: { workspaceId: WORKSPACE_ID, name: str('The skill\'s name.'), enabled: bool('true to make it available to agents, false to hide it from them.') },
      required: ['workspaceId', 'name', 'enabled'],
      // Off takes away a skill agents may be relying on, as unsharing does.
      destructive: true,
      screen: before(settingsPage('skills')),
      run: ({ workspaceId, name, enabled }) => api.setWorkspaceSkillEnabled(workspaceId, name, enabled),
    }),
    tool({
      name: 'listWorkspaceSkillShares',
      description: 'The other workspaces one of a workspace\'s own skills is shared into.',
      properties: { workspaceId: WORKSPACE_ID, name: str('The skill\'s name.') },
      required: ['workspaceId', 'name'],
      readOnly: true,
      run: ({ workspaceId, name }) => api.fetchWorkspaceSkillShares(workspaceId, name),
    }),
    tool({
      name: 'shareWorkspaceSkill',
      description:
        'Share one of a workspace\'s own skills into another workspace of the same account, where it can be read but not changed.',
      properties: {
        workspaceId: WORKSPACE_ID,
        name: str('The skill\'s name.'),
        targetWorkspaceId: str('The workspace to share it into.'),
      },
      required: ['workspaceId', 'name', 'targetWorkspaceId'],
      screen: before(settingsPage('skills')),
      run: ({ workspaceId, name, targetWorkspaceId }) => api.shareWorkspaceSkill(workspaceId, name, targetWorkspaceId),
    }),
    tool({
      name: 'unshareWorkspaceSkill',
      description: 'Stop sharing a skill into a workspace; its agents can no longer load it.',
      properties: {
        workspaceId: WORKSPACE_ID,
        name: str('The skill\'s name.'),
        targetWorkspaceId: str('The workspace to stop sharing it into.'),
      },
      required: ['workspaceId', 'name', 'targetWorkspaceId'],
      destructive: true,
      screen: before(settingsPage('skills')),
      run: ({ workspaceId, name, targetWorkspaceId }) => api.unshareWorkspaceSkill(workspaceId, name, targetWorkspaceId),
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
      screen: before(settingsPage('slack')),
      run: ({ workspaceId, channelId, channelName }) =>
        api.setWorkspaceSlackChannel(workspaceId, channelId, channelName),
    }),
    tool({
      name: 'removeWorkspaceSlackChannel',
      description: 'Disconnect a workspace from its Slack channel.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      screen: before(settingsPage('slack')),
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
        clearContext: bool(
          'Send /clear to the agent before it picks this task up, so it starts on a clean ' +
          'context. Ignored when the workspace has no running Claude Code session.'
        ),
      },
      required: ['workspaceId', 'title', 'body'],
      screen: after((a, r) => (r?.task?.id ? taskPage({ ...a, taskId: r.task.id }) : workspacePage(a))),
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
        clearContext = false,
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
          workflowId,
          clearContext
        ),
    }),
    tool({
      name: 'replyToTask',
      description: 'Send a message in a task conversation, as the signed-in user.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, text: str('The message.') },
      required: ['workspaceId', 'taskId', 'text'],
      screen: before(taskPage),
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
      screen: before(taskPage),
      run: ({ workspaceId, taskId, action, text = '' }) =>
        api.respondToTask(workspaceId, taskId, action, text),
    }),
    tool({
      name: 'forkTask',
      description:
        'Fork a task: copy its conversation, up to and including one message, into a new task for the agent.',
      properties: {
        workspaceId: WORKSPACE_ID,
        taskId: TASK_ID,
        messageId: str('The last message to copy into the new task.'),
      },
      required: ['workspaceId', 'taskId', 'messageId'],
      screen: after((a, r) => (r?.task?.id ? taskPage({ ...a, taskId: r.task.id }) : taskPage(a))),
      run: ({ workspaceId, taskId, messageId }) => api.forkTask(workspaceId, taskId, messageId),
    }),
    tool({
      name: 'updateTaskStatus',
      description:
        'Set a task\'s status: notstarted, ongoing, completed, rejected, cron or blocked.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, status: str('The new status.') },
      required: ['workspaceId', 'taskId', 'status'],
      screen: before(taskPage),
      run: ({ workspaceId, taskId, status }) => api.updateTaskStatus(workspaceId, taskId, status),
    }),
    tool({
      name: 'updateTaskAssignee',
      description: 'Hand a task to the agent or back to a human.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, assignee: str('"agent" or "human".') },
      required: ['workspaceId', 'taskId', 'assignee'],
      screen: before(taskPage),
      run: ({ workspaceId, taskId, assignee }) => api.updateTaskAssignee(workspaceId, taskId, assignee),
    }),
    tool({
      name: 'updateTaskTitle',
      description: 'Rename a task. Only possible in the first 7 days after it was created.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, title: str('The new title.') },
      required: ['workspaceId', 'taskId', 'title'],
      screen: before(taskPage),
      run: ({ workspaceId, taskId, title }) => api.updateTaskTitle(workspaceId, taskId, title),
    }),
    tool({
      name: 'updateTaskOrder',
      description: 'Reorder a task on the board.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID, order: int('The new position.') },
      required: ['workspaceId', 'taskId', 'order'],
      screen: before(workspacePage),
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
      screen: before((a) => taskPage({ workspaceId: a.destinationWorkspaceId, taskId: a.taskId })),
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
      screen: before(taskPage),
      run: ({ workspaceId, taskId, allowAllCommands }) =>
        api.updateTaskAllowAllCommands(workspaceId, taskId, allowAllCommands),
    }),
    tool({
      name: 'stopTask',
      description: 'Interrupt a running task. Refuses when whatever is connected has no stop.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID },
      required: ['workspaceId', 'taskId'],
      screen: before(taskPage),
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
      screen: before(settingsPage()),
      run: ({ workspaceId, modelId }) => api.setAgentModel(workspaceId, modelId),
    }),
    tool({
      name: 'setAgentConcurrency',
      description:
        'Ask the workspace\'s connected gateway to run a different number of tasks at once. Only ' +
        'offered where the gateway reported a limit and said it can change it; the value may be ' +
        'clamped to the range it reported, and asking is not the same as it having changed, which ' +
        'the workspace reports separately.',
      properties: {
        workspaceId: WORKSPACE_ID,
        maxConcurrency: int('How many tasks to run at once, within the range the gateway reported.'),
      },
      required: ['workspaceId', 'maxConcurrency'],
      screen: before(settingsPage()),
      run: ({ workspaceId, maxConcurrency }) => api.setAgentConcurrency(workspaceId, maxConcurrency),
    }),
    tool({
      name: 'deleteTask',
      description: 'Permanently delete a task and its conversation. This cannot be undone.',
      properties: { workspaceId: WORKSPACE_ID, taskId: TASK_ID },
      required: ['workspaceId', 'taskId'],
      destructive: true,
      screen: before(workspacePage),
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
      screen: before(taskPage),
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
      screen: before(taskPage),
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
      screen: before(taskPage),
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

    // ---- Machines ----------------------------------------------------------
    //
    // A machine is a computer running agentrqd. These mirror what the machines
    // pages can do, with one deliberate omission: there is no tool that types
    // into a terminal. That is a materially different grant from "anything the
    // interface can do" and is exempted on purpose — see
    // `test/webmcpTools.test.js`.
    tool({
      name: 'listMachines',
      description:
        'Computers enrolled against this account that can host agents, with whether each is online and what it has left.' +
        DAEMON_VERSION_NOTE,
      readOnly: true,
      run: () => api.fetchMachines(),
    }),
    tool({
      name: 'getMachine',
      description: 'One machine: its state, and its memory, CPU and disk.' + DAEMON_VERSION_NOTE,
      properties: { machineId: MACHINE_ID },
      required: ['machineId'],
      readOnly: true,
      run: ({ machineId }) => api.getMachine(machineId),
    }),
    tool({
      name: 'getSession',
      description: 'One agent session: which machine and workspace it belongs to, and whether it is still running.',
      properties: { sessionId: str('The session ID (base62), as it appears in the URL.') },
      required: ['sessionId'],
      readOnly: true,
      run: ({ sessionId }) => api.getSession(sessionId),
    }),
    tool({
      name: 'listMachineSessions',
      description: 'The agent sessions that have run on a machine, newest first.',
      properties: { machineId: MACHINE_ID },
      required: ['machineId'],
      readOnly: true,
      run: ({ machineId }) => api.fetchMachineSessions(machineId),
    }),
    tool({
      name: 'getWorkspaceSession',
      description:
        'The agent session running for a workspace, or null if none is. Answers "which agent is working here, and can I watch it" in one call, rather than asking every machine in turn.',
      properties: { workspaceId: WORKSPACE_ID },
      required: ['workspaceId'],
      readOnly: true,
      run: ({ workspaceId }) => api.fetchWorkspaceSession(workspaceId),
    }),
    tool({
      name: 'renameMachine',
      description: 'Rename a machine. The name is what the machines list and the launcher show.',
      properties: { machineId: MACHINE_ID, name: str('The new name.') },
      required: ['machineId', 'name'],
      screen: before(machinePage),
      run: ({ machineId, name }) => api.updateMachine(machineId, { name }),
    }),
    tool({
      name: 'setMachineEnabled',
      description:
        'Turn a machine on or off. Disabling is the kill switch: its connection is closed immediately and the next one is refused.',
      properties: {
        machineId: MACHINE_ID,
        enabled: bool('False to disable the machine, true to allow it back.'),
      },
      required: ['machineId', 'enabled'],
      destructive: true,
      screen: before(machinePage),
      run: ({ machineId, enabled }) => api.updateMachine(machineId, { enabled }),
    }),
    tool({
      name: 'deleteMachine',
      description:
        'Remove a machine. Its token stops working, so enrolling it again means running the enrolment command on the machine itself.',
      properties: { machineId: MACHINE_ID },
      required: ['machineId'],
      destructive: true,
      screen: before(() => '/machines'),
      run: ({ machineId }) => api.deleteMachine(machineId),
    }),
    tool({
      name: 'createEnrolmentCode',
      description:
        'Mint a single-use code for enrolling a new machine. It is shown once and is useless without access to the machine being enrolled.',
      screen: before(() => '/machines'),
      run: () => api.createEnrolmentCode(),
    }),
    tool({
      name: 'listAcpAgents',
      description:
        'The acp-gateway agents a machine can run, for choosing an agent before launchAgent. Comes back empty ' +
        'rather than failing when the machine is offline or cannot be asked in time — free text still works either way.',
      properties: { machineId: MACHINE_ID },
      required: ['machineId'],
      readOnly: true,
      run: ({ machineId }) => api.fetchAcpAgents(machineId),
    }),
    tool({
      name: 'listAcpModels',
      description:
        'The models one acp-gateway agent supports, once an agent is chosen. Needs the workspace: the gateway has ' +
        'to find a real .mcp.json in its folder to answer this at all, so a workspace with no folder set, or one ' +
        'nothing has launched from yet, comes back with an empty list rather than an error.',
      properties: {
        workspaceId: WORKSPACE_ID,
        machineId: MACHINE_ID,
        agent: str('The acp-gateway agent id, from listAcpAgents.'),
      },
      required: ['workspaceId', 'machineId', 'agent'],
      readOnly: true,
      run: ({ workspaceId, machineId, agent }) => api.fetchAcpModels(workspaceId, machineId, agent),
    }),
    tool({
      name: 'launchAgent',
      description:
        'Start an agent for a workspace on a chosen machine. Answers before it has started: the session reports its own state.',
      properties: {
        workspaceId: WORKSPACE_ID,
        machineId: MACHINE_ID,
        kind: str('Which agent to run: claude-code or acp-gateway.'),
        model: str('The model, for acp-gateway.'),
        agent: str('The agent, for acp-gateway.'),
        cols: int('Terminal width in columns.'),
        rows: int('Terminal height in rows.'),
      },
      required: ['workspaceId', 'machineId', 'kind'],
      screen: before(workspacePage),
      run: ({ workspaceId, machineId, kind, model = '', agent = '', cols = 0, rows = 0 }) =>
        api.launchAgent(workspaceId, { machineId, kind, model, agent, cols, rows }),
    }),
    tool({
      name: 'restartDaemon',
      description:
        "Restart a machine's agentrqd (0.9.3 or newer), updating it first when given the version it offered (getMachine's availableVersion). This stops every session on that machine and starts them again in new terminals: scrollback and in-flight work are lost.",
      properties: {
        machineId: MACHINE_ID,
        version: str('The offered version to update to, exactly as the machine reported it. Leave it out to restart only.'),
      },
      required: ['machineId'],
      destructive: true,
      screen: before(machinePage),
      run: ({ machineId, version = '' }) => api.restartDaemon(machineId, version),
    }),
    tool({
      name: 'killSession',
      description: 'Ask a machine to end an agent session. Anything the agent has not saved is lost.',
      properties: { sessionId: str('The session ID (base62), as it appears in the URL.') },
      required: ['sessionId'],
      destructive: true,
      screen: before(() => '/machines'),
      run: ({ sessionId }) => api.killSession(sessionId),
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
      screen: after((_, r) => (r?.event?.id ? `/events/${seg(r.event.id)}` : '/events')),
      run: ({ name, payloadGuidelines = '' }) => api.createEvent(name, payloadGuidelines),
    }),
    tool({
      name: 'updateEvent',
      description: 'Change an event\'s payload guidelines.',
      properties: { eventId: str('The event ID.'), payloadGuidelines: str('New guidelines.') },
      required: ['eventId', 'payloadGuidelines'],
      screen: before(eventPage),
      run: ({ eventId, payloadGuidelines }) => api.updateEvent(eventId, payloadGuidelines),
    }),
    tool({
      name: 'deleteEvent',
      description: 'Permanently delete an event and its triggers.',
      properties: { eventId: str('The event ID.') },
      required: ['eventId'],
      destructive: true,
      screen: before(() => '/events'),
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
      screen: before(eventPage),
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
      screen: before(eventPage),
      run: ({ eventId, triggerId, ...trigger }) => api.updateEventTrigger(eventId, triggerId, trigger),
    }),
    tool({
      name: 'deleteEventTrigger',
      description: 'Remove a trigger, so this event stops creating that task.',
      properties: { eventId: str('The event ID.'), triggerId: str('The trigger ID.') },
      required: ['eventId', 'triggerId'],
      destructive: true,
      screen: before(eventPage),
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
      screen: after((_, r) => (r?.workflow?.id ? `/workflows/${seg(r.workflow.id)}` : '/workflows')),
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
      screen: before(workflowPage),
      run: ({ workflowId, ...fields }) => api.updateWorkflow(workflowId, fields),
    }),
    tool({
      name: 'deleteWorkflow',
      description: 'Permanently delete a workflow and its steps.',
      properties: { workflowId: str('The workflow ID.') },
      required: ['workflowId'],
      destructive: true,
      screen: before(() => '/workflows'),
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
      screen: before(workflowPage),
      run: ({ workflowId, ...step }) => api.createWorkflowStep(workflowId, step),
    }),
    tool({
      name: 'deleteWorkflowStep',
      description: 'Remove one step from a workflow.',
      properties: { workflowId: str('The workflow ID.'), stepId: str('The step ID.') },
      required: ['workflowId', 'stepId'],
      destructive: true,
      screen: before(workflowPage),
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
      screen: before(workflowPage),
      run: ({ workflowId, text }) => api.replaceWorkflowFromText(workflowId, text),
    }),
  ]
}
