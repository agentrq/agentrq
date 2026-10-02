// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The side panel, mounted, with a stand-in for the `<webview>` it creates —
 * jsdom has no guest pages, so the element records what it was asked to do and
 * the test plays the events a real one would fire.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, nextTick } from 'vue'

import SidePanel from '../src/components/SidePanel.vue'
import {
  MIN_MAIN,
  MIN_WIDTH,
  SIDE_PANEL_STORAGE_KEY,
  resetSidePanelForTests,
  useSidePanel,
} from '../src/composables/useSidePanel'

function fakeGuest() {
  const guest = document.createElement('div')
  guest.history = { back: false, forward: false }
  Object.assign(guest, {
    loadURL: vi.fn(),
    goBack: vi.fn(),
    goForward: vi.fn(),
    reload: vi.fn(),
    stop: vi.fn(),
    canGoBack: () => guest.history.back,
    canGoForward: () => guest.history.forward,
  })
  return guest
}

function fire(guest, type, fields = {}) {
  const event = new Event(type)
  Object.assign(event, fields)
  guest.dispatchEvent(event)
}

let app
let el
let guests

function mount({ url = '', open = true, full = false, width = null, bridge = { openExternal: vi.fn() } } = {}) {
  localStorage.setItem(SIDE_PANEL_STORAGE_KEY, JSON.stringify({ open, full, width, url }))
  resetSidePanelForTests()
  guests = []
  const createGuest = () => {
    const guest = fakeGuest()
    guests.push(guest)
    return guest
  }
  el = document.createElement('div')
  // The row the panel shares with the main column.
  Object.defineProperty(el, 'clientWidth', { configurable: true, value: 1400 })
  document.body.appendChild(el)
  app = createApp({ render: () => h(SidePanel, { createGuest, bridge }) })
  app.mount(el)
  return { bridge, panel: useSidePanel() }
}

const $ = (selector) => el.querySelector(selector)
const click = async (selector) => { $(selector).click(); await nextTick() }

async function type(text) {
  const input = $('[data-side-panel-address]')
  input.value = text
  input.dispatchEvent(new Event('input'))
  input.closest('form').dispatchEvent(new Event('submit'))
  await nextTick()
}

beforeEach(() => localStorage.clear())

afterEach(() => {
  app?.unmount()
  el?.remove()
  app = null
  localStorage.clear()
  resetSidePanelForTests()
})

