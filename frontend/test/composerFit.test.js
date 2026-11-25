// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The New Task description grows with its text up to the room left on
 * screen and keeps a height dragged by hand; the form widens only when asked,
 * by the expand toggle or by dragging its edges.
 * jsdom lays nothing out, so the sizes the fit reads are given by hand.
 */

import { describe, it, expect, afterEach } from 'vitest'
import { createApp, h, nextTick, ref } from 'vue'

import { useComposerFit } from '../src/composables/useComposerFit'

let app

afterEach(() => {
  app?.unmount()
  app = null
})

/**
 * A container 800×1400 with 40px of padding each way, holding a 1000px column
 * whose parts besides the text take 200px: 560px of room for the text, and
 * 1360px for the column.
 */
function rig({ scrollHeight = 100, besides = 200, withElements = true } = {}) {
  const container = document.createElement('main')
  container.style.paddingTop = '24px'
  container.style.paddingBottom = '16px'
  container.style.paddingLeft = '20px'
  container.style.paddingRight = '20px'
  Object.defineProperty(container, 'clientHeight', { get: () => 800 })
  Object.defineProperty(container, 'clientWidth', { get: () => 1400 })
  const textarea = document.createElement('textarea')
  const column = document.createElement('div')
  const text = { scrollHeight }
  Object.defineProperty(textarea, 'scrollHeight', { get: () => text.scrollHeight })
  Object.defineProperty(textarea, 'offsetHeight', { get: () => parseFloat(textarea.style.height) || 0 })
  Object.defineProperty(column, 'offsetHeight', { get: () => besides + textarea.offsetHeight })
  Object.defineProperty(column, 'offsetWidth', { get: () => 1000 })

  const body = ref('')
  let fit
  app = createApp({
    setup() {
      fit = useComposerFit({
        el: ref(withElements ? textarea : null),
        container: ref(container),
        content: ref(column),
        text: body,
      })
      return () => h('div')
    },
  })
  app.mount(document.createElement('div'))
  return { textarea, text, body, fit }
}

describe('useComposerFit', () => {
  it('starts at its least height, with the room as its limit', () => {
    const { textarea } = rig()
    expect(textarea.style.height).toBe('160px')
    expect(textarea.style.maxHeight).toBe('560px')
  })

  it('grows with the text, up to the room left', async () => {
    const { textarea, text, body } = rig()
    text.scrollHeight = 300
    body.value = 'a few lines'
    await nextTick(); await nextTick()
    expect(textarea.style.height).toBe('300px')

    text.scrollHeight = 2000
    body.value = 'a great many lines'
    await nextTick(); await nextTick()
    expect(textarea.style.height).toBe('560px')
  })

  it('keeps a height dragged by hand, within the room', async () => {
    const { textarea, text, body } = rig()
    textarea.style.height = '420px' // the grip, dragged
    text.scrollHeight = 200
    body.value = 'more'
    await nextTick(); await nextTick()
    expect(textarea.style.height).toBe('420px')
    expect(textarea.style.maxHeight).toBe('560px')
  })

  it('keeps the page width until an edge is dragged, centred, within bounds', async () => {
    const { textarea, text, fit } = rig()
    expect(fit.width.value).toBeNull()

    fit.dragging.value = true
    await nextTick()
    fit.resize(50) // both sides move, so the form grows by twice the edge
    expect(fit.width.value).toBe(1100)
    fit.resize(-1000)
    expect(fit.width.value).toBe(384)
    fit.resize(5000)
    expect(fit.width.value).toBe(1360)
    fit.dragging.value = false
    await nextTick()

    // The text wraps anew at the new width, and is fitted to it.
    text.scrollHeight = 210
    await nextTick(); await nextTick()
    expect(textarea.style.height).toBe('210px')

    fit.reset()
    expect(fit.width.value).toBeNull()
  })

  it('fills the room when expanded, and fits the text again when restored', async () => {
    const { textarea, fit } = rig()
    textarea.style.height = '300px' // dragged, then given way to the toggle
    fit.toggleExpanded()
    expect(fit.expanded.value).toBe(true)
    await nextTick(); await nextTick()
    expect(textarea.style.height).toBe('560px')

    fit.toggleExpanded()
    await nextTick(); await nextTick()
    expect(textarea.style.height).toBe('160px')
  })

  it('lets an edge drag and the toggle each clear the other', async () => {
    const { fit } = rig()
    fit.toggleExpanded()
    fit.dragging.value = true
    await nextTick()
    expect(fit.expanded.value).toBe(false)
    fit.resize(10)
    expect(fit.width.value).toBe(1020)
    fit.dragging.value = false
    await nextTick()

    fit.toggleExpanded()
    expect(fit.expanded.value).toBe(true)
    expect(fit.width.value).toBeNull()
  })

  it('refits when the window is resized, and stops listening once gone', () => {
    const { textarea, text } = rig()
    text.scrollHeight = 250
    globalThis.dispatchEvent(new Event('resize'))
    expect(textarea.style.height).toBe('250px')

    app.unmount()
    app = null
    text.scrollHeight = 400
    globalThis.dispatchEvent(new Event('resize'))
    expect(textarea.style.height).toBe('250px')
  })

  it('never gives less than its least height, however little room is left', () => {
    // The rest of the column is already taller than the screen.
    const { textarea } = rig({ scrollHeight: 900, besides: 1000 })
    expect(textarea.style.maxHeight).toBe('160px')
    expect(textarea.style.height).toBe('160px')
  })

  it('does nothing before its elements are drawn', () => {
    const { fit } = rig({ withElements: false })
    expect(() => fit.fit()).not.toThrow()
  })
})
