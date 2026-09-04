import { describe, it, expect, beforeEach } from 'vitest'
import { IDBFactory, IDBKeyRange } from 'fake-indexeddb'

import {
  DB_VERSION,
  STORE_ATTACHMENTS,
  STORE_META,
  STORE_TASKS,
  STORE_THREADS,
  clearWorkspace,
  databaseName,
  destroyCache,
  getCachedTask,
  listCachedTasks,
  metaKey,
  openCache,
  plain,
  putTask,
  requestResult,
  transactionDone,
  toAttachmentRecords,
  toTaskRecord,
  toThreadRecord,
  workspaceRange,
} from '../src/composables/useLocalCache'

/** A fresh in-memory IndexedDB, so no test can see another's data. */
let factory
beforeEach(() => {
  factory = new IDBFactory()
})

const open = (userId = 'user1') => openCache({ userId, factory })

/** A task in the shape the API sends, attachments and all. */
const makeTask = (over = {}) => ({
  id: '0iCYTqxKOqv',
  workspaceId: 'ws1',
  title: 'Ship the billing reconciliation fix',
  body: 'The nightly job double-counts refunds.',
  status: 'notstarted',
  assignee: 'agent',
  createdBy: 'human',
  createdAt: '2026-09-01T10:00:00Z',
  updatedAt: '2026-09-02T10:00:00Z',
  sortOrder: 1,
  messages: [],
  toolCalls: [],
  attachments: [],
  ...over,
})

/** Four base64 characters carry three bytes, so this is 6 bytes of "file". */
const attachment = (id) => ({ id, filename: `${id}.png`, mimeType: 'image/png', data: 'AAAAAAAA' })

describe('plain', () => {
  it('rebuilds a value as plain arrays and objects', () => {
    expect(plain({ a: [1, { b: 'c' }], d: null })).toEqual({ a: [1, { b: 'c' }], d: null })
  })

  it('survives what IndexedDB would refuse to clone', () => {
    // A Vue reactive object is a Proxy, and handing one to put() raises
    // DataCloneError. Rebuilding the value is what makes the write safe.
    const proxy = new Proxy({ title: 'Held in a ref' }, {})

    expect(plain(proxy)).toEqual({ title: 'Held in a ref' })
  })

  it('drops functions and undefined, which cannot be stored', () => {
    expect(plain({ keep: 1, fn: () => {}, gone: undefined })).toEqual({ keep: 1 })
    expect(plain(() => {})).toBeUndefined()
  })
})

describe('toTaskRecord', () => {
  it('keeps the text a search needs', () => {
    const record = toTaskRecord(makeTask(), ['billing', 'refunds'])

    expect(record.title).toBe('Ship the billing reconciliation fix')
    expect(record.body).toBe('The nightly job double-counts refunds.')
    expect(record.terms).toEqual(['billing', 'refunds'])
    expect(record.status).toBe('notstarted')
  })

  it('carries no conversation and no attachment bytes', () => {
    // The whole reason the schema is split. `data` arrives as base64 inside the
    // task JSON, so a record built by spreading the task would bury file bytes
    // in the row a search has to walk.
    const record = toTaskRecord(
      makeTask({ messages: [{ id: 'm1' }], toolCalls: [{ id: 't1' }], attachments: [attachment('a1')] })
    )

    expect(record.messages).toBeUndefined()
    expect(record.toolCalls).toBeUndefined()
    expect(record.attachments).toBeUndefined()
    expect(JSON.stringify(record)).not.toContain('AAAAAAAA')
    expect(record.attachmentCount).toBe(1)
  })

  it('omits fields the task does not carry rather than storing null', () => {
    const record = toTaskRecord(makeTask({ cronSchedule: undefined, eventId: null }))

    expect('cronSchedule' in record).toBe(false)
    expect('eventId' in record).toBe(false)
  })

  it('defaults the terms so a caller may skip them', () => {
    expect(toTaskRecord(makeTask()).terms).toEqual([])
  })
})

