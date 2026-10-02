// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

import {
  ALWAYS_ALLOWED,
  ANSWER_DEADLINE_MS,
  createPermissionBroker,
  permissionKeys,
} from '../../src/main/side-panel/permissions.js'

/** The store's shape, in memory. */
function memoryStore(initial = {}) {
  const record = new Map(Object.entries(initial).map(([origin, p]) => [origin, new Map(Object.entries(p))]))
  return {
    load: vi.fn(async () => {}),
    get: (origin, key) => record.get(origin)?.get(key),
    set: vi.fn(async (origin, key, decision) => {
      if (!record.has(origin)) record.set(origin, new Map())
      record.get(origin).set(key, decision)
    }),
  }
}

/** Timers the test fires by hand. */
function manualTimers() {
  const timers = new Map()
  let next = 1
  return {
    setTimer: (fn, ms) => { const id = next++; timers.set(id, { fn, ms }); return id },
    clearTimer: (id) => timers.delete(id),
    fireAll: () => { for (const [id, { fn }] of [...timers]) { timers.delete(id); fn() } },
    timers,
  }
}

function setup(initial) {
  const store = memoryStore(initial)
  const asked = []
  const clock = manualTimers()
  const broker = createPermissionBroker({ store, ask: (q) => asked.push(q), setTimer: clock.setTimer, clearTimer: clock.clearTimer })
  return { broker, store, asked, clock }
}

const guest = (url = 'https://meet.example/room', id = 7) => ({ id, getURL: () => url })

describe('permissionKeys', () => {
  it('splits media into the camera and the microphone', () => {
    expect(permissionKeys('media', { mediaTypes: ['video', 'audio'] })).toEqual(['camera', 'microphone'])
    expect(permissionKeys('media', { mediaTypes: ['audio'] })).toEqual(['microphone'])
    expect(permissionKeys('media', { mediaType: 'video' })).toEqual(['camera'])
    expect(permissionKeys('media', {})).toEqual([])
    expect(permissionKeys('media')).toEqual([])
  })

  it('asks one question for the names that are one to a person', () => {
    expect(permissionKeys('geolocation-approximate')).toEqual(['geolocation'])
    expect(permissionKeys('midiSysex')).toEqual(['midi'])
    expect(permissionKeys('top-level-storage-access')).toEqual(['storage-access'])
    expect(permissionKeys('pointerLock')).toEqual(['pointer-lock'])
  })

  it('has nothing to ask about a permission it does not know', () => {
    expect(permissionKeys('hid')).toEqual([])
    expect(permissionKeys('a-permission-from-next-year')).toEqual([])
  })
})

