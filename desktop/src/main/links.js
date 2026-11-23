// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Where a link should open.
 *
 * A link out of the app goes to the user's real browser. That is where their
 * extensions, their bookmarks, their other tabs and their existing sessions are,
 * and a second Chromium with none of them is not a browser anyone asked for.
 *
 * The one exception is signing in to AgentRQ, and it is not a preference: the
 * `at` cookie has to land in *this* profile's jar for the app to become
 * authenticated. Handed to the system browser, the sign-in would succeed
 * somewhere the app cannot read, and the app would still be signed out.
 *
 * So this is a classification rather than a boolean:
 *
 * - `app://` is the application; the router already handles those in place.
 * - An AgentRQ auth URL is the sign-in flow, and stays in the app.
 * - Everything else on http(s) is the web, and belongs in the browser.
 * - `mailto:`, `tel:` and their siblings address a program that is not a
 *   browser, and go to the OS the same way.
 * - `javascript:`, `data:` and `file:` are never opened from a link at all.
 *   Handing any of those to `shell.openExternal` is the well-worn way for
 *   injected markup in a message body to reach the machine it is running on.
 *
 * Kept apart from index.js because that module needs a live Electron to import,
 * and this decision is the part worth testing.
 */

import { isAuthPath } from './auth.js'

export const LinkTarget = {
  /** The app itself; let the renderer's router handle it. */
  App: 'app',
  /** Signing in to AgentRQ; run it in the app, on this profile's session. */
  SignIn: 'signin',
  /** Hand to the operating system: browser, mail client, dialler, and so on. */
  System: 'system',
  /** Refuse. */
  Blocked: 'blocked',
}

/**
 * Schemes that address something other than a browser, and that are safe to
 * pass to the OS. An allowlist, because the interesting failure is the scheme
 * nobody thought about.
 */
const SYSTEM_SCHEMES = new Set(['mailto:', 'tel:', 'sms:', 'facetime:'])

/**
 * `URL.origin` is useless here: Node returns the string "null" for any scheme
 * it does not consider special, and `app://` is not special. Protocol and host
 * are populated for every scheme, so they are what the comparison uses.
 */
function originOf(url) {
  return `${url.protocol}//${url.host}`
}

/**
 * Is this an AgentRQ sign-in on the server this app is connected to?
 *
 * The origin has to match as well as the path: `/api/v1/auth/…` on somebody
 * else's host is a stranger's page, and keeping it in the app would hand it the
 * profile's cookie jar.
 */
function isSignIn(url, serverUrl) {
  if (!serverUrl) return false
  let server
  try {
    server = new URL(serverUrl)
  } catch {
    return false
  }
  return originOf(url) === originOf(server) && isAuthPath(url.pathname)
}

/**
 * @param {string} rawUrl
 * @param {{ appOrigin?: string, serverUrl?: string }} [options]
 * @returns {typeof LinkTarget[keyof typeof LinkTarget]}
 */
export function classifyLink(rawUrl, { appOrigin = '', serverUrl = '' } = {}) {
  let url
  try {
    url = new URL(String(rawUrl ?? ''))
  } catch {
    // Not a URL at all — a relative href that reached here, or junk.
    return LinkTarget.Blocked
  }

  if (appOrigin && originOf(url) === appOrigin) return LinkTarget.App
  if (url.protocol === 'http:' || url.protocol === 'https:') {
    return isSignIn(url, serverUrl) ? LinkTarget.SignIn : LinkTarget.System
  }
  if (SYSTEM_SCHEMES.has(url.protocol)) return LinkTarget.System
  return LinkTarget.Blocked
}
