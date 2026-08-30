import { describe, it, expect, vi } from 'vitest'

import {
  CONFIG_PROBE_PATH,
  CONFIG_VERSION,
  DEFAULT_SERVER_URL,
  createServerConfigStore,
  migrateConfig,
  normalizeServerUrl,
  resolveServerUrl,
  validateServerUrl,
} from '../src/main/server-config.js'

describe('normalizeServerUrl', () => {
  it('keeps an explicit http or https URL', () => {
    expect(normalizeServerUrl('https://agentrq.example.com')).toEqual({
      ok: true,
      url: 'https://agentrq.example.com',
    })
    expect(normalizeServerUrl('http://localhost:3000')).toEqual({ ok: true, url: 'http://localhost:3000' })
  })

  it('assumes http for a bare host, which is the self-hosted case', () => {
    expect(normalizeServerUrl('localhost:3000')).toEqual({ ok: true, url: 'http://localhost:3000' })
    expect(normalizeServerUrl('192.168.1.20:8080')).toEqual({ ok: true, url: 'http://192.168.1.20:8080' })
  })

  it('trims surrounding whitespace', () => {
    expect(normalizeServerUrl('  https://agentrq.example.com  ').url).toBe('https://agentrq.example.com')
  })

  it('strips the trailing slash so /api/v1 resolves predictably', () => {
    expect(normalizeServerUrl('https://agentrq.example.com/').url).toBe('https://agentrq.example.com')
  })

  it('preserves a base path, which reverse-proxied deployments rely on', () => {
    expect(normalizeServerUrl('https://example.com/agentrq').url).toBe('https://example.com/agentrq')
  })

  it('drops a query or fragment rather than losing it silently later', () => {
    expect(normalizeServerUrl('https://example.com/?a=1#x').url).toBe('https://example.com')
  })

  it('rejects an empty value', () => {
    expect(normalizeServerUrl('')).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(normalizeServerUrl('   ')).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(normalizeServerUrl(null)).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(normalizeServerUrl(undefined)).toEqual({ ok: false, reason: 'Server URL is required' })
  })

  it('rejects a scheme that is not http or https', () => {
    // A stored value must never be able to smuggle in file:// or javascript:.
    expect(normalizeServerUrl('file:///etc/passwd')).toEqual({
      ok: false,
      reason: 'Server URL must use http or https',
    })
    expect(normalizeServerUrl('javascript://alert(1)')).toEqual({
      ok: false,
      reason: 'Server URL must use http or https',
    })
  })

  it('rejects a URL with no host', () => {
    // http and https are "special" schemes: the parser itself refuses an empty
    // host, so this never reaches the scheme check.
    expect(normalizeServerUrl('http://')).toEqual({ ok: false, reason: 'Not a valid URL' })
  })

  it('rejects unparseable input', () => {
    expect(normalizeServerUrl('http://[')).toEqual({ ok: false, reason: 'Not a valid URL' })
  })
})

describe('resolveServerUrl', () => {
  it('falls back to localhost when nothing is configured', () => {
    expect(resolveServerUrl()).toBe(DEFAULT_SERVER_URL)
    expect(resolveServerUrl({})).toBe(DEFAULT_SERVER_URL)
    expect(resolveServerUrl({ env: {}, stored: '' })).toBe(DEFAULT_SERVER_URL)
  })

  it('prefers the environment override, so a scratch backend needs no stored change', () => {
    expect(resolveServerUrl({ env: { AGENTRQ_SERVER_URL: 'http://localhost:3999' }, stored: 'https://prod.example.com' }))
      .toBe('http://localhost:3999')
  })

  it('uses the stored value when there is no override', () => {
    expect(resolveServerUrl({ env: {}, stored: 'https://prod.example.com' })).toBe('https://prod.example.com')
  })

  it('normalises whatever it takes', () => {
    expect(resolveServerUrl({ env: {}, stored: 'example.com/' })).toBe('http://example.com')
  })

  it('skips an invalid candidate rather than failing the launch', () => {
    // A corrupted stored value should not leave the app with no server at all.
    expect(resolveServerUrl({ env: { AGENTRQ_SERVER_URL: 'file:///x' }, stored: 'https://ok.example.com' }))
      .toBe('https://ok.example.com')
    expect(resolveServerUrl({ env: {}, stored: 'file:///x' })).toBe(DEFAULT_SERVER_URL)
  })
})

