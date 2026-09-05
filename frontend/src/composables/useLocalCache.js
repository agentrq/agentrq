/**
 * The local task cache: schema, writes, and clearing.
 *
 * This is a **cache, never a source of truth.** Everything in it was written
 * from a server response or an SSE event, and an open task always renders what
 * the backend last said. Nothing here is ever replayed back to the server, so
 * there is no write queue and no conflict resolution to get wrong.
 *
 * ## Why the data is split across three stores
 *
 * The point of the cache is to answer "which tasks mention billing" without a
 * request, which means walking every task's title and body. That is fast only
 * while each record stays small — so the searchable record holds text and
 * nothing else:
 *
 * - `tasks` is the scannable part: title, body, status, dates, and the term
 *   index. A workspace with thousands of these is still only a few megabytes.
 * - `threads` holds the message and tool-call history, read by exact key when a
 *   task is opened and never touched by a search.
 * - `attachments` holds *metadata only*. The bytes belong in Cache Storage,
 *   which stores a binary response body rather than a JavaScript string.
 *
 * That last split is the one with teeth. `Attachment.data` arrives as base64
 * **inside the task JSON**, including from the list endpoint, so a task written
 * as it arrives would bury file bytes in the row we most need to keep small —
 * and base64 costs a third again on top of the bytes, paid back on every read
 * of the record holding it. `toTaskRecord` and `toThreadRecord` drop it.
 */

/** Bumping this runs `createSchema` again — see `openCache`. */
export const DB_VERSION = 1;

export const STORE_TASKS = 'tasks';
export const STORE_THREADS = 'threads';
export const STORE_ATTACHMENTS = 'attachments';
export const STORE_META = 'meta';

/**
 * The fields worth caching from a task, in the order the schema lists them.
 *
 * An explicit list rather than a spread, for two reasons: it drops
 * `attachments`, `messages` and `toolCalls` by construction rather than by
 * remembering to delete them, and it produces a plain object from whatever it
 * is handed — which is what makes a Vue reactive proxy safe to pass in.
 */
const TASK_FIELDS = [
  'title',
  'body',
  'status',
  'assignee',
  'createdBy',
  'createdAt',
  'updatedAt',
  'sortOrder',
  'cronSchedule',
  'eventId',
  'workflowId',
];

/**
 * A deep copy containing only structured-cloneable values.
 *
 * IndexedDB clones what it stores, and a Vue reactive object is a Proxy —
 * handing one to `put()` raises `DataCloneError` and loses the write. Rebuilding
 * the value as plain arrays and objects sidesteps that for every shape the API
 * sends, and drops functions and `undefined` on the way through, which the clone
 * algorithm would reject anyway.
 */
export function plain(value) {
  if (Array.isArray(value)) return value.map(plain);
  if (value === null || typeof value !== 'object') {
    return typeof value === 'function' ? undefined : value;
  }

  const out = {};
  for (const [key, raw] of Object.entries(value)) {
    const copied = plain(raw);
    if (copied !== undefined) out[key] = copied;
  }
  return out;
}

/** An attachment without its bytes: everything the UI draws it from. */
function attachmentMeta(attachment) {
  return {
    id: attachment?.id,
    filename: attachment?.filename ?? '',
    mimeType: attachment?.mimeType ?? '',
    // The API sends no size, so it is derived from the base64 it did send —
    // 4 encoded characters carry 3 bytes. Useful for the storage budget even
    // though the bytes themselves are not kept here.
    size: Math.floor(((attachment?.data ?? '').length * 3) / 4),
  };
}

function attachmentsOf(task) {
  return Array.isArray(task?.attachments) ? task.attachments : [];
}

/**
 * The searchable record for a task.
 *
 * `terms` is supplied by the caller rather than computed here: tokenising is a
 * separate concern with its own tests, and the writer already knows whether the
 * text changed.
 *
 * @param {object} task    a task as the API sends it
 * @param {string[]} [terms]
 * @param {number} [now]   stamped as `cachedAt`; injectable for tests
 */
export function toTaskRecord(task, terms = [], now = Date.now()) {
  const record = {
    id: task.id,
    workspaceId: task.workspaceId,
    attachmentCount: attachmentsOf(task).length,
    terms: plain(terms),
    // When this device last had a reason to keep the record, which is what a
    // retention limit is measured against. Deliberately not the server's
    // `updatedAt`: that would expire a task the moment after someone opened an
    // old one, which is the opposite of what a cache should do.
    cachedAt: now,
  };
  for (const field of TASK_FIELDS) {
    if (task[field] !== undefined && task[field] !== null) record[field] = plain(task[field]);
  }
  return record;
}

