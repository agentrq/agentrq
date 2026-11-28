// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A tab left open across a deploy still runs the old build, whose lazy pages
 * are files the server no longer has, and the new service worker has dropped
 * them from its cache. Opening such a page is a full load of where it was
 * going, once per address: a second failure there is a real one and is left
 * to surface.
 */

const KEY = 'agentrq:staleChunkReload'

// Each browser's words for a lazy module or its CSS that would not load.
const STALE_CHUNK = /Failed to fetch dynamically imported module|error loading dynamically imported module|Importing a module script failed|Unable to preload CSS/i

export function isStaleChunkError(err) {
  return STALE_CHUNK.test(String(err?.message ?? err ?? ''))
}

function read(storage) {
  try { return storage.getItem(KEY) } catch { return null }
}

function write(storage, value) {
  try {
    if (value === null) storage.removeItem(KEY)
    else storage.setItem(KEY, value)
  } catch { /* no storage: still reload, just without the loop guard */ }
}

/**
 * Loads `href` from the server, unless this tab already did so for it.
 * Returns whether it did.
 */
export function reloadOnce(href, { storage = window.sessionStorage, location = window.location } = {}) {
  if (read(storage) === href) return false
  write(storage, href)
  location.assign(href)
  return true
}

export function reloadOnStaleChunk(router, { storage = window.sessionStorage, location = window.location, target = window } = {}) {
  router.onError((err, to) => {
    if (isStaleChunkError(err) && to) reloadOnce(router.resolve(to).href, { storage, location })
  })
  // Vite's own preload of a lazy page's imports and CSS.
  target.addEventListener('vite:preloadError', (event) => {
    if (reloadOnce(location.href, { storage, location })) event.preventDefault()
  })
  // A page that loaded clears the guard, so the next deploy is handled too.
  router.afterEach((to, from, failure) => {
    if (!failure) write(storage, null)
  })
}
