// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Follow keeps the trajectory's selection on the stream, a row at a time and
 * slowly enough to read; the arrow keys move it by hand.
 */

import { describe, it, expect, afterEach, beforeEach, vi } from 'vitest'
import { createApp, effectScope, h, nextTick, ref } from 'vue'

import TrajectoryPanel from '../src/components/TrajectoryPanel.vue'
import { FOLLOW_STEP_MS, stepTrajectory, useTrajectoryFollow } from '../src/composables/useTrajectory'

const rows = (...ids) => ids.map((id) => ({ id }))

describe('stepTrajectory', () => {
  it('moves by delta and stops at either end', () => {
    const list = rows('a', 'b', 'c')
    expect(stepTrajectory(list, 'b', 1)).toBe('c')
    expect(stepTrajectory(list, 'b', -1)).toBe('a')
    expect(stepTrajectory(list, 'c', 1)).toBe('c')
    expect(stepTrajectory(list, 'a', -1)).toBe('a')
  })

  it('starts from the newest when nothing visible is selected', () => {
    expect(stepTrajectory(rows('a', 'b'), null, -1)).toBe('b')
    expect(stepTrajectory(rows('a', 'b'), 'gone', 1)).toBe('b')
  })

  it('has nothing to step to in an empty list', () => {
    expect(stepTrajectory([], 'a', 1)).toBeNull()
    expect(stepTrajectory(undefined, 'a', 1)).toBeNull()
  })
})

describe('useTrajectoryFollow', () => {
  let scope

  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => {
    scope?.stop()
    vi.useRealTimers()
  })

  function setup(initial, selected = null) {
    const items = ref(initial)
    const selectedId = ref(selected)
    scope = effectScope()
    const follow = scope.run(() => useTrajectoryFollow(items, selectedId))
    return { items, selectedId, ...follow }
  }

  const tick = async (ms = 0) => {
    await nextTick()
    await vi.advanceTimersByTimeAsync(ms)
  }

  it('is off by default and leaves the selection alone', async () => {
    const { items, selectedId, following } = setup(rows('a'), 'a')
    expect(following.value).toBe(false)
    items.value = rows('a', 'b')
    await tick(FOLLOW_STEP_MS * 3)
    expect(selectedId.value).toBe('a')
  })

  it('jumps to the newest row when turned on', async () => {
    const { selectedId, setFollowing, following } = setup(rows('a', 'b', 'c'), 'a')
    setFollowing(true)
    expect(following.value).toBe(true)
    expect(selectedId.value).toBe('c')
  })

  it('turns on over an empty list without selecting anything', async () => {
    const { items, selectedId, setFollowing } = setup([])
    setFollowing(true)
    await tick(FOLLOW_STEP_MS)
    expect(selectedId.value).toBeNull()
    items.value = rows('a')
    await tick()
    expect(selectedId.value).toBe('a')
  })

  it('shows a burst of new rows one at a time, a step apart', async () => {
    const { items, selectedId, setFollowing } = setup(rows('a'), 'a')
    setFollowing(true)
    items.value = rows('a', 'b', 'c')
    await tick(FOLLOW_STEP_MS - 1)
    expect(selectedId.value).toBe('a')
    await tick(1)
    expect(selectedId.value).toBe('b')
    await tick(FOLLOW_STEP_MS)
    expect(selectedId.value).toBe('c')
    await tick(FOLLOW_STEP_MS * 2)
    expect(selectedId.value).toBe('c')
  })

  it('does not restart a pending step when more rows arrive', async () => {
    const { items, selectedId, setFollowing } = setup(rows('a'), 'a')
    setFollowing(true)
    items.value = rows('a', 'b')
    await tick(FOLLOW_STEP_MS / 2)
    items.value = rows('a', 'b', 'c')
    await tick(FOLLOW_STEP_MS / 2)
    expect(selectedId.value).toBe('b')
  })

  it('jumps to the newest when the selection is filtered out', async () => {
    const { items, selectedId, setFollowing } = setup(rows('a', 'b'), 'b')
    setFollowing(true)
    items.value = rows('c', 'd')
    await tick()
    expect(selectedId.value).toBe('d')
  })

  it('stops stepping when turned off', async () => {
    const { items, selectedId, setFollowing } = setup(rows('a'), 'a')
    setFollowing(true)
    items.value = rows('a', 'b')
    await tick()
    setFollowing(false)
    await tick(FOLLOW_STEP_MS * 2)
    expect(selectedId.value).toBe('a')
  })

  it('does not step after being turned off with the step already due', async () => {
    const { items, selectedId, setFollowing, following } = setup(rows('a'), 'a')
    setFollowing(true)
    items.value = rows('a', 'b')
    await tick()
    // Off without a flush in between: the watcher has not cancelled yet.
    following.value = false
    vi.advanceTimersByTime(FOLLOW_STEP_MS)
    expect(selectedId.value).toBe('a')
  })

  it('cancels its step when its scope ends', async () => {
    const { items, selectedId, setFollowing } = setup(rows('a'), 'a')
    setFollowing(true)
    items.value = rows('a', 'b')
    await tick()
    scope.stop()
    await tick(FOLLOW_STEP_MS * 2)
    expect(selectedId.value).toBe('a')
  })
})

