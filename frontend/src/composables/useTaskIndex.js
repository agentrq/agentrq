// Copyright 2026 Contextual, Inc. https://agentrq.com

/**
 * Searching cached task text without a backend.
 *
 * IndexedDB has no full-text search, so the trick is to build one out of the
 * index type it does have. Every task is tokenised into an array of terms, and
 * `by_term` is declared `multiEntry`, which makes IndexedDB store one index
 * entry per element. "Find the tasks mentioning billing" then becomes a seek to
 * a key, not a scan of every record.
 *
 * That is the whole reason this holds up: the cost tracks the number of
 * *matching* terms rather than the number of tasks, so a workspace with ten
 * thousand tasks answers a keystroke as fast as one with ten.
 *
 * Everything here is a pure function over strings and arrays. The database
 * work lives in `useLocalCache`, and the two only meet in the caller.
 */

/**
 * The most terms kept for one task.
 *
 * A cap rather than a completeness guarantee: a task whose body is a pasted
 * stack trace would otherwise contribute thousands of index entries that nobody
 * will ever search for, and every write would pay for them. Two hundred terms
 * covers a title and several paragraphs of real prose.
 */
export const MAX_TERMS = 200;

/** Single characters are dropped: they match nearly everything and rank nothing. */
export const MIN_TERM_LENGTH = 2;

/**
 * Words too common to be worth an index entry.
 *
 * Deliberately short. A long stopword list starts eating words that carry
 * meaning in a task title — "all", "new" and "run" all mean something here —
 * so this covers only the highest-frequency function words, where the entry
 * would match most of the workspace and rank none of it.
 */
export const STOPWORDS = new Set([
  'a', 'an', 'and', 'are', 'as', 'at', 'be', 'but', 'by', 'for', 'from', 'has',
  'have', 'if', 'in', 'into', 'is', 'it', 'its', 'of', 'on', 'or', 'that',
  'the', 'their', 'then', 'there', 'these', 'they', 'this', 'to', 'was', 'were',
  'will', 'with',
]);

/**
 * Anything that is not a letter or a number separates two words.
 *
 * Unicode-aware on purpose: `\w` is ASCII, so it would cut "café" into "caf"
 * and throw away the rest, and lose a Turkish or Japanese title outright.
 */
const SEPARATOR = /[^\p{L}\p{N}]+/u;

/**
 * The terms to index for a piece of text.
 *
 * Order is the order of first appearance, which makes the cap predictable: a
 * long body is truncated from the end, so a task's title — written first —
 * always makes it into the index.
 *
 * @param {string} text
 * @returns {string[]} unique, lowercased, at most `MAX_TERMS`
 */
export function tokenize(text) {
  if (typeof text !== 'string' || text === '') return [];

  const seen = new Set();
  for (const raw of text.toLowerCase().split(SEPARATOR)) {
    if (raw.length < MIN_TERM_LENGTH || STOPWORDS.has(raw)) continue;
    seen.add(raw);
    if (seen.size === MAX_TERMS) break;
  }
  return [...seen];
}

/**
 * The terms for a task, title first.
 *
 * Title before body so that when the cap bites it is the body that loses.
 *
 * @param {{title?: string, body?: string}} task
 */
export function taskTerms(task) {
  return tokenize(`${task?.title ?? ''} ${task?.body ?? ''}`);
}

/**
 * The key ranges to seek for a query, one per token.
 *
 * Each range is a prefix bound, which is what lets "bill" find *billing*
 * without the index having to store every prefix of every word: `￿` is the
 * largest code unit, so a bound from the token to the token plus it covers
 * exactly the terms beginning with it.
 *
 * An empty result means the query constrains nothing — either it was blank, or
 * it was all stopwords, which cannot match because stopwords are never indexed.
 * The caller shows recent tasks rather than an empty panel.
 *
 * @param {string} query
 * @param {typeof IDBKeyRange} [KeyRange]
 */
