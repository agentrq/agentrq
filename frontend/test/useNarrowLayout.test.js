// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'

import { NARROW_BELOW, isNarrowWidth, useNarrowLayout } from '../src/composables/useNarrowLayout'

describe('isNarrowWidth', () => {
  it('is a phone below the md breakpoint', () => {
    expect(isNarrowWidth(480)).toBe(true)
    expect(isNarrowWidth(NARROW_BELOW - 1)).toBe(true)
    expect(isNarrowWidth(NARROW_BELOW)).toBe(false)
    expect(isNarrowWidth(1200)).toBe(false)
  })

  it('does not take an element that is not laid out yet for a narrow one', () => {
    expect(isNarrowWidth(0)).toBe(false)
  })

  it('takes another breakpoint', () => {
    expect(isNarrowWidth(900, 1024)).toBe(true)
  })
})

describe('useNarrowLayout', () => {
  let app

  afterEach(() => {
    app?.unmount()
    app = null
  })

  /** A ResizeObserver the test drives by hand. */
  function fakeObserver() {
    const observer = { targets: [], callback: null, disconnect: vi.fn() }
    observer.Observer = class {
      constructor(callback) { observer.callback = callback }
      observe(target) { observer.targets.push(target) }
      disconnect() { observer.disconnect() }
    }
    return observer
  }

  function mount(width, options) {
    let narrow
    const el = document.createElement('div')
    app = createApp({
      setup() {
        const element = ref(null)
        narrow = useNarrowLayout(element, options)
        return () => h('div', {
          ref: (node) => {
            if (node) Object.defineProperty(node, 'clientWidth', { configurable: true, value: width })
            element.value = node
          },
        })
      },
    })
    app.mount(el)
    return () => narrow.value
  }

  it('reads the element when it mounts', () => {
    const observer = fakeObserver()
    expect(mount(480, { Observer: observer.Observer })()).toBe(true)
    expect(observer.targets).toHaveLength(1)
  })

  it('follows the element as it is resized, and stops when it unmounts', async () => {
    const observer = fakeObserver()
    const narrow = mount(1200, { Observer: observer.Observer })
    expect(narrow()).toBe(false)

    observer.callback([{ contentRect: { width: 500 } }])
    await nextTick()
    expect(narrow()).toBe(true)

    // An observer that reports no size falls back to measuring the element.
    observer.callback([])
    expect(narrow()).toBe(false)

    app.unmount()
    app = null
    expect(observer.disconnect).toHaveBeenCalledOnce()
  })

  it('is never narrow where nothing can measure it', () => {
    expect(mount(480, { Observer: null })()).toBe(false)
  })

  it('is never narrow without an element', () => {
    const observer = fakeObserver()
    let narrow
    app = createApp({ setup() { narrow = useNarrowLayout(ref(null), { Observer: observer.Observer }); return () => h('div') } })
    app.mount(document.createElement('div'))
    expect(narrow.value).toBe(false)
    expect(observer.targets).toHaveLength(0)
  })

  it('uses the browser ResizeObserver by default', () => {
    const observe = vi.fn()
    globalThis.ResizeObserver = class { observe(target) { observe(target) } disconnect() {} }
    try {
      mount(480)
      expect(observe).toHaveBeenCalledOnce()
    } finally {
      delete globalThis.ResizeObserver
    }
  })
})
