// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest';

import { createToolCatalogue } from '../src/webmcp/tools';
import * as api from '../src/api';
import { MIN_FORK_VERSION } from '../src/composables/useMachineFormat';
import { createRouter, createMemoryHistory } from 'vue-router';
import { routes } from '../src/app';

/**
 * An input carrying every field any tool asks for, so one object can drive the
 * whole catalogue. Tools destructure what they need and ignore the rest.
 */
const EVERY_FIELD = {
  workspaceId: 'ws1',
  taskId: 't1',
  destinationWorkspaceId: 'ws2',
  eventId: 'e1',
  triggerId: 'tr1',
  workflowId: 'wf1',
  modelId: 'gemini-2.5-pro',
  maxConcurrency: 4,
  stepId: 'st1',
  requestId: 'req1',
  path: '/events',
  name: 'a name',
  content: 'some content',
  title: 'a title',
  body: 'a body',
  text: 'some text',
  description: 'a description',
  status: 'ongoing',
  assignee: 'agent',
  behavior: 'allow',
  action: 'accept',
  content: { answer: 'yes' },
  order: 3,
  allowAllCommands: true,
  cronSchedule: '0 9 * * *',
  channelId: 'C1',
  channelName: 'general',
  payloadGuidelines: 'what to send',
  startEventId: 'e0',
  emitEventId: 'e2',
  includeArchived: true,
  range: '30d',
};

/** An API stand-in that records what the catalogue asked of it. */
function recordingApi() {
  const calls = [];
  const stub = new Proxy(
    {},
    {
      get: (_target, method) => (...args) => {
        calls.push({ method, args });
        return Promise.resolve({ ok: true });
      },
    }
  );
  return { stub, calls };
}

const build = (overrides = {}) =>
  createToolCatalogue({
    api: recordingApi().stub,
    navigate: vi.fn().mockResolvedValue(undefined),
    currentPage: () => ({ path: '/', params: {}, query: {} }),
    ...overrides,
  });

