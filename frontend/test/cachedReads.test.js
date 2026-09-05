import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { IDBFactory, IDBKeyRange } from 'fake-indexeddb'
import { createApp, h } from 'vue'

import {
  OFFLINE_NOTICE,
  isOffline,
  readAllCachedTasks,
  readCachedTask,
  readCachedTasks,
  shouldPaintCache,
  useOffline,
} from '../src/composables/useCachedReads'
import { cacheSettingKey, cacheTask, resetSharedCache } from '../src/composables/useCachedTasks'
import { openCache } from '../src/composables/useLocalCache'

let factory
beforeEach(() => {
  factory = new IDBFactory()
})
afterEach(() => {
  resetSharedCache()
})

const allOn = { getItem: () => null }
const offFor = (id) => ({ getItem: (key) => (key === cacheSettingKey(id) ? 'off' : null) })

const makeTask = (over = {}) => ({
  id: '0iCYTqxKOqv',
  workspaceId: 'ws1',
  title: 'Ship the billing reconciliation fix',
  body: 'Refunds double-count.',
  updatedAt: '2026-09-02T10:00:00Z',
  ...over,
})

/** Real lifecycle hooks, for exercising the composable's defaults. */
function mountWith(setup) {
  const app = createApp({ setup, render: () => h('div') })
  app.mount(document.createElement('div'))
  return () => app.unmount()
}

describe('shouldPaintCache', () => {
  it('paints into an empty view', () => {
    expect(shouldPaintCache([], [makeTask()])).toBe(true)
    expect(shouldPaintCache(undefined, [makeTask()])).toBe(true)
    expect(shouldPaintCache(null, [makeTask()])).toBe(true)
  })

  it('never paints over what is already on screen', () => {
    // A cached read arriving after the server's answer would replace something
    // newer with something older, which is the one thing a cache must not do.
    expect(shouldPaintCache([makeTask()], [makeTask({ title: 'Older' })])).toBe(false)
  })

  it('does not paint nothing over nothing', () => {
    expect(shouldPaintCache([], [])).toBe(false)
    expect(shouldPaintCache([], null)).toBe(false)
    expect(shouldPaintCache([], undefined)).toBe(false)
  })
})

describe('readCachedTasks', () => {
  it('returns a workspace newest first', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ id: 'old', updatedAt: '2026-01-01T00:00:00Z' }), { storage: allOn })
    await cacheTask(db, makeTask({ id: 'new', updatedAt: '2026-09-01T00:00:00Z' }), { storage: allOn })

    const rows = await readCachedTasks(db, 'ws1', { storage: allOn, KeyRange: IDBKeyRange })

    expect(rows.map((r) => r.id)).toEqual(['new', 'old'])
    db.close()
  })

  it('honours a limit', async () => {
    const db = await openCache({ userId: 'u1', factory })
    for (const id of ['a', 'b', 'c']) await cacheTask(db, makeTask({ id }), { storage: allOn })

    const rows = await readCachedTasks(db, 'ws1', { storage: allOn, KeyRange: IDBKeyRange, limit: 2 })

    expect(rows).toHaveLength(2)
    db.close()
  })

  it('shows nothing for a workspace whose owner turned the cache off', async () => {
    // Even though the rows may still be there — clearing is best effort, and
    // the setting is the promise.
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask(), { storage: allOn })

    const rows = await readCachedTasks(db, 'ws1', { storage: offFor('ws1'), KeyRange: IDBKeyRange })

    expect(rows).toEqual([])
    db.close()
  })

  it('falls straight through when there is no cache', async () => {
    expect(await readCachedTasks(null, 'ws1', { storage: allOn, KeyRange: IDBKeyRange })).toEqual([])
  })

  it('falls through rather than breaking the view when the read fails', async () => {
    const broken = {
      transaction() {
        throw new Error('the connection went away')
      },
    }

    await expect(
      readCachedTasks(broken, 'ws1', { storage: allOn, KeyRange: IDBKeyRange })
    ).resolves.toEqual([])
  })
})

