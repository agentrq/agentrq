// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest'

import { getOrder, sortColumn, orderBetween, orderForStep } from '../src/composables/useKanbanOrder'

const NOT_STARTED = { id: 'notstarted', statuses: ['notstarted'] }
const DONE = { id: 'done', statuses: ['completed', 'rejected'], sortBy: 'recent' }
const ids = (tasks) => tasks.map((t) => t.id)

afterEach(() => vi.useRealTimers())

describe('getOrder', () => {
  it('is the sort order once one has been written', () => {
    expect(getOrder({ sortOrder: 1790000015.3115, createdAt: '2020-01-01T00:00:00Z' })).toBe(1790000015.3115)
  })

  it('falls back to the creation time, in seconds', () => {
    expect(getOrder({ sortOrder: 0, createdAt: '2026-09-27T15:00:00.500Z' })).toBe(
      Date.parse('2026-09-27T15:00:00.500Z') / 1000,
    )
  })

  it('falls back to now with neither', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-27T15:00:00Z'))
    expect(getOrder({})).toBe(Date.parse('2026-09-27T15:00:00Z') / 1000)
  })
})

describe('sortColumn', () => {
  it('orders a manual column by position, earliest first, and keeps only its statuses', () => {
    const tasks = [
      { id: 'b', status: 'notstarted', sortOrder: 20 },
      { id: 'done', status: 'completed', sortOrder: 1 },
      { id: 'a', status: 'notstarted', sortOrder: 10 },
    ]
    expect(ids(sortColumn(NOT_STARTED, tasks))).toEqual(['a', 'b'])
  })

  it('shows the most recently finished task first in Done, whatever its position', () => {
    // The bug: Done followed the drag position, which is the creation time for
    // a task never dragged, so it read oldest-created first.
    const tasks = [
      { id: 'old-finished', status: 'completed', sortOrder: 30, updatedAt: '2026-09-25T10:00:00Z' },
      { id: 'just-finished', status: 'completed', sortOrder: 10, updatedAt: '2026-09-27T10:00:00Z' },
      { id: 'rejected', status: 'rejected', sortOrder: 20, updatedAt: '2026-09-26T10:00:00Z' },
      { id: 'no-date', status: 'completed', sortOrder: 5 },
    ]
    expect(ids(sortColumn(DONE, tasks))).toEqual(['just-finished', 'rejected', 'old-finished', 'no-date'])
  })
})

describe('orderBetween', () => {
  it('is the midpoint between two neighbours', () => {
    expect(orderBetween({ sortOrder: 10 }, { sortOrder: 11 })).toBe(10.5)
  })

  it('is one before the first card, and one after the last', () => {
    expect(orderBetween(undefined, { sortOrder: 10 })).toBe(9)
    expect(orderBetween({ sortOrder: 10 }, undefined)).toBe(11)
  })

  it('is now in an empty column', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-27T15:00:00Z'))
    expect(orderBetween(undefined, undefined)).toBe(Date.parse('2026-09-27T15:00:00Z') / 1000)
  })
})

describe('orderForStep', () => {
  const column = [{ id: 'a', sortOrder: 10 }, { id: 'b', sortOrder: 20 }, { id: 'c', sortOrder: 30 }]

  it('lands a step up between the two cards above', () => {
    expect(orderForStep(column, column[2], -1)).toBe(15)
    expect(orderForStep(column, column[1], -1)).toBe(9)
  })

  it('lands a step down between the two cards below', () => {
    expect(orderForStep(column, column[0], 1)).toBe(25)
    expect(orderForStep(column, column[1], 1)).toBe(31)
  })

  it('is null past either end, or for a card not in the column', () => {
    expect(orderForStep(column, column[0], -1)).toBeNull()
    expect(orderForStep(column, column[2], 1)).toBeNull()
    expect(orderForStep(column, { id: 'x' }, -1)).toBeNull()
  })
})
