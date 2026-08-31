import { describe, expect, it, vi } from 'vitest'

import {
  IDENTITY_TIMEOUT_MS,
  USER_PATH,
  describeIdentity,
  fetchProfileIdentity,
} from '../src/main/identity.js'

const ok = (body) => ({ ok: true, json: async () => body })

describe('fetchProfileIdentity', () => {
  it('reports who a profile is signed in as', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(ok({ name: 'Ada', email: 'ada@example.com', picture: 'p.png' }))

    const identity = await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://a.test' })

    expect(identity).toEqual({ name: 'Ada', email: 'ada@example.com', picture: 'p.png' })
    expect(fetchImpl).toHaveBeenCalledWith(`https://a.test${USER_PATH}`, {})
  })

  it('asks the profile\'s own server, not a shared one', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(ok({ email: 'b@example.com' }))

    await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://b.test' })

    expect(fetchImpl).toHaveBeenCalledWith(`https://b.test${USER_PATH}`, {})
  })

  it('says nothing for a profile that has no server yet', async () => {
    // A freshly added profile, still on the connection screen.
    const fetchImpl = vi.fn()

    expect(await fetchProfileIdentity({ fetchImpl, serverUrl: '' })).toBeNull()
    expect(fetchImpl).not.toHaveBeenCalled()
  })

  it('treats a signed-out profile as ordinary, not an error', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: false, status: 401 })

    expect(await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://a.test' })).toBeNull()
  })

  it('survives a server that cannot be reached', async () => {
    const fetchImpl = vi.fn().mockRejectedValue(new Error('ECONNREFUSED'))

    expect(await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://a.test' })).toBeNull()
  })

  it('survives a response that is not JSON', async () => {
    const fetchImpl = vi.fn().mockResolvedValue({ ok: true, json: async () => { throw new Error('not json') } })

    expect(await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://a.test' })).toBeNull()
  })

  it('survives a fetch that answers with nothing at all', async () => {
    const fetchImpl = vi.fn().mockResolvedValue(undefined)

    expect(await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://a.test' })).toBeNull()
  })

  it('gives up rather than letting one profile hold up the menu', async () => {
    const timeout = vi.fn().mockReturnValue('signal-object')
    const fetchImpl = vi.fn().mockResolvedValue(ok({ email: 'a@example.com' }))

    await fetchProfileIdentity({ fetchImpl, serverUrl: 'https://a.test', timeout })

    expect(timeout).toHaveBeenCalledWith(IDENTITY_TIMEOUT_MS)
    expect(fetchImpl).toHaveBeenCalledWith(`https://a.test${USER_PATH}`, { signal: 'signal-object' })
  })
})

describe('describeIdentity', () => {
  it('keeps only what the switcher shows', () => {
    expect(describeIdentity({ name: ' Ada ', email: ' ada@example.com ', picture: 'p.png', token: 'secret' }))
      .toEqual({ name: 'Ada', email: 'ada@example.com', picture: 'p.png' })
  })

  it('accepts a user with only one of the two', () => {
    expect(describeIdentity({ email: 'a@example.com' })).toEqual({ name: '', email: 'a@example.com', picture: '' })
    expect(describeIdentity({ name: 'Ada' })).toEqual({ name: 'Ada', email: '', picture: '' })
  })

  it('is null when there is nothing worth showing', () => {
    // Indistinguishable from signed out, as far as anyone can see.
    for (const user of [null, undefined, 'a string', 42, {}, { name: '  ', email: '' }, { name: 7, email: {} }]) {
      expect(describeIdentity(user)).toBeNull()
    }
  })
})
