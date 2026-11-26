// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, beforeEach } from 'vitest'
import { nextTick, ref } from 'vue'
import { isProgressCard, useCardPins } from '../src/composables/useCardPins'

const card = (id) => ({ id, metadata: { type: 'a2ui' } })

beforeEach(() => localStorage.clear())

describe('isProgressCard', () => {
  it('is a reply card, never a question card or plain text', () => {
    expect(isProgressCard(card('a'))).toBe(true)
    expect(isProgressCard({ id: 'b', metadata: { type: 'elicitation_request', mode: 'a2ui' } })).toBe(false)
    expect(isProgressCard({ id: 'c' })).toBe(false)
    expect(isProgressCard(null)).toBe(false)
  })
})

describe('useCardPins', () => {
  it('pins one card and folds cards per task, kept in storage', async () => {
    const taskId = ref('t1')
    const pins = useCardPins(taskId)
    pins.togglePin('a')
    pins.toggleCollapsed('a')
    expect(pins.isPinned('a')).toBe(true)
    expect(pins.isCollapsed('a')).toBe(true)
    expect(JSON.parse(localStorage.getItem('agentrq.cardPins.t1'))).toEqual({ pinned: 'a', collapsed: ['a'] })

    // Pinning another replaces it.
    pins.togglePin('b')
    expect(pins.isPinned('a')).toBe(false)
    expect(pins.isPinned('b')).toBe(true)

    taskId.value = 't2'
    await nextTick()
    expect(pins.isPinned('b')).toBe(false)

    taskId.value = 't1'
    await nextTick()
    expect(pins.isPinned('b')).toBe(true)
    pins.togglePin('b')
    expect(pins.isPinned('b')).toBe(false)
    pins.toggleCollapsed('a')
    expect(pins.isCollapsed('a')).toBe(false)
  })

  it('finds the pinned card among the messages, only a progress card', () => {
    const pins = useCardPins(ref('t1'))
    expect(pins.pinnedCard([card('a')])).toBeNull()
    pins.togglePin('b')
    expect(pins.pinnedCard([card('a'), card('b')]).id).toBe('b')
    expect(pins.pinnedCard(undefined)).toBeNull()
    pins.togglePin('q')
    expect(pins.pinnedCard([{ id: 'q', metadata: { type: 'elicitation_request', mode: 'a2ui' } }])).toBeNull()
  })

  it('ignores a broken or odd stored value', () => {
    localStorage.setItem('agentrq.cardPins.t1', '{nope')
    expect(useCardPins(ref('t1')).isPinned('a')).toBe(false)
    localStorage.setItem('agentrq.cardPins.t1', '{"pinned":["a"],"collapsed":1}')
    const odd = useCardPins(ref('t1'))
    expect(odd.isPinned('a')).toBe(false)
    expect(odd.isCollapsed('a')).toBe(false)
  })

  it('still works when storage refuses', () => {
    const storage = { getItem: () => { throw new Error('denied') }, setItem: () => { throw new Error('full') } }
    const pins = useCardPins(ref('t1'), storage)
    pins.togglePin('a')
    expect(pins.isPinned('a')).toBe(true)
  })
})
