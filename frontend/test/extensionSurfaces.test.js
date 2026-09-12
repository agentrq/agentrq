// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi, afterEach } from 'vitest';

import { plain, useExtensionSurfaces } from '../src/composables/useExtensionSurfaces';

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

/**
 * The bug this file shipped with, and why the fix cannot live anywhere else.
 *
 * A task on the board is a Vue reactive proxy. `contextBridge` converts
 * arguments on the way into the preload's world and refuses a Proxy outright,
 * so the call rejected before any preload or main-process code ran — and the
 * rejection was turned into an empty list. An installed, running extension
 * contributed a menu row that never appeared, silently.
 *
 * Every fake in this file passes a plain object, which is exactly why nothing
 * here caught it; `npm run verify:extensions` is what does now.
 */
describe('plain', () => {
  it('flattens a proxy into something the bridge will accept', () => {
    const task = new Proxy({ id: 't1', title: 'Ship it', messages: [{ text: 'one' }] }, {});

    // Why this is needed at all.
    expect(() => structuredClone(task)).toThrow();
    expect(() => structuredClone(plain(task))).not.toThrow();
    expect(plain(task)).toEqual({ id: 't1', title: 'Ship it', messages: [{ text: 'one' }] });
  });

  it('drops what could never cross anyway', () => {
    expect(plain({ id: 't1', onClick: () => {}, missing: undefined })).toEqual({ id: 't1' });
  });

  it('gives up on a circular structure rather than throwing', () => {
    const circular = { id: 't1' };
    circular.self = circular;
    expect(plain(circular)).toEqual({});
  });

  it('is an object for nothing at all', () => {
    expect(plain(undefined)).toEqual({});
    expect(plain(null)).toEqual({});
  });
});

describe('entriesFor', () => {
  it('sends a plain copy, not the reactive task it was handed', async () => {
    const bridge = fakeBridge();
    const surfaces = useExtensionSurfaces({ bridge });

    await surfaces.entriesFor('task-menu', new Proxy({ id: 't1', title: 'Ship it' }, {}));
    await surfaces.invoke({ owner: 'task-stats', id: 'stats' }, new Proxy({ id: 't1' }, {}));

    expect(() => structuredClone(bridge.entries.mock.calls[0][1])).not.toThrow();
    expect(() => structuredClone(bridge.invoke.mock.calls[0][1])).not.toThrow();
  });

  // Silence is what hid the original failure for three rounds of "it doesn't
  // work" — an empty list and nothing anywhere saying why.
  it('says something when the bridge refuses, rather than only returning nothing', async () => {
    const logger = { warn: vi.fn() };
    const surfaces = useExtensionSurfaces({
      bridge: fakeBridge({ entries: vi.fn(async () => { throw new Error('An object could not be cloned.'); }) }),
      logger,
    });

    expect(await surfaces.entriesFor('task-menu', {})).toEqual([]);
    expect(logger.warn).toHaveBeenCalledWith(expect.stringContaining('task-menu'), 'An object could not be cloned.');
  });

  it('has something to say about a failure that carried no message', async () => {
    const logger = { warn: vi.fn() };
    const surfaces = useExtensionSurfaces({
      bridge: fakeBridge({ entries: vi.fn(async () => { throw 'gone'; }) }), // eslint-disable-line no-throw-literal
      logger,
    });

    await surfaces.entriesFor('task-menu', {});

    expect(logger.warn).toHaveBeenCalledWith(expect.any(String), 'gone');
  });

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


/**
 * `invoke` holds what it drew — one panel, one error, because it is for a
 * person clicking something. A renderer is asked for every claimed block in a
 * message while it is being drawn, and those all need their own answer: putting
 * them through `panel` would have each block overwriting the last and an
 * unrelated dialog opening in the middle of a conversation.
 */
describe('invokeQuietly', () => {
  it('hands the answer straight back, touching nothing shared', async () => {
    const bridge = fakeBridge();
    const surfaces = useExtensionSurfaces({ bridge });

    const answer = await surfaces.invokeQuietly({ owner: 'mermaid', id: 'mermaid', surface: 'code-block' }, {
      language: 'mermaid',
      source: 'graph TD;',
    });

    expect(answer).toEqual({ ok: true, view: view() });
    expect(surfaces.panel.value).toBeNull();
    expect(surfaces.error.value).toBe('');
    expect(surfaces.busy.value).toBe(false);
  });

  it('flattens the context, the way everything crossing the bridge must', async () => {
    const bridge = fakeBridge();
    const surfaces = useExtensionSurfaces({ bridge });

    await surfaces.invokeQuietly({ owner: 'mermaid', id: 'mermaid' }, new Proxy({ source: 'graph TD;' }, {}));

    expect(() => structuredClone(bridge.invoke.mock.calls[0][1])).not.toThrow();
  });

  // Local to the block on every path: a message must not lose its text because
  // one diagram would not render.
  it('reports a refusal rather than raising it', async () => {
    const refusing = useExtensionSurfaces({
      bridge: fakeBridge({ invoke: vi.fn(async () => ({ ok: false, reason: 'too long' })) }),
    });
    expect(await refusing.invokeQuietly({}, {})).toEqual({ ok: false, reason: 'too long' });
    expect(refusing.error.value).toBe('');

    const empty = useExtensionSurfaces({ bridge: fakeBridge({ invoke: vi.fn(async () => undefined) }) });
    expect(await empty.invokeQuietly({}, {})).toEqual({ ok: false, reason: '' });

    const throwing = useExtensionSurfaces({
      bridge: fakeBridge({ invoke: vi.fn(async () => { throw new Error('EPIPE'); }) }),
    });
    expect(await throwing.invokeQuietly({}, {})).toEqual({ ok: false, reason: 'EPIPE' });
  });

  it('has something to say about a throw with no message', async () => {
    const empty = useExtensionSurfaces({
      bridge: fakeBridge({ invoke: vi.fn(async () => { throw new Error(''); }) }),
    });
    expect(await empty.invokeQuietly({}, {})).toEqual({ ok: false, reason: '' });

    // Not everything thrown is an Error, and nothing here may raise.
    const odd = useExtensionSurfaces({
      bridge: fakeBridge({ invoke: vi.fn(async () => { throw 'gone' }) }), // eslint-disable-line no-throw-literal
    });
    expect(await odd.invokeQuietly({}, {})).toEqual({ ok: false, reason: '' });
  });

  it('answers with nothing at all when there is no bridge', async () => {
    const surfaces = useExtensionSurfaces({ bridge: undefined });

    expect(await surfaces.invokeQuietly({}, {})).toEqual({ ok: false, reason: '' });
  });
});