describe('the trajectory panel', () => {
  let app
  let el

  const toolCall = (id, minute) => ({
    id, toolName: 'Bash', description: id, inputPreview: '{}', status: 'completed',
    createdAt: new Date(Date.UTC(2026, 10, 4, 10, minute)).toISOString(),
  })

  beforeEach(() => { vi.useFakeTimers() })
  afterEach(() => {
    app?.unmount()
    el?.remove()
    vi.useRealTimers()
  })

  async function mount(calls) {
    const toolCalls = ref(calls)
    el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(TrajectoryPanel, { toolCalls: toolCalls.value }) })
    app.mount(el)
    await nextTick()
    await nextTick()
    return toolCalls
  }

  const list = () => el.querySelector('[data-testid="trajectory-list"]')
  const row = (id) => el.querySelector(`[data-trajectory-id="t-${id}"]`)
  const selected = () => [...el.querySelectorAll('[data-trajectory-id]')].find((b) => b.className.includes('bg-gray-100 ')).dataset.trajectoryId.replace(/^t-/, '')
  const toggle = () => el.querySelector('[data-testid="trajectory-follow"]')
  const press = async (target, key) => {
    target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true }))
    await vi.advanceTimersByTimeAsync(0)
  }

  it('moves the selection with the arrow keys and keeps focus on it', async () => {
    await mount([toolCall('t1', 1), toolCall('t2', 2), toolCall('t3', 3)])
    expect(selected()).toBe('t3')
    row('t3').focus()
    await press(row('t3'), 'ArrowUp')
    expect(selected()).toBe('t2')
    expect(document.activeElement).toBe(row('t2'))
    await press(document.activeElement, 'ArrowUp')
    await press(document.activeElement, 'ArrowUp')
    expect(selected()).toBe('t1')
    await press(document.activeElement, 'ArrowDown')
    expect(selected()).toBe('t2')
  })

  it('leaves other keys to the browser', async () => {
    await mount([toolCall('t1', 1), toolCall('t2', 2)])
    const e = new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true })
    list().dispatchEvent(e)
    expect(e.defaultPrevented).toBe(false)
    expect(selected()).toBe('t2')
  })

  it('follows new rows when switched on, and a click hands control back', async () => {
    const toolCalls = await mount([toolCall('t1', 1), toolCall('t2', 2)])
    row('t1').click()
    await nextTick()
    expect(selected()).toBe('t1')

    toggle().click()
    await nextTick()
    expect(toggle().getAttribute('aria-checked')).toBe('true')
    expect(selected()).toBe('t2')

    toolCalls.value = [...toolCalls.value, toolCall('t3', 3), toolCall('t4', 4)]
    await vi.advanceTimersByTimeAsync(FOLLOW_STEP_MS)
    expect(selected()).toBe('t3')
    await vi.advanceTimersByTimeAsync(FOLLOW_STEP_MS)
    expect(selected()).toBe('t4')

    row('t1').click()
    await nextTick()
    expect(toggle().getAttribute('aria-checked')).toBe('false')
    toolCalls.value = [...toolCalls.value, toolCall('t5', 5)]
    await vi.advanceTimersByTimeAsync(FOLLOW_STEP_MS * 2)
    expect(selected()).toBe('t1')
  })

  it('an arrow key turns Follow off too', async () => {
    await mount([toolCall('t1', 1), toolCall('t2', 2)])
    toggle().click()
    await nextTick()
    await press(list(), 'ArrowUp')
    expect(selected()).toBe('t1')
    expect(toggle().getAttribute('aria-checked')).toBe('false')
  })
})
