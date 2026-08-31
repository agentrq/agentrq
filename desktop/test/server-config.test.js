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
  const onlyProfile = (raw) => migrateConfig(raw).profiles[0]

  it('turns a pre-profiles config into one profile that keeps its settings', () => {
    // Upgrading must not look like being reconfigured.
    const config = migrateConfig({ version: 2, serverUrl: 'https://example.com', mutedWorkspaces: ['ws1'] })

    expect(config.version).toBe(CONFIG_VERSION)
    expect(config.profiles).toHaveLength(1)
    expect(config.profiles[0]).toMatchObject({
      serverUrl: 'https://example.com',
      mutedWorkspaces: ['ws1'],
    })
    expect(config.activeProfileId).toBe(config.profiles[0].id)
  })

  it('adopts an unversioned file, which predates the version field', () => {
    expect(onlyProfile({ serverUrl: 'example.com' }).serverUrl).toBe('http://example.com')
  })

  it('migrates a v1 file forward by defaulting the new fields', () => {
    // v1 had no mute list, and notifying for everything is what it did.
    expect(onlyProfile({ version: 1, serverUrl: 'https://example.com' })).toMatchObject({
      serverUrl: 'https://example.com',
      mutedWorkspaces: [],
    })
  })

  it('discards a mute list that is not a list of ids', () => {
    for (const bad of ['ws1', 42, { ws1: true }, null]) {
      expect(onlyProfile({ version: 2, serverUrl: 'https://e.com', mutedWorkspaces: bad }).mutedWorkspaces)
        .toEqual([])
    }
    expect(onlyProfile({ version: 2, serverUrl: 'https://e.com', mutedWorkspaces: ['ok', '', 7, null] })
      .mutedWorkspaces).toEqual(['ok'])
  })

  it('normalises whatever it finds rather than trusting it', () => {
    expect(onlyProfile({ version: 1, serverUrl: 'https://example.com/' }).serverUrl).toBe('https://example.com')
  })

  it('re-checks every stored profile URL, not just a legacy one', () => {
    // The profile model knows nothing about URLs, so this is the only place a
    // value read off disk is checked before it becomes a fetch base.
    const config = migrateConfig({
      profiles: [
        { id: 'one', label: 'A', serverUrl: 'file:///etc/passwd' },
        { id: 'two', label: 'B', serverUrl: 'example.com/' },
      ],
    })

    expect(config.profiles.map((p) => p.serverUrl)).toEqual(['', 'http://example.com'])
  })

  it('keeps only what it understands from a newer file', () => {
    const config = migrateConfig({ version: 99, serverUrl: 'https://example.com', theme: 'dark' })

    expect(config.profiles[0].serverUrl).toBe('https://example.com')
    expect(config.profiles[0]).not.toHaveProperty('theme')
  })

  it('degrades to unconfigured for anything unusable', () => {
    // Sending the user to the connection screen is mildly annoying; booting
    // pointed at a URL we could not parse is worse.
    for (const raw of [null, undefined, 'a string', 42, [], { serverUrl: 'file:///x' }, {}]) {
      const config = migrateConfig(raw)
      expect(config.profiles).toHaveLength(1)
      expect(config.profiles[0].serverUrl).toBe('')
    }
  })

  it('always leaves exactly one profile active', () => {
    const config = migrateConfig({
      profiles: [{ id: 'one', label: 'A' }, { id: 'two', label: 'B' }],
      activeProfileId: 'gone',
    })

    expect(config.profiles.some((p) => p.id === config.activeProfileId)).toBe(true)
  })
})

