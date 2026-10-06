// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest';
import { ref } from 'vue';
import { createRouter, createMemoryHistory } from 'vue-router';

import { routes } from '../src/app';

import { connectWebMCP, describePage } from '../src/composables/useWebMCP';
import { onWebMCPChange } from '../src/composables/useWebMCPChanges';
import { WebMCPStatus } from '../src/webmcp/modelContext';
import { useToasts } from '../src/composables/useToasts';

const routerAt = (route) => ({
  push: vi.fn().mockResolvedValue(undefined),
  resolve: () => ({ matched: [{}] }),
  currentRoute: ref(route),
});

const navigateTool = (ctx) => ctx.registerTool.mock.calls.map(([t]) => t).find((t) => t.name === 'navigate');

describe('describePage', () => {
  it('hands over the route params, which are what an agent needs', () => {
    const page = describePage({
      path: '/workspaces/ws1/tasks/t1',
      params: { id: 'ws1', taskId: 't1' },
      query: { tab: 'trajectory' },
    });

    expect(page).toEqual({
      path: '/workspaces/ws1/tasks/t1',
      params: { id: 'ws1', taskId: 't1' },
      query: { tab: 'trajectory' },
    });
  });

  it('copies rather than exposing the router\'s own objects', () => {
    const params = { id: 'ws1' };

    const page = describePage({ path: '/', params, query: {} });
    params.id = 'ws2';

    expect(page.params.id).toBe('ws1');
  });

  it('describes something sane before the router has a route', () => {
    expect(describePage(undefined)).toEqual({ path: '/', params: {}, query: {} });
  });
});

describe('connectWebMCP', () => {
  const context = () => ({ registerTool: vi.fn().mockResolvedValue(undefined) });

  it('registers the whole catalogue', async () => {
    const ctx = context();

    const result = await connectWebMCP({ api: {}, router: routerAt({ path: '/' }), context: ctx });

    expect(result.status).toBe(WebMCPStatus.Registered);
    expect(result.registered).toContain('createTask');
    expect(result.registered).toContain('getCurrentPage');
    expect(ctx.registerTool).toHaveBeenCalledTimes(result.registered.length);
  });

  it('routes the navigate tool through the app\'s own router', async () => {
    const ctx = context();
    const router = routerAt({ path: '/' });
    await connectWebMCP({ api: {}, router, context: ctx });

    const navigate = ctx.registerTool.mock.calls.map(([t]) => t).find((t) => t.name === 'navigate');
    await navigate.execute({ path: '/events' }, {});

    // Not location.href: a full page load would drop the SPA's state.
    expect(router.push).toHaveBeenCalledWith('/events');
  });

  it('refuses a path no route matches, rather than leaving the user on a blank page', async () => {
    const ctx = context();
    const router = { ...routerAt({ path: '/' }), resolve: () => ({ matched: [] }) };
    await connectWebMCP({ api: {}, router, context: ctx });

    await expect(navigateTool(ctx).execute({ path: '/nowhere' }, {})).rejects.toThrow('No page at /nowhere');
    expect(router.push).not.toHaveBeenCalled();
  });

  it('answers getCurrentPage from where the user is now, not where they were', async () => {
    const ctx = context();
    const router = routerAt({ path: '/', params: {}, query: {} });
    await connectWebMCP({ api: {}, router, context: ctx });

    const currentPage = ctx.registerTool.mock.calls.map(([t]) => t).find((t) => t.name === 'getCurrentPage');
    // The tools stay registered while the person moves around the app.
    router.currentRoute.value = { path: '/workspaces/ws1/board', params: { id: 'ws1' }, query: {} };

    expect(await currentPage.execute({}, {})).toEqual({
      path: '/workspaces/ws1/board',
      params: { id: 'ws1' },
      query: {},
    });
  });

  it('withdraws every tool at once when the session ends', async () => {
    const ctx = context();
    const result = await connectWebMCP({ api: {}, router: routerAt({ path: '/' }), context: ctx });

    // The signal each tool was registered against is the unregister mechanism.
    const signals = ctx.registerTool.mock.calls.map(([, options]) => options.signal);
    expect(new Set(signals).size).toBe(1);
    expect(signals[0].aborted).toBe(false);

    result.unregister();

    expect(signals[0].aborted).toBe(true);
  });

  it('reports an unsupported browser without touching anything', async () => {
    const result = await connectWebMCP({ api: {}, router: routerAt({ path: '/' }), context: null });

    expect(result.status).toBe(WebMCPStatus.Unsupported);
    expect(result.registered).toEqual([]);
    // Still safe to call, so the caller needs no branch of its own.
    expect(() => result.unregister()).not.toThrow();
  });
});