describe('the permission broker', () => {
  it('asks the person, and remembers what they answer', async () => {
    const { broker, store, asked } = setup()
    const callback = vi.fn()
    await broker.request(guest(), 'media', callback, { mediaTypes: ['video', 'audio'], requestingUrl: 'https://meet.example/room' })

    expect(asked).toEqual([{ id: 1, guestId: 7, origin: 'https://meet.example', permissions: ['camera', 'microphone'] }])
    expect(callback).not.toHaveBeenCalled()

    expect(await broker.answer(1, 'allow')).toBe(true)
    expect(callback).toHaveBeenCalledWith(true)
    expect(store.get('https://meet.example', 'camera')).toBe('allow')
    expect(store.get('https://meet.example', 'microphone')).toBe('allow')
  })

  it('answers from memory without asking again', async () => {
    const { broker, asked } = setup({ 'https://meet.example': { camera: 'allow', notifications: 'block' } })
    const allowed = vi.fn()
    const blocked = vi.fn()
    await broker.request(guest(), 'media', allowed, { mediaTypes: ['video'] })
    await broker.request(guest(), 'notifications', blocked)
    expect(allowed).toHaveBeenCalledWith(true)
    expect(blocked).toHaveBeenCalledWith(false)
    expect(asked).toEqual([])
  })

  it('asks only when part of a request is undecided, and a block anywhere is a block', async () => {
    const { broker, asked } = setup({ 'https://meet.example': { camera: 'allow' } })
    await broker.request(guest(), 'media', vi.fn(), { mediaTypes: ['video', 'audio'] })
    expect(asked).toHaveLength(1)

    const blocked = setup({ 'https://meet.example': { camera: 'block' } })
    const callback = vi.fn()
    await blocked.broker.request(guest(), 'media', callback, { mediaTypes: ['video', 'audio'] })
    expect(callback).toHaveBeenCalledWith(false)
    expect(blocked.asked).toEqual([])
  })

  it('remembers a Block as firmly as an Allow', async () => {
    const { broker, store } = setup()
    const callback = vi.fn()
    await broker.request(guest(), 'geolocation', callback)
    await broker.answer(1, 'block')
    expect(callback).toHaveBeenCalledWith(false)
    expect(store.get('https://meet.example', 'geolocation')).toBe('block')
  })

  it('treats any answer but allow as a block', async () => {
    const { broker, store } = setup()
    const callback = vi.fn()
    await broker.request(guest(), 'notifications', callback)
    await broker.answer(1, 'whatever')
    expect(callback).toHaveBeenCalledWith(false)
    expect(store.get('https://meet.example', 'notifications')).toBe('block')
  })

  it('allows what a browser allows without asking', async () => {
    const { broker, asked } = setup()
    for (const permission of ALWAYS_ALLOWED) {
      const callback = vi.fn()
      await broker.request(guest(), permission, callback)
      expect(callback, permission).toHaveBeenCalledWith(true)
    }
    expect(asked).toEqual([])
  })

  it('refuses an unknown permission without asking', async () => {
    const { broker, asked } = setup()
    const callback = vi.fn()
    await broker.request(guest(), 'a-permission-from-next-year', callback)
    expect(callback).toHaveBeenCalledWith(false)
    expect(asked).toEqual([])
  })

  it('refuses a request it cannot attribute to a site', async () => {
    const { broker } = setup()
    const callback = vi.fn()
    await broker.request({ id: 1, getURL: () => 'not a url' }, 'notifications', callback)
    expect(callback).toHaveBeenCalledWith(false)
    const noGuest = vi.fn()
    await broker.request(undefined, 'notifications', noGuest)
    expect(noGuest).toHaveBeenCalledWith(false)
  })

  it('still asks for a request that names its site but no page', async () => {
    const { broker, asked } = setup()
    await broker.request(undefined, 'notifications', vi.fn(), { requestingUrl: 'https://meet.example/' })
    expect(asked[0]).toMatchObject({ guestId: 0, origin: 'https://meet.example' })
  })

  it('blocks a question nobody answers, after the deadline, without remembering it', async () => {
    const { broker, store, clock } = setup()
    const callback = vi.fn()
    await broker.request(guest(), 'notifications', callback)
    expect([...clock.timers.values()][0].ms).toBe(ANSWER_DEADLINE_MS)

    clock.fireAll()
    expect(callback).toHaveBeenCalledWith(false)
    expect(store.set).not.toHaveBeenCalled()
    expect(await broker.answer(1, 'allow')).toBe(false)
  })

  it('replaces a waiting question with a newer one from the same page', async () => {
    const { broker, asked, clock } = setup()
    const first = vi.fn()
    const second = vi.fn()
    await broker.request(guest(), 'notifications', first)
    await broker.request(guest(), 'geolocation', second)
    expect(first).toHaveBeenCalledWith(false)
    expect(second).not.toHaveBeenCalled()
    expect(asked.map((q) => q.id)).toEqual([1, 2])
    expect(clock.timers.size).toBe(1)
  })

  it('keeps questions from different pages apart', async () => {
    const { broker } = setup()
    const one = vi.fn()
    const two = vi.fn()
    await broker.request(guest('https://a.example/', 1), 'notifications', one)
    await broker.request(guest('https://b.example/', 2), 'notifications', two)
    expect(one).not.toHaveBeenCalled()
    broker.cancelFor(1)
    expect(one).toHaveBeenCalledWith(false)
    expect(two).not.toHaveBeenCalled()
  })

  it('blocks what a page was asking when it goes away', async () => {
    const { broker, store } = setup()
    const callback = vi.fn()
    await broker.request(guest(), 'notifications', callback)
    broker.cancelFor(7)
    expect(callback).toHaveBeenCalledWith(false)
    expect(store.set).not.toHaveBeenCalled()
    broker.cancelFor(7)
  })

  it('answers a page that only checks from the same record', () => {
    const { broker } = setup({ 'https://meet.example': { camera: 'allow', notifications: 'block' } })
    expect(broker.check(null, 'media', 'https://meet.example', { mediaType: 'video' })).toBe(true)
    expect(broker.check(null, 'media', 'https://meet.example', { mediaType: 'audio' })).toBe(false)
    expect(broker.check(null, 'notifications', 'https://meet.example')).toBe(false)
    expect(broker.check(null, 'notifications', 'https://other.example')).toBe(false)
    expect(broker.check(null, 'clipboard-sanitized-write', 'https://other.example')).toBe(true)
    expect(broker.check(null, 'hid', 'https://meet.example')).toBe(false)
    expect(broker.check(null, 'notifications', '')).toBe(false)
  })

  it('names an Extension page by its own origin', async () => {
    const { broker, asked } = setup()
    await broker.request(guest('agentrq-ext://notes/panel/index.html'), 'notifications', vi.fn())
    expect(asked[0].origin).toBe('agentrq-ext://notes')
  })

  it('says when each question stops waiting, however it ended', async () => {
    const store = memoryStore()
    const settled = vi.fn()
    const clock = manualTimers()
    const broker = createPermissionBroker({ store, ask: () => {}, settled, setTimer: clock.setTimer, clearTimer: clock.clearTimer })
    await broker.request(guest(), 'notifications', vi.fn())
    await broker.answer(1, 'allow')
    await broker.request(guest(), 'geolocation', vi.fn())
    clock.fireAll()
    await broker.request(guest(), 'midi', vi.fn())
    broker.cancelFor(7)
    expect(settled.mock.calls).toEqual([[1], [2], [3]])
  })

  it('uses the real timers by default', async () => {
    vi.useFakeTimers()
    try {
      const callback = vi.fn()
      const broker = createPermissionBroker({ store: memoryStore(), ask: () => {} })
      await broker.request(guest(), 'notifications', callback)
      vi.advanceTimersByTime(ANSWER_DEADLINE_MS)
      expect(callback).toHaveBeenCalledWith(false)
    } finally {
      vi.useRealTimers()
    }
  })
})
