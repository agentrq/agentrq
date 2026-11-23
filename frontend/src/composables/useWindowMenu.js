// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref } from 'vue'
import { copyLinkTarget } from './useMarkdownLinks'

/**
 * The web address of a page of this app, on the server the window is
 * connected to.
 *
 * Joined as text rather than resolved with `new URL(path, server)`, which
 * would drop a server's own base path (`https://host/agentrq`).
 *
 * @param {string} serverUrl
 * @param {string} path the router's full path, query included
 * @returns {string} '' when there is no server to point at
 */
export function pageWebUrl(serverUrl, path) {
  const server = typeof serverUrl === 'string' ? serverUrl.trim().replace(/\/+$/, '') : ''
  if (!server) return ''
  const page = typeof path === 'string' && path ? path : '/'
  return server + (page.startsWith('/') ? page : `/${page}`)
}

/**
 * How the menu names the server: its host, or the address as given when it
 * does not parse.
 *
 * @param {string} serverUrl
 * @returns {string}
 */
export function serverHost(serverUrl) {
  try {
    return new URL(serverUrl).host
  } catch {
    return typeof serverUrl === 'string' ? serverUrl : ''
  }
}

/**
 * What the macOS title bar's window menu does.
 *
 * Kept out of the component so every action is testable without a shell:
 * the bridge, the clipboard and the browser are all passed in.
 *
 * @param {object} deps
 * @param {() => string} deps.serverUrl
 * @param {() => string} deps.path
 * @param {object} [deps.view]    `window.agentrq.view`
 * @param {object} [deps.updates] `window.agentrq.updates`
 * @param {(text: string) => Promise<void>} deps.copyText
 * @param {(url: string) => void} deps.openUrl  handed to the shell, which opens it in the browser
 * @param {() => void} deps.print
 * @param {(result: {tone: string, title: string, message: string}) => void} deps.notify
 */
export function useWindowMenu({ serverUrl, path, view, updates, copyText, openUrl, print, notify }) {
  const zoom = ref(100)
  const fullScreen = ref(false)

  const apply = (state) => {
    if (!state) return
    zoom.value = state.zoom
    fullScreen.value = state.fullScreen
  }

  /** Zoom can change from the keyboard too, so it is read each time the menu opens. */
  async function refresh() {
    apply(await view?.get?.())
  }

  const zoomIn = async () => apply(await view?.zoom?.('in'))
  const zoomOut = async () => apply(await view?.zoom?.('out'))
  const resetZoom = async () => apply(await view?.zoom?.('reset'))
  const toggleFullScreen = async () => apply(await view?.toggleFullScreen?.())

  async function copyUrl() {
    const url = pageWebUrl(serverUrl(), path())
    if (url) notify(await copyLinkTarget(url, { copyText }))
  }

  function openInBrowser() {
    const url = pageWebUrl(serverUrl(), path())
    if (url) openUrl(url)
  }

  // A manual check announces its own result in the update banner.
  const checkForUpdates = () => updates?.check?.()

  return { zoom, fullScreen, refresh, zoomIn, zoomOut, resetZoom, toggleFullScreen, copyUrl, openInBrowser, print, checkForUpdates }
}