describe('the catalogue as a whole', () => {
  const catalogue = build();

  it('offers a substantial set of tools', () => {
    expect(catalogue.length).toBeGreaterThan(40);
  });

  // The machines pages show an old agentrqd in red; an agent reading the
  // same machines should be told the same thing.
  it('says which agentrqd is too old wherever a machine version comes back', () => {
    for (const name of ['listMachines', 'getMachine']) {
      const t = catalogue.find((c) => c.name === name);
      expect(t.description, name).toContain(`older than ${MIN_FORK_VERSION} should be updated`);
    }
  });

  it('names every tool exactly once', () => {
    const names = catalogue.map((t) => t.name);

    expect(new Set(names).size).toBe(names.length);
  });

  it('describes every tool well enough for an agent to choose it', () => {
    for (const t of catalogue) {
      expect(t.name, `${t.name} name`).toMatch(/^[a-zA-Z][a-zA-Z0-9_]*$/);
      expect(t.description.length, `${t.name} description`).toBeGreaterThan(20);
      expect(typeof t.execute, `${t.name} execute`).toBe('function');
    }
  });

  // The server refuses a site's whole announcement over any of these limits
  // (backend/internal/controller/sitetools/limits.go), so one long description
  // silently unshares the app from every workspace.
  it('stays within the limits the server accepts from a shared site', () => {
    expect(catalogue.length).toBeLessThanOrEqual(128);
    for (const t of catalogue) {
      expect(new TextEncoder().encode(t.description).length, `${t.name} description bytes`).toBeLessThanOrEqual(1024);
      expect(JSON.stringify(t.inputSchema).length, `${t.name} schema bytes`).toBeLessThanOrEqual(32 * 1024);
    }
  });

  it('gives every tool an object schema listing its required fields', () => {
    for (const t of catalogue) {
      expect(t.inputSchema.type, t.name).toBe('object');
      for (const field of t.inputSchema.required) {
        expect(t.inputSchema.properties, `${t.name} requires ${field}`).toHaveProperty(field);
      }
    }
  });

  it('describes every property, since the description is what the agent reads', () => {
    for (const t of catalogue) {
      for (const [field, schema] of Object.entries(t.inputSchema.properties)) {
        expect(schema.description, `${t.name}.${field}`).toBeTruthy();
      }
    }
  });

  it('annotates reads as read-only and nothing else', () => {
    const readOnly = catalogue.filter((t) => t.annotations.readOnlyHint).map((t) => t.name);

    expect(readOnly).toContain('listWorkspaces');
    expect(readOnly).toContain('getTask');
    expect(readOnly).toContain('getCurrentPage');
    expect(readOnly).not.toContain('createTask');
    expect(readOnly).not.toContain('deleteWorkspace');
  });

  it('marks everything that removes data as destructive', () => {
    const destructive = catalogue.filter((t) => t.annotations.destructiveHint).map((t) => t.name);

    expect(destructive.sort()).toEqual(
      [
        'deleteEvent',
        'deleteEventTrigger',
        'deleteMachine',
        'deleteTask',
        'deleteWorkflow',
        'deleteWorkflowStep',
        'deleteWorkspace',
        'deleteSkill',
        // Not deletes, but each can take away what an agent relied on: an
        // import with overwrite replaces skills the account already had, and
        // a skill turned off, for the account or in a workspace, is hidden
        // from those agents.
        'importSkills',
        'setSkillEnabled',
        'setWorkspaceSkillEnabled',
        // Not deletes either, and the most destructive things here: each
        // ends every session on a machine and starts them again empty.
        'restartDaemon',
        // Not a delete, but it ends work in progress: disabling a machine
        // closes its connection at once, and stopping a session loses whatever
        // the agent had not saved.
        'killSession',
        'setMachineEnabled',
        'replaceWorkflowFromText',
        // Archiving keeps the tasks but deletes the triggers and workflow
        // steps aimed at the workspace, and unarchiving does not restore them.
        'archiveWorkspace',
        // Not a delete: the tasks survive in the parent. But it stops the
        // fork's agent mid-work and the fork itself is gone.
        'mergeFork',
      ].sort()
    );
  });

  it('never marks a tool both read-only and consequential', () => {
    for (const t of catalogue) {
      expect(t.annotations.consequentialHint, t.name).toBe(!t.annotations.readOnlyHint);
    }
  });

  it('uses camelCase field names, as every other AgentRQ surface does', () => {
    for (const t of catalogue) {
      for (const field of Object.keys(t.inputSchema.properties)) {
        expect(field, `${t.name}.${field}`).not.toMatch(/_/);
      }
    }
  });
});

describe('parity with the interface', () => {
  /**
   * Functions in `api.js` that deliberately have no tool. Anything else must,
   * because the promise of this feature is that whatever the UI can do, an
   * agent can do — and `api.js` is what the UI can do.
   */
  const NOT_TOOLS = {
    // Instrumentation the app records about itself, not an action a person takes.
    recordTelemetry: 'internal telemetry, not a UI capability',
    // Builds a URL for an <img>/<a>; the bytes are fetched by the browser with
    // the session cookie, which an agent cannot replay outside the page.
    getAttachmentUrl: 'returns a URL the page renders, not an action',
    // Exempted deliberately, and this is the reason.
    //
    // Every other exemption here is "not really an action". This one is an
    // action, and a powerful one: the socket it addresses carries raw
    // keystrokes to a shell on somebody's machine. Handing a browser agent
    // that is a materially different grant from "anything the interface can
    // do" — the interface puts a person at a keyboard in front of it, and the
    // whole terminal is a person deciding what to type. It should be a
    // decision somebody makes on purpose, not something acquired as a side
    // effect of a parity rule.
    terminalSocketUrl: 'raw keystrokes into a remote shell; a deliberate non-grant',
    // The credential half of the same non-grant. A ticket opens exactly one
    // terminal socket, so a tool that minted one would be the grant above
    // wearing a different name.
    terminalTicket: 'mints the credential for that same socket; the same non-grant',
    // Where the server is, for the two callers that have to address it from
    // outside the renderer. Not an action.
    serverOrigin: 'returns an address the page uses, not an action',
  };

  const apiFunctions = Object.entries(api)
    .filter(([, value]) => typeof value === 'function')
    .map(([name]) => name)
    .filter((name) => !(name in NOT_TOOLS));

  /**
   * Drive every tool and collect which API functions the catalogue used.
   *
   * Twice over: some tools choose between two API calls depending on what they
   * were given — `listTasks` is per-workspace or global — so a single input
   * would leave one of the two branches unvisited and the parity claim
   * unproven. The empty run is expected to throw for tools with required
   * fields, and those throws are not what is being measured.
   */
  const used = (() => {
    const { stub, calls } = recordingApi();
    const catalogue = createToolCatalogue({
      api: stub,
      navigate: vi.fn().mockResolvedValue(undefined),
      currentPage: () => ({ path: '/', params: {}, query: {} }),
    });
    const drive = (input) =>
      Promise.all(catalogue.map((t) => Promise.resolve().then(() => t.execute(input, {})).catch(() => {})));
    return drive(EVERY_FIELD)
      .then(() => drive({}))
      .then(() => new Set(calls.map((c) => c.method)));
  })();

  it.each(apiFunctions)('exposes %s', async (name) => {
    expect(await used).toContain(name);
  });

  it('exempts only what it documents', () => {
    // Guards the exemption list itself: a name removed from api.js must be
    // removed from here too, or the list silently stops meaning anything.
    for (const name of Object.keys(NOT_TOOLS)) {
      expect(api, name).toHaveProperty(name);
    }
  });
});

