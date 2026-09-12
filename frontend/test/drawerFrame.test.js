import { describe, it, expect, vi } from 'vitest';

import {
  DRAW_TIMEOUT_MS,
  FRAME_SANDBOX,
  DRAWER_FRAME_URL,
  createDrawerSource,
  drawerUrl,
  readFrameMessage,
} from '../src/composables/useDrawerFrame';

/**
 * An extension's drawer runs where it cannot reach anything.
 *
 * The security of this is one attribute and one habit: a sandbox without
 * `allow-same-origin`, and never believing a message that did not come from the
 * window we made. Both are asserted here rather than left to a reviewer to
 * notice, because both are one word away from being useless.
 */

describe('FRAME_SANDBOX', () => {
  /**
   * The line that must never move. With `allow-same-origin` the frame becomes
   * same-origin with `app://` — it could read this page and reach the bridge,
   * and every other precaution here would be decoration.
   */
  it('allows scripts and nothing else', () => {
    expect(FRAME_SANDBOX).toBe('allow-scripts');
    expect(FRAME_SANDBOX).not.toContain('allow-same-origin');
  });

  it('never grants anything that reaches out of the frame', () => {
    for (const capability of ['allow-same-origin', 'allow-top-navigation', 'allow-popups', 'allow-downloads', 'allow-forms']) {
      expect(FRAME_SANDBOX, capability).not.toContain(capability);
    }
  });
});

describe('DRAWER_FRAME_URL', () => {
  /**
   * A real URL, and the reason matters: a `srcdoc`, `data:` or `blob:` document
   * inherits the embedder's CSP, and this app's is `script-src 'self'`. A
   * sandboxed frame has an opaque origin, for which `'self'` matches nothing —
   * so under an inherited policy no script can run in one at all. The document
   * and its own policy live in the main process.
   */
  it('is served, not inlined', () => {
    expect(DRAWER_FRAME_URL).toMatch(/^app:/);
    expect(DRAWER_FRAME_URL).not.toContain('data:');
    expect(DRAWER_FRAME_URL).not.toContain('blob:');
  });
});

describe('readFrameMessage', () => {
  const win = { name: 'the frame' };

  it('reads the three messages a frame may send', () => {
    expect(readFrameMessage({ source: win, data: { type: 'ready' } }, win)).toEqual({ type: 'ready' });
    expect(readFrameMessage({ source: win, data: { type: 'drawn', height: 120 } }, win)).toEqual({
      type: 'drawn',
      height: 120,
    });
    expect(readFrameMessage({ source: win, data: { type: 'failed', reason: 'bad' } }, win)).toEqual({
      type: 'failed',
      reason: 'bad',
    });
  });

  /**
   * The frame's origin is opaque — `event.origin` is the string "null" and
   * identifies nothing. The *source* is what says which window spoke, so every
   * message is checked against the one actually created.
   */
  it('believes only the window it was given', () => {
    const other = { name: 'somebody else' };

    expect(readFrameMessage({ source: other, data: { type: 'drawn', height: 10 } }, win)).toBeNull();
    expect(readFrameMessage({ source: win, data: { type: 'drawn', height: 10 } }, null)).toBeNull();
    expect(readFrameMessage({ source: undefined, data: { type: 'ready' } }, win)).toBeNull();
  });

  it('ignores a message that is not one of the three', () => {
    for (const data of [{ type: 'navigate' }, { type: '' }, 'a string', null, 7, undefined]) {
      expect(readFrameMessage({ source: win, data }, win)).toBeNull();
    }
  });

  // A height that is not a number, or is absurd, is not a height.
  it('refuses a height it cannot use, and caps one it could not draw', () => {
    for (const height of [undefined, 'tall', NaN, Infinity, 0, -5]) {
      expect(readFrameMessage({ source: win, data: { type: 'drawn', height } }, win)).toBeNull();
    }
    expect(readFrameMessage({ source: win, data: { type: 'drawn', height: 1e9 } }, win).height).toBe(20000);
  });

  it('bounds a reason, and always has one', () => {
    expect(readFrameMessage({ source: win, data: { type: 'failed' } }, win).reason).toBe('This could not be drawn.');
    expect(readFrameMessage({ source: win, data: { type: 'failed', reason: 'x'.repeat(500) } }, win).reason).toHaveLength(300);
  });
});

