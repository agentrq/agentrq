import { mergeTaskUpdate } from './useTaskEvents';
import { getCachedTask, openCache, putTask } from './useLocalCache';
import { taskTerms } from './useTaskIndex';

/**
 * Filling the local cache from what the server said.
 *
 * ## The rule this module exists to enforce
 *
 * **The cache is written only from server responses and SSE events.** Never
 * from a user action, and never replayed back. There is no write queue and no
 * conflict resolution, because there is nothing to reconcile: every row here
 * came from the backend, and while the user is online the backend is what any
 * open task renders. The cache can be stale, but it can never be *divergent*.
 *
 * That is a deliberately small promise, and it is what makes the feature safe
 * to ship. Anything that let a local edit outlive a failed request would need
 * an answer for two people editing the same task, and this has none.
 *
 * ## What gets cached
 *
 * Exactly what the user loads, and nothing else. There is no backfill: a
 * workspace nobody opens costs nothing, and the cache grows to fit how the
 * person actually works rather than how large their history happens to be.
 */

/** Per-workspace, per-device. See `isCacheEnabled`. */
export const CACHE_SETTING_PREFIX = 'local_cache_';

/** The stored value that means off. Anything else, including nothing, is on. */
export const SETTING_OFF = 'off';

export function cacheSettingKey(workspaceId) {
  return `${CACHE_SETTING_PREFIX}${workspaceId}`;
}

/**
 * Whether this workspace is cached on this device.
 *
 * On by default: the setting exists to turn something off, so its absence is
 * consent to the default rather than an unanswered question.
 *
 * It is deliberately **local and unsynced**, unlike every other workspace
 * setting. What it controls is a per-device store against a per-device quota,
 * so the same workspace can be cached on one machine and not another, and
 * neither learns about the other. On the desktop each profile has its own
 * Electron session partition, so profiles get their own answer for free.
 *
 * A storage that cannot be read resolves to **off**, not on. Failing to cache
 * costs a little speed; caching for someone who turned it off breaks a promise
 * that was made to them, and only one of those is worth risking.
 */
export function isCacheEnabled(workspaceId, storage = globalThis.localStorage) {
  if (!workspaceId || !storage) return false;
  try {
    return storage.getItem(cacheSettingKey(workspaceId)) !== SETTING_OFF;
  } catch {
    return false;
  }
}

/**
 * Cache one task, if its workspace is cached at all.
 *
 * Terms are computed here rather than by the caller: a write is the only moment
 * the text is known to have changed, and computing them anywhere else invites a
 * record whose index does not match its own title.
 *
 * @returns {Promise<boolean>} whether it was written
 */
export async function cacheTask(db, task, { storage } = {}) {
  if (!db || !task?.workspaceId) return false;
  if (!isCacheEnabled(task.workspaceId, storage)) return false;

  return putTask(db, task, taskTerms(task));
}

/**
 * Cache a page of tasks.
 *
 * Sequential rather than parallel: a list is up to fifty tasks, each opening a
 * transaction across three stores, and firing them all at once buys nothing
 * while making the database contend with itself behind whatever the user does
 * next.
 *
 * @returns {Promise<number>} how many were written
 */
export async function cacheTasks(db, tasks, options = {}) {
  const list = Array.isArray(tasks) ? tasks : [];
  let written = 0;
  for (const task of list) {
    if (await cacheTask(db, task, options)) written += 1;
  }
  return written;
}

/**
 * Fold an SSE payload into the task on screen, and cache the result.
 *
 * **The merged task is what gets cached, never the raw payload.** The backend
 * builds those payloads in several places, and one built without its relations
 * is indistinguishable from one whose relations are genuinely empty — which is
 * the bug `mergeTaskUpdate` exists to make unrepresentable. Caching the raw
 * payload would take that same emptied tool lane and write it to disk, where it
 * would outlive the reload that currently fixes it.
 *
 * Returns the merged task so the caller renders and caches the same thing
 * rather than computing the merge twice.
 */
export function cacheTaskUpdate(db, current, incoming, options = {}) {
  const merged = mergeTaskUpdate(current, incoming);
  // Not awaited: the view should paint the merge now, and a write that has not
  // landed yet is indistinguishable from a cache that never held the task.
  if (merged) cacheTask(db, merged, options);
  return merged;
}

/**
 * Cache a task an event announced, when there is no copy on screen to merge
 * against.
 *
 * The shell watches a stream covering every workspace, so most events it sees
 * are for tasks nobody has open. Writing those payloads straight through would
 * hit the exact problem `cacheTaskUpdate` avoids — a payload built without its
 * relations would overwrite the relations already stored.
 *
 * So the merge happens against the *cached* copy instead, which plays the part
 * the on-screen task plays elsewhere. A task the cache has never seen has
 * nothing to lose, and is written as it arrived.
 */
export async function cacheTaskEvent(db, incoming, options = {}) {
  if (!db || !incoming?.id || !incoming?.workspaceId) return false;
  if (!isCacheEnabled(incoming.workspaceId, options.storage)) return false;

  const held = await getCachedTask(db, incoming.workspaceId, incoming.id);
  return cacheTask(db, mergeTaskUpdate(held, incoming), options);
}

/**
 * The one open connection, and who it belongs to.
 *
 * Module scope because there is one database per signed-in user and no reason
 * for two views to hold separate handles to it. Keyed by user because signing
 * in as someone else must not keep talking to the previous person's database —
 * on the web that is a real sequence, and on the desktop a single profile can
 * change account too.
 */
let current = { userId: null, db: null, pending: null };

/** Test seam, and what signing out calls. */
export function resetSharedCache() {
  current.db?.close?.();
  current = { userId: null, db: null, pending: null };
}

/** The open connection, or null when there is none. Never throws. */
export function sharedCache() {
  return current.db;
}

/**
 * Open the cache for a user, once.
 *
 * Concurrent callers share one attempt rather than racing to open the same
 * database, and a different user closes the old connection first.
 *
 * Resolves to `null` when there is no cache to be had — a private window, a
 * blocked upgrade, storage turned off. Callers treat that the way they treat an
 * empty cache, which is to say they go to the network.
 */
export async function connectCache(userId, options = {}) {
  if (!userId) return null;
  if (current.userId === userId) {
    if (current.db) return current.db;
    if (current.pending) return current.pending;
  } else {
    resetSharedCache();
  }

  current.userId = userId;
  current.pending = openCache({ userId, ...options }).then((db) => {
    // A reset while opening wins: whoever called it wanted this connection gone.
    if (current.userId !== userId) {
      db?.close?.();
      return null;
    }
    current.db = db;
    current.pending = null;
    return db;
  });

  return current.pending;
}