describe('toThreadRecord', () => {
  it('keeps the conversation', () => {
    const thread = toThreadRecord(
      makeTask({ messages: [{ id: 'm1', text: 'hi' }], toolCalls: [{ id: 't1', toolName: 'Bash' }] })
    )

    expect(thread.messages).toEqual([{ id: 'm1', text: 'hi' }])
    expect(thread.toolCalls).toEqual([{ id: 't1', toolName: 'Bash' }])
    expect(thread.taskId).toBe('0iCYTqxKOqv')
  })

  it('reduces an attachment on a message to metadata', () => {
    // Messages carry attachments too, and theirs are base64 in the same way.
    const thread = toThreadRecord(
      makeTask({ messages: [{ id: 'm1', attachments: [attachment('a1')] }] })
    )

    expect(thread.messages[0].attachments).toEqual([
      { id: 'a1', filename: 'a1.png', mimeType: 'image/png', size: 6 },
    ])
    expect(JSON.stringify(thread)).not.toContain('AAAAAAAA')
  })

  it('copes with a task carrying no relations at all', () => {
    const thread = toThreadRecord({ id: 't', workspaceId: 'ws1' })

    expect(thread).toEqual({ workspaceId: 'ws1', taskId: 't', messages: [], toolCalls: [], attachments: [] })
  })
})

describe('toAttachmentRecords', () => {
  it('records one row per attachment, sized from the base64 it dropped', () => {
    const rows = toAttachmentRecords(makeTask({ attachments: [attachment('a1')] }), 1234)

    expect(rows).toEqual([
      {
        id: 'a1',
        filename: 'a1.png',
        mimeType: 'image/png',
        size: 6,
        workspaceId: 'ws1',
        taskId: '0iCYTqxKOqv',
        attachmentId: 'a1',
        lastUsedAt: 1234,
      },
    ])
  })

  it('fills in what a malformed attachment omits', () => {
    const [row] = toAttachmentRecords(makeTask({ attachments: [{ id: 'a1' }] }))

    expect(row.filename).toBe('')
    expect(row.mimeType).toBe('')
    expect(row.size).toBe(0)
    expect(typeof row.lastUsedAt).toBe('number')
  })

  it('has nothing to record for a task with no attachments', () => {
    expect(toAttachmentRecords(makeTask())).toEqual([])
    expect(toAttachmentRecords({ id: 't', workspaceId: 'ws1' })).toEqual([])
  })
})

describe('databaseName and metaKey', () => {
  it('names the database for the signed-in user', () => {
    // A browser origin outlives a session: sign out, sign in as someone else,
    // and a shared name hands over the previous person's task titles.
    expect(databaseName('u1')).toBe('agentrq-cache:u1')
    expect(databaseName('u2')).not.toBe(databaseName('u1'))
  })

  it('prefixes meta keys with the workspace so they clear with it', () => {
    expect(metaKey('ws1', 'lastSyncedAt')).toBe('ws1:lastSyncedAt')
  })
})

describe('workspaceRange', () => {
  it('covers every key in the workspace and nothing beyond it', () => {
    // An empty array sorts above every number, string and date in IndexedDB,
    // so it is the smallest key above every real key in the workspace.
    const range = workspaceRange('ws1', IDBKeyRange)

    expect(range.includes(['ws1', 'aaa'])).toBe(true)
    expect(range.includes(['ws1', 'zzz', 'deep'])).toBe(true)
    expect(range.includes(['ws2', 'aaa'])).toBe(false)
  })
})

