// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { LinkTarget, classifyLink } from '../links.js'

/**
 * The side panel's guest: the page shown in the `<webview>` beside the app.
 *
 * The element lives in the renderer, which is the app's own code on a
 * privileged origin — but what it asks for is not what it gets. Every guest's
 * preferences are decided here, in `will-attach-webview`, and whatever the
 * element carried is overwritten. That is the boundary: a page in the panel has
 * no Node, no bridge to the app, and a cookie jar that is never the profile's,
 * so it can never be sent with the `at` cookie.
 */

/** The scheme an Extension's own pages are served from. */
export const EXT_SCHEME = 'agentrq-ext'

/** What a guest may be created on. `about:blank` is the empty panel. */
const ATTACHABLE = new Set(['http:', 'https:', `${EXT_SCHEME}:`, 'about:'])

/**
 * The guest's cookie jar for a profile.
 *
 * One per profile, so switching profile does not carry one account's browsing
 * into the other's — and never the profile's own `persist:profile-<id>`, which
 * holds the credential.
 */
export function panelPartitionFor(profileId) {
  return `persist:panel-${profileId}`
}

function protocolOf(rawUrl) {
  try {
    return new URL(String(rawUrl ?? '')).protocol
  } catch {
    return ''
  }
}

/**
 * Overwrite a guest's preferences, or refuse it.
 *
 * Mutates `webPreferences` and `params` in place, because that is how Electron
 * hands them over: the objects passed to `will-attach-webview` are the ones the
 * guest is created with.
 *
 * @param {Record<string, unknown>} webPreferences
 * @param {Record<string, string>} params  the element's attributes, `src` among them
 * @param {{ partition: string, preload: string }} options
 * @returns {{ ok: true } | { ok: false, reason: string }}
 */
export function hardenGuest(webPreferences, params, { partition, preload }) {
  const src = params.src ?? ''
  if (src && !ATTACHABLE.has(protocolOf(src))) {
    return { ok: false, reason: `The side panel does not open ${protocolOf(src) || 'that address'}` }
  }
  if (src.startsWith('about:') && src !== 'about:blank') {
    return { ok: false, reason: 'The side panel does not open about: pages' }
  }

  delete webPreferences.nodeIntegration
  delete webPreferences.nodeIntegrationInSubFrames
  delete webPreferences.nodeIntegrationInWorker
  webPreferences.contextIsolation = true
  webPreferences.sandbox = true
  webPreferences.webSecurity = true
  webPreferences.partition = partition
  // Always ours, whatever the element named. The preload decides for itself
  // whether the page it finds is one that gets a bridge.
  webPreferences.preload = preload

  // Counter-intuitive, and the reason popups are not left disabled: a guest
  // that disables them has `window.open` and `target=_blank` dropped before
  // the window-open handler is ever asked, so the link silently does nothing.
  // Enabled, every request reaches the handler in wire.js — which refuses all
  // of them, so no window is ever created, and loads the page in the panel.
  webPreferences.disablePopups = false
  // Blink features an element can switch on with an attribute.
  webPreferences.enableBlinkFeatures = ''
  webPreferences.experimentalFeatures = false
  webPreferences.allowRunningInsecureContent = false

  params.partition = partition
  delete params.allowpopups
  delete params.nodeintegration
  delete params.preload
  return { ok: true }
}

/** Where a navigation inside the panel goes. */
export const PanelTarget = {
  /** Stays in the panel. */
  Panel: 'panel',
  /** AgentRQ's own sign-in, which has to run on the profile's session. */
  SignIn: 'signin',
  /** Handed to the operating system: the browser, the mail client. */
  System: 'system',
  /** Refused. */
  Blocked: 'blocked',
}

/**
 * Where a guest navigating to `rawUrl` should end up.
 *
 * The web and an Extension's pages stay in the panel. AgentRQ's sign-in does
 * not: the panel's cookie jar is not the profile's, so signing in there would
 * succeed and leave the app signed out. Anything else is the existing link
 * rule, which is what keeps `file:` and `javascript:` out.
 */
export function navigationTarget(rawUrl, { serverUrl = '' } = {}) {
  const protocol = protocolOf(rawUrl)
  if (protocol === `${EXT_SCHEME}:` || rawUrl === 'about:blank') return PanelTarget.Panel

  switch (classifyLink(rawUrl, { serverUrl })) {
    case LinkTarget.System:
      return protocol === 'http:' || protocol === 'https:' ? PanelTarget.Panel : PanelTarget.System
    case LinkTarget.SignIn:
      return PanelTarget.SignIn
    default:
      return PanelTarget.Blocked
  }
}

/**
 * Where a guest's `window.open` or `target=_blank` should end up.
 *
 * A guest never gets a second window. Chromium reports a Cmd/Ctrl- or
 * middle-click as `background-tab`, which is somebody asking for the page
 * *somewhere else* — their browser. Every other disposition opens in the panel.
 *
 * @param {{ url: string, disposition?: string }} details
 */
export function openTarget({ url, disposition = '' }, options = {}) {
  const target = navigationTarget(url, options)
  if (target === PanelTarget.Panel && disposition === 'background-tab') {
    const protocol = protocolOf(url)
    return protocol === 'http:' || protocol === 'https:' ? PanelTarget.System : PanelTarget.Panel
  }
  return target
}
