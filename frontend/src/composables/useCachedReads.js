import { onMounted, onUnmounted, ref } from 'vue';

import { isCacheEnabled } from './useCachedTasks';
import { getCachedTask, listAllCachedTasks, listCachedTasks } from './useLocalCache';

/**
 * Reading the local copy, and the rules about when it is allowed to be shown.
 *
 * ## Stale first, then true
 *
 * A list paints from the cache and is then replaced by the server's answer.
 * The cached rows are worth showing because they are almost always right and
 * they are instant; the replacement matters because "almost always" is not a
 * guarantee anyone should have to reason about.
 *
 * ## Except a conversation
 *
 * An open task is the one place this does not apply past the first frame. The
 * backend is the source of truth for a conversation, so the cache may paint it
 * once and is then overwritten wholesale — nothing merges local state into a
 * server response, and a cached copy never wins a disagreement. A stale list is
 * a minor annoyance; a stale conversation is someone replying to a message that
 * has already been answered.
 */

/**
 * Whether a cached read should be painted at all.
 *
 * Only into an empty view. Once the server's answer is on screen, or the user
 * has scrolled another page into place, a cached read arriving late would
 * replace something newer with something older — which is the one thing a cache
 * must never do.
 */
export function shouldPaintCache(currentRows, cachedRows) {
  return (
    Array.isArray(cachedRows) &&
    cachedRows.length > 0 &&
    (!Array.isArray(currentRows) || currentRows.length === 0)
  );
}

/**
 * The cached tasks for one workspace, newest first.
 *
 * Empty whenever there is nothing to show or nothing we are allowed to show —
 * no connection, no cache, or a workspace whose owner turned it off. The caller
 * cannot tell those apart, and does not need to: in every case it goes to the
 * network, which is what it would have done anyway.
 */
export async function readCachedTasks(db, workspaceId, options = {}) {
  const { storage, KeyRange = globalThis.IDBKeyRange, limit = 0 } = options;
  if (!db || !isCacheEnabled(workspaceId, storage)) return [];

  const rows = await listCachedTasks(db, workspaceId, KeyRange);
  return limit > 0 ? rows.slice(0, limit) : rows;
}

/**
 * Every cached task the user is allowed to see, newest first.
 *
 * Deliberately needs no list of workspaces. The global list paints before it
 * has asked the server which workspaces exist, and requiring that list first
 * would put a round trip in front of the one thing the cache is for.
 *
 * The per-workspace opt-out is applied to each row as it comes back, so a
 * workspace someone turned off stays invisible even if its rows outlived the
 * clear.
 */
export async function readAllCachedTasks(db, options = {}) {
  const { storage, limit = 0 } = options;
  if (!db) return [];

  const allowed = new Map();
  const rows = (await listAllCachedTasks(db)).filter((row) => {
    if (!allowed.has(row.workspaceId)) {
      allowed.set(row.workspaceId, isCacheEnabled(row.workspaceId, storage));
    }
    return allowed.get(row.workspaceId);
  });

  return limit > 0 ? rows.slice(0, limit) : rows;
}

/**
 * The cached copy of one task, for the first frame only.
 *
 * The caller must overwrite this with the server's response rather than merging
 * into it. See the note at the top of this file.
 */
export async function readCachedTask(db, workspaceId, taskId, { storage } = {}) {
  if (!db || !taskId || !isCacheEnabled(workspaceId, storage)) return null;
  return getCachedTask(db, workspaceId, taskId);
}

/** Shown where a reply would go when there is no connection to send it over. */
export const OFFLINE_NOTICE =
  'You are offline. Tasks you have opened before are readable, but replying needs a connection.';

/**
 * Whether the browser believes it has no connection.
 *
 * `navigator.onLine` is famously optimistic — it reports a network interface
 * rather than reachability — so this is used only to *disable* an action and
 * explain why, never to decide whether data is trustworthy. Being wrong in the
 * optimistic direction leaves the app exactly as it is today.
 */
export function isOffline(nav = globalThis.navigator) {
  return nav?.onLine === false;
}

/**
 * A ref tracking whether the browser is offline, for as long as the component
 * using it is mounted.
 *
 * The listeners are what make it a live answer rather than a reading taken once
 * at mount, which would leave the composer disabled long after the connection
 * came back.
 */
export function useOffline(options = {}) {
  const {
    target = globalThis.window,
    nav = globalThis.navigator,
    onMounted: mount = onMounted,
    onUnmounted: unmount = onUnmounted,
  } = options;

  const offline = ref(isOffline(nav));
  const sync = () => {
    offline.value = isOffline(nav);
  };

  mount(() => {
    sync();
    target?.addEventListener('online', sync);
    target?.addEventListener('offline', sync);
  });

  unmount(() => {
    target?.removeEventListener('online', sync);
    target?.removeEventListener('offline', sync);
  });

  return { offline, sync };
}
