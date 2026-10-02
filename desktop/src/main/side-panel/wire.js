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
 * @param {{ warn: Function }} [options.logger]
 */
export function wireSidePanel(win, { partition, preload, serverUrl, routeLink, logger = console }) {
  win.webContents.on('will-attach-webview', (event, webPreferences, params) => {
    const hardened = hardenGuest(webPreferences, params, { partition: partition(), preload })
    if (hardened.ok) return
    event.preventDefault()
    logger.warn?.(`[side-panel] ${hardened.reason}`)
  })

  win.webContents.on('did-attach-webview', (_event, guest) => {
    const options = () => ({ serverUrl: serverUrl() })

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
