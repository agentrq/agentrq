import { describe, it, expect, vi } from 'vitest';
import { ref } from 'vue';

import { connectWebMCP, describePage } from '../src/composables/useWebMCP';
import { WebMCPStatus } from '../src/webmcp/modelContext';

const routerAt = (route) => ({ push: vi.fn().mockResolvedValue(undefined), currentRoute: ref(route) });

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
