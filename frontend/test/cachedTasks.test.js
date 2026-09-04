import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { IDBFactory, IDBKeyRange } from 'fake-indexeddb'

import {
  CACHE_SETTING_PREFIX,
  SETTING_OFF,
  cacheSettingKey,
  cacheTask,
  cacheTaskEvent,
  cacheTaskUpdate,
  cacheTasks,
  connectCache,
  isCacheEnabled,
  resetSharedCache,
  sharedCache,
} from '../src/composables/useCachedTasks'
import { getCachedTask, listCachedTasks, openCache } from '../src/composables/useLocalCache'

let factory
beforeEach(() => {
  factory = new IDBFactory()
})
afterEach(() => {
  resetSharedCache()
})

/** A localStorage that answers however the test needs. */
const storageWith = (entries = {}) => ({
  getItem: (key) => (key in entries ? entries[key] : null),
})

/** Every workspace cached. The default, so most tests want this. */
const allOn = storageWith({})

const makeTask = (over = {}) => ({
  id: '0iCYTqxKOqv',
  workspaceId: 'ws1',
  title: 'Ship the billing reconciliation fix',
  body: 'The nightly job double-counts refunds.',
  status: 'notstarted',
  updatedAt: '2026-09-02T10:00:00Z',
  messages: [],
  toolCalls: [],
  attachments: [],
  ...over,
})

const attachment = (id) => ({ id, filename: `${id}.png`, mimeType: 'image/png', data: 'AAAAAAAA' })

describe('isCacheEnabled', () => {
  it('is on unless someone turned it off', () => {
    // The setting exists to turn something off, so its absence is consent to
    // the default rather than an unanswered question.
    expect(isCacheEnabled('ws1', allOn)).toBe(true)
    expect(isCacheEnabled('ws1', storageWith({ [cacheSettingKey('ws1')]: 'on' }))).toBe(true)
  })

  it('is off when this workspace was turned off on this device', () => {
    expect(isCacheEnabled('ws1', storageWith({ [cacheSettingKey('ws1')]: SETTING_OFF }))).toBe(false)
  })

  it('answers per workspace, not for all of them at once', () => {
    const storage = storageWith({ [cacheSettingKey('ws1')]: SETTING_OFF })

    expect(isCacheEnabled('ws1', storage)).toBe(false)
    expect(isCacheEnabled('ws2', storage)).toBe(true)
  })

  it('is off when the opt-out cannot be read', () => {
    // Failing to cache costs a little speed. Caching for someone who turned it
    // off breaks a promise made to them, and only one of those is worth risking.
    const throwing = {
      getItem() {
        throw new Error('storage disabled')
      },
    }

    expect(isCacheEnabled('ws1', throwing)).toBe(false)
    expect(isCacheEnabled('ws1', null)).toBe(false)
  })

  it('falls back to this device’s own storage when none is supplied', () => {
    // Deliberately not asserting which answer comes back. `globalThis.localStorage`
    // is a real Storage under some Node and jsdom combinations and absent under
    // others, so pinning the value here passes on one machine and fails on the
    // next. What must hold is that the default resolves to a decision rather
    // than throwing.
    expect(typeof isCacheEnabled('ws1')).toBe('boolean')
  })

  it('is off without a workspace to answer about', () => {
    expect(isCacheEnabled('', allOn)).toBe(false)
    expect(isCacheEnabled(undefined, allOn)).toBe(false)
  })

  it('namespaces the key so two workspaces cannot collide', () => {
    expect(cacheSettingKey('ws1')).toBe(`${CACHE_SETTING_PREFIX}ws1`)
    expect(cacheSettingKey('ws1')).not.toBe(cacheSettingKey('ws2'))
  })
})