describe('openCache', () => {
  it('creates the four stores and the search index', async () => {
    const db = await open()

    expect([...db.objectStoreNames].sort()).toEqual(
      [STORE_ATTACHMENTS, STORE_META, STORE_TASKS, STORE_THREADS].sort()
    )
    expect(db.version).toBe(DB_VERSION)

    const tasks = db.transaction(STORE_TASKS).objectStore(STORE_TASKS)
    expect([...tasks.indexNames].sort()).toEqual(['by_recent', 'by_status', 'by_term', 'by_workspace'])
    expect(tasks.index('by_term').multiEntry).toBe(true)
    db.close()
  })

  it('reports no cache rather than throwing when there is none to open', async () => {
    // A private window can refuse outright. The caller checks for null and goes
    // to the network, which is what it would have done anyway.
    expect(await openCache({ userId: 'u1', factory: null })).toBeNull()
    expect(await openCache({ factory })).toBeNull()
    expect(await openCache()).toBeNull()
  })

  it('reports no cache when opening throws synchronously', async () => {
    const throwing = {
      open() {
        throw new Error('denied in a private window')
      },
    }

    expect(await openCache({ userId: 'u1', factory: throwing })).toBeNull()
  })

  it('reports no cache when the open errors', async () => {
    const req = {}
    const erroring = { open: () => req }
    const opening = openCache({ userId: 'u1', factory: erroring })

    req.onerror()

    expect(await opening).toBeNull()
  })

  it('reports no cache rather than hanging when an older tab blocks the upgrade', async () => {
    // onupgradeneeded stalls while another connection holds the old version.
    // Waiting for it would freeze the view that asked.
    const req = {}
    const blocking = { open: () => req }
    const opening = openCache({ userId: 'u1', factory: blocking })

    req.onblocked()

    expect(await opening).toBeNull()
  })

  it('closes on request so it never becomes the tab blocking someone else', async () => {
    const db = await open()
    let closed = false
    const realClose = db.close.bind(db)
    db.close = () => {
      closed = true
      realClose()
    }

    db.onversionchange()

    expect(closed).toBe(true)
  })

  it('gives different users different databases', async () => {
    const a = await openCache({ userId: 'u1', factory })
    await putTask(a, makeTask())
    a.close()

    const b = await openCache({ userId: 'u2', factory })
    expect(await listCachedTasks(b, 'ws1', IDBKeyRange)).toEqual([])
    b.close()
  })
})

describe('createSchema', () => {
  it('keys every store by the workspace first, which is what makes clearing possible', async () => {
    // Clearing a workspace is a key-range delete, and that only works while the
    // workspace is the leading part of every key. A future store added without
    // that would silently survive a clear.
    const db = await open()
    const tx = db.transaction([STORE_TASKS, STORE_THREADS, STORE_ATTACHMENTS], 'readonly')

    expect(tx.objectStore(STORE_TASKS).keyPath).toEqual(['workspaceId', 'id'])
    expect(tx.objectStore(STORE_THREADS).keyPath).toEqual(['workspaceId', 'taskId'])
    expect(tx.objectStore(STORE_ATTACHMENTS).keyPath).toEqual([
      'workspaceId',
      'taskId',
      'attachmentId',
    ])
    db.close()
  })
})

