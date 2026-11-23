// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A tap on a notification goes to its page in the app that is already open.
 *
 * Opening a window whenever no open one already showed that exact URL gave a
 * second copy of the app, and on an iPhone nothing useful at all. The worker
 * hands the URL to an open window and the router moves there, with no reload.
 * Only with no window open does it open one.
 */

export const OPEN_NOTIFICATION_URL = 'OPEN_NOTIFICATION_URL'

/** The worker's side: `clients` is the worker's `self.clients`. */
export async function openNotificationUrl(clients, url) {
  const windows = await clients.matchAll({ type: 'window', includeUncontrolled: true })
  const client = windows.find((w) => w.url.endsWith(url)) || windows[0]
  if (!client) return clients.openWindow(url)
  client.postMessage({ type: OPEN_NOTIFICATION_URL, url })
  return client.focus()
}

/**
 * The route for a URL under the base path: the worker's URLs carry it, so the
 * page can open them itself, and the router adds it again.
 */
export function routeForUrl(url, base = '') {
  if (!base) return url
  if (url === base) return '/'
  return url.startsWith(`${base}/`) ? url.slice(base.length) : url
}

/** The page's side: follow the worker's message with the router. */
export function followNotificationClicks(router, container = globalThis.navigator?.serviceWorker) {
  if (!container) return
  container.addEventListener('message', (event) => {
    if (event.data?.type !== OPEN_NOTIFICATION_URL) return
    router.push(routeForUrl(event.data.url, router.options.history.base))
  })
}