describe('createServerConfigStore', () => {
  function makeStore(initial = null) {
    let contents = initial
    let nextId = 0
    const writeFile = vi.fn(async (next) => { contents = next })
    const store = createServerConfigStore({
      readFile: async () => {
        if (contents === null) throw new Error('ENOENT')
        return contents
      },
      writeFile,
      newProfileId: () => `p${++nextId}`,
    })
    return { store, writeFile, read: () => contents }
  }

  const legacy = (extra = {}) =>
    JSON.stringify({ version: 2, serverUrl: 'https://example.com', ...extra })

  it('reads a missing file as not configured — the first run', async () => {
    const { store } = makeStore(null)
    const config = await store.load()

    expect(config.version).toBe(CONFIG_VERSION)
    expect(config.profiles).toHaveLength(1)
    expect(config.profiles[0].serverUrl).toBe('')
  })

  it('reads a stored server back', async () => {
    const { store } = makeStore(JSON.stringify({ version: 1, serverUrl: 'https://example.com' }))
    expect((await store.active()).serverUrl).toBe('https://example.com')
  })

  it('treats corrupt JSON as not configured', async () => {
    // A half-written file must not stop the app from starting.
    const { store } = makeStore('{ not json')
    expect((await store.active()).serverUrl).toBe('')
  })

  it('saves a normalised URL and stamps the version', async () => {
    const { store, read } = makeStore(null)

    expect(await store.save('example.com/')).toEqual({ ok: true, url: 'http://example.com' })
    const written = JSON.parse(read())
    expect(written.version).toBe(CONFIG_VERSION)
    expect(written.profiles[0].serverUrl).toBe('http://example.com')
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
    expect((await store.active()).serverUrl).toBe('https://example.com')
  })

  it('keeps mute choices when the server changes', async () => {
    // Switching server is not a reason to start notifying about workspaces the
    // user deliberately silenced.
    const { store } = makeStore(legacy({ mutedWorkspaces: ['ws1'] }))

    await store.save('https://b.example.com')

    expect(await store.active()).toMatchObject({
      serverUrl: 'https://b.example.com',
      mutedWorkspaces: ['ws1'],
    })
  })

  it('mutes and unmutes a single workspace', async () => {
    const { store } = makeStore(legacy({ mutedWorkspaces: [] }))

    expect(await store.setWorkspaceMuted('ws1', true)).toEqual(['ws1'])
    expect(await store.setWorkspaceMuted('ws2', true)).toEqual(['ws1', 'ws2'])
    expect(await store.setWorkspaceMuted('ws1', false)).toEqual(['ws2'])
    expect((await store.active()).mutedWorkspaces).toEqual(['ws2'])
  })

  it('does not list a workspace twice when muted again', async () => {
    const { store } = makeStore(legacy({ mutedWorkspaces: ['ws1'] }))

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

    expect((await store.active()).serverUrl).toBe('')
  })

  it('settings belong to one profile, not to all of them', async () => {
    // The whole point of a profile: signing a second account into a different
    // server must not move the first one.
    const { store } = makeStore(legacy())
    await store.addProfile('Work')
    await store.save('https://work.example.com')

    const config = await store.load()
    const [first, second] = config.profiles

    expect(first.serverUrl).toBe('https://example.com')
    expect(second.serverUrl).toBe('https://work.example.com')
  })

  it('adds a profile, switches to it, and gives it its own session', async () => {
    const { store } = makeStore(legacy())

    const after = await store.addProfile('Work')

    expect(after.profiles).toHaveLength(2)
    expect(after.activeProfileId).toBe(after.profiles[1].id)
    expect(after.profiles[1].partition).not.toBe(after.profiles[0].partition)
  })

  it('never reuses an id already stored', async () => {
    // Two profiles on one id would share a session: the same account twice.
    const { store } = makeStore(JSON.stringify({ profiles: [{ id: 'p1', label: 'Taken' }] }))

    const after = await store.addProfile('New')

    expect(after.profiles.map((p) => p.id)).toEqual(['p1', 'p2'])
  })

  it('switches, renames and removes', async () => {
    const { store } = makeStore(legacy())
    const withWork = await store.addProfile('Work')
    const [first, work] = withWork.profiles

    expect((await store.activateProfile(first.id)).activeProfileId).toBe(first.id)
    expect((await store.renameProfile(work.id, ' Day job ')).profiles[1].label).toBe('Day job')

    const removed = await store.removeProfile(work.id)
    expect(removed.profiles).toHaveLength(1)
    expect(removed.activeProfileId).toBe(first.id)
  })

  it('keeps the last profile, since the app needs a session to run in', async () => {
    const { store } = makeStore(legacy())
    const config = await store.load()

    expect((await store.removeProfile(config.activeProfileId)).profiles).toHaveLength(1)
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

describe('createServerConfigStore without an injected id generator', () => {
  // The store is constructed in the main process with only its file I/O, so a
  // required id generator meant addProfile threw "newProfileId is not a
  // function" the first time anyone pressed Add profile.
  it('can still add a profile', async () => {
    let contents = null
    const store = createServerConfigStore({
      readFile: async () => {
        if (contents === null) throw new Error('ENOENT')
        return contents
      },
      writeFile: async (next) => { contents = next },
    })

    const after = await store.addProfile('Work')

    expect(after.profiles).toHaveLength(2)
    expect(after.profiles[1].label).toBe('Work')
    expect(after.profiles[1].id).toMatch(/^[a-z0-9][a-z0-9-]{0,63}$/)
    expect(after.activeProfileId).toBe(after.profiles[1].id)
  })
})
