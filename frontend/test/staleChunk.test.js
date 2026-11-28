// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'

import { isStaleChunkError, reloadOnce, reloadOnStaleChunk } from '../src/utils/staleChunk'

function memoryStorage() {
  const m = new Map()
  return {
    getItem: (k) => (m.has(k) ? m.get(k) : null),
    setItem: (k, v) => m.set(k, String(v)),
    removeItem: (k) => m.delete(k),
  }
}

const Page = { render: () => null }

describe('isStaleChunkError', () => {
  it.each([
    'error loading dynamically imported module: https://app.agentrq.com/assets/ScheduledTaskInstancesView-CZc2nIf3.js',
    'Failed to fetch dynamically imported module: https://x/assets/a.js',
    'Importing a module script failed.',
    'Unable to preload CSS for /assets/a.css',
  ])('knows %s', (message) => {
    expect(isStaleChunkError(new TypeError(message))).toBe(true)
  })

  it('leaves other errors alone', () => {
    expect(isStaleChunkError(new TypeError('x is undefined'))).toBe(false)
    expect(isStaleChunkError(undefined)).toBe(false)
  })
})

describe('reloadOnce', () => {
  it('loads an address once, then leaves it to fail', () => {
    const storage = memoryStorage()
    const location = { assign: vi.fn() }
    expect(reloadOnce('/a', { storage, location })).toBe(true)
    expect(reloadOnce('/a', { storage, location })).toBe(false)
    expect(location.assign).toHaveBeenCalledTimes(1)
  })

  it('still reloads when storage throws', () => {
    const broken = { getItem() { throw new Error('denied') }, setItem() { throw new Error('denied') } }
    const location = { assign: vi.fn() }
    expect(reloadOnce('/a', { storage: broken, location })).toBe(true)
    expect(location.assign).toHaveBeenCalledWith('/a')
  })
})

describe('reloadOnStaleChunk', () => {
  let storage, location, target, router

  beforeEach(async () => {
    storage = memoryStorage()
    location = { assign: vi.fn(), href: 'https://app.test/workspaces/w' }
    target = new EventTarget()
    router = createRouter({
      history: createMemoryHistory(),
      routes: [
        { path: '/', component: Page },
        { path: '/ok', component: Page },
        { path: '/gone/:id', component: () => Promise.reject(new TypeError('error loading dynamically imported module: https://app.test/assets/Gone-abc.js')) },
        { path: '/broken', component: () => Promise.reject(new TypeError('x is undefined')) },
      ],
    })
    reloadOnStaleChunk(router, { storage, location, target })
    await router.push('/')
  })

  it('loads the page it was going to from the server', async () => {
    await router.push('/gone/1?tab=a').catch(() => {})
    expect(location.assign).toHaveBeenCalledWith('/gone/1?tab=a')
  })

  it('does not reload twice for the same page', async () => {
    await router.push('/gone/1').catch(() => {})
    await router.push('/gone/1').catch(() => {})
    expect(location.assign).toHaveBeenCalledTimes(1)
  })

  it('reloads again once a page has loaded in between', async () => {
    await router.push('/gone/1').catch(() => {})
    await router.push('/ok')
    await router.push('/gone/1').catch(() => {})
    expect(location.assign).toHaveBeenCalledTimes(2)
  })

  it('does not reload for an error that is not a missing file', async () => {
    await router.push('/broken').catch(() => {})
    expect(location.assign).not.toHaveBeenCalled()
  })

  it("reloads on Vite's preload error, and keeps it from being thrown", () => {
    const event = new Event('vite:preloadError', { cancelable: true })
    target.dispatchEvent(event)
    expect(location.assign).toHaveBeenCalledWith('https://app.test/workspaces/w')
    expect(event.defaultPrevented).toBe(true)

    const again = new Event('vite:preloadError', { cancelable: true })
    target.dispatchEvent(again)
    expect(again.defaultPrevented).toBe(false)
  })
})
