// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Escape closes what a page has open, from wherever focus is, and only when
 * nobody else answered the key first.
 */

import { describe, it, expect, vi } from 'vitest'
import { useEscapeKey } from '../src/composables/useEscapeKey'

function listen(close) {
  const target = new EventTarget()
  const hooks = {}
  useEscapeKey(close, {
    target,
    onMounted: (fn) => { hooks.mount = fn },
    onUnmounted: (fn) => { hooks.unmount = fn },
  })
  hooks.mount()
  const press = (key = 'Escape', init = {}) => {
    const event = new KeyboardEvent('keydown', { key, cancelable: true, ...init })
    target.dispatchEvent(event)
    return event
  }
  return { press, unmount: hooks.unmount }
}

describe('useEscapeKey', () => {
  it('closes on Escape and claims the key', () => {
    const close = vi.fn()
    const { press } = listen(close)
    expect(press().defaultPrevented).toBe(true)
    expect(close).toHaveBeenCalledTimes(1)
  })

  it('ignores every other key', () => {
    const close = vi.fn()
    listen(close).press('Enter')
    expect(close).not.toHaveBeenCalled()
  })

  it('leaves an Escape somebody already answered alone', () => {
    const close = vi.fn()
    const target = new EventTarget()
    target.addEventListener('keydown', (e) => e.preventDefault())
    useEscapeKey(close, { target, onMounted: (fn) => fn(), onUnmounted: () => {} })
    target.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    expect(close).not.toHaveBeenCalled()
  })

  it('leaves an Escape that ends an IME composition alone', () => {
    const close = vi.fn()
    listen(close).press('Escape', { isComposing: true })
    expect(close).not.toHaveBeenCalled()
  })

  it('lets the key go on when there was nothing to close', () => {
    const { press } = listen(() => false)
    expect(press().defaultPrevented).toBe(false)
  })

  it('stops listening once unmounted', () => {
    const close = vi.fn()
    const { press, unmount } = listen(close)
    unmount()
    press()
    expect(close).not.toHaveBeenCalled()
  })

  it('listens on the window by default', () => {
    const close = vi.fn()
    let unmount
    useEscapeKey(close, { onMounted: (fn) => fn(), onUnmounted: (fn) => { unmount = fn } })
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', cancelable: true }))
    unmount()
    expect(close).toHaveBeenCalledTimes(1)
  })
})