describe('cacheTask', () => {
  it('writes the task with its search terms', async () => {
    const db = await openCache({ userId: 'u1', factory })

    expect(await cacheTask(db, makeTask(), { storage: allOn })).toBe(true)

    const [row] = await listCachedTasks(db, 'ws1', IDBKeyRange)
    expect(row.terms).toContain('billing')
    expect(row.terms).toContain('refunds')
    db.close()
  })

  it('writes no attachment bytes', async () => {
    // The trap this whole design is shaped around: `data` is base64 inside the
    // task JSON, and the list endpoint returns it.
    const db = await openCache({ userId: 'u1', factory })

    await cacheTask(db, makeTask({ attachments: [attachment('a1')] }), { storage: allOn })

    const back = await getCachedTask(db, 'ws1', '0iCYTqxKOqv')
    expect(JSON.stringify(back)).not.toContain('AAAAAAAA')
    expect(back.attachmentCount).toBe(1)
    db.close()
  })

  it('writes nothing for a workspace that opted out', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const off = storageWith({ [cacheSettingKey('ws1')]: SETTING_OFF })

    expect(await cacheTask(db, makeTask(), { storage: off })).toBe(false)
    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toEqual([])
    db.close()
  })

  it('writes nothing when there is no cache to write to', async () => {
    expect(await cacheTask(null, makeTask(), { storage: allOn })).toBe(false)
  })

  it('writes nothing for something that is not a task', async () => {
    const db = await openCache({ userId: 'u1', factory })

    expect(await cacheTask(db, null, { storage: allOn })).toBe(false)
    expect(await cacheTask(db, { id: 'x' }, { storage: allOn })).toBe(false)
    db.close()
  })

  it('never breaks the view that triggered it', async () => {
    // A cache is an optimisation. A write that cannot happen is not an error in
    // front of someone who only wanted to read their tasks.
    const broken = {
      transaction() {
        throw new Error('the connection went away')
      },
    }

    await expect(cacheTask(broken, makeTask(), { storage: allOn })).resolves.toBe(false)
  })
})

describe('cacheTasks', () => {
  it('writes a page and reports how many landed', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const page = [makeTask({ id: 'a' }), makeTask({ id: 'b' }), makeTask({ id: 'c' })]

    expect(await cacheTasks(db, page, { storage: allOn })).toBe(3)
    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toHaveLength(3)
    db.close()
  })

  it('skips the workspaces that opted out and keeps the rest', async () => {
    // A global task list spans workspaces, so one page can contain both.
    const db = await openCache({ userId: 'u1', factory })
    const storage = storageWith({ [cacheSettingKey('ws2')]: SETTING_OFF })
    const page = [makeTask({ id: 'a' }), makeTask({ id: 'b', workspaceId: 'ws2' })]

    expect(await cacheTasks(db, page, { storage })).toBe(1)
    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toHaveLength(1)
    expect(await listCachedTasks(db, 'ws2', IDBKeyRange)).toHaveLength(0)
    db.close()
  })

  it('has nothing to write for a list that never arrived', async () => {
    const db = await openCache({ userId: 'u1', factory })

    expect(await cacheTasks(db, undefined, { storage: allOn })).toBe(0)
    expect(await cacheTasks(db, null, { storage: allOn })).toBe(0)
    expect(await cacheTasks(db, [], { storage: allOn })).toBe(0)
    db.close()
  })
})

describe('cacheTaskUpdate', () => {
  it('caches the merged task, not the payload the event carried', async () => {
    // A payload built without its relations is indistinguishable from one whose
    // relations are genuinely empty. Caching the raw one would write an emptied
    // tool lane to disk, where it would outlive the reload that fixes it today.
    const db = await openCache({ userId: 'u1', factory })
    const onScreen = makeTask({ toolCalls: [{ id: 't1' }], messages: [{ id: 'm1' }] })
    const fromEvent = makeTask({ status: 'ongoing', toolCalls: [], messages: [] })

    const merged = cacheTaskUpdate(db, onScreen, fromEvent, { storage: allOn })
    await new Promise((resolve) => setTimeout(resolve, 0))

    expect(merged.status).toBe('ongoing')
    expect(merged.toolCalls).toEqual([{ id: 't1' }])

    const back = await getCachedTask(db, 'ws1', '0iCYTqxKOqv')
    expect(back.toolCalls).toEqual([{ id: 't1' }])
    expect(back.status).toBe('ongoing')
    db.close()
  })

  it('returns the merge so the view and the cache agree', async () => {
    const db = await openCache({ userId: 'u1', factory })

    const merged = cacheTaskUpdate(db, null, makeTask({ title: 'Fresh' }), { storage: allOn })

    expect(merged.title).toBe('Fresh')
    db.close()
  })

  it('caches nothing when the event carried nothing', async () => {
    const db = await openCache({ userId: 'u1', factory })

    expect(cacheTaskUpdate(db, null, null, { storage: allOn })).toBeNull()
    await new Promise((resolve) => setTimeout(resolve, 0))
    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toEqual([])
    db.close()
  })
})