describe('each tool calls the interface the way the UI does', () => {
  /**
   * One row per tool: the input an agent might send, and the API call it must
   * turn into. Positional arguments are the point — a misplaced one is a bug
   * no schema catches.
   */
  const CASES = [
    ['getCurrentUser', {}, 'fetchUser', []],
    ['listWorkspaces', { includeArchived: true }, 'fetchWorkspaces', [true]],
    ['listWorkspaces', {}, 'fetchWorkspaces', [false]],
    ['getWorkspace', { workspaceId: 'ws1' }, 'getWorkspace', ['ws1']],
    [
      'createWorkspace',
      { name: 'Ops', description: 'Run things' },
      'createWorkspace',
      ['Ops', 'Run things', '', '', ''],
    ],
    [
      'createWorkspace',
      { name: 'Ops', description: 'Run things', icon: '🛠', selfLearningLoopNote: 'note', workingDirectory: '/srv' },
      'createWorkspace',
      ['Ops', 'Run things', '🛠', 'note', '/srv'],
    ],
    ['updateWorkspace', { workspaceId: 'ws1', name: 'New' }, 'updateWorkspace', ['ws1', { name: 'New' }]],
    ['archiveWorkspace', { workspaceId: 'ws1' }, 'archiveWorkspace', ['ws1']],
    ['unarchiveWorkspace', { workspaceId: 'ws1' }, 'unarchiveWorkspace', ['ws1']],
    ['deleteWorkspace', { workspaceId: 'ws1' }, 'deleteWorkspace', ['ws1']],
    ['forkWorkspace', { workspaceId: 'ws1' }, 'forkWorkspace', ['ws1', { name: '' }]],
    ['forkWorkspace', { workspaceId: 'ws1', name: 'try' }, 'forkWorkspace', ['ws1', { name: 'try' }]],
    ['mergeFork', { workspaceId: 'ws2' }, 'mergeFork', ['ws2', { deleteFolder: false }]],
    ['mergeFork', { workspaceId: 'ws2', deleteFolder: true }, 'mergeFork', ['ws2', { deleteFolder: true }]],
    ['getWorkspaceToken', { workspaceId: 'ws1' }, 'getWorkspaceToken', ['ws1']],
    ['getWorkspaceStats', { workspaceId: 'ws1' }, 'fetchWorkspaceStats', ['ws1', '7d', 0, 0]],
    [
      'getWorkspaceStats',
      { workspaceId: 'ws1', range: '30d', from: 1, to: 2 },
      'fetchWorkspaceStats',
      ['ws1', '30d', 1, 2],
    ],
    ['listWorkspaceMemories', { workspaceId: 'ws1' }, 'fetchWorkspaceMemories', ['ws1']],
    [
      'getWorkspaceMemory',
      { workspaceId: 'ws1', name: 'MEMORY.md' },
      'getWorkspaceMemory',
      ['ws1', 'MEMORY.md'],
    ],
    ['searchSkills', { q: 'debug', limit: 5 }, 'searchSkills', [{ q: 'debug', limit: 5, offset: undefined }]],
    ['getSkill', { name: 'tdd' }, 'getSkill', ['tdd']],
    ['getSkillFile', { name: 'tdd', path: 'SKILL.md' }, 'getSkillFile', ['tdd', 'SKILL.md']],
    [
      'importSkills',
      { url: 'https://github.com/a/b', overwrite: true, skills: ['guard'], workspaceIds: ['ws1', 'ws2'] },
      'importSkills',
      ['https://github.com/a/b', true, ['guard'], ['ws1', 'ws2']],
    ],
    ['deleteSkill', { name: 'tdd' }, 'deleteSkill', ['tdd']],
    ['setSkillEnabled', { name: 'tdd', enabled: false }, 'setSkillEnabled', ['tdd', false]],
    ['searchWorkspaceSkills', { workspaceId: 'ws1', q: 'debug' }, 'searchWorkspaceSkills', ['ws1', { q: 'debug', limit: undefined, offset: undefined }]],
    ['getWorkspaceSkill', { workspaceId: 'ws1', name: 'tdd' }, 'getWorkspaceSkill', ['ws1', 'tdd']],
    ['getWorkspaceSkillFile', { workspaceId: 'ws1', name: 'tdd', path: 'SKILL.md' }, 'getWorkspaceSkillFile', ['ws1', 'tdd', 'SKILL.md']],
    ['setWorkspaceSkillEnabled', { workspaceId: 'ws1', name: 'tdd', enabled: true }, 'setWorkspaceSkillEnabled', ['ws1', 'tdd', true]],
    [
      'setWorkspaceSlackChannel',
      { workspaceId: 'ws1', channelId: 'C1', channelName: 'general' },
      'setWorkspaceSlackChannel',
      ['ws1', 'C1', 'general'],
    ],
    ['removeWorkspaceSlackChannel', { workspaceId: 'ws1' }, 'removeWorkspaceSlackChannel', ['ws1']],

    ['listTasks', { workspaceId: 'ws1', status: 'ongoing' }, 'fetchTasks', ['ws1', { status: 'ongoing' }]],
    ['listTasks', { status: 'ongoing' }, 'fetchGlobalTasks', [{ status: 'ongoing' }]],
    ['listTasks', {}, 'fetchGlobalTasks', [{}]],
    ['getTask', { workspaceId: 'ws1', taskId: 't1' }, 'getTask', ['ws1', 't1']],
    [
      'createTask',
      { workspaceId: 'ws1', title: 'T', body: 'B' },
      'createTask',
      ['ws1', 'T', 'B', 'agent', [], 'notstarted', '', false, '', '', false],
    ],
    [
      'createTask',
      {
        workspaceId: 'ws1',
        title: 'T',
        body: 'B',
        assignee: 'human',
        status: 'ongoing',
        cronSchedule: '0 9 * * *',
        allowAllCommands: true,
        eventId: 'e1',
        workflowId: 'wf1',
        clearContext: true,
      },
      'createTask',
      ['ws1', 'T', 'B', 'human', [], 'ongoing', '0 9 * * *', true, 'e1', 'wf1', true],
    ],
    ['replyToTask', { workspaceId: 'ws1', taskId: 't1', text: 'hi' }, 'replyToTask', ['ws1', 't1', 'hi']],
    [
      'respondToTask',
      { workspaceId: 'ws1', taskId: 't1', action: 'accept' },
      'respondToTask',
      ['ws1', 't1', 'accept', ''],
    ],
    [
      'respondToTask',
      { workspaceId: 'ws1', taskId: 't1', action: 'reject', text: 'no' },
      'respondToTask',
      ['ws1', 't1', 'reject', 'no'],
    ],
    ['forkTask', { workspaceId: 'ws1', taskId: 't1', messageId: 'm1' }, 'forkTask', ['ws1', 't1', 'm1']],
    [
      'updateTaskStatus',
      { workspaceId: 'ws1', taskId: 't1', status: 'completed' },
      'updateTaskStatus',
      ['ws1', 't1', 'completed'],
    ],
    [
      'updateTaskAssignee',
      { workspaceId: 'ws1', taskId: 't1', assignee: 'human' },
      'updateTaskAssignee',
      ['ws1', 't1', 'human'],
    ],
    [
      'updateTaskTitle',
      { workspaceId: 'ws1', taskId: 't1', title: 'Fix the login page' },
      'updateTaskTitle',
      ['ws1', 't1', 'Fix the login page'],
    ],
    ['updateTaskOrder', { workspaceId: 'ws1', taskId: 't1', order: 4 }, 'updateTaskOrder', ['ws1', 't1', 4]],
    [
      'moveTask',
      { workspaceId: 'ws1', taskId: 't1', destinationWorkspaceId: 'ws2' },
      'moveTask',
      ['ws1', 't1', 'ws2'],
    ],
    [
      'updateTaskAllowAllCommands',
      { workspaceId: 'ws1', taskId: 't1', allowAllCommands: true },
      'updateTaskAllowAllCommands',
      ['ws1', 't1', true],
    ],
    ['stopTask', { workspaceId: 'ws1', taskId: 't1' }, 'stopTask', ['ws1', 't1']],
    ['deleteTask', { workspaceId: 'ws1', taskId: 't1' }, 'deleteTask', ['ws1', 't1']],
    [
      'sendPermissionVerdict',
      { workspaceId: 'ws1', taskId: 't1', requestId: 'r1', behavior: 'allow' },
      'sendPermissionVerdict',
      ['ws1', 't1', 'r1', 'allow'],
    ],
    [
      'respondToElicitation',
      { workspaceId: 'ws1', taskId: 't1', requestId: 'r1', action: 'accept', content: { a: 1 } },
      'respondToElicitation',
      ['ws1', 't1', 'r1', 'accept', { a: 1 }],
    ],
    [
      'respondToElicitation',
      { workspaceId: 'ws1', taskId: 't1', requestId: 'r1', action: 'decline' },
      'respondToElicitation',
      ['ws1', 't1', 'r1', 'decline', {}],
    ],
    [
      'updateScheduledTask',
      { workspaceId: 'ws1', taskId: 't1', title: 'T', body: 'B', assignee: 'agent', cronSchedule: '0 9 * * *' },
      'updateScheduledTask',
      ['ws1', 't1', 'T', 'B', 'agent', '0 9 * * *', false],
    ],
    ['getTaskCounts', { workspaceId: 'ws1' }, 'fetchTaskCounts', ['ws1']],
    ['getGlobalTaskStats', {}, 'fetchGlobalTaskStats', []],

    ['listEvents', {}, 'fetchEvents', []],
    ['getEvent', { eventId: 'e1' }, 'getEvent', ['e1']],
    ['createEvent', { name: 'built' }, 'createEvent', ['built', '']],
    ['createEvent', { name: 'built', payloadGuidelines: 'what' }, 'createEvent', ['built', 'what']],
    ['updateEvent', { eventId: 'e1', payloadGuidelines: 'what' }, 'updateEvent', ['e1', 'what']],
    ['deleteEvent', { eventId: 'e1' }, 'deleteEvent', ['e1']],
    ['listEventTriggers', { eventId: 'e1' }, 'fetchEventTriggers', ['e1']],
    [
      'createEventTrigger',
      { eventId: 'e1', workspaceId: 'ws1', title: 'T', body: 'B' },
      'createEventTrigger',
      ['e1', { workspaceId: 'ws1', title: 'T', body: 'B' }],
    ],
    [
      'updateEventTrigger',
      { eventId: 'e1', triggerId: 'tr1', workspaceId: 'ws1', title: 'T' },
      'updateEventTrigger',
      ['e1', 'tr1', { workspaceId: 'ws1', title: 'T' }],
    ],
    ['deleteEventTrigger', { eventId: 'e1', triggerId: 'tr1' }, 'deleteEventTrigger', ['e1', 'tr1']],
    ['listEventTasks', { eventId: 'e1' }, 'fetchEventTasks', ['e1']],

    ['listWorkflows', {}, 'fetchWorkflows', []],
    ['getWorkflow', { workflowId: 'wf1' }, 'getWorkflow', ['wf1']],
    [
      'createWorkflow',
      { name: 'Ship it' },
      'createWorkflow',
      [{ name: 'Ship it', description: '', startEventId: '' }],
    ],
    [
      'createWorkflow',
      { name: 'Ship it', description: 'D', startEventId: 'e0' },
      'createWorkflow',
      [{ name: 'Ship it', description: 'D', startEventId: 'e0' }],
    ],
    ['updateWorkflow', { workflowId: 'wf1', name: 'New' }, 'updateWorkflow', ['wf1', { name: 'New' }]],
    ['deleteWorkflow', { workflowId: 'wf1' }, 'deleteWorkflow', ['wf1']],
    ['listWorkflowSteps', { workflowId: 'wf1' }, 'fetchWorkflowSteps', ['wf1']],
    [
      'createWorkflowStep',
      { workflowId: 'wf1', eventId: 'e1', workspaceId: 'ws1', title: 'T' },
      'createWorkflowStep',
      ['wf1', { eventId: 'e1', workspaceId: 'ws1', title: 'T' }],
    ],
    ['deleteWorkflowStep', { workflowId: 'wf1', stepId: 'st1' }, 'deleteWorkflowStep', ['wf1', 'st1']],
    ['listWorkflowTasks', { workflowId: 'wf1' }, 'fetchWorkflowTasks', ['wf1']],
    ['getWorkflowText', { workflowId: 'wf1' }, 'fetchWorkflowText', ['wf1']],
    [
      'replaceWorkflowFromText',
      { workflowId: 'wf1', text: 'the doc' },
      'replaceWorkflowFromText',
      ['wf1', 'the doc'],
    ],
  ];

  it.each(CASES)('%s(%o) calls %s', async (toolName, input, method, args) => {
    const { stub, calls } = recordingApi();
    const catalogue = build({ api: stub });
    const found = catalogue.find((t) => t.name === toolName);

    await found.execute(input, { signal: undefined });

    expect(calls).toEqual([{ method, args }]);
  });
});

