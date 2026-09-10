import { describe, it, expect, vi } from 'vitest';

import { findEntry, routeFor, useExtensionPages } from '../src/composables/useExtensionPages';

/**
 * The two surfaces that shipped registered and unreachable.
 *
 * An extension could contribute a `page` or a `workspace-action`, the host held
 * it, and nothing in the renderer ever asked — `standup` registered a page and
 * `digest` registered three surfaces, and only the task menu could be reached
 * by anybody.
 */

const entry = (over = {}) => ({ owner: 'standup', id: 'today', label: 'Standup', order: 20, ...over });

const fakeSurfaces = (over = {}) => ({
  available: true,
  entriesFor: vi.fn(async (surface) => (surface === 'page' ? [entry()] : [entry({ id: 'now', label: 'Digest now' })])),
  panel: { value: null },
  error: { value: '' },
  busy: { value: false },
  invoke: vi.fn(),
  dismiss: vi.fn(),
  ...over,
});

describe('routeFor', () => {
  it('addresses a page by its owner and id', () => {
    expect(routeFor(entry())).toBe('/extensions/standup/today');
  });

  // An extension name is a plain identifier, but a page id is a string its
  // author chose and could hold anything a path would swallow.
  it('encodes what an author might have written', () => {
    expect(routeFor(entry({ id: 'this week' }))).toBe('/extensions/standup/this%20week');
    expect(routeFor(entry({ id: 'a/b' }))).toBe('/extensions/standup/a%2Fb');
  });
});

describe('findEntry', () => {
  const entries = [entry(), entry({ owner: 'digest', id: 'digest' })];

  it('matches on both halves, because an id is only unique within its owner', () => {
    expect(findEntry(entries, { name: 'digest', pageId: 'digest' }).owner).toBe('digest');
    // The same id under a different owner is a different page.
    expect(findEntry(entries, { name: 'standup', pageId: 'digest' })).toBeNull();
  });

  // Following a link to something that has since been uninstalled.
  it('answers with nothing rather than undefined', () => {
    expect(findEntry(entries, { name: 'gone', pageId: 'x' })).toBeNull();
    expect(findEntry([], { name: 'standup', pageId: 'today' })).toBeNull();
  });
});

describe('useExtensionPages', () => {
  it('reads the pages, and gives each one somewhere to live', async () => {
    const surfaces = fakeSurfaces();
    const pages = useExtensionPages({ surfaces });

    await pages.load();

    expect(surfaces.entriesFor).toHaveBeenCalledWith('page', {});
    expect(pages.pages.value[0]).toMatchObject({ owner: 'standup', to: '/extensions/standup/today' });
  });

  // A header action's `when` gets to decide about *this* workspace, so it has
  // to be told which one.
  it('asks for header actions per workspace, and only when there is one', async () => {
    const surfaces = fakeSurfaces();
    const pages = useExtensionPages({ surfaces });

    await pages.load({ workspaceId: 'ws1' });
    expect(surfaces.entriesFor).toHaveBeenCalledWith('workspace-action', { workspaceId: 'ws1' });
    expect(pages.actions.value).toHaveLength(1);

    surfaces.entriesFor.mockClear();
    await pages.load();

    expect(surfaces.entriesFor).not.toHaveBeenCalledWith('workspace-action', expect.anything());
    expect(pages.actions.value).toEqual([]);
  });

  it('stays inert with no bridge, so the web build is absent rather than broken', async () => {
    const surfaces = fakeSurfaces({ available: false });
    const pages = useExtensionPages({ surfaces });

    await pages.load({ workspaceId: 'ws1' });

    expect(pages.available).toBe(false);
    expect(surfaces.entriesFor).not.toHaveBeenCalled();
    expect(pages.pages.value).toEqual([]);
  });

  it('finds its own surfaces when it is given none', () => {
    // The web build: no bridge on the window, and nothing throws.
    expect(useExtensionPages().available).toBe(false);
  });
});