export function termRanges(query, KeyRange = globalThis.IDBKeyRange) {
  return tokenize(query).map((token) => KeyRange.bound(token, `${token}￿`));
}

/**
 * A stable string for a task's primary key, so keys can go in a Set.
 *
 * The key is `[workspaceId, id]`, and two equal arrays are different objects —
 * comparing them by identity would make every intersection empty.
 */
export function keySignature(key) {
  return Array.isArray(key) ? key.join('\u0000') : String(key);
}

/**
 * The unique keys present in every list.
 *
 * AND semantics: typing two words means both, which is what makes a second word
 * narrow the results rather than widen them. Order follows the first list, so
 * whatever ordering the caller seeded it with survives.
 *
 * A single empty list means nothing matched everything, so the answer is empty.
 * No lists at all is a different question — nothing was asked — and the caller
 * handles that by not calling this.
 *
 * ## Why the lists arrive with repeats in them
 *
 * A term list is `getAllKeys()` over a *prefix* range of a `multiEntry` index,
 * and that index holds one record per term. So a task indexed under both "task"
 * and "tasks" has two records inside the range for "task", and its primary key
 * comes back twice — once per matching term, not once per task.
 *
 * Left alone that key is read twice, ranked twice, and the same task is drawn
 * twice in the finder. Singular and plural is enough to trigger it, so in a
 * product whose tasks talk about tasks, events and files it is the common case
 * rather than the odd one.
 *
 * Deduplicating here rather than at the seek: this is the one point every key
 * list passes through on its way to being read, so it is the only place that
 * fixes it once — and being pure, it is the only place that can be tested
 * without a database.
 *
 * @param {Array<Array<any>>} keyLists
 */
export function intersect(keyLists) {
  if (!Array.isArray(keyLists) || keyLists.length === 0) return [];

  const [first, ...rest] = keyLists;
  const others = rest.map((list) => new Set(list.map(keySignature)));
  const taken = new Set();

  return first.filter((key) => {
    const signature = keySignature(key);
    if (taken.has(signature)) return false;
    if (!others.every((set) => set.has(signature))) return false;

    taken.add(signature);
    return true;
  });
}

/**
 * Where a task matches, as a sort rank. Lower is better; -1 is no match.
 *
 * The order is the one the task finder already uses — an exact ID is what you
 * get when you paste one, an ID prefix next, then a title match — extended
 * with a body match, which only became reachable once the body was indexed.
 *
 * Body last because a word in a paragraph is weaker evidence than the same word
 * in the title, and the title is what the reader is scanning.
 */
export function matchRank(task, query) {
  const q = String(query ?? '').trim().toLowerCase();
  if (!q) return -1;

  const id = String(task?.id ?? '').toLowerCase();
  if (id === q) return 0;
  if (id.startsWith(q)) return 1;
  if (String(task?.title ?? '').toLowerCase().includes(q)) return 2;
  if (String(task?.body ?? '').toLowerCase().includes(q)) return 3;
  return -1;
}

/**
 * Order tasks for display, best match first.
 *
 * Tasks the index returned but whose text does not contain the query verbatim
 * still rank — behind everything that does. That is not a bug: the index
 * matches whole terms with a prefix, so searching "reconcile" legitimately
 * finds a task whose body says "reconciliation", and dropping it here would
 * throw away a correct hit.
 *
 * Ties keep their incoming order, which is the caller's — newest first.
 *
 * @param {Array<object>} tasks
 * @param {string} query
 * @param {number} [limit]
 */
export function rank(tasks, query, limit = 8) {
  const list = Array.isArray(tasks) ? tasks : [];

  return list
    .map((task, order) => ({ task, order, rank: matchRank(task, query) }))
    .sort((a, b) => {
      const ra = a.rank === -1 ? Number.MAX_SAFE_INTEGER : a.rank;
      const rb = b.rank === -1 ? Number.MAX_SAFE_INTEGER : b.rank;
      return ra - rb || a.order - b.order;
    })
    .slice(0, limit)
    .map((row) => row.task);
}