describe('the tools that only WebMCP can offer', () => {
  it('reports where the user is, so "this task" can be resolved', async () => {
    const page = { path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} };
    const catalogue = build({ currentPage: () => page });

    const result = await catalogue.find((t) => t.name === 'getCurrentPage').execute({}, {});

    expect(result).toEqual(page);
  });

  it('moves the user to a page and reports where they landed', async () => {
    const navigate = vi.fn().mockResolvedValue(undefined);
    // A guard may send them somewhere else; the answer is the page, not the ask.
    const landed = { path: '/login', params: {}, query: {} };
    const catalogue = build({ navigate, currentPage: () => landed });

    const result = await catalogue.find((t) => t.name === 'navigate').execute({ path: '/events' }, {});

    expect(navigate).toHaveBeenCalledWith('/events');
    expect(result).toEqual({ navigatedTo: '/events', page: landed });
  });

  it.each([
    ['a full URL', 'https://evil.example/'],
    ['a protocol-relative URL', '//evil.example/'],
    ['a relative path', 'events'],
    ['no path at all', undefined],
  ])('refuses %s without moving the user', async (_label, path) => {
    const navigate = vi.fn().mockResolvedValue(undefined);
    const catalogue = build({ navigate });

    await expect(catalogue.find((t) => t.name === 'navigate').execute({ path }, {})).rejects.toThrow(
      'in-app path beginning with "/"'
    );
    expect(navigate).not.toHaveBeenCalled();
  });

  it('lets an API failure reach the agent rather than swallowing it', async () => {
    // The agent has to be told the difference between "done" and "refused".
    const catalogue = build({ api: { getTask: () => Promise.reject(new Error('Failed to fetch task')) } });

    await expect(
      catalogue.find((t) => t.name === 'getTask').execute({ workspaceId: 'ws1', taskId: 't1' }, {})
    ).rejects.toThrow('Failed to fetch task');
  });
});

