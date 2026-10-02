// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { EventEmitter } from 'node:events'
import { describe, expect, it, vi } from 'vitest'

import { wireSidePanel } from '../../src/main/side-panel/wire.js'

const SERVER_URL = 'https://app.agentrq.com'

function fakeGuest() {
  const guest = new EventEmitter()
  guest.loadURL = vi.fn()
  guest.setWindowOpenHandler = vi.fn((handler) => { guest.openHandler = handler })
  return guest
}

function setup() {
  const win = { webContents: new EventEmitter() }
  const routeLink = vi.fn()
  const logger = { warn: vi.fn() }
  wireSidePanel(win, {
    partition: () => 'persist:panel-p1',
    preload: '/app/panel.cjs',
    serverUrl: () => SERVER_URL,
    routeLink,
    logger,
  })
  const guest = fakeGuest()
  win.webContents.emit('did-attach-webview', {}, guest)
  return { win, guest, routeLink, logger }
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
})