describe('SidePanel', () => {
  it('opens empty, with the address field ready, when nothing was showing', async () => {
    mount()
    await nextTick()
    expect($('[data-side-panel-empty]')).not.toBeNull()
    expect(document.activeElement).toBe($('[data-side-panel-address]'))
    expect(guests).toHaveLength(0)
    expect($('[data-side-panel-reload]').disabled).toBe(true)
    expect($('[data-side-panel-external]').disabled).toBe(true)
  })

  it('reopens the page it was showing, at the width it was left', async () => {
    mount({ url: 'https://example.com/', width: 520 })
    expect(guests).toHaveLength(1)
    expect(guests[0].getAttribute('src')).toBe('https://example.com/')
    // A guest is transparent; a page with no background must not show the app's dark theme through it.
    expect(guests[0].className).toContain('bg-white')
    expect($('[data-side-panel]').style.width).toBe('520px')
    expect($('[data-side-panel-address]').value).toBe('https://example.com/')
  })

  it('loads what is typed into the address field', async () => {
    const { panel } = mount()
    await type('example.com')
    expect(guests[0].getAttribute('src')).toBe('https://example.com/')

    await type('https://example.org/next')
    expect(guests).toHaveLength(1)
    expect(guests[0].loadURL).toHaveBeenCalledWith('https://example.org/next')
    expect(panel.state.url).toBe('')
  })

  it('refuses an address the panel will not show, and says so', async () => {
    mount()
    await type('javascript:alert(1)')
    expect(guests).toHaveLength(0)
    expect($('[data-side-panel-address]').className).toContain('ring-red-300')

    await type('example.com')
    expect($('[data-side-panel-address]').className).not.toContain('ring-red-300')
  })

  it('follows the guest as it navigates, and remembers where it went', async () => {
    const { panel } = mount({ url: 'https://example.com/' })
    const guest = guests[0]
    guest.history.back = true

    fire(guest, 'did-start-loading')
    await nextTick()
    expect($('[data-side-panel-reload]').title).toBe('Stop')

    fire(guest, 'did-navigate', { url: 'https://example.com/next' })
    fire(guest, 'did-stop-loading')
    await nextTick()

    expect($('[data-side-panel-address]').value).toBe('https://example.com/next')
    expect(panel.state.url).toBe('https://example.com/next')
    expect($('[data-side-panel-back]').disabled).toBe(false)
    expect($('[data-side-panel-forward]').disabled).toBe(true)
    expect($('[data-side-panel-reload]').title).toBe('Reload')
  })

  it('does not overwrite an address somebody is typing', async () => {
    mount({ url: 'https://example.com/' })
    const input = $('[data-side-panel-address]')
    input.focus()
    input.value = 'half-typ'
    input.dispatchEvent(new Event('input'))
    fire(guests[0], 'did-navigate', { url: 'https://example.com/next' })
    await nextTick()
    expect(input.value).toBe('half-typ')
  })

  it('follows a navigation inside the page, but not one in a frame', async () => {
    mount({ url: 'https://example.com/' })
    fire(guests[0], 'did-navigate-in-page', { url: 'https://example.com/#frame', isMainFrame: false })
    await nextTick()
    expect($('[data-side-panel-address]').value).toBe('https://example.com/')

    fire(guests[0], 'did-navigate-in-page', { url: 'https://example.com/#section', isMainFrame: true })
    await nextTick()
    expect($('[data-side-panel-address]').value).toBe('https://example.com/#section')
  })

  it('drives the guest from the toolbar', async () => {
    const { bridge } = mount({ url: 'https://example.com/' })
    const guest = guests[0]
    guest.history = { back: true, forward: true }
    fire(guest, 'did-stop-loading')
    await nextTick()

    await click('[data-side-panel-back]')
    await click('[data-side-panel-forward]')
    await click('[data-side-panel-reload]')
    expect(guest.goBack).toHaveBeenCalled()
    expect(guest.goForward).toHaveBeenCalled()
    expect(guest.reload).toHaveBeenCalled()

    fire(guest, 'did-start-loading')
    await nextTick()
    await click('[data-side-panel-reload]')
    expect(guest.stop).toHaveBeenCalled()

    await click('[data-side-panel-external]')
    expect(bridge.openExternal).toHaveBeenCalledWith('https://example.com/')
  })

  it('offers no browser for a page that is not on the web', async () => {
    const { bridge } = mount({ url: 'agentrq-ext://notes/panel/index.html' })
    expect($('[data-side-panel-external]').disabled).toBe(true)
    // Disabled buttons still dispatch a programmatic click in jsdom.
    $('[data-side-panel-external]').click()
    expect(bridge.openExternal).not.toHaveBeenCalled()
  })

  it('expands to the whole window, where it has no edge to drag, and collapses back', async () => {
    const { panel } = mount({ url: 'https://example.com/', width: 500 })
    const expand = $('[data-side-panel-full]')
    expect(expand.title).toBe('Expand to the whole window')
    expect(expand.getAttribute('aria-pressed')).toBe('false')

    await click('[data-side-panel-full]')
    expect(panel.state.full).toBe(true)
    expect($('[data-side-panel]').dataset.full).toBe('true')
    expect($('[data-side-panel]').className).toContain('flex-1')
    expect($('[data-side-panel]').style.width).toBe('')
    expect($('[data-side-panel-handle]')).toBeNull()
    expect($('[data-side-panel-full]').title).toBe('Collapse to the side')

    await click('[data-side-panel-full]')
    expect(panel.state.full).toBe(false)
    expect($('[data-side-panel]').style.width).toBe('500px')
    expect($('[data-side-panel-handle]')).not.toBeNull()
  })

  it('reopens expanded when it was left expanded', () => {
    mount({ url: 'https://example.com/', full: true })
    expect($('[data-side-panel]').dataset.full).toBe('true')
  })

  it('closes', async () => {
    const { panel } = mount({ url: 'https://example.com/' })
    await click('[data-side-panel-close]')
    expect(panel.state.open).toBe(false)
  })

  it('shows a failed load with a way to retry, and ignores an aborted one', async () => {
    mount({ url: 'https://example.com/' })
    const guest = guests[0]

    fire(guest, 'did-fail-load', { errorCode: -3, errorDescription: 'ERR_ABORTED', isMainFrame: true })
    await nextTick()
    expect($('[data-side-panel-error]')).toBeNull()

    fire(guest, 'did-fail-load', { errorCode: -105, errorDescription: 'ERR_NAME_NOT_RESOLVED', isMainFrame: false })
    await nextTick()
    expect($('[data-side-panel-error]')).toBeNull()

    fire(guest, 'did-fail-load', { errorCode: -105, errorDescription: 'ERR_NAME_NOT_RESOLVED', isMainFrame: true })
    await nextTick()
    expect($('[data-side-panel-error]').textContent).toContain('ERR_NAME_NOT_RESOLVED')

    await click('[data-side-panel-retry]')
    expect(guest.reload).toHaveBeenCalled()
    expect($('[data-side-panel-error]')).toBeNull()
  })

  it('names the error code when the guest gives no description', async () => {
    mount({ url: 'https://example.com/' })
    fire(guests[0], 'did-fail-load', { errorCode: -7, isMainFrame: true })
    await nextTick()
    expect($('[data-side-panel-error]').textContent).toContain('Error -7')
    expect($('[data-side-panel-error]').textContent).toContain('Open in browser')
  })

  it('opens a page the app asks for while it is already showing', async () => {
    const { panel } = mount({ url: 'https://example.com/' })
    panel.open('https://example.org/linked')
    await nextTick()
    expect(guests[0].loadURL).toHaveBeenCalledWith('https://example.org/linked')
    expect($('[data-side-panel-address]').value).toBe('https://example.org/linked')
  })

  describe('resizing', () => {
    function drag(fromX, toX) {
      const handle = $('[data-side-panel-handle]')
      handle.setPointerCapture = vi.fn()
      handle.dispatchEvent(new MouseEvent('pointerdown', { button: 0, clientX: fromX, bubbles: true }))
      return {
        handle,
        move: (x) => window.dispatchEvent(new MouseEvent('pointermove', { clientX: x })),
        end: () => window.dispatchEvent(new MouseEvent('pointerup')),
        toX,
      }
    }

    it('shows a grip on the edge before anyone hovers it', () => {
      mount()
      expect(el.querySelector('[data-side-panel-grip]').className).toContain('bg-gray-300')
    })

    it('fills the room the main column leaves, until somebody drags it', async () => {
      mount()
      await nextTick()
      expect($('[data-side-panel]').style.width).toBe(`${1400 - MIN_MAIN}px`)
    })

    it('widens as the edge is dragged left, and lets the page ignore the pointer meanwhile', async () => {
      const { panel } = mount({ url: 'https://example.com/', width: 440 })
      const gesture = drag(1000, 900)
      await nextTick()
      expect(gesture.handle.setPointerCapture).toHaveBeenCalled()
      expect(guests[0].parentElement.className).toContain('pointer-events-none')
      expect(el.querySelector('[data-side-panel-grip]').className).toContain('bg-gray-500')

      gesture.move(900)
      await nextTick()
      expect(panel.state.width).toBe(540)
      expect($('[data-side-panel]').style.width).toBe(`${540}px`)

      gesture.end()
      await nextTick()
      expect(guests[0].parentElement.className).not.toContain('pointer-events-none')
      // Moves after the drag ended change nothing.
      window.dispatchEvent(new MouseEvent('pointermove', { clientX: 100 }))
      expect(panel.state.width).toBe(540)
    })

    it('never narrower than its minimum', async () => {
      const { panel } = mount({ width: 440 })
      const gesture = drag(500, 0)
      gesture.move(1400)
      gesture.end()
      expect(panel.state.width).toBe(MIN_WIDTH)
    })

    it('ignores anything but the primary button', async () => {
      mount()
      const handle = $('[data-side-panel-handle]')
      handle.dispatchEvent(new MouseEvent('pointerdown', { button: 2, clientX: 500, bubbles: true }))
      await nextTick()
      expect(el.querySelector('[data-side-panel-grip]').className).not.toContain('bg-gray-500')
    })

    it('goes back to filling the window on a double-click', async () => {
      const { panel } = mount({ width: 700 })
      $('[data-side-panel-handle]').dispatchEvent(new MouseEvent('dblclick', { bubbles: true }))
      await nextTick()
      expect(panel.state.width).toBeNull()
      expect($('[data-side-panel]').style.width).toBe(`${1400 - MIN_MAIN}px`)
    })

    it('gives way when the window becomes too narrow for it, without forgetting what was chosen', async () => {
      const { panel } = mount({ width: 900 })
      Object.defineProperty(el, 'clientWidth', { configurable: true, value: 1000 })
      window.dispatchEvent(new Event('resize'))
      await nextTick()
      expect($('[data-side-panel]').style.width).toBe('520px')
      expect(panel.state.width).toBe(900)
    })

    it('measures the window when the row has no width yet', async () => {
      mount({ width: 900 })
      Object.defineProperty(el, 'clientWidth', { configurable: true, value: 0 })
      window.dispatchEvent(new Event('resize'))
      await nextTick()
      expect($('[data-side-panel]').style.width).toBe(`${Math.max(MIN_WIDTH, Math.min(900, window.innerWidth - MIN_MAIN))}px`)
    })
  })

  describe('measuring the room it shares with the main column', () => {
    const widths = { MAIN: 900, ASIDE: 440 }
    let restore

    beforeEach(() => {
      const descriptor = Object.getOwnPropertyDescriptor(HTMLElement.prototype, 'offsetWidth')
      Object.defineProperty(HTMLElement.prototype, 'offsetWidth', { configurable: true, get() { return widths[this.tagName] ?? 0 } })
      restore = () => Object.defineProperty(HTMLElement.prototype, 'offsetWidth', descriptor)
    })

    afterEach(() => {
      restore()
      delete globalThis.ResizeObserver
    })

    function mountBesideMain() {
      localStorage.setItem(SIDE_PANEL_STORAGE_KEY, JSON.stringify({ open: true, width: null, url: '' }))
      resetSidePanelForTests()
      const observer = { targets: [], disconnect: vi.fn(), fire: null }
      globalThis.ResizeObserver = class {
        constructor(callback) { observer.fire = callback }
        observe(target) { observer.targets.push(target) }
        disconnect() { observer.disconnect() }
      }
      el = document.createElement('div')
      document.body.appendChild(el)
      app = createApp({ render: () => h('div', [h('main'), h(SidePanel, { createGuest: fakeGuest })]) })
      app.mount(el)
      return observer
    }

    it('counts the main column and itself, never the sidebar beside them', async () => {
      mountBesideMain()
      await nextTick()
      expect($('[data-side-panel]').style.width).toBe(`${900 + 440 - MIN_MAIN}px`)
    })

    it('follows the main column as the sidebar collapses, and stops when it closes', async () => {
      const observer = mountBesideMain()
      expect(observer.targets.map((t) => t.tagName)).toEqual(['MAIN'])

      widths.MAIN = 1092
      observer.fire()
      await nextTick()
      expect($('[data-side-panel]').style.width).toBe(`${1092 + 440 - MIN_MAIN}px`)
      widths.MAIN = 900

      app.unmount()
      app = null
      expect(observer.disconnect).toHaveBeenCalledOnce()
    })
  })

  it('makes a real webview element and reaches the shell bridge by default', async () => {
    window.agentrq = { sidePanel: { openExternal: vi.fn() } }
    localStorage.setItem(SIDE_PANEL_STORAGE_KEY, JSON.stringify({ open: true, width: 440, url: 'https://example.com/' }))
    resetSidePanelForTests()
    el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(SidePanel) })
    app.mount(el)
    const webview = el.querySelector('webview')
    expect(webview.getAttribute('src')).toBe('https://example.com/')
    await click('[data-side-panel-external]')
    expect(window.agentrq.sidePanel.openExternal).toHaveBeenCalledWith('https://example.com/')
    delete window.agentrq
  })
})
