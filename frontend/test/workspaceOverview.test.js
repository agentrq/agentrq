// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The workspaces page: forks under their parent's card, each card with its
 * first few Active tasks, two columns filled shortest-first, and the cards
 * kept current by the user's task events.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { createApp, h, ref } from 'vue';
import { createPinia } from 'pinia';

const push = vi.fn();
vi.mock('vue-router', () => ({ useRouter: () => ({ push }), useRoute: () => ({ params: {}, query: {} }) }));

const fetchWorkspaces = vi.fn();
const fetchTaskOverview = vi.fn();
const recordTelemetry = vi.fn();
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchWorkspaces: (...args) => fetchWorkspaces(...args),
  fetchTaskOverview: (...args) => fetchTaskOverview(...args),
  recordTelemetry: (...args) => recordTelemetry(...args),
  fetchGlobalTaskStats: async () => ({}),
  fetchUserStats: async () => ({}),
}));

// One stream the tests can push events into, and see opened and closed.
const handlers = new Set();
const connected = ref(false);
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({
    connect: () => { connected.value = true; },
    disconnect: () => { connected.value = false; },
    onEvent: (fn) => { handlers.add(fn); return () => handlers.delete(fn); },
  }),
}));
const emit = (event) => handlers.forEach((fn) => fn(event));

import WorkspaceView from '../src/views/WorkspaceView.vue';
import WorkspaceOverviewCard from '../src/components/WorkspaceOverviewCard.vue';
import {
  OVERVIEW_TASKS,
  coalesce,
  countChips,
  filterWorkspaceGroups,
  groupWorkspaces,
  indexOverview,
  isFinished,
  moreTasks,
  previewStatusLabel,
  splitIntoColumns,
  workspaceMatches,
} from '../src/composables/useWorkspaceOverview';

const settle = () => new Promise((r) => setTimeout(r, 30));

describe('grouping workspaces into cards', () => {
  const api = { id: 'a', name: 'api' };
  const docs = { id: 'd', name: 'api-docs', forkOfId: 'a' };
  const wp10 = { id: 'w', name: 'tauri-wp10', forkOfId: 'a' };
  const landing = { id: 'l', name: 'landing', description: 'The marketing SITE' };
  const orphan = { id: 'o', name: 'orphan', forkOfId: 'gone' };

  it('puts each fork under its parent, in list order', () => {
    expect(groupWorkspaces([docs, api, landing, wp10])).toEqual([
      { workspace: api, forks: [docs, wp10] },
      { workspace: landing, forks: [] },
    ]);
  });

  it('keeps a fork whose parent is not listed as a card of its own', () => {
    expect(groupWorkspaces([orphan])).toEqual([{ workspace: orphan, forks: [] }]);
  });

  it('matches a name or a description, ignoring case', () => {
    expect(workspaceMatches(landing, 'site')).toBe(true);
    expect(workspaceMatches(landing, 'LAND')).toBe(true);
    expect(workspaceMatches(api, 'site')).toBe(false);
  });

  it('keeps every card when nothing is searched', () => {
    const groups = groupWorkspaces([api, docs, landing]);
    expect(filterWorkspaceGroups(groups, '')).toEqual(groups.map((g) => ({ ...g, dimmed: false })));
  });

  it('keeps a matching parent with all its forks, and a dimmed one for the forks that match', () => {
    const groups = groupWorkspaces([api, docs, wp10, landing]);
    expect(filterWorkspaceGroups(groups, 'api')).toEqual([{ workspace: api, forks: [docs, wp10], dimmed: false }]);
    expect(filterWorkspaceGroups(groups, 'wp10')).toEqual([{ workspace: api, forks: [wp10], dimmed: true }]);
    expect(filterWorkspaceGroups(groups, 'nothing')).toEqual([]);
  });

  it('fills the shortest column first, counting a card with its forks', () => {
    const g = (id, forks = 0) => ({ workspace: { id }, forks: Array.from({ length: forks }, (_, i) => ({ id: `${id}${i}` })) });
    const cols = splitIntoColumns([g('a', 2), g('b'), g('c'), g('d'), g('e')], 2);
    expect(cols.map((c) => c.map((x) => x.workspace.id))).toEqual([['a', 'e'], ['b', 'c', 'd']]);
    expect(splitIntoColumns([g('a'), g('b')], 1)).toEqual([[g('a'), g('b')]]);
  });
});

