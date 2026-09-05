import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { IDBFactory, IDBKeyRange } from 'fake-indexeddb'

import {
  clearWorkspaceData,
  estimateUsage,
  forgetEverything,
  formatBytes,
  requestPersistence,
  setCacheEnabled,
  tellShellToForget,
  tellShellToForgetAll,
  tellWorkerToForget,
} from '../src/composables/useCacheStorage'
import {
  cacheSettingKey,
  cacheTask,
  connectCache,
  isCacheEnabled,
  resetSharedCache,
  sharedCache,
} from '../src/composables/useCachedTasks'
import { listCachedTasks, openCache } from '../src/composables/useLocalCache'

let factory
beforeEach(() => {
  factory = new IDBFactory()
})
afterEach(() => {
  resetSharedCache()
})

/** A localStorage that records what it was told. */
const makeStorage = () => {
  const entries = new Map()
  return {
    entries,
    getItem: (key) => (entries.has(key) ? entries.get(key) : null),
    setItem: (key, value) => entries.set(key, value),
    removeItem: (key) => entries.delete(key),
  }
}

const makeTask = (over = {}) => ({
  id: '0iCYTqxKOqv',
  workspaceId: 'ws1',
  title: 'Ship the billing reconciliation fix',
  body: 'The nightly job double-counts refunds.',
  updatedAt: '2026-09-02T10:00:00Z',
  ...over,
})

const allOn = { getItem: () => null }

describe('setCacheEnabled', () => {
  it('records off, and records on as the absence of the setting', () => {
    // The default lives in the reader, not in every device that has ever opened
    // the workspace — so "on" leaves no key behind.
    const storage = makeStorage()

    setCacheEnabled('ws1', false, storage)
    expect(storage.getItem(cacheSettingKey('ws1'))).toBe('off')

    setCacheEnabled('ws1', true, storage)
    expect(storage.entries.has(cacheSettingKey('ws1'))).toBe(false)
  })

  it('round-trips with the reader', () => {
    const storage = makeStorage()

    setCacheEnabled('ws1', false, storage)
    expect(isCacheEnabled('ws1', storage)).toBe(false)

    setCacheEnabled('ws1', true, storage)
    expect(isCacheEnabled('ws1', storage)).toBe(true)
  })

  it('answers for one workspace without touching another', () => {
    const storage = makeStorage()

    setCacheEnabled('ws1', false, storage)

    expect(isCacheEnabled('ws1', storage)).toBe(false)
    expect(isCacheEnabled('ws2', storage)).toBe(true)
  })

  it('reports it could not record the choice', () => {
    const throwing = {
      setItem() {
        throw new Error('storage full')
      },
      removeItem() {
        throw new Error('storage full')
      },
    }

    expect(setCacheEnabled('ws1', false, throwing)).toBe(false)
    expect(setCacheEnabled('ws1', true, throwing)).toBe(false)
    expect(setCacheEnabled('ws1', false, null)).toBe(false)
    expect(setCacheEnabled('', false, makeStorage())).toBe(false)
  })
})

describe('formatBytes', () => {
  it('reads at a glance at every scale', () => {
    expect(formatBytes(0)).toBe('0 B')
    expect(formatBytes(512)).toBe('512 B')
    expect(formatBytes(1024)).toBe('1.0 KB')
    expect(formatBytes(1024 * 1024 * 9.4)).toBe('9.4 MB')
    expect(formatBytes(1024 * 1024 * 312)).toBe('312 MB')
    expect(formatBytes(1024 * 1024 * 1024 * 2.5)).toBe('2.5 GB')
  })

  it('stops at gigabytes rather than inventing a unit', () => {
    expect(formatBytes(1024 ** 4)).toBe('1024 GB')
  })

  it('says nothing rather than something wrong for a non-number', () => {
    expect(formatBytes(undefined)).toBe('0 B')
    expect(formatBytes(null)).toBe('0 B')
    expect(formatBytes(-5)).toBe('0 B')
    expect(formatBytes(NaN)).toBe('0 B')
    expect(formatBytes(Infinity)).toBe('0 B')
  })
})

