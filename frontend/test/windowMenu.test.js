// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The macOS title bar's ⋮ menu: the rules, then the component mounted on a
 * stand-in shell.
 */

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, h, reactive } from 'vue'

import { pageWebUrl, serverHost, useWindowMenu } from '../src/composables/useWindowMenu'
import { useToasts } from '../src/composables/useToasts'
import WindowMenu from '../src/components/WindowMenu.vue'

const tick = () => new Promise((r) => setTimeout(r, 0))

describe('pageWebUrl', () => {
  it('puts the page on the server', () => {
    expect(pageWebUrl('https://app.agentrq.com', '/workspaces/W1?tab=tasks')).toBe('https://app.agentrq.com/workspaces/W1?tab=tasks')
  })

  it('keeps a server base path, and never doubles a slash', () => {
    expect(pageWebUrl('https://host.example/agentrq/', '/tasks/ongoing')).toBe('https://host.example/agentrq/tasks/ongoing')
    expect(pageWebUrl('https://host.example', 'machines')).toBe('https://host.example/machines')
  })

  it('points at the root for no path', () => {
    expect(pageWebUrl('https://host.example', '')).toBe('https://host.example/')
    expect(pageWebUrl('https://host.example', undefined)).toBe('https://host.example/')
  })

  it('has nothing to point at without a server', () => {
    expect(pageWebUrl('', '/x')).toBe('')
    expect(pageWebUrl(null, '/x')).toBe('')
  })
})

describe('serverHost', () => {
  it('names the server by its host', () => {
    expect(serverHost('https://app.agentrq.com:8443/base')).toBe('app.agentrq.com:8443')
  })

  it('shows an address that does not parse as it is', () => {
    expect(serverHost('not a url')).toBe('not a url')
    expect(serverHost(undefined)).toBe('')
  })
})

describe('useWindowMenu', () => {
  function setup(overrides = {}) {
    const deps = {
      serverUrl: () => 'https://app.agentrq.com',
      path: () => '/workspaces/W1',
      view: {
        get: vi.fn(async () => ({ zoom: 110, fullScreen: false })),
        zoom: vi.fn(async (d) => ({ zoom: { in: 125, out: 90, reset: 100 }[d], fullScreen: false })),
        toggleFullScreen: vi.fn(async () => ({ zoom: 100, fullScreen: true })),
      },
      updates: { check: vi.fn() },
      copyText: vi.fn(async () => {}),
      openUrl: vi.fn(),
      print: vi.fn(),
      notify: vi.fn(),
      ...overrides,
    }
    return { deps, menu: useWindowMenu(deps) }
  }

  it('reads the zoom from the shell and changes it there', async () => {
    const { deps, menu } = setup()
    expect(menu.zoom.value).toBe(100)

    await menu.refresh()
    expect(menu.zoom.value).toBe(110)
    await menu.zoomIn()
    expect(menu.zoom.value).toBe(125)
    await menu.zoomOut()
    expect(menu.zoom.value).toBe(90)
    await menu.resetZoom()
    expect(menu.zoom.value).toBe(100)
    expect(deps.view.zoom.mock.calls.map((c) => c[0])).toEqual(['in', 'out', 'reset'])

    await menu.toggleFullScreen()
    expect(menu.fullScreen.value).toBe(true)
  })

  it('does nothing to its state with no shell to ask', async () => {
    const { menu } = setup({ view: undefined, updates: undefined })
    await menu.refresh()
    await menu.zoomIn()
    await menu.zoomOut()
    await menu.resetZoom()
    await menu.toggleFullScreen()
    expect(menu.checkForUpdates()).toBeUndefined()
    expect(menu.zoom.value).toBe(100)
    expect(menu.fullScreen.value).toBe(false)
  })

  it('copies the page\'s web address and says so', async () => {
    const { deps, menu } = setup()
    await menu.copyUrl()
    expect(deps.copyText).toHaveBeenCalledWith('https://app.agentrq.com/workspaces/W1')
    expect(deps.notify).toHaveBeenCalledWith(expect.objectContaining({ tone: 'success', message: 'https://app.agentrq.com/workspaces/W1' }))
  })

  it('opens the page in the browser', () => {
    const { deps, menu } = setup()
    menu.openInBrowser()
    expect(deps.openUrl).toHaveBeenCalledWith('https://app.agentrq.com/workspaces/W1')
  })

  it('copies and opens nothing without a server', async () => {
    const { deps, menu } = setup({ serverUrl: () => '' })
    await menu.copyUrl()
    menu.openInBrowser()
    expect(deps.copyText).not.toHaveBeenCalled()
    expect(deps.notify).not.toHaveBeenCalled()
    expect(deps.openUrl).not.toHaveBeenCalled()
  })

  it('prints, and asks the shell for an update check', () => {
    const { deps, menu } = setup()
    menu.print()
    menu.checkForUpdates()
    expect(deps.print).toHaveBeenCalled()
    expect(deps.updates.check).toHaveBeenCalled()
  })
})

