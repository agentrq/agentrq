// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * How wide a workflow canvas box is, and where each column starts.
 *
 * Boxes used to be a fixed 200px. Once the kind badge, the START or GLOBAL
 * marker and the step buttons took their share, a name got about ten
 * characters, and on a phone there is no hover to show the tooltip with the
 * rest. So each box is sized to its own label and each column is as wide as
 * its widest box. Edges anchor to a box's right edge, so they need the same
 * width the template draws; that is why the width is estimated from the text
 * here rather than measured after render.
 *
 * Kept out of the view so it can be tested: the project has no component-test
 * harness.
 */

export const MIN_NODE_WIDTH = 200;
// Past this a box is truncated again. Its tooltip still has the full name.
export const MAX_NODE_WIDTH = 360;
export const COLUMN_GAP = 60;

// Widths in CSS px, deliberately rounded up: a box a few pixels too wide costs
// nothing, one a few pixels too narrow cuts the name again.
const LABEL_CHAR = 7; // text-[11px] font-mono
const EMIT_CHAR = 5.8; // text-[9px] font-mono
const PADDING = 24 + 2; // px-3 plus the border
const GAP = 8; // gap-2
const KIND_BADGE = 44; // "EVENT" / "AGENT"
const START_MARKER = 32;
const GLOBAL_LINK = 52;
const STEP_BUTTONS = 18 + GAP + 18; // edit and delete
const EMIT_PREFIX = 28; // "emits "
const EMIT_CLEAR = 6 + 50; // gap-1.5 plus the clear button

function clamp(width) {
  return Math.ceil(Math.min(MAX_NODE_WIDTH, Math.max(MIN_NODE_WIDTH, width)));
}

/**
 * The width a canvas box needs to show its label, and its emitted event if it
 * has one, without truncating.
 *
 * @param {object} node a graph node: {kind, label, isStart?}
 * @param {string} [emitLabel] the name of the event a step emits
 * @returns {number}
 */
export function nodeWidth(node, emitLabel) {
  const label = String(node?.label ?? '');
  let header = PADDING + KIND_BADGE + GAP + label.length * LABEL_CHAR;
  if (node?.isStart) header += GAP + START_MARKER;
  if (node?.kind === 'global') header += GAP + GLOBAL_LINK;
  if (node?.kind === 'step') header += GAP + STEP_BUTTONS;

  let emits = 0;
  if (node?.kind === 'step' && emitLabel) {
    emits = PADDING + EMIT_PREFIX + String(emitLabel).length * EMIT_CHAR + EMIT_CLEAR;
  }
  return clamp(Math.max(header, emits));
}

/**
 * The left edge of every column, each one as far right as the widest box in
 * the column before it needs.
 *
 * @param {Array<{column: number, width: number}>} nodes
 * @param {number} padding the canvas's left padding
 * @returns {number[]} indexed by column
 */
export function columnOffsets(nodes, padding) {
  const widest = [];
  for (const node of nodes) {
    widest[node.column] = Math.max(widest[node.column] ?? 0, node.width);
  }
  const offsets = [];
  let x = padding;
  for (let column = 0; column < widest.length; column++) {
    offsets[column] = x;
    // A column nothing landed in still keeps a slot, so the spacing reads the
    // same as it did with fixed-width columns.
    x += (widest[column] ?? MIN_NODE_WIDTH) + COLUMN_GAP;
  }
  return offsets;
}

/**
 * Pushes a node right until it clears the node its edge comes from by at least
 * COLUMN_GAP, when the two sit side by side. Positions saved while boxes were a
 * fixed 200px otherwise leave today's wider boxes nearly touching, with no room
 * for the arrow. A node dragged onto another row, or to the left of its source,
 * is left where it was put.
 *
 * @param {Array<{from: object, to: object}>} edges nodes carry x, y, width, height
 */
export function enforceEdgeGaps(edges) {
  // Every pass settles at least one more link of the longest chain.
  for (let pass = 0; pass <= edges.length; pass++) {
    let moved = false;
    for (const { from, to } of edges) {
      const sideBySide = to.y < from.y + from.height && from.y < to.y + to.height;
      const minX = from.x + from.width + COLUMN_GAP;
      if (sideBySide && to.x >= from.x && to.x < minX) {
        to.x = minX;
        moved = true;
      }
    }
    if (!moved) return;
  }
}
