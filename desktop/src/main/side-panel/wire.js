// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { PanelTarget, hardenGuest, navigationTarget, openTarget } from './guest.js'

/**
 * Attaches the side panel's rules to the main window.
 *
 * Kept out of index.js so the rules are tested against a fake window rather
 * than trusted to a file that needs a live Electron: what a guest is created
 * with, where its links go, and that it never opens a window of its own.
 *
 * @param {import('electron').BrowserWindow} win
 * @param {object} options
 * @param {() => string} options.partition  the active profile's panel partition
 * @param {string} options.preload          absolute path of the guest preload
 * @param {() => string} options.serverUrl
 * @param {(url: string) => void} options.routeLink  the main window's own link rule
 * @param {{ cancelFor: (guestId: number) => void }} [options.permissions]
 *   the permission broker, told when a page goes away so a question it left
 *   waiting is answered
 * @param {{ warn: Function }} [options.logger]
 * @param {string} [options.platform]  `process.platform`
 */
export function wireSidePanel(win, { partition, preload, serverUrl, routeLink, permissions, logger = console, platform = process.platform }) {
  win.webContents.on('will-attach-webview', (event, webPreferences, params) => {
    const hardened = hardenGuest(webPreferences, params, { partition: partition(), preload })
    if (hardened.ok) return
    event.preventDefault()
    logger.warn?.(`[side-panel] ${hardened.reason}`)
  })

  win.webContents.on('did-attach-webview', (_event, guest) => {
    const options = () => ({ serverUrl: serverUrl() })

    // A question about a page that has gone is about nothing: a new document,
    // or the panel closing, answers it with a Block. Moving within the same
    // document — a hash, a pushState — is still the page that asked.
    guest.on('did-start-navigation', (details) => {
      if (details?.isMainFrame !== false && !details?.isSameDocument) permissions?.cancelFor(guest.id)
    })
    guest.once('destroyed', () => permissions?.cancelFor(guest.id))

    // Cmd/Ctrl+F while the page has focus. The keys go to the guest, which is
    // a different web contents from the app, so the app's own find bar has to
    // be told — and the page must not see them, or its own find would open too.
    guest.on('before-input-event', (event, input) => {
      if (!isFindShortcut(input, platform)) return
      event.preventDefault()
      if (!win.isDestroyed?.()) win.webContents.send('agentrq:side-panel:find')
    })

    // Never a second window. A page that asks for one gets the panel, or the
    // browser when somebody Cmd/Ctrl-clicked.
    guest.setWindowOpenHandler((details) => {
      const target = openTarget(details, options())
      if (target === PanelTarget.Panel) guest.loadURL(details.url)
      else if (target !== PanelTarget.Blocked) routeLink(details.url)
      return { action: 'deny' }
    })

    // Electron 44 puts the URL on the event and keeps the second argument only
    // for compatibility, so the event is read first.
    guest.on('will-navigate', (event, legacyUrl) => {
      const url = event.url ?? legacyUrl
      const target = navigationTarget(url, options())
      if (target === PanelTarget.Panel) return
      event.preventDefault()
      if (target !== PanelTarget.Blocked) routeLink(url)
    })
  })
}

/**
 * Whether a key is the find shortcut: Cmd+F on macOS, Ctrl+F elsewhere, with
 * no other modifier.
 *
 * @param {import('electron').Input} input
 * @param {string} platform
 */
export function isFindShortcut(input, platform) {
  if (input?.type !== 'keyDown' || String(input.key).toLowerCase() !== 'f') return false
  if (input.shift || input.alt) return false
  return platform === 'darwin' ? Boolean(input.meta) && !input.control : Boolean(input.control) && !input.meta
}

/**
 * Route a panel session's permission requests and checks through the broker.
 *
 * Set on the panel's partition only. The app's own session never asks: it is
 * this app's code, and the requests it makes are not a stranger's.
 *
 * @param {import('electron').Session} panelSession
 * @param {ReturnType<import('./permissions.js').createPermissionBroker>} broker
 */
export function wirePanelPermissions(panelSession, broker) {
  panelSession.setPermissionRequestHandler((contents, permission, callback, details) => {
    broker.request(contents, permission, callback, details)
  })
  panelSession.setPermissionCheckHandler((contents, permission, requestingOrigin, details) =>
    broker.check(contents, permission, requestingOrigin, details),
  )
}