describe('WindowMenu', () => {
  let app
  let el
  let view
  let updates

  beforeEach(() => {
    view = {
      get: vi.fn(async () => ({ zoom: 110, fullScreen: false })),
      zoom: vi.fn(async (d) => ({ zoom: { in: 125, out: 100, reset: 100 }[d], fullScreen: false })),
      toggleFullScreen: vi.fn(async () => ({ zoom: 110, fullScreen: true })),
    }
    updates = { check: vi.fn() }
    window.agentrq = { view, updates }
    window.open = vi.fn()
    window.print = vi.fn()
  })

  afterEach(() => {
    app?.unmount()
    el?.remove()
    app = null
    delete window.agentrq
  })

  function mount(props = {}) {
    const state = reactive({ serverUrl: 'https://app.agentrq.com', path: '/tasks/ongoing', version: '1.2.3', copyText: vi.fn(async () => {}), ...props })
    el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(WindowMenu, { ...state }) })
    app.mount(el)
    return state
  }

  const trigger = () => el.querySelector('button[aria-haspopup="menu"]')
  const panel = () => el.querySelector('[data-window-menu]')
  const item = (text) => [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === text)
  const titled = (title) => el.querySelector(`button[title="${title}"]`)
  async function openMenu() {
    trigger().click()
    await tick()
  }

  it('is clickable inside the title bar\'s drag region', () => {
    mount()
    expect(el.firstElementChild.classList.contains('app-no-drag')).toBe(true)
  })

  it('names the app version and the server, and reads the zoom on opening', async () => {
    mount()
    await openMenu()
    expect(view.get).toHaveBeenCalled()
    expect(panel().textContent).toContain('AgentRQ v1.2.3')
    expect(panel().textContent).toContain('app.agentrq.com')
    expect(el.querySelector('[data-zoom]').textContent).toBe('110%')
  })

  it('offers no page address without a server or version to name', async () => {
    mount({ serverUrl: '', version: '' })
    await openMenu()
    expect(item('Copy URL')).toBeUndefined()
    expect(item('Open in browser')).toBeUndefined()
    expect(panel().textContent).toContain('AgentRQ')
    expect(panel().textContent).not.toContain(' v')
  })

  it('copies the page\'s address, closing first', async () => {
    const state = mount()
    await openMenu()
    item('Copy URL').click()
    await tick()
    expect(panel()).toBeNull()
    expect(state.copyText).toHaveBeenCalledWith('https://app.agentrq.com/tasks/ongoing')
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ message: 'https://app.agentrq.com/tasks/ongoing' })
  })

  it('says so when the copy is refused', async () => {
    mount({ copyText: vi.fn(async () => { throw new Error('denied') }) })
    await openMenu()
    item('Copy URL').click()
    await tick()
    expect(useToasts().toasts.value.at(-1).type).toBe('error')
  })

  it('opens the page in the browser, prints and checks for updates', async () => {
    mount()
    await openMenu()
    item('Open in browser').click()
    await tick()
    expect(window.open).toHaveBeenCalledWith('https://app.agentrq.com/tasks/ongoing', '_blank', 'noopener')

    await openMenu()
    item('Print…').click()
    await tick()
    expect(window.print).toHaveBeenCalled()

    await openMenu()
    item('Check for updates…').click()
    await tick()
    expect(updates.check).toHaveBeenCalled()
  })

  it('zooms and goes full screen without closing', async () => {
    mount()
    await openMenu()
    titled('Zoom in').click()
    await tick()
    expect(el.querySelector('[data-zoom]').textContent).toBe('125%')
    titled('Zoom out').click()
    await tick()
    titled('Reset zoom').click()
    await tick()
    expect(view.zoom.mock.calls.map((c) => c[0])).toEqual(['in', 'out', 'reset'])

    titled('Full screen').click()
    await tick()
    expect(titled('Exit full screen')).not.toBeNull()
    expect(panel()).not.toBeNull()
  })

  it('closes on a click elsewhere, Escape or its own button', async () => {
    mount()
    await openMenu()
    panel().click()
    await tick()
    expect(panel()).not.toBeNull()
    document.body.click()
    await tick()
    expect(panel()).toBeNull()

    await openMenu()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'a' }))
    await tick()
    expect(panel()).not.toBeNull()
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await tick()
    expect(panel()).toBeNull()

    await openMenu()
    await openMenu()
    expect(panel()).toBeNull()
    expect(view.get).toHaveBeenCalledTimes(3)
  })
})
