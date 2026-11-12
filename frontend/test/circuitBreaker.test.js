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

  it('falls back to its own cooldown when Retry-After is unreadable or missing', async () => {
    const { call, breaker } = rig(res(429, 'soon'))

    await call('/x')
    expect(breaker.retryIn()).toBe(5000)

    const second = rig(res(429))
    await second.call('/x')
    expect(second.breaker.retryIn()).toBe(5000)
  })

  it('honours Retry-After on the overload failure that trips it', async () => {
    const { call, breaker } = rig(res(503), res(503), res(503, '7'))

    for (let i = 0; i < 3; i++) await call('/x')
    expect(breaker.retryIn()).toBe(7000)
  })

  it('lets one probe through after the cooldown and closes when it succeeds', async () => {
    const { call, server, breaker, advance } = rig(res(429), res(200), res(200))
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
      .mockResolvedValueOnce(res(429))
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
      res(429), res(503), networkError(), res(503), res(503), res(503),
    )
    await call('/x')

    for (const cooldown of [10000, 20000, 40000, 60000, 60000]) {
      advance(breaker.retryIn())
      await call('/probe').catch(() => {})
      expect(breaker.retryIn()).toBe(cooldown)
    }
  })

  it('starts again from five seconds once it has closed', async () => {
    const { call, breaker, advance } = rig(res(429), res(503), res(200), res(429))
    await call('/x')
    advance(5000)
    await call('/x') // the probe fails: 10s
    advance(10000)
    await call('/x') // the probe succeeds
    await call('/x')

    expect(breaker.retryIn()).toBe(5000)
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
