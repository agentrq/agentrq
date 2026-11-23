// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect } from 'vitest'

import {
  COLUMN_GAP,
  MAX_NODE_WIDTH,
  MIN_NODE_WIDTH,
  columnOffsets,
  enforceEdgeGaps,
  nodeWidth,
} from '../src/composables/useWorkflowLayout'

describe('nodeWidth', () => {
  it('keeps the old width for a short name', () => {
    expect(nodeWidth({ kind: 'event', label: 'go' })).toBe(MIN_NODE_WIDTH)
  })

  // The reported bug: at 200px these read "feature_rel…" and "agentrq…".
  it('grows to fit a name that the fixed width cut off', () => {
    const event = nodeWidth({ kind: 'event', label: 'feature_released', isStart: true })
    expect(event).toBeGreaterThan(MIN_NODE_WIDTH)
    expect(event).toBeLessThan(MAX_NODE_WIDTH)
    const global = nodeWidth({ kind: 'global', label: 'agentrq-extensions' })
    expect(global).toBeGreaterThan(MIN_NODE_WIDTH)
  })

  it('makes room for what else shares the header', () => {
    const label = 'agentrq-adhoc-tools'
    const plain = nodeWidth({ kind: 'global-event', label })
    expect(nodeWidth({ kind: 'event', label, isStart: true })).toBeGreaterThan(plain)
    expect(nodeWidth({ kind: 'global', label })).toBeGreaterThan(plain)
    expect(nodeWidth({ kind: 'step', label })).toBeGreaterThan(plain)
  })

  it('fits the emitted event on a step', () => {
    const short = nodeWidth({ kind: 'step', label: 'x' }, 'e')
    const long = nodeWidth({ kind: 'step', label: 'x' }, 'a_very_long_event_name_here')
    expect(short).toBe(MIN_NODE_WIDTH)
    expect(long).toBeGreaterThan(MIN_NODE_WIDTH)
    // Only a step draws an emits line.
    expect(nodeWidth({ kind: 'event', label: 'x' }, 'a_very_long_event_name_here')).toBe(MIN_NODE_WIDTH)
  })

  it('stops growing, leaving the rest to the tooltip', () => {
    expect(nodeWidth({ kind: 'step', label: 'n'.repeat(200) })).toBe(MAX_NODE_WIDTH)
  })

  it('tolerates a node with no label', () => {
    expect(nodeWidth({ kind: 'event' })).toBe(MIN_NODE_WIDTH)
    expect(nodeWidth(undefined)).toBe(MIN_NODE_WIDTH)
  })

  it('returns whole pixels', () => {
    expect(Number.isInteger(nodeWidth({ kind: 'step', label: 'abc' }, 'emitted_event_x'))).toBe(true)
  })
})

describe('columnOffsets', () => {
  it('places each column after the widest box before it', () => {
    const offsets = columnOffsets([
      { column: 0, width: 200 },
      { column: 1, width: 300 },
      { column: 1, width: 220 },
      { column: 2, width: 200 },
    ], 32)
    expect(offsets).toEqual([32, 32 + 200 + COLUMN_GAP, 32 + 200 + 300 + 2 * COLUMN_GAP])
  })

  it('keeps a slot for a column nothing landed in', () => {
    const offsets = columnOffsets([{ column: 0, width: 250 }, { column: 2, width: 200 }], 0)
    expect(offsets[2]).toBe(250 + MIN_NODE_WIDTH + 2 * COLUMN_GAP)
  })

  it('has nothing to place for an empty graph', () => {
    expect(columnOffsets([], 32)).toEqual([])
  })
})

describe('enforceEdgeGaps', () => {
  const box = (x, y = 0) => ({ x, y, width: 230, height: 40 })

  // The reported bug: positions saved at 200px left today's boxes ~27px apart.
  it('pushes a crowded neighbour out to the standard gap, down the chain', () => {
    const a = box(0)
    const b = box(257)
    const c = box(300)
    enforceEdgeGaps([{ from: b, to: c }, { from: a, to: b }])
    expect(b.x).toBe(230 + COLUMN_GAP)
    expect(c.x).toBe(2 * (230 + COLUMN_GAP))
  })

  it('leaves a node that already has room', () => {
    const b = box(500)
    enforceEdgeGaps([{ from: box(0), to: b }])
    expect(b.x).toBe(500)
  })

  it('leaves a node placed on another row or to the left', () => {
    const below = box(10, 100)
    const left = box(-300)
    enforceEdgeGaps([{ from: box(0), to: below }, { from: box(0), to: left }])
    expect(below.x).toBe(10)
    expect(left.x).toBe(-300)
  })

  it('stops on a cycle', () => {
    const a = box(0)
    const b = box(10)
    enforceEdgeGaps([{ from: a, to: b }, { from: b, to: a }])
    expect(b.x).toBe(230 + COLUMN_GAP)
    expect(a.x).toBe(0)
  })
})
