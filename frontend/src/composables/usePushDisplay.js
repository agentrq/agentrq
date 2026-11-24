// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Whether a push becomes a notification.
 *
 * Not when its task is already in front of the person: the message is on the
 * screen they are reading. Except on Safari's engine (iPhone, iPad, Safari on
 * the Mac), which counts a push that shows nothing as silent and cancels the
 * subscription after a few, ending every notification after it.
 */

/** Safari's engine: WebKit, and not one of the browsers built on Chromium. */
export function requiresNotification(userAgent = '') {
  return /AppleWebKit/.test(userAgent) && !/Chrome|Chromium/.test(userAgent)
}

/** Whether a focused, visible window shows the page at `url`, a path. */
export async function urlOnScreen(clients, url) {
  const windows = await clients.matchAll({ type: 'window', includeUncontrolled: true })
  return windows.some((w) => w.focused && w.visibilityState === 'visible' && new URL(w.url).pathname === url)
}

/** The worker's side: `clients` is `self.clients`, `url` the push's path. */
export async function shouldShowPush(clients, url, userAgent) {
  if (!url || requiresNotification(userAgent)) return true
  return !(await urlOnScreen(clients, url))
}
