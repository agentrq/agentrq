// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'

import {
  PRECACHE_PROGRESS,
  percentOf,
  precacheProgressPlugin,
  precacheTotal,
  withUpdateProgress,
} from '../src/composables/usePwaUpdateProgress'
import { progressLabel } from '../src/desktop/useDesktopUpdates'

/** A service worker container that can be told to deliver a message. */
function fakeContainer({ controller = {} } = {}) {
  const target = new EventTarget()
  target.controller = controller
  target.deliver = (data, source) => {
    const event = new Event('message')
    event.data = data
    event.source = source
    target.dispatchEvent(event)
  }
  return target
}

/** A service worker whose state can be moved on. */
function fakeWorker(state = 'installing') {
  const worker = new EventTarget()
  worker.state = state
  worker.moveTo = (next) => {
    worker.state = next
    worker.dispatchEvent(new Event('statechange'))
  }
  return worker
}

function registered(overrides = {}) {
  return { needRefresh: ref(false), offlineReady: ref(false), updateServiceWorker: vi.fn(async () => {}), ...overrides }
}

describe('precacheProgressPlugin', () => {
  it('counts each file cached during install', async () => {
    const report = vi.fn()
    const plugin = precacheProgressPlugin({ total: 3, report })

    await plugin.handlerDidComplete({ event: { type: 'install' } })
    await plugin.handlerDidComplete({ event: { type: 'install' } })

    expect(report.mock.calls.map(([m]) => m)).toEqual([
      { type: PRECACHE_PROGRESS, done: 1, total: 3 },
      { type: PRECACHE_PROGRESS, done: 2, total: 3 },
    ])
  })

  it('ignores the ordinary fetches the same strategy answers', async () => {
    const report = vi.fn()
    const plugin = precacheProgressPlugin({ total: 3, report })

    await plugin.handlerDidComplete({ event: { type: 'fetch' } })
    await plugin.handlerDidComplete({})

    expect(report).not.toHaveBeenCalled()
  })
})

describe('precacheTotal', () => {
  it('counts a file listed twice once, as Workbox caches it', () => {
    const manifest = [
      { url: 'favicon.svg', revision: 'a' },
      { url: 'index.html', revision: 'b' },
      { url: 'favicon.svg', revision: 'a' },
      'assets/app-123.js',
    ]
    expect(precacheTotal(manifest)).toBe(3)
  })
})

describe('percentOf', () => {
  it('is the share of files done', () => {
    expect(percentOf({ done: 1, total: 4 })).toBe(25)
    expect(percentOf({ done: 4, total: 4 })).toBe(100)
  })

  it('never passes 100', () => {
    expect(percentOf({ done: 5, total: 4 })).toBe(100)
  })

  it('is unknown with no total', () => {
    expect(percentOf({ done: 1, total: 0 })).toBeNull()
  })
})

describe('withUpdateProgress', () => {
  it('leaves a registration that has progress alone, as the desktop one does', () => {
    const desktop = registered({ progress: ref(null) })
    expect(withUpdateProgress(desktop, fakeContainer())).toBe(desktop)
  })

  it('leaves it alone with no service worker to hear from', () => {
    const plain = registered()
    expect(withUpdateProgress(plain, undefined)).toBe(plain)
  })

  it('reads the service worker by default', () => {
    // jsdom has none, which is the case above by the other road.
    const plain = registered()
    expect(withUpdateProgress(plain)).toBe(plain)
  })

  it('keeps what App.vue destructures', () => {
    const base = registered()
    const sw = withUpdateProgress(base, fakeContainer())
    expect(sw.needRefresh).toBe(base.needRefresh)
    expect(sw.progress.value).toBeNull()
  })

  it('shows the download as it goes, in the words the desktop uses', () => {
    const container = fakeContainer()
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 3, total: 10 }, fakeWorker())

    expect(progress.value).toEqual({ phase: 'downloading', percent: 30, version: '' })
    expect(progressLabel(progress.value)).toBe('Downloading the update…')
  })

  it('says nothing on a first visit, which installs but is no update', () => {
    const container = fakeContainer({ controller: null })
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 3, total: 10 }, fakeWorker())

    expect(progress.value).toBeNull()
  })

  it('ignores the other messages workers send', () => {
    const container = fakeContainer()
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: 'SOMETHING_ELSE' }, fakeWorker())
    container.deliver(null, fakeWorker())

    expect(progress.value).toBeNull()
  })

  it('takes the bar down when the install ends, for the offer to take over', () => {
    const container = fakeContainer()
    const worker = fakeWorker()
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 9, total: 10 }, worker)
    worker.moveTo('installing')
    expect(progress.value?.phase).toBe('downloading')

    worker.moveTo('installed')
    expect(progress.value).toBeNull()
  })

  it('takes it down when the install fails too', () => {
    const container = fakeContainer()
    const worker = fakeWorker()
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 2, total: 10 }, worker)
    worker.moveTo('redundant')

    expect(progress.value).toBeNull()
  })

  it('ignores a message that lands after the install ended', () => {
    // Messages and state changes arrive separately, and the last file's can
    // come second — which would leave a bar at 100% up for good.
    const container = fakeContainer()
    const worker = fakeWorker()
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 78, total: 79 }, worker)
    worker.moveTo('installed')
    container.deliver({ type: PRECACHE_PROGRESS, done: 79, total: 79 }, worker)

    expect(progress.value).toBeNull()
  })

  it('watches each worker once however many files it reports', () => {
    const container = fakeContainer()
    const worker = fakeWorker()
    const listen = vi.spyOn(worker, 'addEventListener')
    withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 1, total: 10 }, worker)
    container.deliver({ type: PRECACHE_PROGRESS, done: 2, total: 10 }, worker)

    expect(listen).toHaveBeenCalledTimes(1)
  })

  it('shows progress even when the message names no worker', () => {
    const container = fakeContainer()
    const { progress } = withUpdateProgress(registered(), container)

    container.deliver({ type: PRECACHE_PROGRESS, done: 1, total: 2 }, null)

    expect(progress.value.percent).toBe(50)
  })

  it('shows the restart once "Update now" is clicked, and the install ending does not hide it', async () => {
    const container = fakeContainer()
    const worker = fakeWorker()
    const base = registered()
    const sw = withUpdateProgress(base, container)
    container.deliver({ type: PRECACHE_PROGRESS, done: 1, total: 2 }, worker)

    await sw.updateServiceWorker(true)
    worker.moveTo('activated')

    expect(base.updateServiceWorker).toHaveBeenCalledWith(true)
    expect(sw.progress.value).toEqual({ phase: 'restarting', percent: null, version: '' })
    expect(progressLabel(sw.progress.value)).toBe('Restarting to update…')
  })

  it('puts the offer back when the update could not start', async () => {
    const base = registered({ updateServiceWorker: vi.fn(async () => { throw new Error('service worker unregistered') }) })
    const sw = withUpdateProgress(base, fakeContainer())

    await sw.updateServiceWorker(true)

    expect(sw.progress.value).toBeNull()
    expect(base.needRefresh.value).toBe(true)
  })

  it('can be dismissed', () => {
    const container = fakeContainer()
    const sw = withUpdateProgress(registered(), container)
    container.deliver({ type: PRECACHE_PROGRESS, done: 1, total: 2 }, fakeWorker())

    sw.dismissProgress()

    expect(sw.progress.value).toBeNull()
  })
})
