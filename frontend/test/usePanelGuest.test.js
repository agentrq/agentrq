// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

import {
  DEFAULT_GUEST,
  createElectronGuest,
  registerPanelGuest,
  usePanelGuest,
} from '../src/composables/usePanelGuest'

function element() {
  const el = document.createElement('div')
  Object.assign(el, {
    loadURL: vi.fn(),
    goBack: vi.fn(),
    goForward: vi.fn(),
    reload: vi.fn(),
    stop: vi.fn(),
    findInPage: vi.fn(),
    stopFindInPage: vi.fn(),
    canGoBack: () => true,
    canGoForward: () => false,
  })
  return el
}

function fire(el, type, fields = {}) {
  el.dispatchEvent(Object.assign(new Event(type), fields))
}

function mounted() {
  const el = element()
  const guest = createElectronGuest({ createElement: () => el })
  const host = document.createElement('div')
  guest.mount(host, 'https://example.com/')
  return { el, guest, host }
}

describe('the Electron guest', () => {
  it('attaches a white webview that already has its src', () => {
    const { el, host } = mounted()
    expect(host.firstChild).toBe(el)
    expect(el.getAttribute('src')).toBe('https://example.com/')
    expect(el.className).toBe('absolute inset-0 w-full h-full bg-white')
  })

  it('makes a real webview element by default', () => {
    const guest = createElectronGuest()
    const host = document.createElement('div')
    guest.mount(host, 'https://example.com/')
    expect(host.querySelector('webview')).not.toBeNull()
  })

  it('hands every command to the element', () => {
    const { el, guest } = mounted()
    guest.loadURL('https://b.test/')
    guest.goBack()
    guest.goForward()
    guest.reload()
    guest.stop()
    guest.findInPage('x', { forward: true, findNext: false })
    guest.stopFindInPage('keepSelection')
    expect(el.loadURL).toHaveBeenCalledWith('https://b.test/')
    expect(el.goBack).toHaveBeenCalledOnce()
    expect(el.goForward).toHaveBeenCalledOnce()
    expect(el.reload).toHaveBeenCalledOnce()
    expect(el.stop).toHaveBeenCalledOnce()
    expect(el.findInPage).toHaveBeenCalledWith('x', { forward: true, findNext: false })
    expect(el.stopFindInPage).toHaveBeenCalledWith('keepSelection')
    expect(guest.canGoBack()).toBe(true)
    expect(guest.canGoForward()).toBe(false)
  })

  it('focuses the element', () => {
    const { el, guest, host } = mounted()
    document.body.append(host)
    el.tabIndex = 0
    guest.focus()
    expect(document.activeElement).toBe(el)
    host.remove()
  })

  it('answers false for history before the element can say', () => {
    const el = document.createElement('div')
    const guest = createElectronGuest({ createElement: () => el })
    guest.mount(document.createElement('div'), 'https://example.com/')
    expect(guest.canGoBack()).toBe(false)
    expect(guest.canGoForward()).toBe(false)
  })

  it('does nothing before it is mounted, and after it is destroyed', () => {
    const guest = createElectronGuest({ createElement: element })
    expect(() => {
      guest.loadURL('x')
      guest.goBack()
      guest.goForward()
      guest.reload()
      guest.stop()
      guest.focus()
      guest.findInPage('x')
      guest.stopFindInPage()
      guest.destroy()
    }).not.toThrow()
    expect(guest.canGoBack()).toBe(false)
  })

  it('reports loading, navigation and matches', () => {
    const { el, guest } = mounted()
    const seen = []
    guest.on('loading', (v) => seen.push(['loading', v]))
    guest.on('navigate', (v) => seen.push(['navigate', v]))
    guest.on('found', (v) => seen.push(['found', v]))
    fire(el, 'did-start-loading')
    fire(el, 'did-stop-loading')
    fire(el, 'did-navigate', { url: 'https://a.test/' })
    fire(el, 'did-navigate-in-page', { url: 'https://a.test/#x', isMainFrame: true })
    fire(el, 'did-navigate-in-page', { url: 'https://a.test/frame', isMainFrame: false })
    fire(el, 'found-in-page', { result: { activeMatchOrdinal: 2, matches: 5 } })
    fire(el, 'found-in-page', {})
    expect(seen).toEqual([
      ['loading', true],
      ['loading', false],
      ['navigate', { url: 'https://a.test/', inPage: false }],
      ['navigate', { url: 'https://a.test/#x', inPage: true }],
      ['found', { active: 2, total: 5 }],
      ['found', { active: 0, total: 0 }],
    ])
  })

  it('reports a failed load, but not an aborted one or a subframe\'s', () => {
    const { el, guest } = mounted()
    const fail = vi.fn()
    guest.on('fail', fail)
    fire(el, 'did-fail-load', { errorCode: -3, errorDescription: 'ERR_ABORTED', isMainFrame: true })
    fire(el, 'did-fail-load', { errorCode: -105, errorDescription: 'ERR_NAME_NOT_RESOLVED', isMainFrame: false })
    expect(fail).not.toHaveBeenCalled()
    fire(el, 'did-fail-load', { errorCode: -105, errorDescription: 'ERR_NAME_NOT_RESOLVED', isMainFrame: true })
    fire(el, 'did-fail-load', { errorCode: -2 })
    expect(fail.mock.calls).toEqual([
      [{ code: -105, description: 'ERR_NAME_NOT_RESOLVED' }],
      [{ code: -2, description: undefined }],
    ])
  })

  it('tells every listener of an event', () => {
    const { el, guest } = mounted()
    const a = vi.fn()
    const b = vi.fn()
    guest.on('loading', a)
    guest.on('loading', b)
    fire(el, 'did-start-loading')
    expect([a.mock.calls.length, b.mock.calls.length]).toEqual([1, 1])
  })

  it('carries on when nobody is listening', () => {
    const { el } = mounted()
    expect(() => fire(el, 'did-start-loading')).not.toThrow()
  })

  it('stops telling a listener that unsubscribed', () => {
    const { el, guest } = mounted()
    const loading = vi.fn()
    const stop = guest.on('loading', loading)
    stop()
    fire(el, 'did-start-loading')
    expect(loading).not.toHaveBeenCalled()
  })

  it('leaves hiding and dragging to the host, which is part of the page', () => {
    const { el, guest } = mounted()
    guest.setVisible(false)
    guest.setDragging(true)
    expect(el.getAttribute('style')).toBeNull()
  })

  it('removes the element when destroyed', () => {
    const { el, guest, host } = mounted()
    guest.destroy()
    expect(host.contains(el)).toBe(false)
  })
})

describe('choosing the guest', () => {
  it('is Electron\'s when the shell names none', () => {
    expect(DEFAULT_GUEST).toBe('electron')
    expect(typeof usePanelGuest({ bridge: {}, options: { createElement: element } }).mount).toBe('function')
    expect(typeof usePanelGuest({ bridge: undefined, options: { createElement: element } }).mount).toBe('function')
  })

  it('reads the bridge of the window by default', () => {
    window.agentrq = { sidePanel: { guest: 'electron' } }
    expect(typeof usePanelGuest().mount).toBe('function')
    delete window.agentrq
  })

  it('is whichever the shell declares', () => {
    const native = { mount: vi.fn() }
    const factory = vi.fn(() => native)
    registerPanelGuest('native', factory)
    expect(usePanelGuest({ bridge: { guest: 'native' }, options: { a: 1 } })).toBe(native)
    expect(factory).toHaveBeenCalledWith({ a: 1 })
  })

  it('refuses a guest nobody registered rather than guessing', () => {
    expect(() => usePanelGuest({ bridge: { guest: 'nope' } })).toThrow('Unknown side panel guest: nope')
  })
})