describe('readAllCachedTasks', () => {
  it('reads every workspace without being told which exist', async () => {
    // The point of taking no workspace list: the global list can paint before
    // it has asked the server which workspaces there are.
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ id: 'a', updatedAt: '2026-05-01T00:00:00Z' }), { storage: allOn })
    await cacheTask(db, makeTask({ id: 'b', workspaceId: 'ws2', updatedAt: '2026-09-01T00:00:00Z' }), {
      storage: allOn,
    })

    const rows = await readAllCachedTasks(db, { storage: allOn })

    expect(rows.map((r) => r.id)).toEqual(['b', 'a'])
    db.close()
  })

  it('hides a workspace whose rows outlived its opt-out', async () => {
    // Clearing is best effort; the setting is the promise. A row that survived
    // a failed clear must still not be shown.
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ id: 'a' }), { storage: allOn })
    await cacheTask(db, makeTask({ id: 'b', workspaceId: 'ws2' }), { storage: allOn })

    const rows = await readAllCachedTasks(db, { storage: offFor('ws2') })

    expect(rows.map((r) => r.id)).toEqual(['a'])
    db.close()
  })

  it('honours a limit', async () => {
    const db = await openCache({ userId: 'u1', factory })
    for (const id of ['a', 'b', 'c']) await cacheTask(db, makeTask({ id }), { storage: allOn })

    expect(await readAllCachedTasks(db, { storage: allOn, limit: 2 })).toHaveLength(2)
    db.close()
  })

  it('orders undated rows last rather than throwing', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ id: 'dated', updatedAt: '2026-09-01T00:00:00Z' }), {
      storage: allOn,
    })
    await cacheTask(db, makeTask({ id: 'undated', updatedAt: undefined }), { storage: allOn })
    await cacheTask(db, makeTask({ id: 'undated2', updatedAt: undefined }), { storage: allOn })

    const rows = await readAllCachedTasks(db, { storage: allOn })

    expect(rows[0].id).toBe('dated')
    db.close()
  })

  it('falls through when there is no cache or the read fails', async () => {
    expect(await readAllCachedTasks(null, { storage: allOn })).toEqual([])
    expect(
      await readAllCachedTasks(
        {
          transaction() {
            throw new Error('gone')
          },
        },
        { storage: allOn }
      )
    ).toEqual([])
  })
})

describe('readCachedTask', () => {
  it('returns the cached copy with its conversation', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ messages: [{ id: 'm1' }] }), { storage: allOn })

    const task = await readCachedTask(db, 'ws1', '0iCYTqxKOqv', { storage: allOn })

    expect(task.title).toBe('Ship the billing reconciliation fix')
    expect(task.messages).toEqual([{ id: 'm1' }])
    db.close()
  })

  it('returns nothing when it may not or cannot answer', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask(), { storage: allOn })

    expect(await readCachedTask(db, 'ws1', '0iCYTqxKOqv', { storage: offFor('ws1') })).toBeNull()
    expect(await readCachedTask(null, 'ws1', '0iCYTqxKOqv', { storage: allOn })).toBeNull()
    expect(await readCachedTask(db, 'ws1', '', { storage: allOn })).toBeNull()
    expect(await readCachedTask(db, 'ws1', 'never-seen', { storage: allOn })).toBeNull()
    db.close()
  })
})

describe('isOffline', () => {
  it('believes the browser only when it claims to be offline', () => {
    // navigator.onLine reports a network interface rather than reachability, so
    // it is used to disable an action and explain why — never to decide whether
    // data can be trusted.
    expect(isOffline({ onLine: false })).toBe(true)
    expect(isOffline({ onLine: true })).toBe(false)
    expect(isOffline({})).toBe(false)
    expect(isOffline(null)).toBe(false)
  })

  it('explains itself where a reply would go', () => {
    expect(OFFLINE_NOTICE).toContain('offline')
    expect(OFFLINE_NOTICE).toContain('connection')
  })
})

describe('useOffline', () => {
  it('tracks the connection while its component is mounted', () => {
    const listeners = {}
    const target = {
      addEventListener: (name, fn) => {
        listeners[name] = fn
      },
      removeEventListener: vi.fn(),
    }
    const nav = { onLine: true }
    const mounts = []
    const unmounts = []

    const { offline } = useOffline({
      target,
      nav,
      onMounted: (fn) => mounts.push(fn),
      onUnmounted: (fn) => unmounts.push(fn),
    })

    expect(offline.value).toBe(false)
    mounts.forEach((fn) => fn())

    nav.onLine = false
    listeners.offline()
    expect(offline.value).toBe(true)

    nav.onLine = true
    listeners.online()
    expect(offline.value).toBe(false)

    unmounts.forEach((fn) => fn())
    expect(target.removeEventListener).toHaveBeenCalledTimes(2)
  })

  it('re-reads at mount, so a connection lost before then is not missed', () => {
    const nav = { onLine: true }
    const mounts = []
    const { offline } = useOffline({
      target: null,
      nav,
      onMounted: (fn) => mounts.push(fn),
      onUnmounted: () => {},
    })

    nav.onLine = false
    mounts.forEach((fn) => fn())

    expect(offline.value).toBe(true)
  })

  it('runs with the real window and navigator by default', () => {
    let result
    const unmount = mountWith(() => {
      result = useOffline()
      return {}
    })

    // jsdom reports a connection, which is all this asserts: the defaults are
    // wired to something real rather than to undefined.
    expect(result.offline.value).toBe(false)
    expect(() => unmount()).not.toThrow()
  })
})
