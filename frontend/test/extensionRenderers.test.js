// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi, beforeEach } from 'vitest';

import { resetRenderers, useExtensionRenderers } from '../src/composables/useExtensionRenderers';

/**
 * The fourth surface, and the first that changes something AgentRQ was already
 * drawing rather than adding a place of its own.
 *
 * The thing to keep true here is that a renderer is asked for many blocks while
 * one message is drawn, and every failure has to stay local to its block — a
 * message must not lose its text because one diagram would not parse.
 */

const entry = (over = {}) => ({ owner: 'mermaid-agentrq', id: 'mermaid', language: 'mermaid', order: 100, ...over });

const fakeSurfaces = (over = {}) => ({
  available: true,
  entriesFor: vi.fn(async () => [entry()]),
  invokeQuietly: vi.fn(async () => ({ ok: true, view: { nodes: [{ type: 'diagram', format: 'mermaid', source: 'graph TD;' }] } })),
  ...over,
});

beforeEach(() => {
  resetRenderers();
});

describe('useExtensionRenderers', () => {
  it('reads which languages are claimed, for this workspace', async () => {
    const surfaces = fakeSurfaces();
    const renderers = useExtensionRenderers({ surfaces, workspaceId: 'ws1' });

    await renderers.load();

    expect(surfaces.entriesFor).toHaveBeenCalledWith('code-block', { workspaceId: 'ws1' });
    expect(renderers.languages.value).toEqual(['mermaid']);
  });

  // "Enabled for the workspace" is the extension's own answer, given by a
  // `when` the main process evaluates — not a setting AgentRQ keeps.
  it('has nothing claimed where the extension said no', async () => {
    const surfaces = fakeSurfaces({ entriesFor: vi.fn(async () => []) });
    const renderers = useExtensionRenderers({ surfaces, workspaceId: 'ws2' });

    await renderers.load();

    expect(renderers.languages.value).toEqual([]);
  });

  it('asks whoever claimed the language, and passes the source through', async () => {
    const surfaces = fakeSurfaces();
    const renderers = useExtensionRenderers({ surfaces, workspaceId: 'ws1' });
    await renderers.load();

    const answer = await renderers.render('mermaid', 'graph TD;\n  A-->B;');

    expect(surfaces.invokeQuietly).toHaveBeenCalledWith(
      { owner: 'mermaid-agentrq', id: 'mermaid', surface: 'code-block' },
      { workspaceId: 'ws1', language: 'mermaid', source: 'graph TD;\n  A-->B;' },
    );
    expect(answer.ok).toBe(true);
  });

  it('asks nobody about a language nobody claimed', async () => {
    const surfaces = fakeSurfaces();
    const renderers = useExtensionRenderers({ surfaces });
    await renderers.load();

    expect(await renderers.render('vega', 'x')).toEqual({ ok: false });
    expect(surfaces.invokeQuietly).not.toHaveBeenCalled();
  });

  // Local to the block, on every path: a message must not lose its text
  // because one diagram would not parse.
  it('reports a refusal rather than throwing', async () => {
    const refusing = useExtensionRenderers({
      surfaces: fakeSurfaces({ invokeQuietly: vi.fn(async () => ({ ok: false, reason: 'not a diagram' })) }),
    });
    await refusing.load();
    expect(await refusing.render('mermaid', 'x')).toEqual({ ok: false, reason: 'not a diagram' });

    const throwing = useExtensionRenderers({
      surfaces: fakeSurfaces({ invokeQuietly: vi.fn(async () => undefined) }),
    });
    await throwing.load();
    expect(await throwing.render('mermaid', 'x')).toEqual({ ok: false, reason: '' });
  });

  it('stays inert with no bridge, so the web build is absent rather than broken', async () => {
    const surfaces = fakeSurfaces({ available: false });
    const renderers = useExtensionRenderers({ surfaces });

    await renderers.load();

    expect(renderers.available).toBe(false);
    expect(surfaces.entriesFor).not.toHaveBeenCalled();
    expect(renderers.languages.value).toEqual([]);
    expect(await renderers.render('mermaid', 'x')).toEqual({ ok: false });
  });

  it('ignores an entry that claimed no language', async () => {
    const surfaces = fakeSurfaces({ entriesFor: vi.fn(async () => [entry({ language: undefined })]) });
    const renderers = useExtensionRenderers({ surfaces });

    await renderers.load();

    expect(renderers.languages.value).toEqual([]);
  });

  it('finds its own surfaces when it is given none', () => {
    expect(useExtensionRenderers().available).toBe(false);
  });

  /**
   * Shared across the application, not per component.
   *
   * Every message body reads this list, a conversation is many bodies, and each
   * is re-rendered as one is typed near — a list per body would be a bridge
   * call per message and then per keystroke.
   */
  it('is one list for the whole app, read once', async () => {
    const surfaces = fakeSurfaces();
    const first = useExtensionRenderers({ surfaces });
    const second = useExtensionRenderers({ surfaces });

    await first.load();

    expect(second.languages.value).toEqual(['mermaid']);
  });

  it('does not let a slow read overwrite a newer one', async () => {
    let resolveSlow;
    const surfaces = fakeSurfaces({
      entriesFor: vi
        .fn()
        .mockImplementationOnce(() => new Promise((resolve) => { resolveSlow = resolve; }))
        .mockImplementationOnce(async () => [entry({ language: 'vega' })]),
    });
    const renderers = useExtensionRenderers({ surfaces });

    const slow = renderers.load();
    await renderers.load();
    resolveSlow([entry({ language: 'mermaid' })]);
    await slow;

    // The second answer stands, not whichever finished last.
    expect(renderers.languages.value).toEqual(['vega']);
  });
});
