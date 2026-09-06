/**
 * A workspace's memories, as the settings screen shows them.
 *
 * Agents write these through the `saveMemory` tool; this side only reads. The
 * decisions worth keeping out of the component and under test are all about how
 * the list reads: which memory comes first, how big each one is in words a
 * person uses, and whether an empty list means "nothing yet" or "something went
 * wrong" — those look identical on screen and must not.
 */

/** The memory agents read first, and where the index to the others belongs. */
export const INDEX_MEMORY = 'MEMORY.md';

/**
 * The memories in the order they should be read.
 *
 * The index first, because it is the one that explains the rest; everything
 * else alphabetically, which is the order the API already returns and the only
 * one that stays stable as memories are rewritten.
 *
 * @param {Array<{name: string}>} memories
 */
export function orderMemories(memories = []) {
  return [...memories].sort((a, b) => {
    if (a.name === INDEX_MEMORY) return -1;
    if (b.name === INDEX_MEMORY) return 1;
    return a.name.localeCompare(b.name);
  });
}

/**
 * A size in the units a person reads.
 *
 * Bytes below a kilobyte, because at this scale "0.1 KB" is less informative
 * than "94 bytes"; one decimal place above it, since the cap is 16 KB and whole
 * kilobytes would round most memories to the same number.
 *
 * @param {number} bytes
 */
export function formatMemorySize(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n < 0) return '';
  if (n < 1024) return `${n} ${n === 1 ? 'byte' : 'bytes'}`;
  return `${(n / 1024).toFixed(1)} KB`;
}

/** How full a memory is against the 16 KB cap, as a percentage 0–100. */
export const MEMORY_LIMIT_BYTES = 16 * 1024;

export function memoryFullness(bytes) {
  const n = Number(bytes);
  if (!Number.isFinite(n) || n <= 0) return 0;
  return Math.min(100, Math.round((n / MEMORY_LIMIT_BYTES) * 100));
}

/**
 * When a memory was last written, in the terms that matter here.
 *
 * A memory is rewritten whenever an agent learns something, so "how long ago"
 * is the useful reading — a timestamp would make the reader do the subtraction
 * to answer the only question they have: is this still current?
 *
 * @param {string|number|Date} updatedAt
 * @param {Date} [now] injectable, so the test does not depend on the clock
 */
export function memoryUpdatedAgo(updatedAt, now = new Date()) {
  const then = new Date(updatedAt);
  if (Number.isNaN(then.getTime())) return '';

  const seconds = Math.floor((now.getTime() - then.getTime()) / 1000);
  // A clock skew between server and browser can put a write slightly in the
  // future; "in 3 seconds" would read as a bug rather than as rounding.
  if (seconds < 60) return 'just now';
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  if (days < 30) return `${days}d ago`;
  return then.toLocaleDateString();
}

/** What the panel should be showing. */
export const MemoriesState = {
  Loading: 'loading',
  /** The workspace has memories to show. */
  Ready: 'ready',
  /** No memories yet — the ordinary state of a workspace no agent has written in. */
  Empty: 'empty',
  /** The list could not be fetched, which is not the same as there being none. */
  Failed: 'failed',
};

/**
 * @param {{ loading: boolean, error: unknown, memories: Array<unknown> }} state
 */
export function memoriesState({ loading, error, memories }) {
  if (loading) return MemoriesState.Loading;
  // Checked before emptiness: a failed fetch also leaves the list empty, and
  // telling someone their agents have remembered nothing when the request
  // simply failed is the one wrong answer here.
  if (error) return MemoriesState.Failed;
  return memories?.length ? MemoriesState.Ready : MemoriesState.Empty;
}
