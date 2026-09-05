import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { IDBFactory, IDBKeyRange } from 'fake-indexeddb'

import {
  RETENTION_OPTIONS,
  RETENTION_PREFIX,
  SWEEP_INTERVAL_MS,
  dueForSweep,
  expiredRecords,
  getRetentionDays,
  isExpired,
  readLastSweptAt,
  retentionKey,
  setRetentionDays,
  sweepExpired,
  sweepIfDue,
  whenIdle,
  writeLastSweptAt,
} from '../src/composables/useCacheRetention'
import { cacheTask, resetSharedCache } from '../src/composables/useCachedTasks'
import { getCachedTask, listAllCachedTasks, openCache } from '../src/composables/useLocalCache'

const DAY = 24 * 60 * 60 * 1000
const NOW = 1_800_000_000_000

let factory
beforeEach(() => {
  factory = new IDBFactory()
})
afterEach(() => {
  resetSharedCache()
})

const makeStorage = (entries = {}) => {
  const store = new Map(Object.entries(entries))
  return {
    getItem: (k) => (store.has(k) ? store.get(k) : null),
    setItem: (k, v) => store.set(k, String(v)),
    removeItem: (k) => store.delete(k),
    has: (k) => store.has(k),
  }
}

const allOn = { getItem: () => null }

/** Move a cached record's clock, so a test can be about the limit alone. */
function setCachedAt(db, id, cachedAt) {
  return new Promise((resolve) => {
    const tx = db.transaction('tasks', 'readwrite')
    const store = tx.objectStore('tasks')
    const req = store.get(['ws1', id])
    req.onsuccess = () => store.put({ ...req.result, cachedAt })
    tx.oncomplete = resolve
  })
}

const makeTask = (over = {}) => ({
  id: '0iCYTqxKOqv',
  workspaceId: 'ws1',
  title: 'Ship the billing reconciliation fix',
  body: 'Refunds double-count.',
  updatedAt: '2026-09-02T10:00:00Z',
  ...over,
})

describe('getRetentionDays and setRetentionDays', () => {
  it('keeps indefinitely until someone says otherwise', () => {
    expect(getRetentionDays('ws1', makeStorage())).toBe(0)
  })

  it('round-trips a choice', () => {
    const storage = makeStorage()

    expect(setRetentionDays('ws1', 30, storage)).toBe(true)
    expect(getRetentionDays('ws1', storage)).toBe(30)
  })

  it('stores "keep indefinitely" as the absence of a value', () => {
    // The default lives in the reader, not in every device that ever opened the
    // workspace — the same rule the on/off setting follows.
    const storage = makeStorage()
    setRetentionDays('ws1', 30, storage)

    setRetentionDays('ws1', 0, storage)

    expect(storage.has(retentionKey('ws1'))).toBe(false)
    expect(getRetentionDays('ws1', storage)).toBe(0)
  })

  it('answers per workspace', () => {
    const storage = makeStorage()
    setRetentionDays('ws1', 7, storage)

    expect(getRetentionDays('ws1', storage)).toBe(7)
    expect(getRetentionDays('ws2', storage)).toBe(0)
  })

  it('keeps indefinitely rather than trusting a value it cannot read', () => {
    // Holding data slightly too long is a storage cost the budget already
    // bounds. Deleting someone's offline copy because a setting would not parse
    // is a surprise they cannot undo.
    expect(getRetentionDays('ws1', makeStorage({ [retentionKey('ws1')]: 'soon' }))).toBe(0)
    expect(getRetentionDays('ws1', makeStorage({ [retentionKey('ws1')]: '-5' }))).toBe(0)
    expect(getRetentionDays('ws1', null)).toBe(0)
    expect(getRetentionDays('', makeStorage())).toBe(0)
    expect(
      getRetentionDays('ws1', {
        getItem() {
          throw new Error('storage disabled')
        },
      })
    ).toBe(0)
  })

  it('reports it could not record the choice', () => {
    const throwing = {
      setItem() {
        throw new Error('full')
      },
      removeItem() {
        throw new Error('full')
      },
    }

    expect(setRetentionDays('ws1', 30, throwing)).toBe(false)
    expect(setRetentionDays('ws1', 0, throwing)).toBe(false)
    expect(setRetentionDays('ws1', 30, null)).toBe(false)
    expect(setRetentionDays('', 30, makeStorage())).toBe(false)
  })

  it('offers keep-indefinitely first, and namespaces its key', () => {
    expect(RETENTION_OPTIONS[0].days).toBe(0)
    expect(RETENTION_OPTIONS.map((o) => o.days)).toEqual([0, 7, 30, 90])
    expect(retentionKey('ws1')).toBe(`${RETENTION_PREFIX}ws1`)
  })
})