describe('putTask and getCachedTask', () => {
  it('round-trips a task with its conversation reassembled', async () => {
    const db = await open()
    const task = makeTask({ messages: [{ id: 'm1', text: 'hi' }], toolCalls: [{ id: 't1' }] })

    expect(await putTask(db, task, ['billing'])).toBe(true)
    const back = await getCachedTask(db, 'ws1', '0iCYTqxKOqv')

    expect(back.title).toBe('Ship the billing reconciliation fix')
    expect(back.messages).toEqual([{ id: 'm1', text: 'hi' }])
    expect(back.toolCalls).toEqual([{ id: 't1' }])
    db.close()
  })

  it('writes attachment metadata but never the bytes', async () => {
    const db = await open()
    await putTask(db, makeTask({ attachments: [attachment('a1'), attachment('a2')] }))

    const rows = await new Promise((resolve) => {
      const req = db.transaction(STORE_ATTACHMENTS).objectStore(STORE_ATTACHMENTS).getAll()
      req.onsuccess = () => resolve(req.result)
    })

    expect(rows).toHaveLength(2)
    expect(rows.every((r) => !('data' in r))).toBe(true)
    db.close()
  })

  it('accepts a reactive-shaped object without a clone error', async () => {
    const db = await open()
    const proxied = new Proxy(makeTask(), {})

    expect(await putTask(db, proxied)).toBe(true)
    db.close()
  })

  it('declines to write something that is not a task', async () => {
    const db = await open()

    expect(await putTask(db, null)).toBe(false)
    expect(await putTask(db, { id: 'x' })).toBe(false)
    expect(await putTask(db, { workspaceId: 'ws1' })).toBe(false)
    db.close()
  })

  it('does nothing at all without a database', async () => {
    expect(await putTask(null, makeTask())).toBe(false)
    expect(await getCachedTask(null, 'ws1', 't1')).toBeNull()
    expect(await listCachedTasks(null, 'ws1', IDBKeyRange)).toEqual([])
    expect(await clearWorkspace(null, 'ws1', IDBKeyRange)).toBe(false)
  })

  it('reports a miss for a task it has never seen', async () => {
    const db = await open()
    expect(await getCachedTask(db, 'ws1', 'nope')).toBeNull()
    db.close()
  })

  it('returns empty relations for a task whose thread went missing', async () => {
    const db = await open()
    await putTask(db, makeTask())
    await new Promise((resolve) => {
      const tx = db.transaction(STORE_THREADS, 'readwrite')
      tx.objectStore(STORE_THREADS).delete(['ws1', '0iCYTqxKOqv'])
      tx.oncomplete = resolve
    })

    const back = await getCachedTask(db, 'ws1', '0iCYTqxKOqv')

    expect(back.messages).toEqual([])
    expect(back.toolCalls).toEqual([])
    db.close()
  })
})

describe('listCachedTasks', () => {
  it('returns a workspace newest first', async () => {
    const db = await open()
    await putTask(db, makeTask({ id: 'old', updatedAt: '2026-01-01T00:00:00Z' }))
    await putTask(db, makeTask({ id: 'new', updatedAt: '2026-09-01T00:00:00Z' }))
    await putTask(db, makeTask({ id: 'other', workspaceId: 'ws2' }))

    const rows = await listCachedTasks(db, 'ws1', IDBKeyRange)

    expect(rows.map((r) => r.id)).toEqual(['new', 'old'])
    db.close()
  })

  it('orders a task with no timestamp last rather than throwing', async () => {
    const db = await open()
    await putTask(db, makeTask({ id: 'dated', updatedAt: '2026-09-01T00:00:00Z' }))
    await putTask(db, makeTask({ id: 'undated', updatedAt: undefined }))
    await putTask(db, makeTask({ id: 'undated2', updatedAt: undefined }))

    const ids = (await listCachedTasks(db, 'ws1', IDBKeyRange)).map((r) => r.id)

    expect(ids[0]).toBe('dated')
    expect(ids.slice(1).sort()).toEqual(['undated', 'undated2'])
    db.close()
  })
})

describe('requestResult and transactionDone', () => {
  it('resolve with what the operation produced', async () => {
    const req = {}
    const reading = requestResult(req)
    req.result = 'the row'
    req.onsuccess()
    expect(await reading).toBe('the row')

    const tx = {}
    const writing = transactionDone(tx)
    tx.oncomplete()
    expect(await writing).toBe(true)
  })

  it('reject when the operation fails, aborts, or errors', async () => {
    const req = { error: new Error('read failed') }
    const reading = requestResult(req)
    req.onerror()
    await expect(reading).rejects.toThrow('read failed')

    const errored = { error: new Error('write failed') }
    const writing = transactionDone(errored)
    errored.onerror()
    await expect(writing).rejects.toThrow('write failed')

    const aborted = { error: new Error('quota exceeded') }
    const aborting = transactionDone(aborted)
    aborted.onabort()
    await expect(aborting).rejects.toThrow('quota exceeded')
  })
})