describe('cacheTaskEvent', () => {
  it('merges against what the cache holds, since nothing is on screen', async () => {
    // The shell watches every workspace, so most events it sees are for tasks
    // nobody has open. Writing the payload straight through would overwrite the
    // relations already stored with the ones the payload happens to omit.
    const db = await openCache({ userId: 'u1', factory })
    await cacheTask(db, makeTask({ toolCalls: [{ id: 't1' }] }), { storage: allOn })

    const written = await cacheTaskEvent(db, makeTask({ status: 'ongoing', toolCalls: [] }), {
      storage: allOn,
    })

    expect(written).toBe(true)
    const back = await getCachedTask(db, 'ws1', '0iCYTqxKOqv')
    expect(back.status).toBe('ongoing')
    expect(back.toolCalls).toEqual([{ id: 't1' }])
    db.close()
  })

  it('writes a task the cache has never seen as it arrived', async () => {
    const db = await openCache({ userId: 'u1', factory })

    expect(await cacheTaskEvent(db, makeTask({ toolCalls: [{ id: 't1' }] }), { storage: allOn })).toBe(
      true
    )

    const back = await getCachedTask(db, 'ws1', '0iCYTqxKOqv')
    expect(back.toolCalls).toEqual([{ id: 't1' }])
    db.close()
  })

  it('writes nothing for an opted-out workspace, without even reading it', async () => {
    const db = await openCache({ userId: 'u1', factory })
    const off = storageWith({ [cacheSettingKey('ws1')]: SETTING_OFF })

    expect(await cacheTaskEvent(db, makeTask(), { storage: off })).toBe(false)
    db.close()
  })

  it('writes nothing without a cache or a usable payload', async () => {
    const db = await openCache({ userId: 'u1', factory })

    expect(await cacheTaskEvent(null, makeTask(), { storage: allOn })).toBe(false)
    expect(await cacheTaskEvent(db, null, { storage: allOn })).toBe(false)
    expect(await cacheTaskEvent(db, { id: 'x' }, { storage: allOn })).toBe(false)
    expect(await cacheTaskEvent(db, { workspaceId: 'ws1' }, { storage: allOn })).toBe(false)
    db.close()
  })
})

describe('connectCache', () => {
  it('opens once and hands the same connection to everyone', async () => {
    const db = await connectCache('u1', { factory })

    expect(db).not.toBeNull()
    expect(await connectCache('u1', { factory })).toBe(db)
    expect(sharedCache()).toBe(db)
  })

  it('shares one attempt between callers who ask at the same time', async () => {
    const opened = await Promise.all([
      connectCache('u1', { factory }),
      connectCache('u1', { factory }),
      connectCache('u1', { factory }),
    ])

    expect(opened[0]).toBe(opened[1])
    expect(opened[1]).toBe(opened[2])
  })

  it('drops the previous connection when a different user signs in', async () => {
    // A real sequence on the web, where one origin outlives a session.
    const first = await connectCache('u1', { factory })
    let closed = false
    const realClose = first.close.bind(first)
    first.close = () => {
      closed = true
      realClose()
    }

    const second = await connectCache('u2', { factory })

    expect(closed).toBe(true)
    expect(second).not.toBe(first)
    expect(sharedCache()).toBe(second)
  })

  it('lets a reset during the open win', async () => {
    const opening = connectCache('u1', { factory })
    resetSharedCache()

    expect(await opening).toBeNull()
    expect(sharedCache()).toBeNull()
  })

  it('reports no cache without a user to name the database after', async () => {
    expect(await connectCache('', { factory })).toBeNull()
    expect(await connectCache(undefined, { factory })).toBeNull()
    expect(sharedCache()).toBeNull()
  })

  it('reports no cache when there is none to be had', async () => {
    // A private window, or storage turned off. Callers go to the network, which
    // is what they would have done anyway.
    expect(await connectCache('u1', { factory: null })).toBeNull()
    expect(sharedCache()).toBeNull()
  })

  it('retries after a failed open rather than caching the failure', async () => {
    expect(await connectCache('u1', { factory: null })).toBeNull()

    expect(await connectCache('u1', { factory })).not.toBeNull()
  })

  it('closes cleanly when there was never a connection', () => {
    expect(() => resetSharedCache()).not.toThrow()
    expect(sharedCache()).toBeNull()
  })
})

describe('the rule this module enforces', () => {
  it('offers no way to write a task the server did not send', async () => {
    // There is no queue, no replay and no local-edit path — every export either
    // reads a setting or writes something that arrived from the backend. If a
    // later change adds one, this list is where it shows up.
    const surface = await import('../src/composables/useCachedTasks')
    const writers = Object.keys(surface).filter((name) => /^(cache|connect)/.test(name))

    expect(writers.sort()).toEqual([
      'cacheSettingKey',
      'cacheTask',
      'cacheTaskEvent',
      'cacheTaskUpdate',
      'cacheTasks',
      'connectCache',
    ])
  })
})
