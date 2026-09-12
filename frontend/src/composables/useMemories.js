// Copyright 2026 Contextual, Inc. https://agentrq.com

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
export const INDEX_MEMORY = 'memory.md';

/**
 * Where `renderMarkdown` parks a link to another memory, and what the click
 * handler looks for.
 *
 * Agents write `[how we ship](memory://deploys.md)` in the index. The scheme is
 * what makes that unambiguous — a bare `deploys.md` could equally be a repo
 * path or a typo — and the sanitizer strips the href for us, so the link is
 * never navigable and the name has to travel in an attribute instead.
 */
export const MEMORY_LINK_ATTR = 'data-memory-link';
export const MEMORY_LINK_SELECTOR = `[${MEMORY_LINK_ATTR}]`;

// Two slashes is the spelling agents are taught, but one or none cost nothing
// to accept and are the obvious things to mistype. Same leniency as the names
// themselves: strict about what is stored, forgiving about what arrives.
const MEMORY_SCHEME = /^memory:\/{0,2}/i;

/**
 * The memory a `memory://` link names, canonicalised the way the tools store
 * it, or '' when the link is not one.
 *
 * Parsed by stripping the prefix rather than with `new URL`. The two agree
 * today only because names are slugs — `new URL('memory://release notes.md')`
 * throws, and a name is not required by anything here to stay space-free
 * forever. Whoever relaxes that rule should not discover this by watching links
 * break.
 *
 * @param {string} raw
 * @returns {string}
 */
export function memoryLinkTarget(raw) {
  const text = String(raw ?? '');
  if (!MEMORY_SCHEME.test(text)) return '';
  return text.replace(MEMORY_SCHEME, '').trim().toLowerCase();
}

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

/**
 * The memory a click asked for, or '' when the click was not on one.
 *
 * @param {Event} event
 */
export function memoryLinkFromEvent(event) {
  const anchor = event?.target?.closest?.(MEMORY_LINK_SELECTOR);
  return anchor?.getAttribute(MEMORY_LINK_ATTR) || '';
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