describe('a card\'s numbers', () => {
  it('indexes the overview by workspace, tolerating missing parts', () => {
    const index = indexOverview({ workspaces: [{ workspaceId: 'a', counts: { ongoing: 1 }, tasks: [{ id: 't' }] }, { workspaceId: 'b' }] });
    expect(index.get('a')).toEqual({ counts: { ongoing: 1 }, tasks: [{ id: 't' }] });
    expect(index.get('b')).toEqual({ counts: {}, tasks: [] });
    expect(indexOverview(undefined).size).toBe(0);
  });

  it('shows the counts that are not zero, what needs the person first', () => {
    const chips = countChips({ ongoing: 2, notstarted: 0, scheduled: 1, pending: 1, completed: 9 });
    expect(chips.map((c) => [c.key, c.count, c.short])).toEqual([
      ['pending', 1, 'Action'],
      ['ongoing', 2, 'Ongoing'],
      ['scheduled', 1, 'Scheduled'],
    ]);
    expect(countChips()).toEqual([]);
  });

  it('counts the active tasks left out, never the finished ones shown', () => {
    const tasks = [{ status: 'ongoing' }, { status: 'cron' }, { status: 'completed' }, { status: 'rejected' }];
    expect(moreTasks({ ongoing: 3, notstarted: 4, scheduled: 1, completed: 50 }, tasks)).toBe(6);
    expect(moreTasks({ ongoing: 1, scheduled: 1 }, tasks)).toBe(0);
    expect(moreTasks()).toBe(0);
  });

  it('names each status, and greys only the finished', () => {
    expect(['ongoing', 'blocked', 'notstarted', 'cron', 'completed', 'rejected', 'odd'].map(previewStatusLabel))
      .toEqual(['Ongoing', 'Blocked', 'Not started', 'Scheduled', 'Completed', 'Rejected', 'odd']);
    expect(isFinished({ status: 'rejected' })).toBe(true);
    expect(isFinished({ status: 'blocked' })).toBe(false);
  });
});

describe('coalescing reloads', () => {
  it('runs once for a burst, then once more for what arrived during it', async () => {
    let release;
    const reload = vi.fn(() => new Promise((r) => { release = r; }));
    const run = coalesce(reload);
    const first = run();
    expect(run()).toBe(first);
    run();
    expect(reload).toHaveBeenCalledTimes(1);
    release();
    await settle();
    expect(reload).toHaveBeenCalledTimes(2);
    release();
    await first;
    run();
    expect(reload).toHaveBeenCalledTimes(3);
    release();
  });

  it('logs a failed reload and keeps going', async () => {
    const error = vi.spyOn(console, 'error').mockImplementation(() => {});
    const run = coalesce(async () => { throw new Error('down'); });
    await run();
    expect(error).toHaveBeenCalledWith('Refreshing the workspaces overview failed:', expect.any(Error));
    error.mockRestore();
  });
});

// ---- Mounted ----------------------------------------------------------------

let app;
let el;

function mountComponent(component, props = {}) {
  el = document.createElement('div');
  document.body.appendChild(el);
  app = createApp({ render: () => h(component, props) });
  app.use(createPinia());
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) });
  app.directive('click-outside', {});
  app.mount(el);
  return settle();
}

const $$ = (sel) => [...el.querySelectorAll(sel)];
const text = (node) => node.textContent.replace(/\s+/g, ' ').trim();
// The template compiler drops the space between sibling tags, so a row reads
// as its parts joined.
const row = (node) => [...node.children].map(text).filter(Boolean).join(' ');
const card = (name) => $$('[data-test="workspace-card"]').find((c) => text(c.querySelector('h3')) === name);

