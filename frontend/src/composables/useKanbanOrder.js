// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * How the board orders the cards in a column, and where a dropped card goes.
 *
 * A column is ordered one of two ways. A *manual* column follows each task's
 * sort order, which dragging writes. A *recent* column (Done) shows the most
 * recently finished task first, the same order the server pages it in. It
 * cannot be dragged into shape: a drag there would write a sort order that
 * nothing reads.
 */

/** A task's position in a manual column. Before a first drag it is its creation time. */
export function getOrder(t) {
  if (t.sortOrder) return t.sortOrder;
  if (!t.createdAt) return Date.now() / 1000.0;
  return new Date(t.createdAt).getTime() / 1000.0;
}

function updatedAtMs(t) {
  const ms = new Date(t.updatedAt).getTime();
  return isNaN(ms) ? 0 : ms;
}

/** The tasks that belong in `col`, in the order the column shows them. */
export function sortColumn(col, tasks) {
  const cards = tasks.filter(t => col.statuses.includes(t.status));
  if (col.sortBy === 'recent') return cards.sort((a, b) => updatedAtMs(b) - updatedAtMs(a));
  return cards.sort((a, b) => getOrder(a) - getOrder(b));
}

/** The sort order that puts a card between `prev` and `next`, either of which may be missing. */
export function orderBetween(prev, next) {
  if (!prev && !next) return Date.now() / 1000.0;
  if (!prev) return getOrder(next) - 1;
  if (!next) return getOrder(prev) + 1;
  return (getOrder(prev) + getOrder(next)) / 2;
}

/**
 * The sort order that moves `task` one place up (-1) or down (1) in `tasks`, a
 * manual column in the order shown, or null when it is already at that end.
 * Passing a neighbour means landing between it and the card beyond it: a step
 * relative to the neighbour alone never gets past it.
 */
export function orderForStep(tasks, task, direction) {
  const idx = tasks.findIndex(t => String(t.id) === String(task.id));
  const target = idx + direction;
  if (idx === -1 || target < 0 || target >= tasks.length) return null;
  return direction < 0
    ? orderBetween(tasks[target - 1], tasks[target])
    : orderBetween(tasks[target], tasks[target + 1]);
}
