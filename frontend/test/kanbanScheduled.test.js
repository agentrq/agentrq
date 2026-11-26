// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/** The board's Scheduled column setting, where this browser keeps it. */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'

const load = async () => (await import('../src/composables/useKanbanScheduled')).useKanbanScheduled

describe('useKanbanScheduled', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
  })
  afterEach(() => {
    vi.unstubAllGlobals()
  })

  it('is off unless this browser kept it on', async () => {
    expect((await load())().showScheduled.value).toBe(false)
    vi.resetModules()
    localStorage.setItem('agentrq:kanban:scheduled', '1')
    expect((await load())().showScheduled.value).toBe(true)
  })

  it('keeps each change, and forgets it when turned off', async () => {
    const { showScheduled, toggleScheduled } = (await load())()
    toggleScheduled()
    expect(showScheduled.value).toBe(true)
    expect(localStorage.getItem('agentrq:kanban:scheduled')).toBe('1')
    toggleScheduled()
    expect(showScheduled.value).toBe(false)
    expect(localStorage.getItem('agentrq:kanban:scheduled')).toBeNull()
  })

  it('still follows the toggle where storage is refused', async () => {
    const refused = { getItem() { throw new Error('denied') }, setItem() { throw new Error('denied') }, removeItem() {} }
    vi.stubGlobal('localStorage', refused)
    const useKanbanScheduled = await load()
    const { showScheduled, setShowScheduled } = useKanbanScheduled(refused)
    expect(showScheduled.value).toBe(false)
    setShowScheduled(true)
    expect(showScheduled.value).toBe(true)
  })

  it('works with no storage at all', async () => {
    vi.stubGlobal('localStorage', undefined)
    const { showScheduled, setShowScheduled } = (await load())(null)
    expect(showScheduled.value).toBe(false)
    setShowScheduled(true)
    setShowScheduled(false)
    expect(showScheduled.value).toBe(false)
  })
})
