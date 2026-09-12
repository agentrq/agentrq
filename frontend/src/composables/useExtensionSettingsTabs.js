// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { computed, ref } from 'vue';

import { useExtensionSurfaces } from './useExtensionSurfaces';

/**
 * Extensions' own tabs in a workspace's settings.
 *
 * The fourth UI surface, and the only one that answers *"configure this
 * extension, here"*. The other three are places to go (`page`), things to do
 * (`workspace-action`) and things to do to a task (`task-menu`) — none of them
 * is where somebody looks for a setting, and an extension that wanted one had
 * nowhere to put it but the manifest's declared config fields, which the app
 * renders and whose shape the app decides.
 *
 * ## Always per workspace, which is the point
 *
 * Resolved with `workspaceId` in context, like a header action. That is what
 * lets one extension hold different settings for different workspaces without
 * inventing its own scheme for keying them — and it pairs with `ctx.storage`'s
 * `workspace(id)` scope on the other side of the bridge.
 *
 * ## The tab's content is a view, not a component
 *
 * The extension returns the same node vocabulary everything else returns, now
 * including inputs. It never gets to style anything, so a tab cannot look like
 * somebody else's software sitting inside this one — which is the only version
 * of "an extension cannot break the design" that is a fact rather than a
 * request.
 *
 * ## Loaded when the screen opens, not held in a store
 *
 * Settings is a screen somebody navigates to deliberately, so the bridge call
 * is on a gesture — and an extension installed a moment ago has its tab on the
 * next visit rather than after a reload.
 */

/** The tab id a contributed entry answers to, namespaced so it cannot collide. */
export function tabIdFor(entry) {
  return `ext:${entry.owner}:${entry.id}`;
}

/** Whether the screen is currently showing an extension's tab. */
export function isExtensionTab(id) {
  return String(id ?? '').startsWith('ext:');
}

/**
 * The entry a tab id names, or null.
 *
 * Matched against the loaded list rather than parsed out of the id: an
 * extension can be uninstalled while its settings tab is open, and the honest
 * answer then is that there is no such tab rather than a target to invoke.
 */
export function entryForTab(entries, id) {
  return entries.find((entry) => tabIdFor(entry) === id) ?? null;
}

export function useExtensionSettingsTabs({ surfaces = useExtensionSurfaces() } = {}) {
  const entries = ref([]);
  /** Which workspace the loaded tabs belong to, so a stale list is not reused. */
  const loadedFor = ref('');

  async function load(workspaceId) {
    const id = String(workspaceId ?? '');
    if (!surfaces.available || !id) {
      entries.value = [];
      loadedFor.value = '';
      return;
    }
    entries.value = await surfaces.entriesFor('workspace-settings-tab', { workspaceId: id });
    loadedFor.value = id;
  }

  /**
   * Draw one extension's tab.
   *
   * The workspace goes in the context, so the extension's handler reads it the
   * same way a header action does — and the same way a submit will, since a
   * submit is an invoke of this same entry with the typed values added.
   */
  async function open(entry) {
    if (!entry) return;
    await surfaces.invoke(
      { owner: entry.owner, id: entry.id, surface: 'workspace-settings-tab' },
      { workspaceId: loadedFor.value },
    );
  }

  return {
    available: surfaces.available,
    /** One nav row per contributed tab, in the shape the settings screen draws. */
    tabs: computed(() =>
      entries.value.map((entry) => ({
        id: tabIdFor(entry),
        label: entry.label,
        owner: entry.owner,
        entry,
      })),
    ),
    entries,
    panel: surfaces.panel,
    values: surfaces.values,
    error: surfaces.error,
    busy: surfaces.busy,
    load,
    open,
    setValue: surfaces.setValue,
    submit: surfaces.submit,
    dismiss: surfaces.dismiss,
  };
}
