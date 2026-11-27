// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { onMounted, onUnmounted, ref } from 'vue';

/** Marks the elements whose selected text the toolbar offers to quote or copy. */
export const QUOTE_SOURCE_ATTR = 'data-quote-source';

/** The toolbar's width, about: its right edge is never further left than this. */
const MIN_RIGHT = 64;

/**
 * The line ending of the OS the person is on: CRLF on Windows, LF elsewhere.
 *
 * @param {{ os?: string }} [platform] the platform store's state; `os` is set
 *   only in the desktop app
 * @param {Navigator} [nav]
 * @returns {'\r\n'|'\n'}
 */
export function lineEnding(platform = {}, nav = globalThis.navigator) {
  const reported = platform.os || nav?.userAgentData?.platform || nav?.platform || '';
  // `^win`: "darwin" also contains "win".
  return /^win/i.test(reported) ? '\r\n' : '\n';
}

/**
 * `text` as a markdown quote: every line behind `> `, then a blank line, so
 * the reply starts under it.
 *
 * @param {string} text
 * @param {string} eol
 * @returns {string}
 */
export function quoteText(text, eol) {
  const lines = text.replace(/\r\n?/g, '\n').replace(/^\n+|\n+$/g, '').split('\n');
  return lines.map((line) => (line ? `> ${line}` : '>')).join(eol) + eol + eol;
}

/**
 * Appends `quote` to the draft, on a line of its own.
 *
 * @param {string} draft
 * @param {string} quote
 * @param {string} eol
 * @returns {string}
 */
export function appendQuote(draft, quote, eol) {
  if (!draft) return quote;
  return draft + (/\n$/.test(draft) ? '' : eol) + quote;
}

/** The marked element a DOM node sits in, inside `root`, or null. */
function sourceOf(node, root) {
  const el = node?.nodeType === 1 ? node : node?.parentElement;
  const source = el?.closest?.(`[${QUOTE_SOURCE_ATTR}]`);
  return source && root.contains(source) ? source : null;
}

/**
 * A toolbar under text selected inside one marked element of `container`.
 *
 * `toolbar` is null while there is nothing to offer, or `{ text, top, right }`:
 * the selected text and where its toolbar goes, in the container's own
 * coordinates (it scrolls with the text): under the selection's bottom right,
 * `right` being the x its right edge lines up with.
 *
 * @param {import('vue').Ref<HTMLElement|null>} container the positioned,
 *   scrolling element holding the messages
 * @param {{ doc?: Document, onMounted?: Function, onUnmounted?: Function }} [options]
 */
export function useSelectionQuote(container, {
  doc = globalThis.document,
  onMounted: mounted = onMounted,
  onUnmounted: unmounted = onUnmounted,
} = {}) {
  const toolbar = ref(null);

  function update() {
    const root = container.value;
    const sel = doc.getSelection?.();
    if (!root || !sel || sel.isCollapsed || sel.rangeCount === 0) {
      toolbar.value = null;
      return;
    }
    const text = sel.toString();
    const source = sourceOf(sel.anchorNode, root);
    if (!text.trim() || !source || source !== sourceOf(sel.focusNode, root)) {
      toolbar.value = null;
      return;
    }
    // The selection's last line: a selection over several lines ends there.
    const range = sel.getRangeAt(0);
    const rect = [...(range.getClientRects?.() ?? [])].at(-1) ?? range.getBoundingClientRect();
    const box = root.getBoundingClientRect();
    toolbar.value = {
      text,
      top: rect.bottom - box.top + root.scrollTop,
      // Never so far left that the toolbar, drawn leftwards from here, leaves the pane.
      right: Math.max(rect.right - box.left + root.scrollLeft, MIN_RIGHT),
    };
  }

  /** Drops the selection, and with it the toolbar. */
  function clear() {
    doc.getSelection?.()?.removeAllRanges();
    toolbar.value = null;
  }

  mounted(() => doc.addEventListener('selectionchange', update));
  unmounted(() => doc.removeEventListener('selectionchange', update));

  return { toolbar, update, clear };
}
