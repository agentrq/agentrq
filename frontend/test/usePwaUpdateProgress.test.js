// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'

import {
  PRECACHE_PROGRESS,
  claimOpenPages,
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

/** A registration that can be told a new worker has been found. */
function fakeRegistration({ installing = null, waiting = null } = {}) {
  const registration = new EventTarget()
  registration.installing = installing
  registration.waiting = waiting
  registration.find = (worker) => {
    registration.installing = worker
    registration.dispatchEvent(new Event('updatefound'))
  }
  // The browser sets waiting before the worker says it is installed.
  registration.install = (worker) => {
    registration.installing = null
    registration.waiting = worker
    worker.moveTo('installed')
  }
  return registration
}

/** A container whose registration is ready, after the microtasks it takes. */
function readyContainer(registration, options) {
  const container = fakeContainer(options)
  container.ready = Promise.resolve(registration)
  return container
}

const settle = () => new Promise((resolve) => setTimeout(resolve))

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

describe('claimOpenPages', () => {
  it('takes over every open page once the worker activates', async () => {
    const worker = new EventTarget()
    worker.clients = { claim: vi.fn(() => Promise.resolve()) }
    claimOpenPages(worker)
    expect(worker.clients.claim).not.toHaveBeenCalled()

    const event = new Event('activate')
    event.waitUntil = vi.fn()
    worker.dispatchEvent(event)

    expect(worker.clients.claim).toHaveBeenCalledTimes(1)
    expect(event.waitUntil).toHaveBeenCalledWith(worker.clients.claim.mock.results[0].value)
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

describe('withUpdateProgress offering the update itself', () => {
  it('offers a version found after Workbox stopped listening', async () => {
    // Workbox drops its updatefound listener after the first update it did
    // not start, so in a tab left open the next version was never offered.
    const registration = fakeRegistration()
    const container = readyContainer(registration)
    const base = registered()
    const sw = withUpdateProgress(base, container)
    await settle()

    const worker = fakeWorker()
    registration.find(worker)
    container.deliver({ type: PRECACHE_PROGRESS, done: 10, total: 10 }, worker)
    expect(sw.progress.value?.percent).toBe(100)

    registration.install(worker)

    expect(base.needRefresh.value).toBe(true)
    expect(sw.progress.value).toBeNull()
  })

  it('offers it again after the last offer was dismissed', async () => {
    const registration = fakeRegistration()
    const base = registered()
    withUpdateProgress(base, readyContainer(registration))
    await settle()

    const first = fakeWorker()
    registration.find(first)
    registration.install(first)
    base.needRefresh.value = false

    const second = fakeWorker()
    registration.find(second)
    registration.install(second)

    expect(base.needRefresh.value).toBe(true)
  })

  it('follows a worker that was already installing when the page started', async () => {
    const worker = fakeWorker()
    const registration = fakeRegistration({ installing: worker })
    const base = registered()
    withUpdateProgress(base, readyContainer(registration))
    await settle()

    registration.install(worker)

    expect(base.needRefresh.value).toBe(true)
  })

  it('offers a worker that was already waiting', async () => {
    const base = registered()
    withUpdateProgress(base, readyContainer(fakeRegistration({ waiting: fakeWorker('installed') })))
    await settle()

    expect(base.needRefresh.value).toBe(true)
  })

  it('offers nothing to a page no worker controls', async () => {
    const base = registered()
    const registration = fakeRegistration({ waiting: fakeWorker('installed') })
    withUpdateProgress(base, readyContainer(registration, { controller: null }))
    await settle()

    expect(base.needRefresh.value).toBe(false)
  })

  it('takes the bar down when the install fails, even if no message named the worker', async () => {
    const registration = fakeRegistration()
    const container = readyContainer(registration)
    const base = registered()
    const sw = withUpdateProgress(base, container)
    await settle()

    const worker = fakeWorker()
    registration.find(worker)
    container.deliver({ type: PRECACHE_PROGRESS, done: 3, total: 10 }, null)
    worker.moveTo('redundant')

    expect(sw.progress.value).toBeNull()
    expect(base.needRefresh.value).toBe(false)
  })

  it('offers nothing for a worker that installed but is not the waiting one', async () => {
    const registration = fakeRegistration()
    const base = registered()
    withUpdateProgress(base, readyContainer(registration))
    await settle()

    const worker = fakeWorker()
    registration.find(worker)
    worker.moveTo('installed')

    expect(base.needRefresh.value).toBe(false)
  })

  it('ignores an update found with nothing installing', async () => {
    const registration = fakeRegistration()
    const base = registered()
    withUpdateProgress(base, readyContainer(registration))
    await settle()

    registration.find(null)

    expect(base.needRefresh.value).toBe(false)
  })

  it('reloads once the new version takes over, as Workbox does only for its own offers', async () => {
    const container = fakeContainer()
    const reload = vi.fn()
    const sw = withUpdateProgress(registered(), container, reload)

    await sw.updateServiceWorker(true)
    expect(reload).not.toHaveBeenCalled()

    container.dispatchEvent(new Event('controllerchange'))
    container.dispatchEvent(new Event('controllerchange'))
    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads an uncontrolled page that asked for the update', async () => {
    const container = fakeContainer({ controller: null })
    const reload = vi.fn()
    const sw = withUpdateProgress(registered(), container, reload)

    await sw.updateServiceWorker(true)
    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not reload an uncontrolled page that was asked not to', async () => {
    const container = fakeContainer({ controller: null })
    const reload = vi.fn()
    const sw = withUpdateProgress(registered(), container, reload)

    await sw.updateServiceWorker(false)
    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).not.toHaveBeenCalled()
  })

  it('reloads a tab whose update another tab started', () => {
    // Its old version's files are gone from the cache and the server, so its
    // next lazy page would fail to load.
    const container = fakeContainer()
    const reload = vi.fn()
    withUpdateProgress(registered(), container, reload)

    container.dispatchEvent(new Event('controllerchange'))
    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('reloads at once when "Update now" finds another tab already started the update', async () => {
    const base = registered()
    const reload = vi.fn()
    const container = readyContainer(fakeRegistration())
    const sw = withUpdateProgress(base, container, reload)
    await settle()

    await sw.updateServiceWorker(true)
    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).toHaveBeenCalledTimes(1)
    expect(base.updateServiceWorker).not.toHaveBeenCalled()
  })

  it('asks the waiting worker to take over when there is one', async () => {
    const base = registered()
    const reload = vi.fn()
    const sw = withUpdateProgress(base, readyContainer(fakeRegistration({ waiting: fakeWorker('installed') })), reload)
    await settle()

    await sw.updateServiceWorker(true)

    expect(base.updateServiceWorker).toHaveBeenCalledWith(true)
    expect(reload).not.toHaveBeenCalled()
  })

  it('reloads an uncontrolled tab showing the offer when another tab updates', () => {
    // A hard reload starts with no controller, yet Workbox still offers it the update.
    const container = fakeContainer({ controller: null })
    const reload = vi.fn()
    const base = registered()
    withUpdateProgress(base, container, reload)
    base.needRefresh.value = true

    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).toHaveBeenCalledTimes(1)
  })

  it('does not reload a first visit that the new worker claims', () => {
    const container = fakeContainer({ controller: null })
    const reload = vi.fn()
    withUpdateProgress(registered(), container, reload)

    container.dispatchEvent(new Event('controllerchange'))

    expect(reload).not.toHaveBeenCalled()
  })

  it('reloads the page by default', async () => {
    const reload = vi.fn()
    vi.stubGlobal('location', { reload })
    try {
      const container = fakeContainer()
      const sw = withUpdateProgress(registered(), container)
      await sw.updateServiceWorker(true)
      container.dispatchEvent(new Event('controllerchange'))
      expect(reload).toHaveBeenCalled()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