describe('migrateConfig', () => {
  it('passes a current-shape config through', () => {
    expect(migrateConfig({ version: 2, serverUrl: 'https://example.com', mutedWorkspaces: ['ws1'] })).toEqual({
      version: CONFIG_VERSION,
      serverUrl: 'https://example.com',
      mutedWorkspaces: ['ws1'],
    })
  })

  it('adopts an unversioned file, which predates the version field', () => {
    expect(migrateConfig({ serverUrl: 'example.com' })).toEqual({
      version: CONFIG_VERSION,
      serverUrl: 'http://example.com',
      mutedWorkspaces: [],
    })
  })

  it('migrates a v1 file forward by defaulting the new field', () => {
    // v1 had no mute list, and notifying for everything is what it did.
    expect(migrateConfig({ version: 1, serverUrl: 'https://example.com' })).toEqual({
      version: CONFIG_VERSION,
      serverUrl: 'https://example.com',
      mutedWorkspaces: [],
    })
  })

  it('discards a mute list that is not a list of ids', () => {
    for (const bad of ['ws1', 42, { ws1: true }, null]) {
      expect(migrateConfig({ version: 2, serverUrl: 'https://e.com', mutedWorkspaces: bad }).mutedWorkspaces)
        .toEqual([])
    }
    expect(migrateConfig({ version: 2, serverUrl: 'https://e.com', mutedWorkspaces: ['ok', '', 7, null] })
      .mutedWorkspaces).toEqual(['ok'])
  })

  it('normalises whatever it finds rather than trusting it', () => {
    expect(migrateConfig({ version: 1, serverUrl: 'https://example.com/' }).serverUrl).toBe('https://example.com')
  })

  it('keeps only what it understands from a newer file', () => {
    // Written by a build that knows keys this one does not: take the server
    // URL, drop the rest rather than guessing at its meaning.
    expect(migrateConfig({ version: 99, serverUrl: 'https://example.com', theme: 'dark' })).toEqual({
      version: CONFIG_VERSION,
      serverUrl: 'https://example.com',
      mutedWorkspaces: [],
    })
  })

  it('degrades to unconfigured for anything unusable', () => {
    // Sending the user to the connection screen is mildly annoying; booting
    // pointed at a URL we could not parse is worse.
    for (const raw of [null, undefined, 'a string', 42, [], { serverUrl: 'file:///x' }, {}]) {
      expect(migrateConfig(raw)).toEqual({ version: CONFIG_VERSION, serverUrl: '', mutedWorkspaces: [] })
    }
  })
})

describe('createServerConfigStore', () => {
  function makeStore(initial = null) {
    let contents = initial
    const writeFile = vi.fn(async (next) => { contents = next })
    const store = createServerConfigStore({
      readFile: async () => {
        if (contents === null) throw new Error('ENOENT')
        return contents
      },
      writeFile,
    })
    return { store, writeFile, read: () => contents }
  }

  it('reads a missing file as not configured — the first run', async () => {
    const { store } = makeStore(null)
    expect(await store.load()).toEqual({ version: CONFIG_VERSION, serverUrl: '', mutedWorkspaces: [] })
  })

  it('reads a stored server back', async () => {
    const { store } = makeStore(JSON.stringify({ version: 1, serverUrl: 'https://example.com' }))
    expect((await store.load()).serverUrl).toBe('https://example.com')
  })

  it('treats corrupt JSON as not configured', async () => {
    // A half-written file must not stop the app from starting.
    const { store } = makeStore('{ not json')
    expect(await store.load()).toEqual({ version: CONFIG_VERSION, serverUrl: '', mutedWorkspaces: [] })
  })

  it('saves a normalised URL and stamps the version', async () => {
    const { store, read } = makeStore(null)

    expect(await store.save('example.com/')).toEqual({ ok: true, url: 'http://example.com' })
    expect(JSON.parse(read())).toEqual({
      version: CONFIG_VERSION,
      serverUrl: 'http://example.com',
      mutedWorkspaces: [],
    })
  })

  it('refuses to store an invalid URL', async () => {
    const { store, writeFile } = makeStore(null)

    expect(await store.save('file:///etc/passwd')).toEqual({
      ok: false,
      reason: 'Server URL must use http or https',
    })
    expect(writeFile).not.toHaveBeenCalled()
  })

  it('round-trips a save through a load', async () => {
    const { store } = makeStore(null)
    await store.save('https://example.com')
    expect((await store.load()).serverUrl).toBe('https://example.com')
  })

  it('keeps mute choices when the server changes', async () => {
    // Switching server is not a reason to start notifying about workspaces the
    // user deliberately silenced.
    const { store } = makeStore(
      JSON.stringify({ version: 2, serverUrl: 'https://a.example.com', mutedWorkspaces: ['ws1'] })
    )

    await store.save('https://b.example.com')

    expect(await store.load()).toEqual({
      version: CONFIG_VERSION,
      serverUrl: 'https://b.example.com',
      mutedWorkspaces: ['ws1'],
    })
  })

  it('mutes and unmutes a single workspace', async () => {
    const { store } = makeStore(JSON.stringify({ version: 2, serverUrl: 'https://e.com', mutedWorkspaces: [] }))

    expect(await store.setWorkspaceMuted('ws1', true)).toEqual(['ws1'])
    expect(await store.setWorkspaceMuted('ws2', true)).toEqual(['ws1', 'ws2'])
    expect(await store.setWorkspaceMuted('ws1', false)).toEqual(['ws2'])
    expect((await store.load()).mutedWorkspaces).toEqual(['ws2'])
  })

  it('does not list a workspace twice when muted again', async () => {
    const { store } = makeStore(JSON.stringify({ version: 2, serverUrl: 'https://e.com', mutedWorkspaces: ['ws1'] }))

    expect(await store.setWorkspaceMuted('ws1', true)).toEqual(['ws1'])
  })

  it('replaces the whole mute list, dropping junk entries', async () => {
    const { store } = makeStore(null)

    expect(await store.setMutedWorkspaces(['ws1', 'ws1', '', null, 'ws2'])).toEqual(['ws1', 'ws2'])
    expect(await store.setMutedWorkspaces()).toEqual([])
  })

  it('clears the stored server, sending the app back to the first-run screen', async () => {
    const { store } = makeStore(JSON.stringify({ version: 1, serverUrl: 'https://example.com' }))

    await store.clear()

    expect((await store.load()).serverUrl).toBe('')
  })
})