describe('isExpired', () => {
  it('expires a record older than the limit', () => {
    expect(isExpired({ cachedAt: NOW - 8 * DAY }, 7, NOW)).toBe(true)
    expect(isExpired({ cachedAt: NOW - 6 * DAY }, 7, NOW)).toBe(false)
  })

  it('never expires anything when the limit is indefinite', () => {
    expect(isExpired({ cachedAt: 0 }, 0, NOW)).toBe(false)
    expect(isExpired({ cachedAt: 0 }, undefined, NOW)).toBe(false)
  })

  it('treats a record written before this existed as current', () => {
    // An upgrade must not delete someone's cache on its first run.
    expect(isExpired({}, 7, NOW)).toBe(false)
    expect(isExpired({ cachedAt: 'yesterday' }, 7, NOW)).toBe(false)
    expect(isExpired(null, 7, NOW)).toBe(false)
  })
})

describe('expiredRecords', () => {
  it('applies each workspace its own limit', () => {
    const records = [
      { id: 'a', workspaceId: 'ws1', cachedAt: NOW - 10 * DAY },
      { id: 'b', workspaceId: 'ws2', cachedAt: NOW - 10 * DAY },
    ]
    const limitFor = (id) => (id === 'ws1' ? 7 : 0)

    expect(expiredRecords(records, limitFor, NOW).map((r) => r.id)).toEqual(['a'])
  })

  it('looks a limit up once per workspace, not once per record', () => {
    // A sweep reads thousands of rows and would otherwise hit local storage for
    // every one of them.
    const limitFor = vi.fn(() => 7)
    const records = Array.from({ length: 50 }, (_, i) => ({
      id: `${i}`,
      workspaceId: i % 2 ? 'ws1' : 'ws2',
      cachedAt: NOW,
    }))

    expiredRecords(records, limitFor, NOW)

    expect(limitFor).toHaveBeenCalledTimes(2)
  })

  it('ignores a record with no workspace to answer for', () => {
    expect(expiredRecords([{ id: 'x', cachedAt: 0 }], () => 7, NOW)).toEqual([])
  })

  it('has nothing to expire from nothing', () => {
    expect(expiredRecords([], () => 7, NOW)).toEqual([])
    expect(expiredRecords(null, () => 7, NOW)).toEqual([])
  })
})

describe('dueForSweep', () => {
  it('is due when a day has passed, and not before', () => {
    expect(dueForSweep(NOW - SWEEP_INTERVAL_MS, NOW)).toBe(true)
    expect(dueForSweep(NOW - SWEEP_INTERVAL_MS + 1, NOW)).toBe(false)
  })

  it('is due when it has never run', () => {
    expect(dueForSweep(null, NOW)).toBe(true)
    expect(dueForSweep(undefined, NOW)).toBe(true)
    expect(dueForSweep('never', NOW)).toBe(true)
  })

  it('is due when the clock has moved backwards', () => {
    // A timezone change or a corrected system clock would otherwise park the
    // sweep until the future caught up.
    expect(dueForSweep(NOW + 5 * DAY, NOW)).toBe(true)
  })

  it('accepts a different interval', () => {
    expect(dueForSweep(NOW - 100, NOW, 50)).toBe(true)
    expect(dueForSweep(NOW - 10, NOW, 50)).toBe(false)
  })
})

describe('readLastSweptAt and writeLastSweptAt', () => {
  it('round-trips through the database rather than local storage', async () => {
    // Kept beside the data it describes: clearing the cache and keeping the
    // timestamp would skip the next sweep for a day over nothing.
    const db = await openCache({ userId: 'u1', factory })

    expect(await readLastSweptAt(db)).toBeNull()
    expect(await writeLastSweptAt(db, NOW)).toBe(true)
    expect(await readLastSweptAt(db)).toBe(NOW)
    db.close()
  })

  it('says nothing rather than throwing without a database', async () => {
    expect(await readLastSweptAt(null)).toBeNull()
    expect(await writeLastSweptAt(null)).toBe(false)
  })

  it('says nothing rather than throwing on a closed connection', async () => {
    const db = await openCache({ userId: 'u1', factory })
    db.close()

    expect(await readLastSweptAt(db)).toBeNull()
    expect(await writeLastSweptAt(db, NOW)).toBe(false)
  })

  it('says nothing when the read itself fails', async () => {
    // The timestamp is an optimisation, so a failed read means "sweep now",
    // not "give up".
    const req = {}
    const db = { transaction: () => ({ objectStore: () => ({ get: () => req }) }) }
    const reading = readLastSweptAt(db)

    req.onerror()

    expect(await reading).toBeNull()
  })

  it('reports a failed or aborted write rather than claiming it swept', async () => {
    // Claiming a sweep that did not happen would skip the next one for a day.
    const errored = { objectStore: () => ({ put: () => {} }) }
    const failing = writeLastSweptAt({ transaction: () => errored }, NOW)
    errored.onerror()
    expect(await failing).toBe(false)

    const aborted = { objectStore: () => ({ put: () => {} }) }
    const aborting = writeLastSweptAt({ transaction: () => aborted }, NOW)
    aborted.onabort()
    expect(await aborting).toBe(false)
  })
})