describe('createDrawerSource', () => {
  const bridge = (over = {}) => ({
    drawer: vi.fn(async (format) => (format === 'mermaid' ? { ok: true, owner: 'mermaid' } : { ok: false, reason: `Nothing installed draws "${format}".` })),
    ...over,
  });

  // Only the answer crosses the bridge, never the code: a real drawer is
  // megabytes, and the frame fetches it from a URL the browser caches.
  it('answers with where the frame should import from', async () => {
    const source = createDrawerSource({ bridge: bridge() });

    expect(await source.find('mermaid')).toEqual({ ok: true, url: drawerUrl('mermaid') });
  });

  it('folds the format, so one drawer is not two', async () => {
    const b = bridge();
    const source = createDrawerSource({ bridge: b });

    await source.find('MERMAID');

    expect(b.drawer).toHaveBeenCalledWith('mermaid');
  });

  // Asked once per format, failures included: an unclaimed fence is the common
  // case, and a bridge call per diagram per render is the cost of not caching.
  it('asks once, and remembers a no as firmly as a yes', async () => {
    const b = bridge();
    const source = createDrawerSource({ bridge: b });

    await Promise.all([source.find('mermaid'), source.find('mermaid'), source.find('mermaid')]);
    await source.find('vega-lite');
    await source.find('vega-lite');

    expect(b.drawer).toHaveBeenCalledTimes(2);
  });

  it('forgets when what is installed has changed', async () => {
    const b = bridge();
    const source = createDrawerSource({ bridge: b });

    await source.find('mermaid');
    source.forget();
    await source.find('mermaid');

    expect(b.drawer).toHaveBeenCalledTimes(2);
  });

  // The web build, and an older desktop whose bridge predates this method.
  it('is inert without a bridge that can answer', async () => {
    for (const b of [undefined, {}, { entries: () => [] }]) {
      const source = createDrawerSource({ bridge: b });

      expect(source.available).toBe(false);
      expect(await source.find('mermaid')).toEqual({ ok: false, reason: '' });
    }
  });

  // Never throws: this runs while a message is being drawn, and an exception
  // would take the whole message rather than the one block.
  it('answers rather than throwing when the bridge does', async () => {
    const thrown = createDrawerSource({ bridge: bridge({ drawer: vi.fn(async () => { throw new Error('EPIPE'); }) }) });
    expect(await thrown.find('mermaid')).toEqual({ ok: false, reason: 'EPIPE' });

    const nonsense = createDrawerSource({ bridge: bridge({ drawer: vi.fn(async () => undefined) }) });
    expect((await nonsense.find('mermaid')).ok).toBe(false);

    // A rejection carrying nothing to say still has to answer with a shape —
    // an Error with an empty message, and something thrown that is not an Error
    // at all and has no message to read.
    const mute = createDrawerSource({ bridge: bridge({ drawer: vi.fn(() => Promise.reject(new Error(''))) }) });
    expect(await mute.find('mermaid')).toEqual({ ok: false, reason: '' });

    const bare = createDrawerSource({ bridge: bridge({ drawer: vi.fn(() => Promise.reject('boom')) }) });
    expect(await bare.find('mermaid')).toEqual({ ok: false, reason: '' });
  });

  it('has nothing to look up for no format', async () => {
    const source = createDrawerSource({ bridge: bridge() });

    expect((await source.find('')).ok).toBe(false);
    expect((await source.find(undefined)).ok).toBe(false);
    expect((await source.find(null)).ok).toBe(false);
  });
});

describe('DRAW_TIMEOUT_MS', () => {
  // A drawer that never answers must not leave "Drawing…" on screen forever.
  it('is a wait somebody would actually sit through', () => {
    expect(DRAW_TIMEOUT_MS).toBeGreaterThan(1000);
    expect(DRAW_TIMEOUT_MS).toBeLessThanOrEqual(30000);
  });
});

describe('drawerUrl', () => {
  it('addresses a drawer by format, on this app origin', () => {
    expect(drawerUrl('mermaid')).toBe('app://agentrq/__agentrq/drawer/mermaid.js');
  });

  // The format reaches a URL, so it is encoded rather than trusted to be safe
  // in one — even though the manifest already refuses anything but a word.
  it('encodes a format that has no business in a path', () => {
    expect(drawerUrl('../evil')).not.toContain('../');
    expect(drawerUrl('a b')).not.toContain(' ');
    expect(drawerUrl('MERMAID')).toBe(drawerUrl('mermaid'));
    // And nothing at all is still a URL rather than a crash.
    expect(drawerUrl(undefined)).toBe('app://agentrq/__agentrq/drawer/.js');
  });
});
