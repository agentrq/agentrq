import { describe, it, expect, vi, afterEach } from 'vitest';

import { useExtensionSurfaces } from '../src/composables/useExtensionSurfaces';

/**
 * The renderer's half. Two things are load-bearing here and both are about
 * failure: a broken bridge must never stop a task's own context menu opening,
 * and a spec the renderer will not draw must be refused in one place with a
 * reason its author can act on.
 */

const view = (over = {}) => ({ title: 'Stats', nodes: [{ type: 'text', value: 'two words' }], ...over });

const fakeBridge = (over = {}) => ({
  entries: vi.fn(async () => [{ owner: 'task-stats', id: 'stats', label: 'Task Stats', order: 10 }]),
  invoke: vi.fn(async () => ({ ok: true, view: view() })),
  ...over,
});

afterEach(() => {
  delete window.agentrq;
});

describe('entriesFor', () => {
  it('asks the main process about this surface and this task', async () => {
    const bridge = fakeBridge();
    const surfaces = useExtensionSurfaces({ bridge });

    const rows = await surfaces.entriesFor('task-menu', { id: 't1', status: 'ongoing' });

    expect(bridge.entries).toHaveBeenCalledWith('task-menu', { id: 't1', status: 'ongoing' });
    expect(rows[0].label).toBe('Task Stats');
  });

  // The built-in items are the ones somebody actually came for; a broken bridge
  // must not be able to stop the menu opening.
  it('answers with nothing rather than throwing, however the bridge fails', async () => {
    const throwing = useExtensionSurfaces({ bridge: fakeBridge({ entries: vi.fn(async () => { throw new Error('gone'); }) }) });
    const empty = useExtensionSurfaces({ bridge: fakeBridge({ entries: vi.fn(async () => undefined) }) });
    const absent = useExtensionSurfaces({ bridge: undefined });

    expect(await throwing.entriesFor('task-menu', {})).toEqual([]);
    expect(await empty.entriesFor('task-menu', {})).toEqual([]);
    expect(await absent.entriesFor('task-menu', {})).toEqual([]);
  });

  it('is absent rather than broken in the browser build', () => {
    expect(useExtensionSurfaces({ bridge: undefined }).available).toBe(false);
    // A bridge from an older build with no `entries` counts as no bridge.
    expect(useExtensionSurfaces({ bridge: { invoke: vi.fn() } }).available).toBe(false);
  });

  it('finds the bridge on the window when none is passed', () => {
    window.agentrq = { extensions: fakeBridge() };
    expect(useExtensionSurfaces().available).toBe(true);
  });
});

describe('invoke', () => {
  it('runs the entry and holds what it drew', async () => {
    const bridge = fakeBridge();
    const surfaces = useExtensionSurfaces({ bridge });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats', surface: 'task-menu' }, { title: 'Ship it' });

    expect(bridge.invoke).toHaveBeenCalledWith(
      { owner: 'task-stats', id: 'stats', surface: 'task-menu' },
      { title: 'Ship it' },
    );
    expect(surfaces.panel.value).toMatchObject({ title: 'Stats', owner: 'task-stats', id: 'stats' });
    expect(surfaces.error.value).toBe('');
  });

  // The whole point of validating here: the renderer draws a closed vocabulary,
  // and a node type nobody knows is the author's mistake to fix.
  it('refuses a view the renderer will not draw, and names who sent it', async () => {
    const bridge = fakeBridge({
      invoke: vi.fn(async () => ({ ok: true, view: { title: 'x', nodes: [{ type: 'headding', value: 'oops' }] } })),
    });
    const surfaces = useExtensionSurfaces({ bridge });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.panel.value).toBeNull();
    expect(surfaces.error.value).toContain('task-stats:');
    expect(surfaces.error.value).toContain('headding');
  });

  it('clamps and normalises before anything is drawn', async () => {
    const bridge = fakeBridge({
      invoke: vi.fn(async () => ({
        ok: true,
        // A tone nobody offers and a scheme nobody follows: both are reduced
        // here rather than reaching a component.
        view: { title: 'x', nodes: [{ type: 'row', label: 'a', value: 'b', href: 'file:///etc/passwd', tone: 'neon' }] },
      })),
    });
    const surfaces = useExtensionSurfaces({ bridge });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.panel.value.nodes[0].href).toBe('');
    expect(surfaces.panel.value.nodes[0].tone).toBe('default');
  });

  // Three outcomes, kept distinct: a view, a completed action that drew
  // nothing, and a failure. Collapsing the middle makes success look broken.
  it('draws no panel and reports nothing when the entry drew nothing', async () => {
    const surfaces = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => ({ ok: true, view: null })) }) });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.panel.value).toBeNull();
    expect(surfaces.error.value).toBe('');
  });

  it('shows the reason the main process gave', async () => {
    const surfaces = useExtensionSurfaces({
      bridge: fakeBridge({ invoke: vi.fn(async () => ({ ok: false, reason: 'task-stats failed: boom' })) }),
    });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.error.value).toBe('task-stats failed: boom');
  });

  it('has something to say about a refusal that carried no reason', async () => {
    const surfaces = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => ({ ok: false })) }) });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.error.value).toBe('That did not work.');
  });

  it('has something to say about an answer that never arrived', async () => {
    const surfaces = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => undefined) }) });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.error.value).toBe('That did not work.');
  });

  it('reports a bridge call that threw, with and without a message', async () => {
    const named = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => { throw new Error('EPIPE'); }) }) });
    await named.invoke({ owner: 'task-stats', id: 'stats' }, {});
    expect(named.error.value).toBe('EPIPE');

    const silent = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => { throw new Error(''); }) }) });
    await silent.invoke({ owner: 'task-stats', id: 'stats' }, {});
    expect(silent.error.value).toBe('That did not work.');
  });

  it('clears whatever was on screen before running the next one', async () => {
    const surfaces = useExtensionSurfaces({ bridge: fakeBridge() });
    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    surfaces.dismiss();

    expect(surfaces.panel.value).toBeNull();
    expect(surfaces.error.value).toBe('');
  });

  it('is not busy once it has finished, whatever happened', async () => {
    const surfaces = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => { throw new Error('x'); }) }) });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.busy.value).toBe(false);
  });

  it('does nothing at all with no bridge', async () => {
    const surfaces = useExtensionSurfaces({ bridge: undefined });

    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, {});

    expect(surfaces.panel.value).toBeNull();
    expect(surfaces.error.value).toBe('');
  });
});
