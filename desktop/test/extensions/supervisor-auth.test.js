// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect, vi } from 'vitest'

import { REDIRECT_URI, createSupervisorAuth, readRedirect } from '../../src/main/extensions/supervisor-auth.js'

/**
 * The credential an account-wide extension needs, and the person who has to
 * agree to it.
 *
 * Nothing here happens on its own. An app that quietly acquires a token
 * reaching every workspace because somebody installed something has taken a
 * decision that was not its to take — so this is only ever reached by asking.
 */

const redirect = (params) => `${REDIRECT_URI}?${new URLSearchParams(params)}`

const SERVER = 'http://localhost:3000'

/** What the backend publishes: the core MCP server's endpoints, under /mcp. */
const METADATA = {
  issuer: SERVER,
  registration_endpoint: `${SERVER}/mcp/oauth2/register`,
  authorization_endpoint: `${SERVER}/mcp/oauth2/authorize`,
  token_endpoint: `${SERVER}/mcp/oauth2/token`,
}

function build(over = {}) {
  const calls = []
  const fetchImpl = vi.fn(async (url, init) => {
    calls.push({ url, init })
    if (url.endsWith('/.well-known/oauth-authorization-server')) {
      return { ok: true, status: 200, json: async () => METADATA }
    }
    if (url.endsWith('/oauth2/register')) {
      return { ok: true, status: 201, json: async () => ({ client_id: 'client-1' }) }
    }
    return {
      ok: true,
      status: 200,
      json: async () => ({ access_token: 'at-1', refresh_token: 'rt-1', token_type: 'Bearer' }),
    }
  })

  const auth = createSupervisorAuth({
    serverUrl: () => SERVER,
    fetchImpl,
    openWindow: vi.fn(async () => redirect({ code: 'the-code', state: 'st' })),
    randomState: () => 'st',
    ...over,
  })
  return { auth, fetchImpl, calls }
}

describe('readRedirect', () => {
  it('reads the code out of the redirect', () => {
    expect(readRedirect(redirect({ code: 'abc', state: 'st' }), { state: 'st' })).toEqual({ ok: true, code: 'abc' })
  })

  // State is what ties this answer to the request this app made; an answer to
  // somebody else's request is not one to act on.
  it('refuses an answer to a different request', () => {
    const { ok, reason } = readRedirect(redirect({ code: 'abc', state: 'elsewhere' }), { state: 'st' })

    expect(ok).toBe(false)
    expect(reason).toContain('did not match')
  })

  // Declining is a person saying no, which is a result rather than a failure.
  it('says plainly when somebody declined', () => {
    const { ok, reason } = readRedirect(redirect({ error: 'access_denied', state: 'st' }), { state: 'st' })

    expect(ok).toBe(false)
    expect(reason).toBe('You declined to authorise AgentRQ.')
  })

  it('prefers the description the server gave', () => {
    const url = redirect({ error: 'invalid_request', error_description: 'redirect_uri did not match', state: 'st' })

    expect(readRedirect(url, { state: 'st' }).reason).toBe('redirect_uri did not match')
  })

  it('names an error it has no words of its own for', () => {
    expect(readRedirect(redirect({ error: 'server_error' }), {}).reason).toContain('server_error')
    expect(readRedirect(redirect({ error: 'invalid_scope' }), {}).reason).toContain('does not offer')
  })

  it('says so when there is no code and no error', () => {
    expect(readRedirect(redirect({ state: 'st' }), { state: 'st' }).reason).toContain('without a code')
  })

  it('holds up against something that is not a URL', () => {
    expect(readRedirect('not a url').reason).toContain('not a URL')
    expect(readRedirect(undefined).ok).toBe(false)
  })

  it('does not demand state when none was asked for', () => {
    expect(readRedirect(redirect({ code: 'abc' }), {}).ok).toBe(true)
  })
})

