import { cacheSettingKey, resetSharedCache, SETTING_OFF } from './useCachedTasks';
import { clearWorkspace, destroyCache } from './useLocalCache';

/**
 * Owning the local copy: turning it off, seeing what it costs, and getting rid
 * of it.
 *
 * The cache holds a readable copy of someone's task titles and conversations on
 * their disk, so the three things they must be able to do are see how much of
 * it there is, stop it, and delete it. Everything here exists for one of those.
 */

/**
 * Turn the local copy on or off for one workspace, on this device.
 *
 * Off is stored; on is the absence of the setting. That keeps the default in
 * one place — the reader — rather than in every device that has ever opened the
 * workspace, and it means a workspace nobody has an opinion about takes up no
 * storage keys at all.
 *
 * @returns {boolean} whether the preference was recorded
 */
export function setCacheEnabled(workspaceId, enabled, storage = globalThis.localStorage) {
  if (!workspaceId || !storage) return false;
  try {
    if (enabled) storage.removeItem(cacheSettingKey(workspaceId));
    else storage.setItem(cacheSettingKey(workspaceId), SETTING_OFF);
    return true;
  } catch {
    // A storage that cannot be written is one the reader will refuse to trust
    // either, so this resolves to the same place: no local copy.
    return false;
  }
}

/** Sizes as people read them, at the precision the number deserves. */
const UNITS = ['B', 'KB', 'MB', 'GB'];

/**
 * A byte count as a short human string.
 *
 * One decimal place below 10 and none above it, so "9.4 MB" and "312 MB" both
 * read at a glance without implying precision the estimate does not have —
 * browsers deliberately round what they report.
 */
export function formatBytes(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n <= 0) return '0 B';

  let value = n;
  let unit = 0;
  while (value >= 1024 && unit < UNITS.length - 1) {
    value /= 1024;
    unit += 1;
  }
  const rounded = value < 10 && unit > 0 ? value.toFixed(1) : Math.round(value);
  return `${rounded} ${UNITS[unit]}`;
}

/**
 * How much storage this origin is using, as the browser reports it.
 *
 * Whole-origin rather than per-workspace: the browser does not break its
 * estimate down, and inventing a per-workspace figure by adding up record sizes
 * would be a guess presented as a measurement. Better to say what is actually
 * known.
 *
 * Resolves to null when the browser will not say — older engines, and private
 * windows that decline. The caller hides the figure rather than showing zero,
 * because zero is a claim and null is an absence.
 */
export async function estimateUsage(manager = globalThis.navigator?.storage) {
  if (!manager?.estimate) return null;
  try {
    const { usage, quota } = (await manager.estimate()) ?? {};
    if (!Number.isFinite(usage)) return null;
    return { usage, quota: Number.isFinite(quota) ? quota : 0 };
  } catch {
    return null;
  }
}

/**
 * Ask the browser not to evict this data under storage pressure.
 *
 * Worth asking once, and worth not caring about the answer: every engine has
 * its own rules for granting it, and a cache that gets evicted behaves exactly
 * like a cache that was never written. Never throws.
 */
export async function requestPersistence(manager = globalThis.navigator?.storage) {
  if (!manager?.persist) return false;
  try {
    return (await manager.persist()) === true;
  } catch {
    return false;
  }
}

/**
 * Forget one workspace's local copy, and report what that freed.
 *
 * The figure is measured rather than calculated — the difference between two
 * estimates either side of the delete. That makes it honest about being an
 * estimate, and it stays right when the stored shape changes later.
 *
 * A browser that will not estimate still clears; it just cannot say how much,
 * and `reclaimed` is null rather than a made-up zero.
 *
 * @returns {Promise<{cleared: boolean, reclaimed: number|null}>}
 */
export async function clearWorkspaceData(db, workspaceId, options = {}) {
  const { KeyRange = globalThis.IDBKeyRange, manager } = options;
  if (!db || !workspaceId) return { cleared: false, reclaimed: null };

  const before = await estimateUsage(manager);
  const cleared = await clearWorkspace(db, workspaceId, KeyRange);
  if (!cleared) return { cleared: false, reclaimed: null };

  const after = await estimateUsage(manager);
  const reclaimed =
    before && after ? Math.max(0, before.usage - after.usage) : null;
  return { cleared: true, reclaimed };
}

/**
 * Everything this device holds for a user, gone.
 *
 * Signing out runs this, and it is deliberately unconditional. A browser that
 * several people use must not leave one person's task titles readable by the
 * next, and there is no partial version of that guarantee worth having — so it
 * does not consult the per-workspace setting, and does not care whether the
 * delete was blocked by another tab. The connection is dropped either way.
 */
export async function forgetEverything({ userId, factory = globalThis.indexedDB } = {}) {
  resetSharedCache();
  if (!userId) return false;
  return destroyCache({ userId, factory });
}