describe('a cache that fails mid-operation', () => {
  // The contract for the whole module: a cache is an optimisation, so its
  // failures never reach a user who only wanted to read their tasks. Every one
  // of these returns the same answer as having had no cache at all.
  const broken = {
    transaction() {
      throw new Error('the connection went away')
    },
  }

  it('reports a write as not written', async () => {
    expect(await putTask(broken, makeTask())).toBe(false)
  })

  it('reports a read as a miss', async () => {
    expect(await getCachedTask(broken, 'ws1', 't1')).toBeNull()
  })

  it('reports a list as empty', async () => {
    expect(await listCachedTasks(broken, 'ws1', IDBKeyRange)).toEqual([])
  })

  it('reports a clear as not cleared', async () => {
    expect(await clearWorkspace(broken, 'ws1', IDBKeyRange)).toBe(false)
  })

  it('does the same when the connection closes underneath it', async () => {
    // A real path: the tab was told to close for an upgrade while a read was
    // in flight.
    const db = await open()
    await putTask(db, makeTask())
    db.close()

    expect(await getCachedTask(db, 'ws1', '0iCYTqxKOqv')).toBeNull()
    expect(await putTask(db, makeTask())).toBe(false)
  })
})

describe('clearWorkspace', () => {
  it('forgets one workspace and leaves the others alone', async () => {
    const db = await open()
    await putTask(db, makeTask({ attachments: [attachment('a1')] }))
    await putTask(db, makeTask({ id: 'keep', workspaceId: 'ws2', attachments: [attachment('a2')] }))
    await new Promise((resolve) => {
      const tx = db.transaction(STORE_META, 'readwrite')
      tx.objectStore(STORE_META).put({ key: metaKey('ws1', 'lastSyncedAt'), value: 1 })
      tx.objectStore(STORE_META).put({ key: metaKey('ws2', 'lastSyncedAt'), value: 2 })
      tx.oncomplete = resolve
    })

    expect(await clearWorkspace(db, 'ws1', IDBKeyRange)).toBe(true)

    expect(await listCachedTasks(db, 'ws1', IDBKeyRange)).toEqual([])
    expect(await getCachedTask(db, 'ws1', '0iCYTqxKOqv')).toBeNull()
    expect((await listCachedTasks(db, 'ws2', IDBKeyRange)).map((r) => r.id)).toEqual(['keep'])

    const meta = await new Promise((resolve) => {
      const req = db.transaction(STORE_META).objectStore(STORE_META).getAll()
      req.onsuccess = () => resolve(req.result)
    })
    expect(meta.map((m) => m.key)).toEqual(['ws2:lastSyncedAt'])

    const atts = await new Promise((resolve) => {
      const req = db.transaction(STORE_ATTACHMENTS).objectStore(STORE_ATTACHMENTS).getAll()
      req.onsuccess = () => resolve(req.result)
    })
    expect(atts.map((a) => a.workspaceId)).toEqual(['ws2'])
    db.close()
  })
})

describe('destroyCache', () => {
  it('removes the whole database, which is what signing out needs', async () => {
    const db = await open()
    await putTask(db, makeTask())
    db.close()

    expect(await destroyCache({ userId: 'user1', factory })).toBe(true)

    const fresh = await open()
    expect(await listCachedTasks(fresh, 'ws1', IDBKeyRange)).toEqual([])
    fresh.close()
  })

  it('has nothing to delete without a user or a factory', async () => {
    expect(await destroyCache({ userId: 'u1', factory: null })).toBe(false)
    expect(await destroyCache({ factory })).toBe(false)
    expect(await destroyCache()).toBe(false)
  })

  it('reports failure rather than throwing when the delete cannot run', async () => {
    const throwing = {
      deleteDatabase() {
        throw new Error('denied')
      },
    }
    expect(await destroyCache({ userId: 'u1', factory: throwing })).toBe(false)

    const req = {}
    const erroring = { deleteDatabase: () => req }
    const erroringRun = destroyCache({ userId: 'u1', factory: erroring })
    req.onerror()
    expect(await erroringRun).toBe(false)

    const blockedReq = {}
    const blocking = { deleteDatabase: () => blockedReq }
    const blockedRun = destroyCache({ userId: 'u1', factory: blocking })
    blockedReq.onblocked()
    expect(await blockedRun).toBe(false)
  })
})
