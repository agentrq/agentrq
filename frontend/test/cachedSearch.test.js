import { describe, it, expect, beforeEach, afterEach } from 'vitest'
import { IDBFactory, IDBKeyRange } from 'fake-indexeddb'

import { searchCachedTasks } from '../src/composables/useCachedReads'
import { cacheSettingKey, cacheTask, resetSharedCache } from '../src/composables/useCachedTasks'
import { openCache, taskKeysForTerm, tasksByKeys } from '../src/composables/useLocalCache'

let factory
beforeEach(() => {
  factory = new IDBFactory()
})
afterEach(() => {
  resetSharedCache()
})

const allOn = { getItem: () => null }
const offFor = (id) => ({ getItem: (key) => (key === cacheSettingKey(id) ? 'off' : null) })
const opts = { storage: allOn, KeyRange: IDBKeyRange }

const makeTask = (over = {}) => ({
  id: 'aaaaaaaaaaa',
  workspaceId: 'ws1',
  title: 'Ship the billing reconciliation fix',
  body: 'The nightly job double-counts refunds.',
  updatedAt: '2026-09-02T10:00:00Z',
  ...over,
})

/** A cache holding three tasks that overlap in useful ways. */
async function seeded() {
  const db = await openCache({ userId: 'u1', factory })
  await cacheTask(db, makeTask(), { storage: allOn })
  await cacheTask(
    db,
    makeTask({ id: 'bbbbbbbbbbb', title: 'Billing dashboard copy', body: 'Wording only.' }),
    { storage: allOn }
  )
  await cacheTask(
    db,
    makeTask({
      id: 'ccccccccccc',
      workspaceId: 'ws2',
      title: 'Unrelated groundwork',
      body: 'Nothing to do with money.',
    }),
    { storage: allOn }
  )
  return db
}

describe('taskKeysForTerm and tasksByKeys', () => {
  it('finds keys by term and reads only those records', async () => {
    // Keys first, records second: a two-word query asks the index twice and
    // only the intersection is worth reading.
    const db = await seeded()

    const keys = await taskKeysForTerm(db, IDBKeyRange.bound('billing', 'billing￿'))
    expect(keys).toHaveLength(2)

    const rows = await tasksByKeys(db, keys)
    expect(rows.map((r) => r.id).sort()).toEqual(['aaaaaaaaaaa', 'bbbbbbbbbbb'])
    db.close()
  })

  it('returns nothing rather than throwing when there is no cache', async () => {
    expect(await taskKeysForTerm(null, IDBKeyRange.only('x'))).toEqual([])
    expect(await tasksByKeys(null, [['ws1', 'a']])).toEqual([])
  })

  it('returns nothing for an empty or missing key set', async () => {
    const db = await seeded()

    expect(await tasksByKeys(db, [])).toEqual([])
    expect(await tasksByKeys(db, undefined)).toEqual([])
    expect(await tasksByKeys(db, [['ws1', 'never-existed']])).toEqual([])
    db.close()
  })

  it('falls through rather than breaking when the read fails', async () => {
    const broken = {
      transaction() {
        throw new Error('gone')
      },
    }

    expect(await taskKeysForTerm(broken, IDBKeyRange.only('x'))).toEqual([])
    expect(await tasksByKeys(broken, [['ws1', 'a']])).toEqual([])
  })
})

describe('searchCachedTasks', () => {
  it('searches titles across every workspace with no request at all', async () => {
    const db = await seeded()

    const rows = await searchCachedTasks(db, 'billing', opts)

    expect(rows.map((r) => r.id).sort()).toEqual(['aaaaaaaaaaa', 'bbbbbbbbbbb'])
    db.close()
  })

  it('searches descriptions, which the old finder could not reach', async () => {
    // "refunds" appears only in a body. This is the requirement the whole
    // feature exists for.
    const db = await seeded()

    const rows = await searchCachedTasks(db, 'refunds', opts)

    expect(rows.map((r) => r.id)).toEqual(['aaaaaaaaaaa'])
    db.close()
  })

  it('narrows on a second word rather than widening', async () => {
    const db = await seeded()

    expect(await searchCachedTasks(db, 'billing', opts)).toHaveLength(2)
    expect((await searchCachedTasks(db, 'billing dashboard', opts)).map((r) => r.id)).toEqual([
      'bbbbbbbbbbb',
    ])
    db.close()
  })

  it('matches a word the user has only partly typed', async () => {
    const db = await seeded()

    expect((await searchCachedTasks(db, 'reconcil', opts)).map((r) => r.id)).toEqual([
      'aaaaaaaaaaa',
    ])
    db.close()
  })

  it('ranks a title match above a body match', async () => {
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ id: 'inbody', title: 'Something else', body: 'about billing' }), {
      storage: allOn,
    })
    await cacheTask(db, makeTask({ id: 'intitle', title: 'Billing work', body: 'nothing' }), {
      storage: allOn,
    })

    expect((await searchCachedTasks(db, 'billing', opts)).map((r) => r.id)).toEqual([
      'intitle',
      'inbody',
    ])
    db.close()
  })

  it('leaves out a workspace whose owner turned the cache off', async () => {
    const db = await seeded()
    await cacheTask(db, makeTask({ id: 'ddddddddddd', workspaceId: 'ws2', title: 'Billing in ws2' }), {
      storage: allOn,
    })

    const rows = await searchCachedTasks(db, 'billing', { ...opts, storage: offFor('ws2') })

    expect(rows.map((r) => r.id).sort()).toEqual(['aaaaaaaaaaa', 'bbbbbbbbbbb'])
    db.close()
  })

  it('reports an honest empty when it searched and found nothing', async () => {
    // Empty, not null: the caller must be able to tell "no match here" from
    // "I could not look", because only one of them justifies falling back.
    const db = await seeded()

    expect(await searchCachedTasks(db, 'quarterly', opts)).toEqual([])
    db.close()
  })

  it('declines to answer when it cannot, so the caller falls back', async () => {
    const db = await seeded()

    expect(await searchCachedTasks(null, 'billing', opts)).toBeNull()
    // Nothing but stopwords constrains nothing, and an unconstrained search is
    // just the recent list the caller already has.
    expect(await searchCachedTasks(db, 'the and of', opts)).toBeNull()
    expect(await searchCachedTasks(db, '', opts)).toBeNull()
    expect(await searchCachedTasks(db, '   ', opts)).toBeNull()
    db.close()
  })

  it('caps the list so the panel stays a panel', async () => {
    const db = await openCache({ userId: 'u1', factory })
    for (let i = 0; i < 20; i += 1) {
      await cacheTask(db, makeTask({ id: `id${i}`.padEnd(11, 'x'), title: 'billing' }), {
        storage: allOn,
      })
    }

    expect(await searchCachedTasks(db, 'billing', opts)).toHaveLength(8)
    expect(await searchCachedTasks(db, 'billing', { ...opts, limit: 3 })).toHaveLength(3)
    db.close()
  })
})
