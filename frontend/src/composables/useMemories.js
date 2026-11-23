// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A workspace's memories, as the settings screen shows them.
 *
 * Agents write these through the `saveMemory` tool; this side only reads. The
 * screen shows the index and follows its `memory://` links, the way an agent
 * reads them. The decisions worth keeping out of the component and under test
 * are how that reading moves, what the index leaves out, and whether an empty
 * list means "nothing yet" or "something went wrong" — those look identical on
 * screen and must not.
 */

import { computed, reactive, ref, toValue } from 'vue';
import { marked } from 'marked';

/** The memory agents read first, and where the index to the others belongs. */
export const INDEX_MEMORY = 'memory.md';

/**
 * How a memory's name reads on screen. The index is stored lowercase, like
 * every name, but people know it as MEMORY.md, and that is how it is shown.
 *
 * @param {string} name
 */
export function memoryTitle(name) {
  return name === INDEX_MEMORY ? 'MEMORY.md' : name;
}

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
  /** The workspace has an index to show. */
  Ready: 'ready',
  /** Memories, but no index yet: an agent saved one without writing memory.md. */
  NoIndex: 'noindex',
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
  if (!memories?.length) return MemoriesState.Empty;
  return memories.some((m) => m.name === INDEX_MEMORY) ? MemoriesState.Ready : MemoriesState.NoIndex;
}

/**
 * The memories a piece of markdown links to, in the order it names them.
 *
 * Read from the parsed links rather than by searching the text, so a name in
 * inline code or prose counts only when it is a link somebody can follow.
 *
 * @param {string} content
 * @returns {string[]}
 */
export function memoryLinksIn(content) {
  const names = new Set();
  marked.walkTokens(marked.lexer(String(content ?? '')), (token) => {
    const name = token.type === 'link' && memoryLinkTarget(token.href);
    if (name) names.add(name);
  });
  return [...names];
}

/**
 * The memories the index does not link to, alphabetically. Without this list
 * they could only be reached by an agent.
 *
 * @param {Array<{name: string}>} memories
 * @param {string} indexContent
 */
export function unlinkedMemories(memories = [], indexContent = '') {
  const linked = new Set(memoryLinksIn(indexContent));
  return orderMemories(memories)
    .map((m) => m.name)
    .filter((name) => name !== INDEX_MEMORY && !linked.has(name));
}

/**
 * The reader: memory.md first, and every `memory://` link followed in place,
 * with Back retracing the way the reader came. `current.name` is '' while no
 * memory is open, which is where a workspace without an index starts.
 *
 * @param {import('vue').MaybeRefOrGetter<string>} workspaceId
 * @param {{ fetchList: (id: string) => Promise<{memories?: Array<{name: string}>}>,
 *           fetchOne: (id: string, name: string) => Promise<{memory?: {content?: string}}> }} api
 */
export function useMemoryReader(workspaceId, { fetchList, fetchOne }) {
  const memories = ref([]);
  const loading = ref(false);
  const error = ref(null);
  const history = ref([]);
  const current = reactive({ name: '', content: '', loading: false, error: '', missing: '' });

  const state = computed(() => memoriesState({ loading: loading.value, error: error.value, memories: memories.value }));

  // What to list under the page: on the index, what it leaves out; with no
  // memory open, all of them. A memory the index links to is read from there.
  const listed = computed(() => {
    if (current.name === '') return unlinkedMemories(memories.value);
    if (current.name !== INDEX_MEMORY || current.loading || current.error) return [];
    return unlinkedMemories(memories.value, current.content);
  });

  // Loads overlap when a reader clicks faster than the server answers, or the
  // workspace changes under a request; only the latest may write.
  let listSeq = 0;
  let readSeq = 0;

  function clear() {
    readSeq++;
    Object.assign(current, { name: '', content: '', loading: false, error: '', missing: '' });
  }

  async function show(name) {
    const seq = ++readSeq;
    Object.assign(current, { name, content: '', error: '', missing: '', loading: true });
    // An index written ahead of its entries is normal, so a link to a memory
    // nobody has saved says so in place rather than failing.
    if (!memories.value.some((m) => m.name === name)) {
      Object.assign(current, { missing: `Nothing saved under ${name} yet.`, loading: false });
      return;
    }
    try {
      // The list is fetched without content, so reading one is a second request.
      const res = await fetchOne(toValue(workspaceId), name);
      if (seq === readSeq) current.content = res.memory?.content || '';
    } catch {
      if (seq === readSeq) current.error = 'Could not load this memory.';
    } finally {
      if (seq === readSeq) current.loading = false;
    }
  }

  async function load() {
    const seq = ++listSeq;
    loading.value = true;
    error.value = null;
    history.value = [];
    clear();
    try {
      const res = await fetchList(toValue(workspaceId));
      if (seq !== listSeq) return;
      memories.value = res.memories || [];
    } catch (err) {
      if (seq !== listSeq) return;
      // Kept apart from an empty list: "your agents have remembered nothing"
      // and "we could not ask" must not read the same.
      memories.value = [];
      error.value = err;
      return;
    } finally {
      if (seq === listSeq) loading.value = false;
    }
    if (state.value === MemoriesState.Ready) show(INDEX_MEMORY);
  }

  function open(name) {
    if (!name || name === current.name) return;
    history.value.push(current.name);
    show(name);
  }

  /** Follow a `memory://` link clicked inside the memory being read. */
  function follow(event) {
    const name = memoryLinkFromEvent(event);
    if (!name) return;
    event.preventDefault();
    open(name);
  }

  function back() {
    if (!history.value.length) return;
    const previous = history.value.pop();
    if (previous) show(previous);
    else clear();
  }

  return { memories, state, current, history, listed, load, open, follow, back };
}
