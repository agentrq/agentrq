import { cacheSettingKey } from './useCachedTasks';
import { deleteTasks, listAllCachedTasks, metaKey, STORE_META } from './useLocalCache';

/**
 * How long the local copy is kept, and the sweep that enforces it.
 *
 * ## What the clock measures
 *
 * A record's `cachedAt` — when this device last had a reason to keep it, which
 * is stamped on every write. Deliberately **not** the server's `updatedAt`: a
 * limit measured against that would delete a task the moment after someone
 * opened an old one, which is the opposite of what a cache is for. Anything you
 * keep touching stays; anything you stop touching ages out.
 *
 * ## What this is not
 *
 * Not an operating-system background job. Browsers do not reliably offer one —
 * Periodic Background Sync is Chromium-only and gated on engagement heuristics
 * nobody can predict — so the sweep runs while the app is open. That is enough
 * for something whose only failure mode is holding data slightly longer than
 * asked, and it is worth saying plainly rather than implying otherwise.
 */

/** Stored beside the on/off setting, and per device for the same reason. */
export const RETENTION_PREFIX = 'local_cache_days_';

/** How often a sweep is worth running. Expiry is measured in days. */
export const SWEEP_INTERVAL_MS = 24 * 60 * 60 * 1000;

const DAY_MS = 24 * 60 * 60 * 1000;

/** `0` means keep indefinitely, which is the default. */
export const RETENTION_OPTIONS = [
  { days: 0, label: 'Until I clear it' },
  { days: 7, label: '7 days' },
  { days: 30, label: '30 days' },
  { days: 90, label: '90 days' },
];

export function retentionKey(workspaceId) {
  return `${RETENTION_PREFIX}${workspaceId}`;
}

/**
 * How many days this workspace keeps its local copy, on this device.
 *
 * Zero means indefinitely, and is what an unset, unreadable or nonsensical
 * value resolves to. Keeping data slightly too long is a storage cost the
 * budget already bounds; deleting someone's offline copy because a setting
 * could not be parsed is a surprise they cannot undo.
 */
export function getRetentionDays(workspaceId, storage = globalThis.localStorage) {
  if (!workspaceId || !storage) return 0;
  try {
    const days = Number(storage.getItem(retentionKey(workspaceId)));
    return Number.isFinite(days) && days > 0 ? Math.floor(days) : 0;
  } catch {
    return 0;
  }
}

/** Record the choice. Zero clears it rather than storing a zero. */
export function setRetentionDays(workspaceId, days, storage = globalThis.localStorage) {
  if (!workspaceId || !storage) return false;
  try {
    if (Number(days) > 0) storage.setItem(retentionKey(workspaceId), String(Math.floor(days)));
    else storage.removeItem(retentionKey(workspaceId));
    return true;
  } catch {
    return false;
  }
}

/**
 * Whether a record has outlived its workspace's limit.
 *
 * A record with no `cachedAt` was written before this existed, and is treated
 * as current rather than as infinitely old — an upgrade should not delete
 * someone's cache on first run.
 */
export function isExpired(record, days, now = Date.now()) {
  if (!(days > 0)) return false;
  const cachedAt = Number(record?.cachedAt);
  if (!Number.isFinite(cachedAt)) return false;
  return now - cachedAt > days * DAY_MS;
}

/**
 * The records to remove, given every record and the per-workspace limits.
 *
 * The limit is looked up once per workspace rather than once per record: a
 * sweep reads thousands of rows and would otherwise hit local storage for each
 * of them.
 *
 * @param {Array<object>} records
 * @param {(workspaceId: string) => number} limitFor
 * @param {number} [now]
 */
export function expiredRecords(records, limitFor, now = Date.now()) {
  const list = Array.isArray(records) ? records : [];
  const limits = new Map();

  return list.filter((record) => {
    const workspaceId = record?.workspaceId;
    if (!workspaceId) return false;
    if (!limits.has(workspaceId)) limits.set(workspaceId, limitFor(workspaceId));
    return isExpired(record, limits.get(workspaceId), now);
  });
}

/** Whether enough time has passed since the last sweep to run another. */
export function dueForSweep(lastSweptAt, now = Date.now(), interval = SWEEP_INTERVAL_MS) {
  const last = Number(lastSweptAt);
  if (!Number.isFinite(last)) return true;
  // A clock that has moved backwards — a timezone change, a corrected system
  // clock — would otherwise park the sweep until the future caught up.
  if (last > now) return true;
  return now - last >= interval;
}

const SWEPT_AT = 'lastSweptAt';

/** When the last sweep finished, or null if none has. */
export async function readLastSweptAt(db) {
  if (!db) return null;
  return new Promise((resolve) => {
    try {
      const req = db.transaction(STORE_META, 'readonly').objectStore(STORE_META).get(SWEPT_AT);
      req.onsuccess = () => resolve(Number(req.result?.value) || null);
      req.onerror = () => resolve(null);
    } catch {
      resolve(null);
    }
  });
}

/**
 * Record that a sweep finished.
 *
 * Kept in the database rather than in local storage so it survives alongside
 * the data it describes: clearing the cache and keeping the timestamp would
 * mean the next sweep is skipped for a day over nothing.
 */
export async function writeLastSweptAt(db, now = Date.now()) {
  if (!db) return false;
  return new Promise((resolve) => {
    try {
      const tx = db.transaction(STORE_META, 'readwrite');
      tx.objectStore(STORE_META).put({ key: SWEPT_AT, value: now });
      tx.oncomplete = () => resolve(true);
      tx.onerror = () => resolve(false);
      tx.onabort = () => resolve(false);
    } catch {
      resolve(false);
    }
  });
}

/**
 * Remove everything that has outlived its workspace's limit.
 *
 * Returns the number removed, which is what the caller reports and what the
 * tests assert on. Doing nothing is the common case and costs one scan.
 */
export async function sweepExpired(db, options = {}) {
  const { storage, now = Date.now(), KeyRange } = options;
  if (!db) return 0;

  const records = await listAllCachedTasks(db);
  const doomed = expiredRecords(records, (id) => getRetentionDays(id, storage), now);
  if (doomed.length === 0) return 0;

  return deleteTasks(db, doomed, KeyRange);
}

/**
 * Sweep, but at most once a day.
 *
 * The guard is what makes this safe to call on every startup and on a timer:
 * the work happens when it is due and is skipped the rest of the time, without
 * the caller having to know which.
 */
export async function sweepIfDue(db, options = {}) {
  const { now = Date.now(), interval = SWEEP_INTERVAL_MS } = options;
  if (!db) return { swept: false, removed: 0 };

  if (!dueForSweep(await readLastSweptAt(db), now, interval)) {
    return { swept: false, removed: 0 };
  }

  const removed = await sweepExpired(db, options);
  await writeLastSweptAt(db, now);
  return { swept: true, removed };
}

/**
 * Run `task` when the browser is not busy, or soon if it never says so.
 *
 * A sweep competes with rendering for the main thread, and there is never a
 * reason for it to win — it is a day's worth of tidying, not something anyone
 * is waiting on.
 */
export function whenIdle(task, scheduler = globalThis.requestIdleCallback) {
  if (typeof scheduler === 'function') return scheduler(() => task());
  return setTimeout(task, 0);
}