describe('estimateUsage', () => {
  it('reports what the browser says', async () => {
    const manager = { estimate: async () => ({ usage: 1234, quota: 99999 }) }

    expect(await estimateUsage(manager)).toEqual({ usage: 1234, quota: 99999 })
  })

  it('reports a usage without a quota rather than nothing', async () => {
    const manager = { estimate: async () => ({ usage: 1234 }) }

    expect(await estimateUsage(manager)).toEqual({ usage: 1234, quota: 0 })
  })

  it('says nothing when the browser will not', async () => {
    // Null is an absence and zero is a claim. The caller hides the figure
    // rather than reporting a number it does not have.
    expect(await estimateUsage(undefined)).toBeNull()
    expect(await estimateUsage({})).toBeNull()
    expect(await estimateUsage({ estimate: async () => ({}) })).toBeNull()
    expect(await estimateUsage({ estimate: async () => null })).toBeNull()
    expect(
      await estimateUsage({
        estimate() {
          throw new Error('denied in a private window')
        },
      })
    ).toBeNull()
  })
})

describe('requestPersistence', () => {
  it('reports whether the browser agreed', async () => {
    expect(await requestPersistence({ persist: async () => true })).toBe(true)
    expect(await requestPersistence({ persist: async () => false })).toBe(false)
  })

  it('never throws, whatever the engine does', async () => {
    // A cache that gets evicted behaves exactly like one that was never
    // written, so a refusal is not worth an error.
    expect(await requestPersistence(undefined)).toBe(false)
    expect(await requestPersistence({})).toBe(false)
    expect(
      await requestPersistence({
        persist() {
          throw new Error('not supported')
        },
      })
    ).toBe(false)
  })
})

describe('clearWorkspaceData', () => {
  it('forgets one workspace and leaves the others', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask(), { storage: allOn })
    await cacheTask(db, makeTask({ id: 'keep', workspaceId: 'ws2' }), { storage: allOn })

    const posted = []
    const worker = { controller: { postMessage: (msg) => posted.push(msg) } }

    const result = await clearWorkspaceData(db, 'ws1', { KeyRange: IDBKeyRange, worker })

    expect(result.cleared).toBe(true)
    // The bytes go with the rows.
    expect(posted).toEqual([{ type: 'FORGET_WORKSPACE', workspaceId: 'ws1' }])
    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toEqual([])
    expect(await listCachedTasks(db, 'ws2', IDBKeyRange)).toHaveLength(1)
    db.close()
  })

  it('measures what it freed rather than calculating it', async () => {
    // The difference between two estimates either side of the delete, so it
    // stays right when the stored shape changes later.
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask(), { storage: allOn })
    const readings = [{ usage: 5000 }, { usage: 1200 }]
    const manager = { estimate: async () => readings.shift() }

    const result = await clearWorkspaceData(db, 'ws1', { KeyRange: IDBKeyRange, manager })

    expect(result).toEqual({ cleared: true, reclaimed: 3800 })
    db.close()
  })

  it('never reports a negative saving when the estimate wobbles', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const readings = [{ usage: 1000 }, { usage: 1400 }]
    const manager = { estimate: async () => readings.shift() }

    const result = await clearWorkspaceData(db, 'ws1', { KeyRange: IDBKeyRange, manager })

    expect(result.reclaimed).toBe(0)
    db.close()
  })

  it('still clears when the browser will not estimate', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask(), { storage: allOn })

    const result = await clearWorkspaceData(db, 'ws1', { KeyRange: IDBKeyRange, manager: {} })

    expect(result).toEqual({ cleared: true, reclaimed: null })
    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toEqual([])
    db.close()
  })

  it('reports nothing cleared when there is nothing to clear from', async () => {
    expect(await clearWorkspaceData(null, 'ws1', { KeyRange: IDBKeyRange })).toEqual({
      cleared: false,
      reclaimed: null,
    })
    const db = await openCache({ userId: 'u1', factory })
    expect(await clearWorkspaceData(db, '', { KeyRange: IDBKeyRange })).toEqual({
      cleared: false,
      reclaimed: null,
    })
    db.close()
  })

  it('reports failure rather than throwing when the clear cannot run', async () => {
    const broken = {
      transaction() {
        throw new Error('the connection went away')
      },
    }

    expect(await clearWorkspaceData(broken, 'ws1', { KeyRange: IDBKeyRange })).toEqual({
      cleared: false,
      reclaimed: null,
    })
  })
})

