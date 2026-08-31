import { describe, expect, it, vi } from 'vitest'

import { createAuthedFetch } from '../src/composables/useAuthedFetch'

const REFRESH = '/api/v1/auth/refresh'
const res = (status) => ({ status, ok: status >= 200 && status < 300 })

const build = (fetchImpl) => createAuthedFetch({ fetchImpl, refreshUrl: REFRESH })

describe('createAuthedFetch', () => {
  it('passes a successful response straight through', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(res(200))

    expect(await build(fetchImpl)('/api/v1/workspaces')).toEqual(res(200))
    expect(fetchImpl).toHaveBeenCalledOnce()
  })

  it('does not touch a failure that is not about the session', async () => {
    // A 500 or a 404 has nothing to do with the token.
    for (const status of [400, 403, 404, 500]) {
      const fetchImpl = vi.fn().mockResolvedValue(res(status))

      expect(await build(fetchImpl)('/api/v1/workspaces')).toEqual(res(status))
      expect(fetchImpl).toHaveBeenCalledOnce()
    }
  })

  it('renews an expired session and replays the request', async () => {
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401))  // the original
      .mockResolvedValueOnce(res(200))  // the refresh
      .mockResolvedValueOnce(res(200))  // the replay

    expect(await build(fetchImpl)('/api/v1/workspaces')).toEqual(res(200))
    expect(fetchImpl).toHaveBeenNthCalledWith(2, REFRESH, { method: 'POST' })
    expect(fetchImpl).toHaveBeenNthCalledWith(3, '/api/v1/workspaces', undefined)
  })

  it('replays with the original method and body', async () => {
    const init = { method: 'POST', body: '{"a":1}', headers: { 'Content-Type': 'application/json' } }
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401))
      .mockResolvedValueOnce(res(200))
      .mockResolvedValueOnce(res(201))

    await build(fetchImpl)('/api/v1/tasks', init)

    expect(fetchImpl).toHaveBeenNthCalledWith(3, '/api/v1/tasks', init)
  })

  it('gives up when the session cannot be renewed', async () => {
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401))
      .mockResolvedValueOnce(res(401))  // refresh itself rejected

    expect(await build(fetchImpl)('/api/v1/workspaces')).toEqual(res(401))
    expect(fetchImpl).toHaveBeenCalledTimes(2)  // no replay
  })

  it('returns the original failure when the refresh cannot be reached', async () => {
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401))
      .mockRejectedValueOnce(new Error('offline'))

    expect(await build(fetchImpl)('/api/v1/workspaces')).toEqual(res(401))
  })

  it('does not retry a second time when the replay also fails', async () => {
    // Otherwise a server that simply says no becomes an infinite loop.
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401))
      .mockResolvedValueOnce(res(200))
      .mockResolvedValueOnce(res(401))

    expect(await build(fetchImpl)('/api/v1/workspaces')).toEqual(res(401))
    expect(fetchImpl).toHaveBeenCalledTimes(3)
  })

  it('refreshes once for a burst of simultaneous expiries', async () => {
    // The app fires several requests at once, so one expiry means several
    // 401s. Each starting its own refresh would rotate the cookie out from
    // under the others and can sign the user out.
    let refreshes = 0
    const fetchImpl = vi.fn(async (input) => {
      if (input === REFRESH) {
        refreshes += 1
        return res(200)
      }
      return refreshes === 0 ? res(401) : res(200)
    })

    const authed = build(fetchImpl)
    const results = await Promise.all([authed('/a'), authed('/b'), authed('/c')])

    expect(refreshes).toBe(1)
    expect(results.every((r) => r.status === 200)).toBe(true)
  })

  it('refreshes again for a later expiry, rather than only ever once', async () => {
    const fetchImpl = vi.fn()
      .mockResolvedValueOnce(res(401)).mockResolvedValueOnce(res(200)).mockResolvedValueOnce(res(200))
      .mockResolvedValueOnce(res(401)).mockResolvedValueOnce(res(200)).mockResolvedValueOnce(res(200))

    const authed = build(fetchImpl)
    await authed('/a')
    await authed('/b')

    expect(fetchImpl).toHaveBeenCalledTimes(6)
  })

  it('never tries to refresh the refresh call itself', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(res(401))

    expect(await build(fetchImpl)(REFRESH, { method: 'POST' })).toEqual(res(401))
    expect(fetchImpl).toHaveBeenCalledOnce()
  })

  it('copes with a fetch that resolves to nothing', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(undefined)

    expect(await build(fetchImpl)('/a')).toBeUndefined()
  })
})