// A view that loaded on mount only shows an agent's change if it is told.
describe('telling the open page about changes', () => {
  const toolNamed = (ctx, name) => ctx.registerTool.mock.calls.map(([t]) => t).find((t) => t.name === name);
  const tick = () => new Promise((r) => setTimeout(r, 0));

  async function connected(api) {
    const ctx = { registerTool: vi.fn().mockResolvedValue(undefined) };
    await connectWebMCP({ api, router: routerAt({ path: '/' }), context: ctx });
    const changed = vi.fn();
    const off = onWebMCPChange(changed);
    return { ctx, changed, off };
  }

  it('announces a change once the tool has succeeded, and still returns its result', async () => {
    const api = { updateWorkflow: vi.fn().mockResolvedValue({ workflow: { id: 'wf1' } }) };
    const { ctx, changed, off } = await connected(api);

    const result = await toolNamed(ctx, 'updateWorkflow').execute({ workflowId: 'wf1', startEventId: 'e1' }, {});
    await tick();
    off();

    expect(result).toEqual({ workflow: { id: 'wf1' } });
    expect(api.updateWorkflow).toHaveBeenCalledWith('wf1', { startEventId: 'e1' });
    expect(changed).toHaveBeenCalledTimes(1);
  });

  it('announces nothing when the tool fails', async () => {
    const api = { updateWorkflow: vi.fn().mockRejectedValue(new Error('workflow not found')) };
    const { ctx, changed, off } = await connected(api);

    await expect(toolNamed(ctx, 'updateWorkflow').execute({ workflowId: 'wf1' }, {})).rejects.toThrow('workflow not found');
    await tick();
    off();

    expect(changed).not.toHaveBeenCalled();
  });

  it('announces nothing for a read, or for navigating', async () => {
    const api = { fetchWorkflows: vi.fn().mockResolvedValue({ workflows: [] }) };
    const { ctx, changed, off } = await connected(api);

    await toolNamed(ctx, 'listWorkflows').execute({}, {});
    await toolNamed(ctx, 'navigate').execute({ path: '/events' }, {});
    await tick();
    off();

    expect(changed).not.toHaveBeenCalled();
  });
});

