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

/**
 * Has a newly activated worker take over every open page.
 *
 * A page the old worker controlled changes over without this, but one it did
 * not, such as a hard reload, does not: no `controllerchange` arrives, and its
 * "Restarting to update…" never ends.
 */
export function claimOpenPages(worker) {
  worker.addEventListener('activate', (event) => event.waitUntil(worker.clients.claim()))
}

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
 * It also makes the "Update now" offer itself, from the registration. Workbox
 * stops listening for updates after the first one it did not start, so in a
 * tab left open every later version would download and never be offered.
 *
 * Returned as is when the registration already has progress (the desktop
 * build), or when there is no service worker to hear from.
 */
export function withUpdateProgress(
  registered,
  container = globalThis.navigator?.serviceWorker,
  reload = () => globalThis.location.reload(),
) {
  if (registered.progress || !container) return registered

  const progress = ref(null)
  let watched = null

  const endDownload = () => {
    if (progress.value?.phase === 'downloading') progress.value = null
  }

  container.ready?.then((registration) => {
    const offer = () => {
      // An uncontrolled page has no older version running to replace.
      if (!registration.waiting || !container.controller) return
      endDownload()
      registered.needRefresh.value = true
    }
    const follow = (worker) => {
      if (!worker) return
      // Followed from 'installing', so any change means the install is over.
      worker.addEventListener('statechange', () => {
        if (worker.state === 'installed' && registration.waiting === worker) offer()
        else endDownload()
      })
    }
    registration.addEventListener('updatefound', () => follow(registration.installing))
    follow(registration.installing)
    offer()
  })

  container.addEventListener('message', (event) => {
    if (event.data?.type !== PRECACHE_PROGRESS) return
    // A first visit installs too, and that is not an update to announce.
    if (!container.controller) return
    // The last file's message can land after the install has ended, and would
    // put a finished bar back up for good.
    const worker = event.source
    if (worker && worker.state !== 'installing') return

    progress.value = { phase: 'downloading', percent: percentOf(event.data), version: '' }

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
      // Workbox reloads only for an offer it made, and this one may not be.
      if (reloadPage) container.addEventListener('controllerchange', () => reload(), { once: true })
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