describe('authorize', () => {
  it('registers a client, asks the user, and exchanges the code', async () => {
    const { auth, calls } = build()

    expect(await auth.authorize()).toEqual({ ok: true })

    expect(calls.map((call) => new URL(call.url).pathname)).toEqual([
      '/.well-known/oauth-authorization-server',
      '/mcp/oauth2/register',
      '/mcp/oauth2/token',
    ])
    expect(auth.authorized()).toBe(true)
    expect(auth.token()).toBe('at-1')
  })

  it('registers the redirect it will actually come back to', async () => {
    const { auth, calls } = build()
    await auth.authorize()

    const registered = JSON.parse(calls[1].init.body)
    expect(registered.redirect_uris).toEqual([REDIRECT_URI])
    // A public client: no secret is issued and none is expected.
    expect(registered.token_endpoint_auth_method).toBe('none')
  })

  it('sends the user to the authorisation page with what it registered', async () => {
    const openWindow = vi.fn(async () => redirect({ code: 'the-code', state: 'st' }))
    const { auth } = build({ openWindow })

    await auth.authorize()

    const url = new URL(openWindow.mock.calls[0][0])
    expect(url.pathname).toBe('/mcp/oauth2/authorize')
    expect(url.searchParams.get('client_id')).toBe('client-1')
    expect(url.searchParams.get('redirect_uri')).toBe(REDIRECT_URI)
    expect(url.searchParams.get('response_type')).toBe('code')
    expect(url.searchParams.get('state')).toBe('st')
    // And the window is told which URI ends the flow, so it closes on it.
    expect(openWindow.mock.calls[0][1]).toBe(REDIRECT_URI)
  })

  it('exchanges the code as a public client', async () => {
    const { auth, calls } = build()
    await auth.authorize()

    const body = new URLSearchParams(calls[2].init.body)
    expect(body.get('grant_type')).toBe('authorization_code')
    expect(body.get('code')).toBe('the-code')
    expect(body.get('client_id')).toBe('client-1')
    expect(body.get('redirect_uri')).toBe(REDIRECT_URI)
  })

  // Closing the window is a decision, and it is the commonest one.
  it('says so when the window was closed, and holds nothing', async () => {
    const { auth } = build({ openWindow: vi.fn(async () => null) })

    const result = await auth.authorize()

    expect(result.reason).toContain('closed')
    expect(auth.authorized()).toBe(false)
  })

  it('carries a refusal from the redirect through', async () => {
    const { auth } = build({ openWindow: vi.fn(async () => redirect({ error: 'access_denied', state: 'st' })) })

    expect((await auth.authorize()).reason).toBe('You declined to authorise AgentRQ.')
  })

  it('reports a server that will not register a client', async () => {
    const { auth } = build({ fetchImpl: vi.fn(async () => ({ ok: false, status: 404 })) })

    expect((await auth.authorize()).reason).toContain('could not register')
  })

  it('reports a registration that answered with no client id', async () => {
    const { auth } = build({ fetchImpl: vi.fn(async () => ({ ok: true, json: async () => ({}) })) })

    expect((await auth.authorize()).reason).toContain('no client id')
  })

  it('reports an exchange that failed, or answered with nothing', async () => {
    const refused = build({
      fetchImpl: vi.fn(async (url) =>
        url.endsWith('/oauth2/register')
          ? { ok: true, json: async () => ({ client_id: 'c' }) }
          : { ok: false, status: 400 },
      ),
    })
    expect((await refused.auth.authorize()).reason).toContain('could not complete')

    const empty = build({
      fetchImpl: vi.fn(async (url) => ({
        ok: true,
        json: async () => (url.endsWith('/oauth2/register') ? { client_id: 'c' } : {}),
      })),
    })
    expect((await empty.auth.authorize()).reason).toContain('no access token')
  })

  it('says so when nothing is configured to authorise against', async () => {
    const { auth } = build({ serverUrl: () => '' })

    expect((await auth.authorize()).reason).toBe('No server is configured.')
  })

  // Never throws: this is reached from an extension asking for something, and
  // the answer has to be something a screen can show.
  it('answers with a reason when the network itself fails', async () => {
    const { auth } = build({ fetchImpl: vi.fn(async () => { throw new Error('ECONNREFUSED') }) })

    expect(await auth.authorize()).toEqual({ ok: false, reason: 'ECONNREFUSED' })
  })

  it('has something to say about a failure with no message', async () => {
    const { auth } = build({ fetchImpl: vi.fn(async () => { throw new Error('') }) })

    expect((await auth.authorize()).reason).toBe('The authorisation could not be completed.')
  })

  it('takes a grant that issued no refresh token', async () => {
    // Legitimate: a server need not issue one, and the app then simply asks
    // again when the access token lapses.
    const { auth } = build({
      fetchImpl: vi.fn(async (url) => ({
        ok: true,
        json: async () => (url.endsWith('/oauth2/register') ? { client_id: 'c' } : { access_token: 'at-only' }),
      })),
    })

    expect(await auth.authorize()).toEqual({ ok: true })
    expect(auth.saved()).toEqual({ token: 'at-only', refreshToken: '' })
  })

  // The state only has to be unguessable and different each time; this is the
  // one thing in here with no injected substitute in the real app.
  it('makes its own state when none is provided', async () => {
    const states = new Set()
    for (let i = 0; i < 3; i += 1) {
      const openWindow = vi.fn(async (url) => {
        states.add(new URL(url).searchParams.get('state'))
        return null
      })
      const auth = createSupervisorAuth({
        serverUrl: () => 'http://localhost:3000',
        fetchImpl: vi.fn(async () => ({ ok: true, json: async () => ({ client_id: 'c' }) })),
        openWindow,
      })
      await auth.authorize()
    }

    expect(states.size).toBe(3)
    for (const state of states) expect(state.length).toBeGreaterThan(16)
  })

  // The regression this exists for: the endpoints are under /mcp, and posting
  // to the origin root reached the SPA's GET-only catch-all, which answers a
  // POST with 405 — the person was told AgentRQ could not register.
  it('registers where the core MCP server actually is, with no metadata to read', async () => {
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) return { ok: false, status: 404 }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const openWindow = vi.fn(async () => redirect({ code: 'the-code', state: 'st' }))
    const { auth } = build({ fetchImpl, openWindow })

    expect(await auth.authorize()).toEqual({ ok: true })

    expect(calls.map((call) => new URL(call.url).pathname)).toEqual([
      '/.well-known/oauth-authorization-server',
      '/mcp/oauth2/register',
      '/mcp/oauth2/token',
    ])
    expect(new URL(openWindow.mock.calls[0][0]).pathname).toBe('/mcp/oauth2/authorize')
  })

  // A host that serves the core MCP server at its root says so, and is believed.
  it('follows the endpoints the server publishes', async () => {
    const published = {
      registration_endpoint: `${SERVER}/oauth2/register`,
      authorization_endpoint: `${SERVER}/oauth2/authorize`,
      token_endpoint: `${SERVER}/oauth2/token`,
    }
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) {
        return { ok: true, json: async () => published }
      }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const { auth } = build({ fetchImpl })

    expect(await auth.authorize()).toEqual({ ok: true })
    expect(calls.map((call) => new URL(call.url).pathname)).toEqual([
      '/.well-known/oauth-authorization-server',
      '/oauth2/register',
      '/oauth2/token',
    ])
  })

  // The fetch carries the session cookie, so an endpoint pointing somewhere
  // else is a document talking this app into spending that cookie off-origin.
  it('will not be sent off-origin by the metadata', async () => {
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) {
        return {
          ok: true,
          json: async () => ({
            registration_endpoint: 'https://elsewhere.example/oauth2/register',
            authorization_endpoint: 'https://elsewhere.example/oauth2/authorize',
            token_endpoint: 'https://elsewhere.example/oauth2/token',
          }),
        }
      }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const { auth } = build({ fetchImpl })

    await auth.authorize()

    for (const call of calls) expect(new URL(call.url).origin).toBe(SERVER)
  })

  // Half a document read is a flow that registers in one place and redeems the
  // code in another, which fails later and less legibly.
  it('keeps the known layout when the metadata is only half there', async () => {
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) {
        return { ok: true, json: async () => ({ registration_endpoint: `${SERVER}/oauth2/register` }) }
      }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const { auth } = build({ fetchImpl })

    await auth.authorize()

    expect(new URL(calls[1].url).pathname).toBe('/mcp/oauth2/register')
  })

  // A document can be well-formed JSON and still name nothing followable.
  it('keeps the known layout when an endpoint is not a URL at all', async () => {
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) {
        return {
          ok: true,
          json: async () => ({
            registration_endpoint: 'not a url',
            authorization_endpoint: '',
            token_endpoint: `${SERVER}/oauth2/token`,
          }),
        }
      }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const openWindow = vi.fn(async () => redirect({ code: 'the-code', state: 'st' }))
    const { auth } = build({ fetchImpl, openWindow })

    expect(await auth.authorize()).toEqual({ ok: true })
    expect(new URL(calls[1].url).pathname).toBe('/mcp/oauth2/register')
    expect(new URL(openWindow.mock.calls[0][0]).pathname).toBe('/mcp/oauth2/authorize')
  })

  // A server answering `null`, which is JSON and is not a metadata document.
  it('keeps the known layout when the metadata is not an object', async () => {
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) {
        return { ok: true, json: async () => null }
      }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const { auth } = build({ fetchImpl })

    expect(await auth.authorize()).toEqual({ ok: true })
    expect(new URL(calls[1].url).pathname).toBe('/mcp/oauth2/register')
  })

  // A configured server is a thing people paste, and people paste trailing
  // slashes. `${base}/mcp/...` would otherwise become a double slash.
  it('takes a server URL with a trailing slash', async () => {
    const calls = []
    const fetchImpl = vi.fn(async (url, init) => {
      calls.push({ url, init })
      if (url.endsWith('/.well-known/oauth-authorization-server')) return { ok: false, status: 404 }
      if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
      return { ok: true, json: async () => ({ access_token: 'at' }) }
    })
    const { auth } = build({ fetchImpl, serverUrl: () => `${SERVER}/` })

    expect(await auth.authorize()).toEqual({ ok: true })
    expect(calls[1].url).toBe(`${SERVER}/mcp/oauth2/register`)
  })

  // Asked once. A second flow does not re-read a document that has not moved.
  it('asks the server where its endpoints are only once', async () => {
    const { auth, calls } = build()

    await auth.authorize()
    await auth.authorize()

    const discoveries = calls.filter((call) => call.url.endsWith('/.well-known/oauth-authorization-server'))
    expect(discoveries).toHaveLength(1)
  })

  it('ties each attempt to its own state', async () => {
    // A stale redirect from an abandoned attempt must not satisfy a new one.
    const { auth } = build({
      randomState: () => 'fresh',
      openWindow: vi.fn(async () => redirect({ code: 'abc', state: 'stale' })),
    })

    expect((await auth.authorize()).reason).toContain('did not match')
  })
})