describe('validateServerUrl', () => {
  const authConfig = { basePath: '', rootLoginEnabled: true, githubLoginEnabled: false }
  const okResponse = () => ({ ok: true, status: 200, json: async () => authConfig })

  it('probes the auth config endpoint on the normalised URL', async () => {
    const fetchImpl = vi.fn(async () => okResponse())

    const result = await validateServerUrl('localhost:3000/', fetchImpl)

    expect(fetchImpl).toHaveBeenCalledWith(`http://localhost:3000${CONFIG_PROBE_PATH}`)
    expect(result).toEqual({ ok: true, url: 'http://localhost:3000', config: authConfig })
  })

  it('rejects a malformed URL without reaching the network', async () => {
    const fetchImpl = vi.fn()

    expect(await validateServerUrl('', fetchImpl)).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(fetchImpl).not.toHaveBeenCalled()
  })

  it('reports an unreachable host', async () => {
    const fetchImpl = async () => { throw new Error('ECONNREFUSED') }

    const result = await validateServerUrl('localhost:9', fetchImpl)

    expect(result.ok).toBe(false)
    expect(result.reason).toBe('Could not reach http://localhost:9')
    expect(result.detail).toBe('ECONNREFUSED')
  })

  it('reports a thrown value that is not an Error', async () => {
    const result = await validateServerUrl('localhost:9', async () => { throw 'socket hang up' })
    expect(result.detail).toBe('socket hang up')
  })

  it('reports a non-OK status', async () => {
    const result = await validateServerUrl('example.com', async () => ({ ok: false, status: 502 }))
    expect(result).toEqual({ ok: false, reason: 'Server answered with 502' })
  })

  it('rejects a host that answers but is not AgentRQ', async () => {
    // Being up is not the same as being the right server: a bare 200 with a
    // JSON body could be anything.
    const notJson = { ok: true, status: 200, json: async () => { throw new Error('not json') } }
    expect(await validateServerUrl('example.com', async () => notJson)).toEqual({
      ok: false,
      reason: 'That URL is not an AgentRQ server',
    })

    const wrongShape = { ok: true, status: 200, json: async () => ({ hello: 'world' }) }
    expect(await validateServerUrl('example.com', async () => wrongShape)).toEqual({
      ok: false,
      reason: 'That URL is not an AgentRQ server',
    })
  })

  it('accepts a server that advertises only github login', async () => {
    const res = { ok: true, status: 200, json: async () => ({ githubLoginEnabled: true }) }
    expect((await validateServerUrl('example.com', async () => res)).ok).toBe(true)
  })
})