/**
 * The conversation record: messages and tool calls, with every attachment
 * anywhere inside them reduced to metadata.
 */
export function toThreadRecord(task) {
  const strip = (items) =>
    (Array.isArray(items) ? items : []).map((item) => {
      const copied = plain(item);
      if (Array.isArray(item?.attachments)) copied.attachments = item.attachments.map(attachmentMeta);
      return copied;
    });

  return {
    workspaceId: task.workspaceId,
    taskId: task.id,
    messages: strip(task.messages),
    toolCalls: strip(task.toolCalls),
    attachments: attachmentsOf(task).map(attachmentMeta),
  };
}

/**
 * One metadata row per attachment on the task itself.
 *
 * `lastUsedAt` exists for the eviction pass that arrives with the bytes: the
 * budget is spent on what people actually open.
 */
export function toAttachmentRecords(task, now = Date.now()) {
  return attachmentsOf(task).map((attachment) => ({
    ...attachmentMeta(attachment),
    workspaceId: task.workspaceId,
    taskId: task.id,
    attachmentId: attachment?.id,
    lastUsedAt: now,
  }));
}

/**
 * The database for one signed-in user.
 *
 * The user is in the name because a browser origin outlives a session: sign out
 * of the web app, sign in as someone else, and a shared name would hand the
 * second person the first person's task titles. The desktop app needs no such
 * care between *profiles* — each runs on its own Electron session partition,
 * which gives it its own storage — but a single profile whose account changes
 * has exactly the web build's problem.
 */
export function databaseName(userId) {
  return `agentrq-cache:${userId}`;
}

/** Create the stores and indexes. Runs inside `onupgradeneeded`. */
export function createSchema(db) {
  const tasks = db.createObjectStore(STORE_TASKS, { keyPath: ['workspaceId', 'id'] });
  tasks.createIndex('by_workspace', 'workspaceId');
  tasks.createIndex('by_recent', ['workspaceId', 'updatedAt']);
  tasks.createIndex('by_status', ['workspaceId', 'status']);
  // The search index. `multiEntry` gives one entry per term, which turns
  // "find every task mentioning X" into a seek instead of a scan.
  tasks.createIndex('by_term', 'terms', { multiEntry: true });

  db.createObjectStore(STORE_THREADS, { keyPath: ['workspaceId', 'taskId'] });
  db.createObjectStore(STORE_ATTACHMENTS, { keyPath: ['workspaceId', 'taskId', 'attachmentId'] });
  db.createObjectStore(STORE_META, { keyPath: 'key' });
}

/**
 * Every key beginning with `workspaceId`, for a store whose key is an array
 * starting with it.
 *
 * The upper bound is `[workspaceId, []]` because IndexedDB sorts an array after
 * every number, string and date — so an empty array is the smallest key that
 * sorts above every real key in the workspace, whatever its remaining parts.
 */
export function workspaceRange(workspaceId, KeyRange) {
  return KeyRange.bound([workspaceId], [workspaceId, []]);
}

/** Meta rows are keyed `<workspaceId>:<name>` so they clear by prefix too. */
export function metaKey(workspaceId, name) {
  return `${workspaceId}:${name}`;
}

/**
 * An IndexedDB request as a promise.
 *
 * Exported so the failure path has a test: a request that errors is an ordinary
 * event on a disk-backed store, not a hypothetical.
 */
