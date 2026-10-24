// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A stand-in `window.agentrq`, enough for desktop's renderer to boot.
 *
 * WP3 replaces this file with the real bridge, shaped exactly like
 * `desktop/src/preload/index.js`. Until then every call rejects and every
 * `on*` listener is a no-op, except `connection.get`, which goes to Rust so the
 * IPC path is exercised: its `todo` answer is the rejection the renderer
 * already treats as "no server yet", so the connection screen goes up.
 */
import { invoke } from '@tauri-apps/api/core'

/** Node's name for the OS, as Electron's `process.platform` gives it. */
export function platformOf(userAgent) {
  const ua = String(userAgent ?? '')
  if (/Mac/.test(ua)) return 'darwin'
  if (/Windows/.test(ua)) return 'win32'
  return 'linux'
}

const notYet = (member) => () => Promise.reject(new Error(`window.agentrq.${member} is not implemented yet`))
const noListener = () => () => {}

/** Every member of one namespace, stubbed. */
export function stubNamespace(name, members = {}) {
  return new Proxy(members, {
    get(target, key) {
      if (Object.hasOwn(target, key)) return target[key]
      // Never a thenable, or `await window.agentrq.x` would wait forever.
      if (typeof key !== 'string' || key === 'then') return undefined
      return key.startsWith('on') ? noListener : notYet(`${name}.${key}`)
    },
  })
}

export function createBridge(userAgent) {
  const known = {
    isDesktop: true,
    platform: platformOf(userAgent),
    versions: { tauri: '', webview: String(userAgent ?? '') },
    connection: stubNamespace('connection', { get: () => invoke('connection_get') }),
  }
  return new Proxy(known, {
    get(target, key) {
      if (Object.hasOwn(target, key)) return target[key]
      if (typeof key !== 'string' || key === 'then') return undefined
      return stubNamespace(key)
    },
  })
}

window.agentrq = createBridge(navigator.userAgent)
