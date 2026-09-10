import { computed, ref } from 'vue';

import { useExtensionSurfaces } from './useExtensionSurfaces';

/**
 * The pages and header actions extensions contribute, and where they live.
 *
 * The other two surfaces shipped registered and unreachable: an extension could
 * add a `page` or a `workspace-action`, the host held it, and nothing in the
 * renderer ever asked. `standup` registered a page and a shortcut, `digest`
 * registered three surfaces, and only the task menu could be reached by anybody.
 *
 * ## Held, not fetched per render
 *
 * A task menu asks on right-click, which is a gesture. A sidebar cannot: it is
 * drawn on every navigation, and a bridge call per render would be a call per
 * keystroke in the address bar. So this is loaded once and refreshed when
 * something is installed or removed — the two moments the answer changes.
 *
 * ## Addressed by owner and id
 *
 * `/extensions/:name/:pageId`. The name is the extension's, which the manifest
 * made an address rather than a label, and the id only has to be unique within
 * it — which is exactly what the `ui` registry's owner scope guarantees.
 */

/** Where one contributed page lives. */
export function routeFor(entry) {
  return `/extensions/${encodeURIComponent(entry.owner)}/${encodeURIComponent(entry.id)}`;
}

/**
 * Matches a route back to the entry it names.
 *
 * Compared after decoding, because the route carries what `routeFor` encoded
 * and an extension name is a plain identifier either way — but a page id is an
 * author's string and could hold anything.
 */
export function findEntry(entries, { name, pageId }) {
  return entries.find((entry) => entry.owner === name && entry.id === pageId) ?? null;
}

export function useExtensionPages({ surfaces = useExtensionSurfaces() } = {}) {
  const pages = ref([]);
  const actions = ref([]);

  /**
   * Read what is contributed now.
   *
   * The context is empty for pages: a page belongs to the application rather
   * than to a workspace, so there is nothing about *this* moment for a `when`
   * to decide on. Header actions are asked per workspace, because there is.
   */
  async function load({ workspaceId = '' } = {}) {
    if (!surfaces.available) return;
    pages.value = await surfaces.entriesFor('page', {});
    actions.value = workspaceId ? await surfaces.entriesFor('workspace-action', { workspaceId }) : [];
  }

  return {
    available: surfaces.available,
    pages: computed(() => pages.value.map((entry) => ({ ...entry, to: routeFor(entry) }))),
    actions,
    load,
    surfaces,
  };
}
