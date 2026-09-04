/**
 * Which requests the service worker may keep, and how much of it.
 *
 * ## Why attachments are not in IndexedDB
 *
 * The UI never renders an attachment from the base64 the API sends inline.
 * Every image, video, PDF and download points at the attachment URL, so a
 * response kept in Cache Storage is served to `<img src>` by the browser with
 * no component change at all. Cache Storage also holds a binary body, where a
 * base64 string in an IndexedDB record would cost a third again on top of the
 * bytes and be paid back on every read of the record holding it.
 *
 * ## Why the API route had to be narrowed
 *
 * The service worker used to keep *every* `/api/` response. Now that the local
 * database owns task data, that would be a second cache with a different
 * lifetime answering the same question — and the two would disagree in a way
 * that looks like a bug in the database rather than in the service worker.
 *
 * So task reads are excluded and everything else is left alone. The workspace
 * and user endpoints still benefit from being cached: they are what the shell
 * needs to render at all, and nothing else claims them.
 */

/** Attachment bytes above this are streamed every time rather than kept. */
export const MAX_ATTACHMENT_BYTES = 2 * 1024 * 1024;

/**
 * How much of this device to spend on attachment bytes.
 *
 * The desktop app writes into a partition folder it owns and is under no
 * browser quota pressure, so it can afford an order of magnitude more than a
 * web origin, which shares a quota with everything else on it.
 */
export const WEB_BUDGET_BYTES = 100 * 1024 * 1024;
export const DESKTOP_BUDGET_BYTES = 1024 * 1024 * 1024;

export function budgetFor(platform) {
  return platform === 'desktop' ? DESKTOP_BUDGET_BYTES : WEB_BUDGET_BYTES;
}

/** Where an attachment lives: `/api/v1/workspaces/:id/tasks/:id/attachments/:id`. */
const ATTACHMENT = /\/tasks\/[^/]+\/attachments\/[^/]+$/;

/**
 * A task read the local database now owns.
 *
 * The task list, the global list and a single task. Deliberately not every path
 * containing `/tasks/`: an attachment lives under one, and so do the action
 * endpoints, which are not reads at all.
 */
const TASK_READ = /\/(?:workspaces\/[^/]+\/)?tasks(?:\/[^/]+)?$/;

export function isAttachmentRequest(pathname) {
  return ATTACHMENT.test(String(pathname ?? ''));
}

export function isTaskRead(pathname) {
  const path = String(pathname ?? '');
  return path.includes('/api/') && !isAttachmentRequest(path) && TASK_READ.test(path);
}

/**
 * Whether the service worker should keep this API response.
 *
 * Everything under `/api/` except the task reads the database owns and the
 * attachments that have a cache of their own.
 */
export function isCacheableApiRead(pathname) {
  const path = String(pathname ?? '');
  return path.includes('/api/') && !isTaskRead(path) && !isAttachmentRequest(path);
}

/**
 * Whether a response is small enough to keep.
 *
 * Read from `content-length` rather than by buffering the body: deciding this
 * must not cost the memory it is trying to save, and a response with no length
 * header is kept, because refusing everything unmeasurable would mean caching
 * almost nothing.
 */
export function withinSizeCap(response, max = MAX_ATTACHMENT_BYTES) {
  const declared = Number(response?.headers?.get?.('content-length'));
  if (!Number.isFinite(declared)) return true;
  return declared <= max;
}

/**
 * Which entries to drop to get back under budget, least-recently-used first.
 *
 * `entries` arrives in recency order, oldest first, which is what Cache Storage
 * gives for free: keys come back in insertion order, and the service worker
 * re-inserts an entry when it is served. That turns insertion order into a
 * recency list without a second store to keep in step with the first.
 *
 * Nothing is dropped while the total fits, and the newest entry is never
 * dropped to make room for itself.
 *
 * @param {Array<{url: string, size: number}>} entries oldest first
 * @param {number} budget
 * @returns {string[]} urls to delete
 */
export function evictionPlan(entries, budget) {
  const list = Array.isArray(entries) ? entries : [];
  let total = list.reduce((sum, entry) => sum + (Number(entry?.size) || 0), 0);
  if (total <= budget) return [];

  const doomed = [];
  for (const entry of list) {
    if (total <= budget || doomed.length === list.length - 1) break;
    doomed.push(entry.url);
    total -= Number(entry?.size) || 0;
  }
  return doomed;
}

/**
 * The cached attachment URLs belonging to one workspace.
 *
 * Clearing a workspace has to reach the bytes as well as the rows, and the
 * workspace is in the path, so no extra bookkeeping is needed to find them.
 */
export function attachmentUrlsForWorkspace(urls, workspaceId) {
  if (!workspaceId) return [];
  const needle = `/workspaces/${workspaceId}/`;
  return (Array.isArray(urls) ? urls : []).filter(
    (url) => String(url).includes(needle) && isAttachmentRequest(String(url))
  );
}
