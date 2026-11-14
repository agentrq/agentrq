// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The list logic behind ItemPicker, the app's own dropdown: what the search
 * keeps, where the arrow keys go, and which item typing a letter lands on.
 * Pure, so the component keeps only its markup and open state.
 */

/** Past this many items the menu offers a search box. */
export const SEARCH_THRESHOLD = 8;

/** The items whose label holds the query, ignoring case; all for none. */
export function filterPickerItems(items = [], query = '') {
  const q = query.trim().toLowerCase();
  if (!q) return items;
  return items.filter((it) => (it.label || '').toLowerCase().includes(q));
}

/** One step from `i` over `n` items, wrapping at either end; -1 for none. */
export function stepIndex(i, delta, n) {
  if (n <= 0) return -1;
  if (i < 0) return delta > 0 ? 0 : n - 1;
  return (((i + delta) % n) + n) % n;
}

/**
 * The next item after `from` whose label starts with what was typed, going
 * round; `from` itself last, so typing the same letter again moves on. -1
 * when none does.
 */
export function typeaheadIndex(items = [], typed = '', from = -1) {
  const t = typed.toLowerCase();
  if (!t) return -1;
  const n = items.length;
  for (let k = 1; k <= n; k += 1) {
    const i = (((from + k) % n) + n) % n;
    if ((items[i].label || '').toLowerCase().startsWith(t)) return i;
  }
  return -1;
}

/** Whether a key is one typed character, which typeahead reads. */
export const isTypeaheadKey = (e) => e.key.length === 1 && e.key !== ' ' && !e.ctrlKey && !e.metaKey && !e.altKey;
