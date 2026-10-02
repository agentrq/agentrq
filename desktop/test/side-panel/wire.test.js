// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { EventEmitter } from 'node:events'
import { describe, expect, it, vi } from 'vitest'

import { wirePanelPermissions, wireSidePanel } from '../../src/main/side-panel/wire.js'

const SERVER_URL = 'https://app.agentrq.com'

function fakeGuest() {
  const guest = new EventEmitter()
  guest.id = 42
  guest.loadURL = vi.fn()
  guest.setWindowOpenHandler = vi.fn((handler) => { guest.openHandler = handler })
  return guest
}

function setup() {
  const win = { webContents: new EventEmitter() }
  const routeLink = vi.fn()
  const logger = { warn: vi.fn() }
  const permissions = { cancelFor: vi.fn() }
  wireSidePanel(win, {
    permissions,
    partition: () => 'persist:panel-p1',
    preload: '/app/panel.cjs',
    serverUrl: () => SERVER_URL,
    routeLink,
    logger,
  })
  const guest = fakeGuest()
  win.webContents.emit('did-attach-webview', {}, guest)
  return { win, guest, routeLink, logger, permissions }
}

const navigation = (url) => ({ url, preventDefault: vi.fn() })

describe('wireSidePanel', () => {
  it('hardens a guest before it is attached', () => {
    const { win } = setup()
    const event = { preventDefault: vi.fn() }
    const webPreferences = { nodeIntegration: true }
    win.webContents.emit('will-attach-webview', event, webPreferences, { src: 'https://example.com' })

    expect(event.preventDefault).not.toHaveBeenCalled()
    expect(webPreferences).toMatchObject({ partition: 'persist:panel-p1', preload: '/app/panel.cjs', sandbox: true })
    expect(webPreferences.nodeIntegration).toBeUndefined()
  })

  it('refuses a guest on a scheme the panel does not open, and says why', () => {
    const { win, logger } = setup()
    const event = { preventDefault: vi.fn() }
    win.webContents.emit('will-attach-webview', event, {}, { src: 'file:///etc/passwd' })

    expect(event.preventDefault).toHaveBeenCalled()
    expect(logger.warn).toHaveBeenCalledWith('[side-panel] The side panel does not open file:')
  })

  it('opens a page that asks for a new window in the panel itself', () => {
    const { guest, routeLink } = setup()
    expect(guest.openHandler({ url: 'https://example.com/a', disposition: 'foreground-tab' })).toEqual({ action: 'deny' })
    expect(guest.loadURL).toHaveBeenCalledWith('https://example.com/a')
    expect(routeLink).not.toHaveBeenCalled()
  })

  it('sends a Cmd/Ctrl-clicked link to the browser', () => {
    const { guest, routeLink } = setup()
    expect(guest.openHandler({ url: 'https://example.com/a', disposition: 'background-tab' })).toEqual({ action: 'deny' })
    expect(routeLink).toHaveBeenCalledWith('https://example.com/a')
    expect(guest.loadURL).not.toHaveBeenCalled()
  })

  it('opens nothing at all for a refused address', () => {
    const { guest, routeLink } = setup()
    expect(guest.openHandler({ url: 'file:///etc/passwd', disposition: 'foreground-tab' })).toEqual({ action: 'deny' })
    expect(routeLink).not.toHaveBeenCalled()
    expect(guest.loadURL).not.toHaveBeenCalled()
  })

  it('lets the panel navigate on the web', () => {
    const { guest, routeLink } = setup()
    const event = navigation('https://example.com/next')
    guest.emit('will-navigate', event)
    expect(event.preventDefault).not.toHaveBeenCalled()
    expect(routeLink).not.toHaveBeenCalled()
  })

  it('takes AgentRQ sign-in out of the panel and gives it to the main window', () => {
    const { guest, routeLink } = setup()
    const event = navigation(`${SERVER_URL}/api/v1/auth/github/login`)
    guest.emit('will-navigate', event)
    expect(event.preventDefault).toHaveBeenCalled()
    expect(routeLink).toHaveBeenCalledWith(`${SERVER_URL}/api/v1/auth/github/login`)
  })

  it('stops a navigation to a refused address without routing it', () => {
    const { guest, routeLink } = setup()
    const event = { preventDefault: vi.fn() }
    // An older event shape, with the URL only as the second argument.
    guest.emit('will-navigate', event, 'javascript:alert(1)')
    expect(event.preventDefault).toHaveBeenCalled()
    expect(routeLink).not.toHaveBeenCalled()
  })

  it('logs to the console when no logger is given', () => {
    const win = { webContents: new EventEmitter() }
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    wireSidePanel(win, { partition: () => 'p', preload: '/p.cjs', serverUrl: () => '', routeLink: vi.fn() })
    win.webContents.emit('will-attach-webview', { preventDefault: vi.fn() }, {}, { src: 'file:///x' })
    expect(warn).toHaveBeenCalled()
    warn.mockRestore()
  })

  it('answers a question a page left waiting when it moves to a new document', () => {
    const { guest, permissions } = setup()
    guest.emit('did-start-navigation', { isMainFrame: true, isSameDocument: true })
    guest.emit('did-start-navigation', { isMainFrame: false, isSameDocument: false })
    expect(permissions.cancelFor).not.toHaveBeenCalled()

    guest.emit('did-start-navigation', { isMainFrame: true, isSameDocument: false })
    expect(permissions.cancelFor).toHaveBeenCalledWith(42)
    // An older event shape with nothing on it is a new document too.
    guest.emit('did-start-navigation')
    expect(permissions.cancelFor).toHaveBeenCalledTimes(2)
  })

  it('answers it when the panel closes and the page goes away', () => {
    const { guest, permissions } = setup()
    guest.emit('destroyed')
    expect(permissions.cancelFor).toHaveBeenCalledWith(42)
  })

  it('carries on without a broker', () => {
    const win = { webContents: new EventEmitter() }
    wireSidePanel(win, { partition: () => 'p', preload: '/p.cjs', serverUrl: () => '', routeLink: vi.fn() })
    const guest = fakeGuest()
    win.webContents.emit('did-attach-webview', {}, guest)
    expect(() => {
      guest.emit('did-start-navigation', { isMainFrame: true, isSameDocument: false })
      guest.emit('destroyed')
    }).not.toThrow()
  })
})

describe('wirePanelPermissions', () => {
  it('sends the panel session’s requests and checks to the broker', () => {
    const handlers = {}
    const session = {
      setPermissionRequestHandler: (fn) => { handlers.request = fn },
      setPermissionCheckHandler: (fn) => { handlers.check = fn },
    }
    const broker = { request: vi.fn(), check: vi.fn(() => true) }
    wirePanelPermissions(session, broker)

    const callback = vi.fn()
    handlers.request('guest', 'media', callback, { mediaTypes: ['video'] })
    expect(broker.request).toHaveBeenCalledWith('guest', 'media', callback, { mediaTypes: ['video'] })
    expect(handlers.check('guest', 'notifications', 'https://a.example', {})).toBe(true)
    expect(broker.check).toHaveBeenCalledWith('guest', 'notifications', 'https://a.example', {})
  })
})

