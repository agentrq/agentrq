// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Splice `text` into `current` at the field's cursor, replacing any selected
 * text, with a space on either side only where the neighbouring text needs one.
 *
 * The field's selection is trusted only while its value is `current`: a field
 * that is missing, or showing something else, appends instead.
 *
 * @param {string} current
 * @param {string} text
 * @param {HTMLTextAreaElement|HTMLInputElement|null|undefined} el
 * @returns {{ value: string, caret: number }} the new value, and where the
 *   cursor belongs: just after the inserted text
 */
export function insertAtCursor(current, text, el) {
  const value = current || '';
  let start = value.length;
  let end = value.length;
  if (el && el.value === value && typeof el.selectionStart === 'number') {
    start = el.selectionStart;
    end = el.selectionEnd ?? start;
  }

  const before = value.slice(0, start);
  const after = value.slice(end);
  const lead = before && !/\s$/.test(before) ? ' ' : '';
  const trail = after && !/^\s/.test(after) ? ' ' : '';

  return {
    value: before + lead + text + trail + after,
    caret: before.length + lead.length + text.length,
  };
}