describe('tellWorkerToForget', () => {
  it('asks the worker, because only it can reach the attachment bytes', () => {
    const posted = []
    const worker = { controller: { postMessage: (msg) => posted.push(msg) } }

    expect(tellWorkerToForget('ws1', worker)).toBe(true)
    expect(posted).toEqual([{ type: 'FORGET_WORKSPACE', workspaceId: 'ws1' }])
  })

  it('is a no-op where there is no worker to ask', () => {
    // The desktop renderer is built without one, so there is nothing listening
    // and nothing there to forget.
    expect(tellWorkerToForget('ws1', undefined)).toBe(false)
    expect(tellWorkerToForget('ws1', {})).toBe(false)
    expect(tellWorkerToForget('ws1', { controller: null })).toBe(false)
    expect(tellWorkerToForget('', { controller: { postMessage: () => {} } })).toBe(false)
  })

  it('reports failure rather than throwing when the message cannot be sent', () => {
    const worker = {
      controller: {
        postMessage() {
          throw new Error('worker went away')
        },
      },
    }

    expect(tellWorkerToForget('ws1', worker)).toBe(false)
  })
})

describe('tellShellToForget', () => {
  it('asks the desktop shell, which owns the files there', async () => {
    // The desktop build has no service worker, so the bytes are files the main
    // process owns and the renderer goes through the bridge.
    const calls = []
    const bridge = { attachments: { forgetWorkspace: async (id) => calls.push(id) } }

    expect(await tellShellToForget('ws1', bridge)).toBe(true)
    expect(calls).toEqual(['ws1'])
  })

  it('is a no-op in the browser, where there is no shell', async () => {
    // Both paths are attempted rather than branched on the platform: exactly
    // one exists in any build, and asking the absent one already does nothing.
    expect(await tellShellToForget('ws1', undefined)).toBe(false)
    expect(await tellShellToForget('ws1', {})).toBe(false)
    expect(await tellShellToForget('ws1', { attachments: {} })).toBe(false)
    expect(await tellShellToForget('', { attachments: { forgetWorkspace: async () => {} } })).toBe(
      false
    )
  })

  it('reports failure rather than throwing when the bridge rejects', async () => {
    const bridge = {
      attachments: {
        forgetWorkspace: async () => {
          throw new Error('main process went away')
        },
      },
    }

    expect(await tellShellToForget('ws1', bridge)).toBe(false)
  })
})

describe('tellShellToForgetAll', () => {
  it('drops everything the shell holds for this profile', async () => {
    let called = false
    const bridge = { attachments: { forgetAll: async () => { called = true } } }

    expect(await tellShellToForgetAll(bridge)).toBe(true)
    expect(called).toBe(true)
  })

  it('is a no-op without a shell, and never throws', async () => {
    expect(await tellShellToForgetAll(undefined)).toBe(false)
    expect(await tellShellToForgetAll({})).toBe(false)
    expect(
      await tellShellToForgetAll({
        attachments: {
          forgetAll: async () => {
            throw new Error('gone')
          },
        },
      })
    ).toBe(false)
  })
})

describe('forgetEverything', () => {
  it('deletes the database and drops the connection', async () => {
    // Signing out. A browser several people use must not leave one person's
    // task titles readable by the next.
    const db = await connectCache('u1', { factory })
    await cacheTask(db, makeTask(), { storage: allOn })

    expect(await forgetEverything({ userId: 'u1', factory })).toBe(true)
    expect(sharedCache()).toBeNull()

    const fresh = await openCache({ userId: 'u1', factory })
    expect(await listCachedTasks(fresh, 'ws1', IDBKeyRange)).toEqual([])
    fresh.close()
  })

  it('also clears what the desktop shell holds outside the database', async () => {
    let clearedShell = false
    const bridge = { attachments: { forgetAll: async () => { clearedShell = true } } }

    await forgetEverything({ userId: 'u1', factory, bridge })

    expect(clearedShell).toBe(true)
  })

  it('does not consult the per-workspace setting', async () => {
    // Deliberately unconditional: there is no partial version of this
    // guarantee worth having.
    const db = await connectCache('u1', { factory })
    await cacheTask(db, makeTask(), { storage: allOn })

    expect(await forgetEverything({ userId: 'u1', factory })).toBe(true)
  })

  it('still drops the connection when there is no user to name a database', async () => {
    await connectCache('u1', { factory })

    expect(await forgetEverything({})).toBe(false)
    expect(sharedCache()).toBeNull()
  })

  it('runs safely when nothing was ever opened', async () => {
    expect(await forgetEverything()).toBe(false)
    expect(sharedCache()).toBeNull()
  })
})