const minutesAgo = (n) => new Date(Date.now() - n * 60000).toISOString();
const task = (id, status, extra = {}) => ({ id, workspaceId: 'a', title: `task ${id}`, status, assignee: 'agent', updatedAt: minutesAgo(5), ...extra });

beforeEach(() => {
  app?.unmount();
  el?.remove();
  app = null;
  localStorage.clear();
  handlers.clear();
  push.mockReset();
  recordTelemetry.mockReset();
  fetchWorkspaces.mockReset();
  fetchTaskOverview.mockReset();
  fetchTaskOverview.mockResolvedValue({ workspaces: [] });
});

afterEach(() => {
  app?.unmount();
  app = null;
});

describe('a workspace card', () => {
  it('shows the counts, the first tasks with their status and time, and how many more there are', async () => {
    const opened = vi.fn();
    const openedTask = vi.fn();
    await mountComponent(WorkspaceOverviewCard, {
      workspace: { id: 'a', name: 'My Api', agentConnected: false },
      overview: {
        counts: { pending: 1, ongoing: 2, notstarted: 5, scheduled: 1 },
        tasks: [task('1', 'ongoing'), task('2', 'blocked'), task('3', 'notstarted'), task('4', 'cron', { cronSchedule: '0 9 * * *' }), task('5', 'completed')],
      },
      onOpen: opened,
      onOpenTask: openedTask,
    });

    expect(text(el.querySelector('h3'))).toBe('my-api');
    expect(text(el)).toContain('Agent Offline');
    expect($$('[data-test="count-chip"]').map(text)).toEqual(['1Action', '2Ongoing', '5To do', '1Scheduled']);
    const rows = $$('[data-test="preview-task"]');
    expect(rows).toHaveLength(OVERVIEW_TASKS);
    expect(row(rows[0])).toBe('task 1 Ongoing 5m ago');
    expect(row(rows[3])).toMatch(/^task 4 Scheduled In /);
    expect(text(el.querySelector('[data-test="more-tasks"]'))).toBe('+4 more →');

    rows[1].click();
    expect(openedTask).toHaveBeenCalledWith(expect.objectContaining({ id: '2' }));
    expect(opened).not.toHaveBeenCalled();
    el.querySelector('[data-test="workspace-card"]').click();
    expect(opened).toHaveBeenCalledOnce();
  });

  it('on a phone shows three tasks, bare numbers and no status words', async () => {
    await mountComponent(WorkspaceOverviewCard, {
      workspace: { id: 'a', name: 'api', agentConnected: true },
      overview: { counts: { ongoing: 4 }, tasks: [task('1', 'ongoing'), task('2', 'ongoing'), task('3', 'ongoing'), task('4', 'ongoing')] },
      narrow: true,
    });
    expect($$('[data-test="count-chip"]').map(text)).toEqual(['4']);
    expect($$('[data-test="preview-task"]').map(row)).toEqual(['task 1 5m ago', 'task 2 5m ago', 'task 3 5m ago']);
    expect(text(el.querySelector('[data-test="more-tasks"]'))).toBe('+1 more →');
    expect(text(el)).toContain('Agent Live');
  });

  it('says so when a workspace has nothing active, and marks a fork', async () => {
    await mountComponent(WorkspaceOverviewCard, {
      workspace: { id: 'f', name: 'api-fork', forkOfId: 'a', forkOf: { name: 'api' } },
      dimmed: true,
    });
    expect(text(el)).toContain('No active tasks');
    expect(el.querySelector('[data-test="card-fork"]').getAttribute('title')).toBe('Fork of api');
    expect(el.querySelector('[data-test="more-tasks"]')).toBeNull();
    expect(el.querySelector('[data-test="workspace-card"]').className).toContain('opacity-60');
  });
});