describe('showing the screen a tool acts on', () => {
  const toolNamed = (ctx, name) => ctx.registerTool.mock.calls.map(([t]) => t).find((t) => t.name === name);

  async function connected(api, { draft = false } = {}) {
    const ctx = { registerTool: vi.fn().mockResolvedValue(undefined) };
    const router = {
      push: vi.fn().mockResolvedValue(undefined),
      resolve: (path) => ({ matched: [{}], fullPath: path }),
      currentRoute: ref({ path: '/', fullPath: '/' }),
    };
    useToasts().toasts.value = [];
    document.body.innerHTML = draft ? '<textarea>half a reply</textarea>' : '';
    await connectWebMCP({ api, router, context: ctx });
    return { ctx, router };
  }

  it('opens the machine before restarting its daemon, and toasts', async () => {
    const order = [];
    const api = { restartDaemon: vi.fn(async () => order.push('tool')) };
    const { ctx, router } = await connected(api);
    router.push.mockImplementation(async () => order.push('push'));

    await toolNamed(ctx, 'restartDaemon').execute({ machineId: 'm1' }, {});

    expect(order).toEqual(['push', 'tool']);
    expect(router.push).toHaveBeenCalledWith('/machines/m1');
    const toast = useToasts().toasts.value.at(-1);
    expect(toast.message).toBe('Browser agent ran: restart daemon');
    expect(toast.link).toBeNull();
  });

  it('opens the task a tool created once it exists', async () => {
    const api = { createTask: vi.fn().mockResolvedValue({ task: { id: 't9' } }) };
    const { ctx, router } = await connected(api);

    await toolNamed(ctx, 'createTask').execute({ workspaceId: 'w1', title: 'x', body: 'y' }, {});

    expect(router.push).toHaveBeenCalledWith('/workspaces/w1/tasks/t9');
  });

  it('holds the move over a draft, and the toast waits with a Show link', async () => {
    const api = { restartDaemon: vi.fn().mockResolvedValue({}) };
    const { ctx, router } = await connected(api, { draft: true });

    await toolNamed(ctx, 'restartDaemon').execute({ machineId: 'm1' }, {});

    expect(router.push).not.toHaveBeenCalled();
    const toast = useToasts().toasts.value.at(-1);
    expect(toast.link).toEqual({ path: '/machines/m1', label: 'Show' });
    expect(toast.persistent).toBe(true);
  });

  it('leaves a read, and navigate itself, alone', async () => {
    const api = { fetchMachines: vi.fn().mockResolvedValue({ machines: [] }) };
    const { ctx, router } = await connected(api);

    await toolNamed(ctx, 'listMachines').execute({}, {});

    expect(router.push).not.toHaveBeenCalled();
    expect(useToasts().toasts.value).toHaveLength(0);
    expect(toolNamed(ctx, 'navigate')).not.toHaveProperty('screen');
  });

  it('does not offer the screen to the browser', async () => {
    const { ctx } = await connected({});

    expect(toolNamed(ctx, 'restartDaemon')).not.toHaveProperty('screen');
  });
});

describe('the navigate tool\'s map of the interface', () => {
  // Every page the description names, spelled as an agent would build it. If a
  // route is added, the last test fails until the description (and this list)
  // tells agents about it.
  const DESCRIBED = [
    '/',
    '/workspaces/new',
    '/tasks/pending',
    '/tasks/active/ws1/t1',
    '/tasks/scheduled/ws1/t1/instances',
    '/workspaces/ws1?filter=completed',
    '/workspaces/ws1/board',
    '/workspaces/ws1/analytics',
    '/workspaces/ws1/settings?tab=skills',
    '/workspaces/ws1/settings/skills/tdd',
    '/workspaces/ws1/tasks/t1',
    '/workspaces/ws1/tasks/t1/instances',
    '/workspaces/ws1/tasks/t1/edit',
    '/workspaces/ws1/tasks/new',
    '/kanban',
    '/events',
    '/events/e1',
    '/workflows',
    '/workflows/wf1',
    '/machines',
    '/machines/m1',
    '/skills',
    '/skills/tdd',
    '/sessions/s1',
    '/extensions',
    '/extensions/acme/home',
  ];
  const router = createRouter({ history: createMemoryHistory(), routes });

  it.each(DESCRIBED)('%s is a real page', (path) => {
    expect(router.resolve(path).matched.length).toBeGreaterThan(0);
  });

  it('covers every page except sign-in', () => {
    const reached = new Set(DESCRIBED.map((path) => router.resolve(path).matched.at(-1).path));
    const pages = router.getRoutes().filter((r) => !r.meta?.public && r.components).map((r) => r.path);

    expect(pages.filter((p) => !reached.has(p))).toEqual([]);
  });

  it('is what the description names', async () => {
    const ctx = { registerTool: vi.fn().mockResolvedValue(undefined) };
    await connectWebMCP({ api: {}, router: routerAt({ path: '/' }), context: ctx });
    const { description } = navigateTool(ctx);

    for (const word of ['board', 'analytics', 'settings', 'instances', 'edit', 'new', 'events', 'workflows',
      'machines', 'sessions', 'extensions', 'notstarted', 'pending', 'ongoing', 'completed', 'scheduled',
      '?filter=', '?tab=', 'memories', 'danger']) {
      expect(description).toContain(word);
    }
  });
});
