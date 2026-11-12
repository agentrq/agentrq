// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

import { apiBreaker, createCircuitBreaker } from '../src/utils/circuitBreaker'

const res = (status, retryAfter) => ({
  status,
  ok: status >= 200 && status < 300,
  headers: new Headers(retryAfter === undefined ? {} : { 'Retry-After': retryAfter }),
})

/** A breaker on a clock the test moves, around a server whose answers it scripts. */
function rig(...answers) {
  let t = 0
  const server = vi.fn()
  for (const a of answers) {
    if (a instanceof Error) server.mockRejectedValueOnce(a)
    else server.mockResolvedValueOnce(a)
  }
  const breaker = createCircuitBreaker({ now: () => t })
  const call = breaker.wrap(server)
  return { breaker, call, server, advance: (ms) => { t += ms } }
}

const networkError = () => new TypeError('Failed to fetch')

describe('createCircuitBreaker', () => {
  it('passes calls and their answers straight through while the server is fine', async () => {
    const { call, server, breaker } = rig(res(200), res(404), res(422), res(500))

    for (const status of [200, 404, 422, 500]) {
      expect((await call('/api/v1/x', { method: 'GET' })).status).toBe(status)
    }
    expect(server).toHaveBeenCalledTimes(4)
    expect(server).toHaveBeenCalledWith('/api/v1/x', { method: 'GET' })
    expect(breaker.retryIn()).toBe(0)
  })

  it('opens after three overload failures in a row and fails fast without the server', async () => {
    const { call, server, breaker } = rig(res(502), res(503), res(504))

    for (let i = 0; i < 3; i++) await call('/x')
    expect(breaker.retryIn()).toBe(5000)

    const busy = await call('/x')
    expect(busy.status).toBe(503)
    expect(busy.headers.get('Retry-After')).toBe('5')
    expect(await busy.json()).toEqual({ error: { message: 'server is busy, try again shortly', code: 503 } })
    expect(server).toHaveBeenCalledTimes(3)
  })

  it('counts network errors, and still rethrows them', async () => {
    const { call, breaker } = rig(networkError(), networkError(), networkError())

    for (let i = 0; i < 3; i++) await expect(call('/x')).rejects.toThrow('Failed to fetch')
    expect(breaker.retryIn()).toBe(5000)
  })

  it('does not count a call the app cancelled', async () => {
    const abort = () => Object.assign(new Error('aborted'), { name: 'AbortError' })
    const { call, breaker } = rig(abort(), abort(), abort())

    for (let i = 0; i < 3; i++) await expect(call('/x')).rejects.toThrow('aborted')
    expect(breaker.retryIn()).toBe(0)
  })

  it('needs the failures in a row: an answer in between starts the count again', async () => {
    const { call, breaker } = rig(res(503), res(503), res(200), res(503), res(503))

    for (let i = 0; i < 5; i++) await call('/x')
    expect(breaker.retryIn()).toBe(0)
  })

  it('opens at once on a 429, for as long as Retry-After says', async () => {
    const { call, breaker } = rig(res(429, '42'))

    await call('/x')
    expect(breaker.retryIn()).toBe(42000)
  })

  it('reads Retry-After as an HTTP date too', async () => {
    const { call, breaker } = rig(res(429, new Date(90_000).toUTCString()))

    await call('/x')
    expect(breaker.retryIn()).toBe(90000)
  })

  it('falls back to its own cooldown when Retry-After is unreadable', async () => {
    const { call, breaker } = rig(res(429, 'soon'))

    await call('/x')
    expect(breaker.retryIn()).toBe(5000)
  })

  // A per-user quota (tasks, workspaces, forks, skills, hooks) answers 429
  // with no Retry-After. It limits one kind of call, so the rest of the app
  // must keep working, and it is the server answering, not failing.
  it('does not open on a quota 429, which has no Retry-After', async () => {
    const { call, server, breaker } = rig(res(503), res(503), res(429), res(429), res(503), res(200))

    for (let i = 0; i < 4; i++) await call('/x')
    expect(breaker.retryIn()).toBe(0)
    // It ended the run of failures, too.
    await call('/x')
    expect(breaker.retryIn()).toBe(0)
    expect((await call('/x')).status).toBe(200)
    expect(server).toHaveBeenCalledTimes(6)
  })

  it('honours Retry-After on the overload failure that trips it', async () => {
    const { call, breaker } = rig(res(503), res(503), res(503, '7'))

    for (let i = 0; i < 3; i++) await call('/x')
    expect(breaker.retryIn()).toBe(7000)
  })

  it('lets one probe through after the cooldown and closes when it succeeds', async () => {
    const { call, server, breaker, advance } = rig(res(429, '5'), res(200), res(200))
    await call('/x')
    advance(5000)

    expect((await call('/x')).status).toBe(200)
    expect(breaker.retryIn()).toBe(0)
    expect((await call('/x')).status).toBe(200)
    expect(server).toHaveBeenCalledTimes(3)
  })

  it('holds the other calls back while the probe is out', async () => {
    let answer
    const server = vi.fn()
      .mockResolvedValueOnce(res(429, '5'))
      .mockReturnValueOnce(new Promise((resolve) => { answer = resolve }))
    let t = 0
    const breaker = createCircuitBreaker({ now: () => t })
    const call = breaker.wrap(server)
    await call('/x')
    t = 5000

    const probe = call('/probe')
    const held = await call('/other')
    expect(held.status).toBe(503)
    expect(held.headers.get('Retry-After')).toBe('5')
    expect(server).toHaveBeenCalledTimes(2)

    answer(res(200))
    expect((await probe).status).toBe(200)
  })

  it('re-opens for twice as long when the probe fails, up to a minute', async () => {
    const { call, breaker, advance } = rig(
      res(429, '5'), res(503), networkError(), res(503), res(503), res(503),
    )
    await call('/x')

    for (const cooldown of [10000, 20000, 40000, 60000, 60000]) {
      advance(breaker.retryIn())
      await call('/probe').catch(() => {})
      expect(breaker.retryIn()).toBe(cooldown)
    }
  })

  it('starts again from five seconds once it has closed', async () => {
    const { call, breaker, advance } = rig(res(429, '5'), res(503), res(200), res(429, '5'))
    await call('/x')
    advance(5000)
    await call('/x') // the probe fails: 10s
    advance(10000)
    await call('/x') // the probe succeeds
    await call('/x')

    expect(breaker.retryIn()).toBe(5000)
  })

  it('lets a slow probe hold the others back for five seconds at most', async () => {
    const answers = []
    const server = vi.fn()
      .mockResolvedValueOnce(res(429, '5'))
      .mockImplementation(() => new Promise((resolve) => { answers.push(resolve) }))
    let t = 0
    const breaker = createCircuitBreaker({ now: () => t })
    const call = breaker.wrap(server)
    await call('/x')
    t = 5000

    const slow = call('/hooks/upload') // the probe: a 30s compile
    t = 9000
    const held = await call('/other')
    expect(held.status).toBe(503)
    expect(held.headers.get('Retry-After')).toBe('1')

    // The hold is over: the next call probes too, and holds the rest in turn.
    t = 10000
    const quick = call('/quick')
    expect(server).toHaveBeenCalledTimes(3)
    expect((await call('/other')).status).toBe(503)

    // The slow probe ending does not lift the quick one's hold.
    answers[0](res(200))
    expect((await slow).status).toBe(200)
    expect(breaker.retryIn()).toBe(0)
    answers[1](res(200))
    expect((await quick).status).toBe(200)
    call('/after')
    expect(server).toHaveBeenCalledTimes(4)
  })

  it('keeps the latest probe\'s hold when an older probe is cancelled', async () => {
    const answers = []
    const server = vi.fn()
      .mockResolvedValueOnce(res(429, '5'))
      .mockImplementation(() => new Promise((resolve, reject) => { answers.push({ resolve, reject }) }))
    let t = 0
    const breaker = createCircuitBreaker({ now: () => t })
    const call = breaker.wrap(server)
    await call('/x')
    t = 5000
    const first = call('/first')
    t = 10000
    const second = call('/second')

    answers[0].reject(Object.assign(new Error('aborted'), { name: 'AbortError' }))
    await expect(first).rejects.toThrow('aborted')
    t = 12000
    expect((await call('/other')).status).toBe(503)
    expect(server).toHaveBeenCalledTimes(3)

    answers[1].resolve(res(200))
    await second
    call('/next')
    expect(server).toHaveBeenCalledTimes(4)
  })

  it('drops a probe\'s hold once the breaker closes or trips again', async () => {
    const answers = []
    const server = vi.fn()
      .mockResolvedValueOnce(res(429, '5'))
      .mockImplementation(() => new Promise((resolve) => { answers.push(resolve) }))
    let t = 0
    const breaker = createCircuitBreaker({ now: () => t })
    const call = breaker.wrap(server)
    await call('/x')
    t = 5000
    const first = call('/first')
    t = 10000
    const second = call('/second') // holds until 15s

    answers[0](res(429, '0')) // the first probe is throttled, for no time at all
    await first
    t = 11000
    // The second probe was sent before that trip: its hold is gone with it.
    call('/third')
    expect(server).toHaveBeenCalledTimes(4)
    answers[1](res(200)) // and its answer is about then
    await second
    expect((await call('/fourth')).status).toBe(503)
  })

  it('ignores a success from a call sent before the trip', async () => {
    let answer
    const server = vi.fn()
      .mockReturnValueOnce(new Promise((resolve) => { answer = resolve }))
      .mockResolvedValueOnce(res(503))
      .mockResolvedValueOnce(res(503))
      .mockResolvedValueOnce(res(503))
    const { call, breaker } = (() => {
      const b = createCircuitBreaker({ now: () => 0 })
      return { call: b.wrap(server), breaker: b }
    })()

    const early = call('/slow')
    for (let i = 0; i < 3; i++) await call('/x')
    expect(breaker.retryIn()).toBe(5000)

    answer(res(200))
    expect((await early).status).toBe(200)
    expect(breaker.retryIn()).toBe(5000)
  })

  it('trips once for one overload, however many calls were out', async () => {
    const answers = []
    const server = vi.fn().mockImplementation(() => new Promise((resolve, reject) => { answers.push({ resolve, reject }) }))
    const breaker = createCircuitBreaker({ now: () => 0 })
    const call = breaker.wrap(server)

    const calls = Array.from({ length: 8 }, () => call('/x').catch(() => {}))
    answers.forEach((a, i) => (i === 7 ? a.reject(networkError()) : a.resolve(res(502, i === 6 ? '99' : undefined))))
    await Promise.all(calls)

    expect(breaker.retryIn()).toBe(5000)
  })

  it('ignores a failure from a call sent before the probe closed it', async () => {
    const answers = []
    const server = vi.fn()
      .mockResolvedValueOnce(res(429, '5'))
      .mockImplementation(() => new Promise((resolve) => { answers.push(resolve) }))
    let t = 0
    const breaker = createCircuitBreaker({ now: () => t })
    const call = breaker.wrap(server)
    await call('/x')
    t = 5000
    const slow = call('/slow')
    t = 10000
    const quick = call('/quick')

    answers[1](res(200)) // the later probe closes it
    await quick
    answers[0](res(503)) // the older one fails afterwards: it is about then
    await slow

    expect(breaker.retryIn()).toBe(0)
  })

  it('shares one breaker across the app', () => {
    expect(apiBreaker.retryIn()).toBe(0)
  })

  it('reads the real clock by default', async () => {
    const breaker = createCircuitBreaker()
    await breaker.wrap(vi.fn().mockResolvedValue(res(429, '30')))('/x')

    expect(breaker.retryIn()).toBeGreaterThan(29000)
    expect(breaker.retryIn()).toBeLessThanOrEqual(30000)
  })
})
