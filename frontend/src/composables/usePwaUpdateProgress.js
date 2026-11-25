// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { ref } from 'vue'

/** The message a new service worker posts to open pages as it caches itself. */
export const PRECACHE_PROGRESS = 'PRECACHE_PROGRESS'

/**
 * A precache plugin that reports each file of a new version as it is cached.
 *
 * Workbox caches the manifest one entry at a time during install, so the count
 * of finished entries over the manifest's length is the download's progress.
 * A file count, not bytes: the bundle's sizes are not known until each arrives.
 * `handlerDidComplete` also fires for ordinary fetches, hence the install check.
 */
export function precacheProgressPlugin({ total, report }) {
  let done = 0
  return {
    handlerDidComplete: async ({ event }) => {
      if (event?.type !== 'install') return
      done += 1
      await report({ type: PRECACHE_PROGRESS, done, total })
    },
  }
}

/**
 * How many files an install caches: one per URL. The build lists a file twice
 * when it is both an included asset and matched by the glob, and Workbox
 * caches it once, so counting entries would stop the bar short of 100.
 */
export function precacheTotal(manifest) {
  return new Set(manifest.map((entry) => (typeof entry === 'string' ? entry : entry.url))).size
}

/** How long the bar waits for the next file before taking itself down. */
export const PROGRESS_STALL_MS = 60_000

/** A 0–100 percentage from a progress message, or null when there is no total. */
export function percentOf({ done, total }) {
  if (!total) return null
  return Math.min(100, (done / total) * 100)
}

/**
 * The web build's side of the desktop's update progress.
 *
 * vite-plugin-pwa offers a waiting worker but says nothing while it is still
 * downloading, so this adds the `progress` ref the desktop stand-in has, fed
 * from the worker's messages, and the same shape App.vue's banner reads:
 * `{ phase, percent, version }`.
 *
 * Returned as is when the registration already has progress (the desktop
 * build), or when there is no service worker to hear from.
 */
export function withUpdateProgress(registered, container = globalThis.navigator?.serviceWorker) {
  if (registered.progress || !container) return registered

  const progress = ref(null)
  let watched = null
  let stall = null

  const endDownload = () => {
    clearTimeout(stall)
    if (progress.value?.phase === 'downloading') progress.value = null
  }

  container.addEventListener('message', (event) => {
    if (event.data?.type !== PRECACHE_PROGRESS) return
    // A first visit installs too, and that is not an update to announce.
    if (!container.controller) return
    // The last file's message can land after the install has ended, and would
    // put a finished bar back up for good.
    const worker = event.source
    if (worker && worker.state !== 'installing') return

    // Every file is cached: the download is over whatever the worker's state
    // says, and a state change this page never sees must not hold the bar up.
    if (event.data.total && event.data.done >= event.data.total) {
      endDownload()
      return
    }

    progress.value = { phase: 'downloading', percent: percentOf(event.data), version: '' }
    // A download that stops reporting has ended one way or another.
    clearTimeout(stall)
    stall = setTimeout(endDownload, PROGRESS_STALL_MS)

    // The worker leaves 'installing' whether it succeeded or failed: either
    // way the bar is done, and on success the "Update now" offer takes over.
    if (worker && worker !== watched) {
      watched = worker
      worker.addEventListener('statechange', () => {
        if (worker.state !== 'installing') endDownload()
      })
    }
  })

  return {
    ...registered,
    progress,
    dismissProgress: () => {
      progress.value = null
    },
    updateServiceWorker: async (reloadPage) => {
      // Up until the page reloads under the new version.
      progress.value = { phase: 'restarting', percent: null, version: '' }
      try {
        await registered.updateServiceWorker(reloadPage)
      } catch {
        // No reload is coming, so put the offer back rather than a bar that
        // never finishes.
        progress.value = null
        registered.needRefresh.value = true
      }
    },
  }
}