describe('a card\'s agent line', () => {
  const live = (extra) => ({ id: 'a', name: 'api', agentConnected: true, ...extra });
  const agentLine = () => row(el.querySelector('h3').nextElementSibling);

  it('names the client and its model when the model cannot be changed from here', async () => {
    await mountComponent(WorkspaceOverviewCard, {
      workspace: live({ agentClient: { name: 'Claude Code' }, agentModels: { currentModel: 'opus', models: [{ id: 'opus', name: 'Opus' }] } }),
    });
    expect(agentLine()).toBe('Claude Code · Opus');
  });

  it('names a client that reports no model', async () => {
    await mountComponent(WorkspaceOverviewCard, { workspace: live({ agentClient: { name: 'Codex' } }) });
    expect(agentLine()).toBe('Codex');
  });

  it('offers the model and effort pickers, whose clicks do not open the workspace', async () => {
    const opened = vi.fn();
    await mountComponent(WorkspaceOverviewCard, {
      workspace: live({
        agentModels: { canSet: true, currentModel: 'opus', models: [{ id: 'opus', name: 'Opus' }, { id: 'sonnet', name: 'Sonnet' }] },
        agentEffort: { canSet: true, currentEffort: 'high', efforts: [{ id: 'high', name: 'High' }, { id: 'low', name: 'Low' }] },
      }),
      onOpen: opened,
    });
    const pickers = [...el.querySelector('h3').nextElementSibling.querySelectorAll(':scope > div.relative')];
    expect(pickers).toHaveLength(2);
    pickers.forEach((p) => p.click());
    expect(opened).not.toHaveBeenCalled();
  });

  it('shows a picker with nothing chosen yet, after a client', async () => {
    await mountComponent(WorkspaceOverviewCard, {
      workspace: live({ agentClient: { name: 'Gemini' }, agentModels: { canSet: true, models: [{ id: 'pro', name: 'Pro' }] } }),
    });
    expect(el.querySelector('h3').nextElementSibling.querySelectorAll(':scope > div.relative')).toHaveLength(1);
    expect(agentLine()).toMatch(/^Gemini ·/);
  });

  it('names a fork\'s parent as another workspace when the list does not say which', async () => {
    await mountComponent(WorkspaceOverviewCard, { workspace: { id: 'f', name: 'api-fork', forkOfId: 'a' } });
    expect(el.querySelector('[data-test="card-fork"]').getAttribute('title')).toBe('Fork of another workspace');
  });

  it('greys a finished task', async () => {
    await mountComponent(WorkspaceOverviewCard, {
      workspace: live({}),
      overview: { counts: {}, tasks: [task('1', 'completed')] },
    });
    expect(el.querySelector('[data-test="preview-task"] span:nth-child(2)').className).toContain('text-gray-500');
  });
});