describe('refresh', () => {
  it('trades the refresh token for a new one', async () => {
    const { auth, calls } = build()
    await auth.authorize()
    calls.length = 0

    expect(await auth.refresh()).toEqual({ ok: true })
    expect(new URLSearchParams(calls[0].init.body).get('grant_type')).toBe('refresh_token')
  })

  // A refused refresh means the session ended; holding a token that will not
  // work only delays the question.
  it('forgets everything when the refresh is refused', async () => {
    let refreshing = false
    const { auth } = build({
      fetchImpl: vi.fn(async (url) => {
        if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
        if (refreshing) return { ok: false, status: 401 }
        return { ok: true, json: async () => ({ access_token: 'at', refresh_token: 'rt' }) }
      }),
    })
    await auth.authorize()
    refreshing = true

    const result = await auth.refresh()

    expect(result.reason).toContain('expired')
    expect(auth.authorized()).toBe(false)
  })

  it('forgets when the refresh answers with no token', async () => {
    let refreshing = false
    const { auth } = build({
      fetchImpl: vi.fn(async (url) => {
        if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
        return { ok: true, json: async () => (refreshing ? {} : { access_token: 'at', refresh_token: 'rt' }) }
      }),
    })
    await auth.authorize()
    refreshing = true

    expect((await auth.refresh()).reason).toContain('no access token')
    expect(auth.authorized()).toBe(false)
  })

  it('has nothing to refresh before anybody has authorised', async () => {
    const { auth } = build()

    expect((await auth.refresh()).reason).toContain('nothing to refresh')
  })

  it('answers with a reason when the network fails', async () => {
    const { auth } = build()
    await auth.authorize()
    const broken = build({ fetchImpl: vi.fn(async () => { throw new Error('ETIMEDOUT') }) })
    broken.auth.restore({ token: 'at', refreshToken: 'rt' })

    expect((await broken.auth.refresh()).reason).toBe('ETIMEDOUT')
  })

  it('has something to say about a failure with no message', async () => {
    const { auth } = build({ fetchImpl: vi.fn(async () => { throw new Error('') }) })
    auth.restore({ token: 'at', refreshToken: 'rt' })

    expect((await auth.refresh()).reason).toBe('The authorisation could not be refreshed.')
  })

  it('keeps the refresh token when a new one is not issued', async () => {
    let refreshing = false
    const { auth, calls } = build({
      fetchImpl: vi.fn(async (url) => {
        if (url.endsWith('/oauth2/register')) return { ok: true, json: async () => ({ client_id: 'c' }) }
        return { ok: true, json: async () => (refreshing ? { access_token: 'at-2' } : { access_token: 'at', refresh_token: 'rt' }) }
      }),
    })
    await auth.authorize()
    refreshing = true

    await auth.refresh()

    expect(auth.saved()).toEqual({ token: 'at-2', refreshToken: 'rt' })
    void calls
  })
})

describe('what is held', () => {
  it('starts with nothing', () => {
    const { auth } = build()

    expect(auth.authorized()).toBe(false)
    expect(auth.saved()).toEqual({ token: '', refreshToken: '' })
  })

  it('can be restored from storage without asking anybody', () => {
    const { auth } = build()

    auth.restore({ token: 'at', refreshToken: 'rt' })

    expect(auth.authorized()).toBe(true)
    expect(auth.token()).toBe('at')
  })

  it('copes with a stored shape it does not recognise', () => {
    const { auth } = build()

    auth.restore(undefined)
    auth.restore({})

    expect(auth.authorized()).toBe(false)
  })

  it('forgets on request, so the next call asks again', async () => {
    const { auth } = build()
    await auth.authorize()

    auth.forget()

    expect(auth.authorized()).toBe(false)
    expect(auth.saved()).toEqual({ token: '', refreshToken: '' })
  })
})