export function requestResult(req) {
  return new Promise((resolve, reject) => {
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

/** A transaction as a promise. Abort and error are the same answer: it did not happen. */
export function transactionDone(tx) {
  return new Promise((resolve, reject) => {
    tx.oncomplete = () => resolve(true);
    tx.onerror = () => reject(tx.error);
    tx.onabort = () => reject(tx.error);
  });
}

/**
 * Run a cache operation, and treat any failure as "there was no cache".
 *
 * The contract for this whole module is that a cache is an optimisation, so its
 * failures must never reach a user who only wanted to read their tasks. A
 * transaction can abort for reasons that have nothing to do with the caller —
 * the quota filled, the store was evicted mid-flight, the connection closed
 * underneath it — and in every one of those cases the right behaviour is the
 * same as never having had a cache: return the empty answer and let the caller
 * go to the network.
 */
async function attempt(run, fallback) {
  try {
    return await run();
  } catch {
    return fallback;
  }
}

/**
 * Open the cache, or report that there isn't one.
 *
 * **Every failure resolves to `null`, never to an exception.** A cache is an
 * optimisation, and there are ordinary reasons it cannot be had: a private
 * window may refuse to open a database at all, WebKit discards one that has not
 * been used for a week, and an older tab still holding a connection blocks an
 * upgrade indefinitely. None of those are worth an error in front of a user who
 * only wanted to read their tasks — the caller checks for `null` and goes to the
 * network, which is what it would have done anyway.
 *
 * The `versionchange` handler is what stops *this* tab becoming that older tab:
 * it closes on request so a newer one can upgrade instead of hanging.
 *
 * @param {object} options
 * @param {string} options.userId
 * @param {IDBFactory} [options.factory]
 * @param {number} [options.version]
 * @returns {Promise<IDBDatabase|null>}
 */
export async function openCache({ userId, factory = globalThis.indexedDB, version = DB_VERSION } = {}) {
  if (!factory || !userId) return null;

  let req;
  try {
    req = factory.open(databaseName(userId), version);
  } catch {
    // Some browsers throw synchronously rather than firing onerror.
    return null;
  }

  return new Promise((resolve) => {
    req.onupgradeneeded = () => createSchema(req.result);
    req.onerror = () => resolve(null);
    // Another tab is holding the old version open. Waiting would hang the app.
    req.onblocked = () => resolve(null);
    req.onsuccess = () => {
      const db = req.result;
      db.onversionchange = () => db.close();
      resolve(db);
    };
  });
}

/**
 * Cache one task: its searchable record, its thread, and its attachment
 * metadata, in a single transaction so a search can never find a task whose
 * thread was not written.
 */
export async function putTask(db, task, terms = []) {
  if (!db || !task?.id || !task?.workspaceId) return false;

  return attempt(() => {
    const tx = db.transaction([STORE_TASKS, STORE_THREADS, STORE_ATTACHMENTS], 'readwrite');
    tx.objectStore(STORE_TASKS).put(toTaskRecord(task, terms));
    tx.objectStore(STORE_THREADS).put(toThreadRecord(task));
    const attachments = tx.objectStore(STORE_ATTACHMENTS);
    for (const record of toAttachmentRecords(task)) attachments.put(record);
    return transactionDone(tx);
  }, false);
}

/** The cached task and its thread, or null when either is missing. */
export async function getCachedTask(db, workspaceId, taskId) {
  if (!db) return null;

  return attempt(async () => {
    const tx = db.transaction([STORE_TASKS, STORE_THREADS], 'readonly');
    const [task, thread] = await Promise.all([
      requestResult(tx.objectStore(STORE_TASKS).get([workspaceId, taskId])),
      requestResult(tx.objectStore(STORE_THREADS).get([workspaceId, taskId])),
    ]);

    if (!task) return null;
    return { ...task, messages: thread?.messages ?? [], toolCalls: thread?.toolCalls ?? [] };
  }, null);
}

/** Every cached task in a workspace, newest first. */
export async function listCachedTasks(db, workspaceId, KeyRange = globalThis.IDBKeyRange) {
  if (!db) return [];

  return attempt(async () => {
    const tx = db.transaction(STORE_TASKS, 'readonly');
    const rows = await requestResult(
      tx.objectStore(STORE_TASKS).index('by_workspace').getAll(KeyRange.only(workspaceId))
    );
    return rows.sort((a, b) => String(b.updatedAt ?? '').localeCompare(String(a.updatedAt ?? '')));
  }, []);
}

/**
 * The primary keys of every task carrying a term in `range`.
 *
 * Keys rather than records, because a query with two words asks this twice and
 * only the intersection is worth reading. Fetching whole records for each word
 * and discarding most of them is the version of this that does not scale.
 */
export async function taskKeysForTerm(db, range) {
  if (!db) return [];

  return attempt(async () => {
    const tx = db.transaction(STORE_TASKS, 'readonly');
    return requestResult(tx.objectStore(STORE_TASKS).index('by_term').getAllKeys(range));
  }, []);
}

/** The task records for a set of primary keys, in the order given. */
export async function tasksByKeys(db, keys) {
  const list = Array.isArray(keys) ? keys : [];
  if (!db || list.length === 0) return [];

  return attempt(async () => {
    const tx = db.transaction(STORE_TASKS, 'readonly');
    const store = tx.objectStore(STORE_TASKS);
    const rows = await Promise.all(list.map((key) => requestResult(store.get(key))));
    return rows.filter(Boolean);
  }, []);
}

/**
 * Every cached task, across every workspace, newest first.
 *
 * One scan rather than a read per workspace, because the caller that needs this
 * is the global list on first paint — and at that moment it does not yet know
 * which workspaces exist. Asking the server for that list first would put a
 * round trip in front of the very thing the cache exists to avoid.
 */
export async function listAllCachedTasks(db) {
  if (!db) return [];

  return attempt(async () => {
    const tx = db.transaction(STORE_TASKS, 'readonly');
    const rows = await requestResult(tx.objectStore(STORE_TASKS).getAll());
    return rows.sort((a, b) => String(b.updatedAt ?? '').localeCompare(String(a.updatedAt ?? '')));
  }, []);
}

/**
 * The cached record for a task id, whichever workspace holds it.
 *
 * Needed because a deletion is announced with the task id alone — the event
 * envelope carries no workspace — while the store is keyed by both. Task ids
 * are monoflakes and globally unique, so at most one record can match, and a
 * scan is the honest way to find it: deletions are rare, and an index existing
 * only for them would be paid for on every write.
 */
export async function findCachedTaskById(db, taskId) {
  if (!db || !taskId) return null;
  const rows = await listAllCachedTasks(db);
  return rows.find((row) => String(row.id) === String(taskId)) ?? null;
}

/**
 * Forget specific tasks, wherever they live.
 *
 * Takes whole records rather than keys because the caller has just read them to
 * decide, and re-deriving the key from a record is one more place to get the
 * key shape wrong.
 *
 * @param {Array<{workspaceId: string, id: string}>} records
 * @param {typeof IDBKeyRange} [KeyRange]
 * @returns {Promise<number>} how many were removed
 */
export async function deleteTasks(db, records, KeyRange = globalThis.IDBKeyRange) {
  const list = Array.isArray(records) ? records : [];
  if (!db || list.length === 0) return 0;

  return attempt(async () => {
    const tx = db.transaction([STORE_TASKS, STORE_THREADS, STORE_ATTACHMENTS], 'readwrite');
    const tasks = tx.objectStore(STORE_TASKS);
    const threads = tx.objectStore(STORE_THREADS);
    const attachments = tx.objectStore(STORE_ATTACHMENTS);

    for (const record of list) {
      const key = [record.workspaceId, record.id];
      tasks.delete(key);
      threads.delete(key);
      // The attachment key carries the attachment id as a third part, so this
      // is a range over everything belonging to the task rather than one key.
      attachments.delete(
        KeyRange.bound([record.workspaceId, record.id], [record.workspaceId, record.id, []])
      );
    }

    await transactionDone(tx);
    return list.length;
  }, 0);
}

/**
 * Forget everything cached for one workspace, leaving the others untouched.
 *
 * This is what the settings screen's Clear button runs, and what opting out of
 * the cache runs so the setting never leaves orphaned data behind.
 */
export async function clearWorkspace(db, workspaceId, KeyRange = globalThis.IDBKeyRange) {
  if (!db) return false;

  return attempt(() => {
    const tx = db.transaction(
      [STORE_TASKS, STORE_THREADS, STORE_ATTACHMENTS, STORE_META],
      'readwrite'
    );
    const range = workspaceRange(workspaceId, KeyRange);
    tx.objectStore(STORE_TASKS).delete(range);
    tx.objectStore(STORE_THREADS).delete(range);
    tx.objectStore(STORE_ATTACHMENTS).delete(range);
    tx.objectStore(STORE_META).delete(KeyRange.bound(`${workspaceId}:`, `${workspaceId}:￿`));
    return transactionDone(tx);
  }, false);
}

/**
 * Delete the whole database.
 *
 * Signing out runs this. A browser that several people use must not leave one
 * person's task titles readable by the next, and there is no partial version of
 * that guarantee worth having.
 */
export async function destroyCache({ userId, factory = globalThis.indexedDB } = {}) {
  if (!factory || !userId) return false;

  return new Promise((resolve) => {
    let req;
    try {
      req = factory.deleteDatabase(databaseName(userId));
    } catch {
      return resolve(false);
    }
    req.onsuccess = () => resolve(true);
    req.onerror = () => resolve(false);
    req.onblocked = () => resolve(false);
  });
}