describe('the workspaces page', () => {
  const workspaces = [
    { id: 'a', name: 'api' },
    { id: 'd', name: 'api-docs', forkOfId: 'a', forkOf: { name: 'api' } },
    { id: 'w', name: 'api-wp10', forkOfId: 'a', forkOf: { name: 'api' } },
    { id: 'l', name: 'landing' },
    { id: 's', name: 'supervisor' },
    { id: 'x', name: 'old', archivedAt: '2026-10-01T00:00:00Z' },
  ];

  it('lays the cards out in two columns with the forks under their parent', async () => {
    fetchWorkspaces.mockResolvedValue({ workspaces });
    fetchTaskOverview.mockResolvedValue({ workspaces: [{ workspaceId: 'a', counts: { ongoing: 1 }, tasks: [task('1', 'ongoing')] }] });
    await mountComponent(WorkspaceView);

    expect(connected.value).toBe(true);
    const columns = $$('[data-test="workspace-column"]');
    expect(columns).toHaveLength(2);
    const names = (node) => [...node.querySelectorAll('h3')].map(text);
    // The supervisor leads the list, so the parent with two forks goes beside
    // it, and the next card back under the supervisor, the shorter column.
    expect(names(columns[0])).toEqual(['supervisor', 'landing']);
    expect(names(columns[1])).toEqual(['api', 'api-docs', 'api-wp10']);
    expect($$('[data-test="fork-card"]')).toHaveLength(2);
    expect(text(columns[1])).toContain('task 1');
  });

  it('opens a task from its card, and counts it against the task\'s workspace', async () => {
    fetchWorkspaces.mockResolvedValue({ workspaces });
    fetchTaskOverview.mockResolvedValue({ workspaces: [{ workspaceId: 'a', counts: { ongoing: 1 }, tasks: [task('1', 'ongoing')] }] });
    await mountComponent(WorkspaceView);

    el.querySelector('[data-test="preview-task"]').click();
    expect(push).toHaveBeenCalledWith('/workspaces/a/tasks/1');
    expect(recordTelemetry).toHaveBeenCalledWith('ui_overview_task_open', 'a');

    card('api-docs').click();
    expect(push).toHaveBeenLastCalledWith('/workspaces/d');
    card('landing').click();
    expect(push).toHaveBeenLastCalledWith('/workspaces/l');
  });

  it('keeps a parent, dimmed, for a fork that matches the search', async () => {
    fetchWorkspaces.mockResolvedValue({ workspaces });
    await mountComponent(WorkspaceView);

    const search = el.querySelector('input[placeholder="Search workspaces..."]');
    search.value = 'wp10';
    search.dispatchEvent(new Event('input'));
    await settle();
    expect($$('h3').map(text)).toEqual(['api', 'api-wp10']);
    expect(card('api').className).toContain('opacity-60');

    search.value = 'nothing';
    search.dispatchEvent(new Event('input'));
    await settle();
    expect(text(el)).toContain('No matches for "nothing"');

    // The archived list answers the same search.
    [...el.querySelectorAll('button')].find((b) => b.title === 'Archived').click();
    search.value = 'OLD';
    search.dispatchEvent(new Event('input'));
    await settle();
    expect(text(el)).toContain('Archived Workspaces');
    expect(text(el)).toContain('old');
    expect(text(el)).not.toContain('No archived workspaces found');
  });

  it('fetches the overview again on a task change, and stops listening when left', async () => {
    fetchWorkspaces.mockResolvedValue({ workspaces });
    await mountComponent(WorkspaceView);
    expect(fetchTaskOverview).toHaveBeenCalledTimes(1);

    fetchTaskOverview.mockResolvedValue({ workspaces: [{ workspaceId: 'l', counts: { scheduled: 1 }, tasks: [task('9', 'cron', { workspaceId: 'l', cronSchedule: '0 9 * * *' })] }] });
    emit({ type: 'agent.connected', payload: {} });
    emit({ type: 'task.updated', payload: {} });
    await settle();
    expect(fetchTaskOverview).toHaveBeenCalledTimes(2);
    expect(text(card('landing'))).toContain('task 9');

    emit({ type: 'task.created', payload: {} });
    emit({ type: 'task.deleted', payload: {} });
    await settle();
    expect(fetchTaskOverview).toHaveBeenCalledTimes(4);

    app.unmount();
    app = null;
    expect(connected.value).toBe(false);
  });

  it('shows one column of phone-sized cards on a page too narrow for two', async () => {
    fetchWorkspaces.mockResolvedValue({ workspaces });
    fetchTaskOverview.mockResolvedValue({ workspaces: [{ workspaceId: 'a', counts: { ongoing: 1 }, tasks: [task('1', 'ongoing')] }] });
    const original = globalThis.ResizeObserver;
    globalThis.ResizeObserver = class {
      constructor(cb) { this.cb = cb; }
      observe() { this.cb([{ contentRect: { width: 600 } }]); }
      disconnect() {}
    };
    try {
      await mountComponent(WorkspaceView);
    } finally {
      globalThis.ResizeObserver = original;
    }
    expect($$('[data-test="workspace-column"]')).toHaveLength(1);
    expect(row(el.querySelector('[data-test="preview-task"]'))).toBe('task 1 5m ago');
  });
});