describe('sweepExpired', () => {
  it('removes what has outlived its limit and keeps the rest', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const storage = makeStorage()
    setRetentionDays('ws1', 7, storage)
    await cacheTask(db, makeTask({ id: 'stale' }), { storage: allOn })
    await cacheTask(db, makeTask({ id: 'fresh' }), { storage: allOn })
    // Both were stamped with the real clock, which is not `NOW`. Setting both
    // explicitly is what makes this test about the limit rather than about how
    // far the fixture's clock happens to sit from the machine's.
    await setCachedAt(db, 'stale', NOW - 30 * DAY)
    await setCachedAt(db, 'fresh', NOW - 1 * DAY)

    expect(await sweepExpired(db, { storage, now: NOW, KeyRange: IDBKeyRange })).toBe(1)

    expect(await getCachedTask(db, 'ws1', 'stale')).toBeNull()
    expect(await getCachedTask(db, 'ws1', 'fresh')).not.toBeNull()
    db.close()
  })

  it('removes the conversation with the task, not just the row', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const storage = makeStorage()
    setRetentionDays('ws1', 1, storage)
    await cacheTask(db, makeTask({ messages: [{ id: 'm1' }] }), { storage: allOn })

    await sweepExpired(db, { storage, now: NOW + 10 * DAY, KeyRange: IDBKeyRange })

    const threads = await new Promise((resolve) => {
      const req = db.transaction('threads').objectStore('threads').getAll()
      req.onsuccess = () => resolve(req.result)
    })
    expect(threads).toEqual([])
    db.close()
  })

  it('does nothing when nothing has a limit', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask(), { storage: allOn })

    expect(await sweepExpired(db, { storage: makeStorage(), now: NOW, KeyRange: IDBKeyRange })).toBe(0)
    expect(await listAllCachedTasks(db)).toHaveLength(1)
    db.close()
  })

  it('does nothing without a database', async () => {
    expect(await sweepExpired(null, { storage: makeStorage() })).toBe(0)
  })
})

describe('sweepIfDue', () => {
  it('sweeps when due and records that it did', async () => {
    const db = await openCache({ userId: 'u1', factory })

    const first = await sweepIfDue(db, { storage: makeStorage(), now: NOW, KeyRange: IDBKeyRange })

    expect(first.swept).toBe(true)
    expect(await readLastSweptAt(db)).toBe(NOW)
    db.close()
  })

  it('skips the rest of the day', async () => {
    // The guard is what makes this safe to call on every startup and on a
    // timer: the work happens when due and is skipped the rest of the time.
    const db = await openCache({ userId: 'u1', factory })
    await sweepIfDue(db, { storage: makeStorage(), now: NOW, KeyRange: IDBKeyRange })

    const again = await sweepIfDue(db, {
      storage: makeStorage(),
      now: NOW + 1000,
      KeyRange: IDBKeyRange,
    })

    expect(again).toEqual({ swept: false, removed: 0 })
    db.close()
  })

  it('sweeps again the next day', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await sweepIfDue(db, { storage: makeStorage(), now: NOW, KeyRange: IDBKeyRange })

    const next = await sweepIfDue(db, {
      storage: makeStorage(),
      now: NOW + SWEEP_INTERVAL_MS,
      KeyRange: IDBKeyRange,
    })

    expect(next.swept).toBe(true)
    db.close()
  })

  it('reports how much it removed', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const storage = makeStorage()
    setRetentionDays('ws1', 1, storage)
    await cacheTask(db, makeTask(), { storage: allOn })

    const result = await sweepIfDue(db, {
      storage,
      now: NOW + 10 * DAY,
      KeyRange: IDBKeyRange,
    })

    expect(result).toEqual({ swept: true, removed: 1 })
    db.close()
  })

  it('does nothing without a database', async () => {
    expect(await sweepIfDue(null, {})).toEqual({ swept: false, removed: 0 })
  })
})

describe('whenIdle', () => {
  it('waits for the browser to be free when it can say', () => {
    // A sweep competes with rendering, and there is never a reason for it to
    // win — it is a day's tidying, not something anyone is waiting on.
    const ran = []
    const scheduler = (fn) => {
      ran.push('scheduled')
      fn()
      return 7
    }

    expect(whenIdle(() => ran.push('swept'), scheduler)).toBe(7)
    expect(ran).toEqual(['scheduled', 'swept'])
  })

  it('falls back to the next tick where idle time is not offered', async () => {
    const ran = []

    whenIdle(() => ran.push('swept'), undefined)
    await new Promise((resolve) => setTimeout(resolve, 5))

    expect(ran).toEqual(['swept'])
  })
})
