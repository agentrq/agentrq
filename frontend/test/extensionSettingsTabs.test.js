// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest';

import {
  entryForTab,
  isExtensionTab,
  tabIdFor,
  useExtensionSettingsTabs,
} from '../src/composables/useExtensionSettingsTabs';

/**
 * The fourth UI surface, and the only one that answers "configure this
 * extension, *here*".
 *
 * Two things are load-bearing. It is always resolved for a workspace, which is
 * what lets one extension hold different settings for different ones without
 * inventing its own scheme for keying them. And what it draws is the same node
 * vocabulary as everything else — the extension never styles anything, so a tab
 * cannot look like somebody else's software sitting inside this one.
 */

const entry = (over = {}) => ({ owner: 'guardrail', id: 'rules', label: 'Guardrail', order: 100, ...over });

function fakeSurfaces(over = {}) {
  return {
    available: true,
    entriesFor: vi.fn(async () => [entry()]),
    invoke: vi.fn(async () => {}),
    panel: { value: null },
    values: {},
    error: { value: '' },
    busy: { value: false },
    setValue: vi.fn(),
    submit: vi.fn(),
    dismiss: vi.fn(),
    ...over,
  };
}

describe('tabIdFor', () => {
  /** Namespaced, so an extension cannot claim `general` or `danger`. */
  it('names a tab after the extension and its entry', () => {
    expect(tabIdFor(entry())).toBe('ext:guardrail:rules');
  });

  it('tells an extension tab from one of ours', () => {
    expect(isExtensionTab('ext:guardrail:rules')).toBe(true);
    expect(isExtensionTab('general')).toBe(false);
    expect(isExtensionTab(undefined)).toBe(false);
  });
});

describe('entryForTab', () => {
  it('finds the entry a tab id names', () => {
    expect(entryForTab([entry()], 'ext:guardrail:rules')).toEqual(entry());
  });

  /**
   * Matched against what is loaded rather than parsed out of the id: an
   * extension can be uninstalled while its settings tab is open, and the honest
   * answer then is that there is no such tab.
   */
  it('has nothing for a tab whose extension is gone', () => {
    expect(entryForTab([], 'ext:guardrail:rules')).toBeNull();
    expect(entryForTab([entry()], 'ext:standup:today')).toBeNull();
  });
});

describe('useExtensionSettingsTabs', () => {
  it('asks for this workspace, and draws one nav row per tab', async () => {
    const surfaces = fakeSurfaces();
    const tabs = useExtensionSettingsTabs({ surfaces });

    await tabs.load('ws1');

    expect(surfaces.entriesFor).toHaveBeenCalledWith('workspace-settings-tab', { workspaceId: 'ws1' });
    expect(tabs.tabs.value).toEqual([
      { id: 'ext:guardrail:rules', label: 'Guardrail', owner: 'guardrail', entry: entry() },
    ]);
  });

  /**
   * Desktop only. With no bridge the list is empty and the divider never
   * appears, so the browser build is unchanged rather than showing an empty
   * section with nothing under it.
   */
  it('offers nothing where there is no bridge', async () => {
    const surfaces = fakeSurfaces({ available: false });
    const tabs = useExtensionSettingsTabs({ surfaces });

    await tabs.load('ws1');

    expect(tabs.tabs.value).toEqual([]);
    expect(surfaces.entriesFor).not.toHaveBeenCalled();
  });

  /** A tab is per workspace, so without one there is nothing to ask about. */
  it('offers nothing without a workspace', async () => {
    const surfaces = fakeSurfaces();
    const tabs = useExtensionSettingsTabs({ surfaces });

    await tabs.load('');

    expect(tabs.tabs.value).toEqual([]);
    expect(surfaces.entriesFor).not.toHaveBeenCalled();
  });

  it('forgets a previous workspace\'s tabs when it is asked about none', async () => {
    const surfaces = fakeSurfaces();
    const tabs = useExtensionSettingsTabs({ surfaces });
    await tabs.load('ws1');

    await tabs.load('');
    expect(tabs.tabs.value).toEqual([]);

    // And with nothing passed at all, which is what a screen that has not
    // resolved its workspace yet hands over.
    await tabs.load('ws1');
    await tabs.load();
    expect(tabs.tabs.value).toEqual([]);
  });

  /**
   * The workspace goes in the context, so the extension's handler reads it the
   * same way a header action does — and the same way a submit will, since a
   * submit is an invoke of this same entry with the typed values added.
   */
  it('draws a tab against the workspace it was loaded for', async () => {
    const surfaces = fakeSurfaces();
    const tabs = useExtensionSettingsTabs({ surfaces });
    await tabs.load('ws1');

    await tabs.open(entry());

    expect(surfaces.invoke).toHaveBeenCalledWith(
      { owner: 'guardrail', id: 'rules', surface: 'workspace-settings-tab' },
      { workspaceId: 'ws1' },
    );
  });

  it('draws nothing for a tab that is no longer there', async () => {
    const surfaces = fakeSurfaces();
    const tabs = useExtensionSettingsTabs({ surfaces });

    await tabs.open(null);

    expect(surfaces.invoke).not.toHaveBeenCalled();
  });

  it('passes the drawing and the typing straight through', () => {
    const surfaces = fakeSurfaces();
    const tabs = useExtensionSettingsTabs({ surfaces });

    expect(tabs.panel).toBe(surfaces.panel);
    expect(tabs.values).toBe(surfaces.values);
    expect(tabs.error).toBe(surfaces.error);
    expect(tabs.busy).toBe(surfaces.busy);
    expect(tabs.setValue).toBe(surfaces.setValue);
    expect(tabs.submit).toBe(surfaces.submit);
    expect(tabs.dismiss).toBe(surfaces.dismiss);
    expect(tabs.available).toBe(true);
  });

  it('builds its own surfaces when it is given none', () => {
    // The browser, where there is no bridge at all — it has to construct
    // without one rather than requiring a caller to pass a double.
    expect(useExtensionSettingsTabs().tabs.value).toEqual([]);
  });
});