// A write tool declares the page it acts on, so the person sees what an agent
// did. Reads and `navigate` (the move itself) are exempt; nothing else is.
describe('the page every write tool shows', () => {
  const router = createRouter({ history: createMemoryHistory(), routes });
  const writes = build().filter((t) => !t.annotations.readOnlyHint && t.name !== 'navigate');

  it('is declared by every tool that is not read-only', () => {
    expect(writes.length).toBeGreaterThan(40);
    for (const t of writes) {
      expect(typeof (t.screen?.before ?? t.screen?.after), `${t.name} screen`).toBe('function');
    }
  });

  it('is not declared by a read, or by navigate', () => {
    for (const t of build().filter((c) => c.annotations.readOnlyHint || c.name === 'navigate')) {
      expect(t.screen, t.name).toBeUndefined();
    }
  });

  it('is a page the app has, however the tool is called', () => {
    // Results shaped as the API answers, and empty ones: a tool that only
    // knows its page from the result must still land somewhere.
    const results = [
      {},
      { workspace: { id: 'w9' }, task: { id: 't9' }, event: { id: 'e9' }, workflow: { id: 'wf9' }, parentId: 'p9' },
    ];
    for (const t of writes) {
      for (const result of results) {
        const path = t.screen.before ? t.screen.before(EVERY_FIELD) : t.screen.after(EVERY_FIELD, result);
        expect(router.resolve(path).matched.length, `${t.name} -> ${path}`).toBeGreaterThan(0);
        expect(path, t.name).not.toMatch(/undefined|\/\/|\.\./);
      }
    }
  });

  it('follows the result to the page a tool made', () => {
    const by = (name) => writes.find((t) => t.name === name).screen;

    expect(by('createTask').after({ workspaceId: 'w1' }, { task: { id: 't9' } })).toBe('/workspaces/w1/tasks/t9');
    expect(by('createWorkspace').after({}, { workspace: { id: 'w9' } })).toBe('/workspaces/w9');
    expect(by('forkWorkspace').after({ workspaceId: 'w1' }, { workspace: { id: 'w9' } })).toBe('/workspaces/w9');
    expect(by('mergeFork').after({ workspaceId: 'w1' }, { parentId: 'p9' })).toBe('/workspaces/p9');
    expect(by('forkTask').after({ workspaceId: 'w1', taskId: 't1' }, { task: { id: 't9' } })).toBe('/workspaces/w1/tasks/t9');
    expect(by('createEvent').after({}, { event: { id: 'e9' } })).toBe('/events/e9');
    expect(by('createWorkflow').after({}, { workflow: { id: 'wf9' } })).toBe('/workflows/wf9');
  });

  it('falls back to where the thing lives when the result names nothing', () => {
    const by = (name) => writes.find((t) => t.name === name).screen;

    expect(by('createTask').after({ workspaceId: 'w1' }, {})).toBe('/workspaces/w1');
    expect(by('createWorkspace').after({}, {})).toBe('/');
    expect(by('forkWorkspace').after({ workspaceId: 'w1' }, {})).toBe('/workspaces/w1');
    expect(by('mergeFork').after({ workspaceId: 'w1' }, {})).toBe('/workspaces/w1');
    expect(by('forkTask').after({ workspaceId: 'w1', taskId: 't1' }, {})).toBe('/workspaces/w1/tasks/t1');
    expect(by('createEvent').after({}, {})).toBe('/events');
    expect(by('createWorkflow').after({}, {})).toBe('/workflows');
  });

  it('opens the Skills page, or the skill, for an account-wide skill change', () => {
    const by = (name, input) => writes.find((t) => t.name === name).screen.before(input);

    expect(by('importSkills', {})).toBe('/skills');
    expect(by('deleteSkill', { name: 'tdd' })).toBe('/skills');
    expect(by('setSkillEnabled', { name: 'a/b' })).toBe('/skills/a%2Fb');
  });

  it('encodes an ID so it cannot become another page', () => {
    const { screen } = writes.find((t) => t.name === 'restartDaemon');

    expect(screen.before({ machineId: '../events' })).toBe('/machines/..%2Fevents');
  });

  it('opens the settings tab a setting lives on', () => {
    const by = (name) => writes.find((t) => t.name === name).screen.before({ workspaceId: 'w1' });

    expect(by('setWorkspaceSkillEnabled')).toBe('/workspaces/w1/settings?tab=skills');
    expect(by('setWorkspaceSlackChannel')).toBe('/workspaces/w1/settings?tab=slack');
    expect(by('updateWorkspace')).toBe('/workspaces/w1/settings');
  });
});
