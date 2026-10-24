// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it } from 'vitest'
import { nextTick } from 'vue'

import { overlayOpen, useOverlayOpen, watchOverlays } from '../src/composables/useOverlayOpen'
import { useToasts } from '../src/composables/useToasts'

const flush = () => new Promise((resolve) => setTimeout(resolve))

let stop
let root

function start(html = '') {
  stop?.()
  root?.remove()
  root = document.createElement('div')
  root.innerHTML = html
  document.body.append(root)
  stop = watchOverlays(root)
}

afterEach(() => {
  stop?.()
  stop = null
  root?.remove()
  useToasts().toasts.value = []
})

describe('overlayOpen', () => {
  it('is the same signal wherever it is asked for', () => {
    expect(useOverlayOpen()).toBe(overlayOpen)
  })

  it('is false on a page with nothing over it', () => {
    start('<main>page</main>')
    expect(overlayOpen.value).toBe(false)
  })

  it('is true when a dialog or a menu is already there', () => {
    start('<div role="dialog"></div>')
    expect(overlayOpen.value).toBe(true)
    stop()
    start('<div role="menu"></div>')
    expect(overlayOpen.value).toBe(true)
  })

  it('follows a dialog that opens and closes, even inside another element', async () => {
    start()
    const wrapper = document.createElement('section')
    wrapper.innerHTML = '<div role="dialog"></div>'
    root.append(wrapper)
    await flush()
    expect(overlayOpen.value).toBe(true)
    wrapper.remove()
    await flush()
    expect(overlayOpen.value).toBe(false)
  })

  it('follows an element that becomes a menu', async () => {
    start('<div id="m"></div>')
    root.querySelector('#m').setAttribute('role', 'menu')
    await flush()
    expect(overlayOpen.value).toBe(true)
    root.querySelector('#m').removeAttribute('role')
    await flush()
    expect(overlayOpen.value).toBe(false)
  })

  it('ignores what comes and goes that is not an overlay, text included', async () => {
    start()
    root.append(document.createElement('p'), document.createTextNode('hi'))
    await flush()
    expect(overlayOpen.value).toBe(false)
  })

  it('is true while a toast is showing', async () => {
    start()
    const { addToast, removeToast } = useToasts()
    const id = addToast('Saved', 'success', null, 0)
    await nextTick()
    expect(overlayOpen.value).toBe(true)
    removeToast(id)
    await nextTick()
    expect(overlayOpen.value).toBe(false)
  })

  it('stops watching, and reports nothing open, once stopped', async () => {
    start('<div role="dialog"></div>')
    stop()
    expect(overlayOpen.value).toBe(false)
    root.append(Object.assign(document.createElement('div'), { role: 'menu' }))
    root.firstChild.setAttribute('role', 'dialog')
    await flush()
    expect(overlayOpen.value).toBe(false)
  })

  it('watches the body unless told otherwise', () => {
    stop = watchOverlays()
    expect(overlayOpen.value).toBe(false)
  })
})
