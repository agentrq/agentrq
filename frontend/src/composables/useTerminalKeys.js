// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The keys a phone keyboard has no way to type, as buttons under the terminal.
 *
 * Arrows carry only their final letter: the prefix depends on whether the
 * program asked for application cursor mode, and a fixed `ESC [` is ignored by
 * programs that did.
 */
export const TERMINAL_KEYS = [
  { id: 'esc', label: 'Esc', title: 'Escape', bytes: '\x1b' },
  { id: 'tab', label: 'Tab', title: 'Tab', bytes: '\t' },
  { id: 'shift-tab', label: '⇧Tab', title: 'Shift+Tab', bytes: '\x1b[Z' },
  { id: 'up', label: '↑', title: 'Up', cursor: 'A' },
  { id: 'down', label: '↓', title: 'Down', cursor: 'B' },
  { id: 'left', label: '←', title: 'Left', cursor: 'D' },
  { id: 'right', label: '→', title: 'Right', cursor: 'C' },
  { id: 'ctrl-c', label: '^C', title: 'Ctrl+C', bytes: '\x03' },
]

/** The bytes a key sends, given the terminal's cursor mode. */
export function keyBytes(key, { applicationCursor = false } = {}) {
  if (key.cursor) return (applicationCursor ? '\x1bO' : '\x1b[') + key.cursor
  return key.bytes
}

/**
 * Type a key into the terminal.
 *
 * Through `term.input`, so it reaches the session by the same `onData` path as
 * a typed key — including the check that an ended session takes no input.
 */
export function pressKey(term, key) {
  if (!term) return
  term.input(keyBytes(key, { applicationCursor: !!term.modes?.applicationCursorKeysMode }), true)
}
