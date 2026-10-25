// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import * as fs from 'node:fs/promises'
import { spawn, spawnSync } from 'node:child_process'
import { createHash } from 'node:crypto'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { pathToFileURL, fileURLToPath } from 'node:url'

import { createExtensionHost, createProfileFetch, panelSeam, unavailable } from '../src/ext-host/runtime.js'
import { SEALED_PREFIX } from '../src/ext-host/vault.js'
import { REDIRECT_URI } from '../../desktop/src/main/extensions/supervisor-auth.js'

const EXAMPLES = fileURLToPath(new URL('../../examples/extensions/', import.meta.url))
const SERVER = 'http://server.test'

const json = (body, status = 200, headers = {}) => ({
  ok: true,
  status,
  headers: [['content-type', 'application/json'], ...Object.entries(headers)],
  body: Buffer.from(JSON.stringify(body)).toString('base64'),
})

/**
 * The shell, as the extension host sees it over the wire: the keychain, the
 * profile's fetch (answered by a stand-in AgentRQ server), and the windows.
 */
function fakeShell({ keychain = true, routes = {} } = {}) {
  const notes = []
  const fetched = []
  const tools = { workspace: [], supervisor: [] }
  const server = {
    token: 'ws-token',
    tokenStatus: 200,
    supervisorStatus: 200,
    refreshStatus: 200,
    verdictStatus: 200,
    answers: {},
    redirect: (url) => `${REDIRECT_URI}?code=c1&state=${new URL(url).searchParams.get('state')}`,
  }

  function mcp(path, body, surface) {
    const message = JSON.parse(Buffer.from(body, 'base64').toString())
    if (message.method === 'initialize') {
      return json({ jsonrpc: '2.0', id: message.id, result: {} }, 200, { 'mcp-session-id': `${surface}-session` })
    }
    if (message.method !== 'tools/call') return { ok: true, status: 202, headers: [], body: '' }
    const { name, arguments: args } = message.params
    const tool = `${name}.${args.action}`
    tools[surface].push({ path, tool, args: args.payload })
    if (surface === 'supervisor' && server.supervisorStatus !== 200) {
      const status = server.supervisorStatus
      server.supervisorStatus = 200
      return { ok: true, status, headers: [], body: '' }
    }
    const answer = server.answers[tool]
    const text = typeof answer === 'function' ? answer(args.payload) : (answer ?? '')
    return json({
      jsonrpc: '2.0',
      id: message.id,
      result: { content: [{ type: 'text', text: typeof text === 'string' ? text : JSON.stringify(text) }] },
    })
  }

  const handlers = {
    vault: () => ({ available: keychain }),
    seal: ({ texts }) =>
      keychain
        ? { ok: true, sealed: texts.map((t) => SEALED_PREFIX + Buffer.from(t).toString('base64')) }
        : { ok: false, reason: 'no keychain' },
    open: ({ blobs }) => ({
      opened: blobs.map((b) => (keychain ? Buffer.from(b.slice(SEALED_PREFIX.length), 'base64').toString() : null)),
    }),
    authWindow: ({ url, redirectUri }) => {
      expect(redirectUri).toBe(REDIRECT_URI)
      return server.redirect(url)
    },
    profileFetch: ({ url, method, headers, body }) => {
      fetched.push({ url, method, headers, body })
      const { pathname } = new URL(url)
      if (routes[pathname]) return routes[pathname]({ url, method, headers, body })
      if (/^\/api\/v1\/workspaces\/[^/]+\/token$/.test(pathname)) {
        return json({ token: server.token }, server.tokenStatus)
      }
      if (/^\/mcp\/[^/]+$/.test(pathname)) return mcp(pathname, body, 'workspace')
      if (pathname === '/mcp') return mcp(pathname, body, 'supervisor')
      if (pathname === '/.well-known/oauth-authorization-server') return json({}, 404)
      if (pathname === '/mcp/oauth2/register') return json({ client_id: 'client' })
      if (pathname === '/mcp/oauth2/token') {
        const form = new URLSearchParams(Buffer.from(body, 'base64').toString())
        if (form.get('grant_type') === 'refresh_token') {
          return server.refreshStatus === 200
            ? json({ access_token: 'access-2' })
            : json({}, server.refreshStatus)
        }
        return json({ access_token: 'access-1', refresh_token: 'refresh-1' })
      }
      if (pathname.endsWith('/permission')) return json({}, server.verdictStatus)
      return json({ error: 'no route' }, 404)
    },
  }

  return {
    notes,
    fetched,
    tools,
    server,
    handlers,
    rpc: {
      async request(method, params) {
        return handlers[method](params)
      },
      notify(method, params) {
        notes.push({ method, params })
      },
    },
  }
}

let dataDir
const quiet = { info: vi.fn(), warn: vi.fn() }

const jsonFile = (path) => ({
  read: async () => JSON.parse(await fs.readFile(path, 'utf8')),
  write: async (value) => {
    await fs.mkdir(join(path, '..'), { recursive: true })
    await fs.writeFile(path, JSON.stringify(value), 'utf8')
  },
})

function host(shell = fakeShell(), { fetchImpl = vi.fn(), files = jsonFile } = {}) {
  const made = createExtensionHost({
    rpc: shell.rpc,
    dataDir,
    appVersion: '0.13.4',
    load: (dir) => import(pathToFileURL(join(dir, 'index.js')).href),
    jsonFile: files,
    fs,
    spawn,
    join,
    fetchImpl,
    logger: quiet,
  })
  made.notifications.context({ serverUrl: SERVER })
  return { ...made, shell, call: (method, params) => made.handlers[method](params) }
}

/** A manifest for an extension a test writes itself. */
const manifest = (name, provides, extra = {}) =>
  JSON.stringify({
    ...extra,
    name,
    displayName: name,
    version: '1.0.0',
    description: 'A test extension.',
    license: 'MIT',
    engines: { agentrq: '>=0.5' },
    provides,
    artifact: { release: 'v1.0.0', asset: `${name}-1.0.0.tgz`, sha256: '0'.repeat(64) },
  })

const workspaceGrant = (workspaceId = 'ws1') => ({
  scope: 'workspace',
  workspaces: [workspaceId],
  tools: { workspace: ['workspace.get', 'memory.load', 'task.get'], supervisor: [] },
})

const supervisorGrant = {
  scope: 'supervisor',
  workspaces: [],
  tools: { workspace: [], supervisor: ['workspace.list', 'task.listAll', 'task.create'] },
}

beforeEach(async () => {
  dataDir = await fs.mkdtemp(join(tmpdir(), 'ext-host-'))
  quiet.info.mockClear()
  quiet.warn.mockClear()
})

afterEach(async () => {
  await fs.rm(dataDir, { recursive: true, force: true })
})

describe('the wiring Electron did in its main process', () => {
  it('names what is not wired yet', () => {
    expect(unavailable('The side panel', 'WP7b')).toBe('The side panel is unavailable in this preview until WP7b.')
  })

  it('gives an extension a panel that says it is not there yet', async () => {
    const panel = panelSeam('notes')
    const refused = { ok: false, reason: 'The side panel is unavailable in this preview until WP7b.' }
    expect(await panel.open({ page: 'notes' })).toEqual(refused)
    expect(panel.post({})).toEqual(refused)
    expect(() => panel.onMessage(() => {})).not.toThrow()
    expect(() => panel.onMessage('no')).toThrow('notes: onMessage needs a function.')
  })

  it('fetches through the shell, bodies as base64', async () => {
    const shell = fakeShell({
      routes: {
        '/echo': ({ method, headers, body }) =>
          json({ method, headers, body: body && Buffer.from(body, 'base64').toString() }),
        '/empty': () => ({ ok: true, status: 204, headers: [['x-a', '1']], body: 'ignored' }),
        '/nobody': () => ({ ok: true, status: 200, headers: [] }),
        '/refused': () => ({ ok: false, reason: 'Not this server.' }),
        '/silent': () => null,
      },
    })
    const profileFetch = createProfileFetch(shell.rpc)

    const echoed = await (await profileFetch(`${SERVER}/echo`, { method: 'POST', headers: { 'X-B': '2' }, body: 'hi' })).json()
    expect(echoed).toEqual({ method: 'POST', headers: [['x-b', '2']], body: 'hi' })
    expect(await (await profileFetch(`${SERVER}/echo`)).json()).toEqual({ method: 'GET', headers: [], body: null })
    expect(await (await profileFetch(`${SERVER}/echo`, { body: null })).json()).toMatchObject({ body: null })

    const empty = await profileFetch(`${SERVER}/empty`)
    expect([empty.status, empty.headers.get('x-a'), await empty.text()]).toEqual([204, '1', ''])
    expect(await (await profileFetch(`${SERVER}/nobody`)).text()).toBe('')
    await expect(profileFetch(`${SERVER}/refused`)).rejects.toThrow('Not this server.')
    await expect(profileFetch(`${SERVER}/silent`)).rejects.toThrow('The request failed.')
  })
})

describe('installing, listing and removing the examples', () => {
  it('installs each example, lists it, and takes it away again', async () => {
    const h = host()
    await h.start()
    for (const name of ['task-stats', 'standup', 'digest', 'guardrail', 'panel-notes']) {
      const grant = name === 'standup' ? workspaceGrant() : name === 'digest' ? supervisorGrant : null
      const installed = await h.call('installLocal', { path: join(EXAMPLES, name), grant })
      expect(installed.ok, installed.reason).toBe(true)
    }

    const { installed, index } = await h.call('state', {})
    expect(installed.map((i) => i.name).sort()).toEqual(['digest', 'guardrail', 'panel-notes', 'standup', 'task-stats'])
    expect(index.entries).toEqual([])
    // Every one was loaded, and the shell was told which were running.
    expect(h.shell.notes.filter((n) => n.method === 'loading').map((n) => n.params.name)).toContain('standup')
    expect(h.shell.notes.at(-1)).toMatchObject({ method: 'running' })
    expect(h.shell.notes.at(-1).params.names).toHaveLength(5)

    // The grants outlive the process.
    const grants = JSON.parse(await fs.readFile(join(dataDir, 'extensions-grants.json'), 'utf8'))
    expect(Object.keys(grants).sort()).toEqual(['digest', 'standup'])

    for (const name of ['task-stats', 'standup', 'digest', 'guardrail', 'panel-notes']) {
      const removed = await h.call('uninstall', { name })
      expect(removed.ok, removed.reason).toBe(true)
    }
    expect((await h.call('state', {})).installed).toEqual([])
    // Linked folders are the person's own: an uninstall leaves them.
    await expect(fs.access(join(EXAMPLES, 'standup', 'index.js'))).resolves.toBeUndefined()
  })

  it('reads a folder before anything is installed from it', async () => {
    const h = host()
    expect(await h.call('inspect', { path: join(EXAMPLES, 'standup') })).toMatchObject({ ok: true, path: join(EXAMPLES, 'standup') })
    expect((await h.call('inspect', { path: join(dataDir, 'nothing') })).ok).toBe(false)
    expect(await h.call('installLocal', {})).toEqual({ ok: false, reason: 'No folder was chosen.' })
  })

  it('enables, disables and configures one', async () => {
    const h = host()
    await h.call('installLocal', { path: join(EXAMPLES, 'standup'), grant: workspaceGrant() })
    const off = await h.call('setEnabled', { name: 'standup', enabled: false })
    expect(off.ok).toBe(true)
    expect(await h.call('entries', { surface: 'page' })).toEqual([])

    const on = await h.call('setEnabled', { name: 'standup', enabled: true })
    expect(on.ok, on.reason).toBe(true)
    expect((await h.call('entries', { surface: 'page' })).map((e) => e.owner)).toEqual(['standup'])
    expect(await h.call('entries', {})).toEqual([])

    expect(await h.call('configure', { name: 'standup', values: { memory: 'notes.md' } })).toMatchObject({ ok: true })
    expect(await h.call('configure', { name: 'standup' })).toMatchObject({ ok: true })
    const config = JSON.parse(await fs.readFile(join(dataDir, 'extensions-config.json'), 'utf8'))
    expect(config.config.standup).toEqual({ memory: 'notes.md' })
  })

  it('keeps a secret sealed by the shell, and refuses one with no keychain', async () => {
    const h = host()
    await h.call('installLocal', { path: join(EXAMPLES, 'digest'), grant: supervisorGrant })
    const values = { workspaceId: 'ws1', cron: '0 6 * * *', webhookToken: 'hunter2' }
    expect((await h.call('configure', { name: 'digest', values })).ok).toBe(true)
    const written = await fs.readFile(join(dataDir, 'extensions-config.json'), 'utf8')
    expect(written).not.toContain('hunter2')
    expect(written).toContain(SEALED_PREFIX)

    const bare = host(fakeShell({ keychain: false }))
    await bare.start()
    const refused = await bare.call('configure', { name: 'digest', values })
    expect(refused.ok).toBe(false)
    expect(refused.reason).toMatch(/no secure storage/)
  })

  it('changes what an extension may answer for the person', async () => {
    const h = host()
    await h.call('installLocal', { path: join(EXAMPLES, 'guardrail'), grant: null })
    expect(await h.call('setHookConsent', { name: 'guardrail', level: 'none' })).toEqual({ ok: true, level: 'none' })
    expect((await h.call('setHookConsent', { name: 'guardrail', level: 'everything' })).ok).toBe(false)
    const grants = JSON.parse(await fs.readFile(join(dataDir, 'extensions-grants.json'), 'utf8'))
    expect(grants.guardrail.hooks).toEqual({ toolCall: 'none' })
  })

  it('carries on when a grant cannot be written', async () => {
    const files = (path) => (path.endsWith('extensions-grants.json') ? { read: async () => ({}), write: async () => { throw new Error('disk full') } } : jsonFile(path))
    const h = host(fakeShell(), { files })
    expect((await h.call('installLocal', { path: join(EXAMPLES, 'task-stats') })).ok).toBe(true)
  })
})

describe('what a page asks for', () => {
  it('runs a menu entry, through the workspace server it was granted', async () => {
    const shell = fakeShell()
    shell.server.answers = {
      'workspace.get': 'Workspace: Backend\n\nTask Statistics:\n- Not Started: 3\n- Ongoing: 1\n- Completed: 12\n- Rejected: 0\n- Blocked: 2',
      'memory.load': '# memory',
    }
    const h = host(shell)
    await h.call('installLocal', { path: join(EXAMPLES, 'standup'), grant: workspaceGrant() })

    const [page] = await h.call('entries', { surface: 'page', context: { workspaceId: 'ws1' } })
    const result = await h.call('invoke', { target: { owner: page.owner, id: page.id, surface: 'page' }, context: { workspaceId: 'ws1' } })
    expect(result.ok, result.reason).toBe(true)
    expect(shell.tools.workspace.map((t) => t.tool)).toEqual(['workspace.get', 'memory.load'])
    // The token went with the call, and never through the extension.
    const call = shell.fetched.find((f) => f.url === `${SERVER}/mcp/ws1` && Buffer.from(f.body, 'base64').toString().includes('tools/call'))
    expect(call.headers).toContainEqual(['authorization', 'Bearer ws-token'])
    // One session per workspace, kept.
    await h.call('invoke', { target: { owner: page.owner, id: page.id, surface: 'page' }, context: { workspaceId: 'ws1' } })
    const opened = shell.fetched.filter((f) => Buffer.from(f.body ?? '', 'base64').toString().includes('"initialize"'))
    expect(opened).toHaveLength(1)

    expect(await h.call('invoke', {})).toMatchObject({ ok: false })
  })

  it('says why a workspace gave no token', async () => {
    for (const [status, token, reason] of [
      [404, 'x', 'could not get a token for that workspace (404)'],
      [200, null, 'did not give up an MCP token'],
    ]) {
      const shell = fakeShell({
        routes: token === null ? { '/api/v1/workspaces/ws1/token': () => json(null) } : {},
      })
      shell.server.tokenStatus = status
      shell.server.token = token
      const h = host(shell)
      await h.call('installLocal', { path: join(EXAMPLES, 'standup'), grant: workspaceGrant() })
      const [page] = await h.call('entries', { surface: 'page' })
      const result = await h.call('invoke', { target: { owner: 'standup', id: page.id, surface: 'page' }, context: { workspaceId: 'ws1' } })
      expect(JSON.stringify(result)).toContain(reason)
    }
  })

  it('asks the supervisor nothing once the server is gone', async () => {
    const shell = fakeShell()
    const h = host(shell)
    expect(await h.call('authorize')).toEqual({ ok: true })
    await h.call('installLocal', { path: join(EXAMPLES, 'digest'), grant: supervisorGrant })
    h.notifications.context({ serverUrl: '' })
    const [page] = await h.call('entries', { surface: 'page' })
    const before = shell.fetched.length
    const result = await h.call('invoke', { target: { owner: 'digest', id: page.id, surface: 'page' }, context: {} })
    expect(JSON.stringify(result)).toContain('No server is configured.')
    expect(shell.fetched.length).toBe(before)
  })

  it('answers no server at all with a refusal, not a request', async () => {
    const h = host()
    h.notifications.context({ serverUrl: '' })
    h.notifications.context(null)
    h.notifications.context({ serverUrl: 7 })
    await h.call('installLocal', { path: join(EXAMPLES, 'standup'), grant: workspaceGrant() })
    const [page] = await h.call('entries', { surface: 'page' })
    const result = await h.call('invoke', { target: { owner: 'standup', id: page.id, surface: 'page' }, context: { workspaceId: 'ws1' } })
    expect(JSON.stringify(result)).toContain('No server is configured.')
    expect(h.shell.fetched).toEqual([])
  })

  it('serves a drawer an extension ships, and says when nothing draws a format', async () => {
    const dir = join(dataDir, 'src', 'chart')
    await fs.mkdir(join(dir, 'drawers'), { recursive: true })
    await fs.writeFile(
      join(dir, 'agentrq-extension.json'),
      manifest('chart', { drawers: [{ format: 'chart', entry: 'drawers/chart.js' }] }),
    )
    await fs.writeFile(join(dir, 'index.js'), 'export function apply() {}\n')
    await fs.writeFile(join(dir, 'drawers', 'chart.js'), 'export default function draw() {}\n')

    const h = host()
    const installed = await h.call('installLocal', { path: dir })
    expect(installed.ok, installed.reason).toBe(true)
    expect(await h.call('hasDrawer', { format: 'Chart' })).toEqual({ ok: true, owner: 'chart' })
    expect(await h.call('drawerFor', { format: 'chart' })).toEqual({ ok: true, owner: 'chart', code: 'export default function draw() {}\n' })
    expect((await h.call('drawerFor', { format: 'mermaid' })).reason).toBe('Nothing installed draws "mermaid".')
  })

  it('lists the panel pages enabled extensions declared', async () => {
    const h = host()
    await h.call('installLocal', { path: join(EXAMPLES, 'panel-notes') })
    expect(await h.call('panelPages')).toEqual([
      { owner: 'panel-notes', extension: expect.any(String), id: 'notes', label: 'Notes', url: 'agentrq-ext://panel-notes/panel/index.html' },
    ])
    expect(await h.call('panelPagesFor', { name: 'panel-notes' })).toHaveLength(1)
    expect(await h.call('panelPagesFor', { name: 'other' })).toEqual([])
  })
})

describe('the account-wide tools', () => {
  async function authorised(shell = fakeShell()) {
    const h = host(shell)
    expect(await h.call('supervisor')).toEqual({ authorized: false })
    expect(await h.call('authorize')).toEqual({ ok: true })
    expect(await h.call('supervisor')).toEqual({ authorized: true })
    return h
  }

  it('authorises through the shell s window, and keeps it sealed', async () => {
    const shell = fakeShell()
    await authorised(shell)
    const kept = JSON.parse(await fs.readFile(join(dataDir, 'extensions-supervisor-auth.json'), 'utf8'))
    expect(kept.sealed.startsWith(SEALED_PREFIX)).toBe(true)

    // The next run reads it back.
    const next = host(fakeShell())
    await next.start()
    expect(await next.call('supervisor')).toEqual({ authorized: true })

    expect(await next.call('deauthorize')).toEqual({ ok: true })
    expect(await next.call('supervisor')).toEqual({ authorized: false })
    expect(JSON.parse(await fs.readFile(join(dataDir, 'extensions-supervisor-auth.json'), 'utf8'))).toEqual({})
  })

  it('reports a window that came back with nothing', async () => {
    const shell = fakeShell()
    shell.handlers.authWindow = () => null
    expect(await host(shell).call('authorize')).toEqual({ ok: false, reason: 'The authorisation window was closed.' })
    shell.handlers.authWindow = () => {
      throw new Error('The authorisation window is unavailable in this preview until WP9 part 2.')
    }
    expect((await host(shell).call('authorize')).reason).toBe('The authorisation window is unavailable in this preview until WP9 part 2.')
  })

  it('does not keep it where there is no keychain, or when sealing fails', async () => {
    await authorised(fakeShell({ keychain: false }))
    await expect(fs.access(join(dataDir, 'extensions-supervisor-auth.json'))).rejects.toThrow()

    const refusing = fakeShell()
    const h = host(refusing)
    await h.start()
    refusing.handlers.seal = () => ({ ok: false })
    expect(await h.call('authorize')).toEqual({ ok: true })
    await expect(fs.access(join(dataDir, 'extensions-supervisor-auth.json'))).rejects.toThrow()

    refusing.handlers.seal = () => {
      throw new Error('gone')
    }
    expect(await h.call('authorize')).toEqual({ ok: true })
    expect(quiet.warn).toHaveBeenCalledWith('could not store the supervisor authorisation:', 'gone')
    refusing.handlers.seal = () => {
      throw 'bare'
    }
    expect(await h.call('authorize')).toEqual({ ok: true })
    expect(quiet.warn).toHaveBeenCalledWith('could not store the supervisor authorisation:', 'bare')
  })

  it('asks again when what was kept cannot be opened', async () => {
    await fs.writeFile(join(dataDir, 'extensions-supervisor-auth.json'), JSON.stringify({ sealed: `${SEALED_PREFIX}x` }))
    const h = host(fakeShell({ keychain: false }))
    await h.start()
    expect(await h.call('supervisor')).toEqual({ authorized: false })

    await fs.writeFile(join(dataDir, 'extensions-supervisor-auth.json'), 'null')
    const empty = host()
    await empty.start()
    expect(await empty.call('supervisor')).toEqual({ authorized: false })
  })

  it('forgets in memory even when the file cannot be written', async () => {
    const files = (path) =>
      path.endsWith('supervisor-auth.json') ? { read: async () => ({}), write: async () => { throw new Error('no') } } : jsonFile(path)
    const h = host(fakeShell(), { files })
    expect(await h.call('deauthorize')).toEqual({ ok: true })
  })

  it('runs a scheduled extension through the supervisor, refreshing an expired token once', async () => {
    const shell = fakeShell()
    shell.server.answers = {
      'workspace.list': { workspaces: [{ id: 'ws1', name: 'Backend' }] },
      'task.listAll': { tasks: [] },
      'task.create': (args) => ({ task: { id: 'tk1', ...args } }),
    }
    const h = await authorised(shell)
    const values = { workspaceId: 'ws1', cron: '0 6 * * *' }
    expect((await h.call('installLocal', { path: join(EXAMPLES, 'digest'), grant: supervisorGrant, config: values })).ok).toBe(true)
    expect(shell.tools.supervisor.map((t) => t.tool)).toContain('task.create')

    // A lapsed access token is refreshed and the call made again.
    shell.server.supervisorStatus = 401
    const [page] = await h.call('entries', { surface: 'page' })
    const before = shell.tools.supervisor.length
    const result = await h.call('invoke', { target: { owner: 'digest', id: page.id, surface: 'page' }, context: {} })
    expect(result.ok, result.reason).toBe(true)
    expect(shell.tools.supervisor.length).toBeGreaterThan(before + 1)
    const last = shell.fetched.filter((f) => f.url === `${SERVER}/mcp`).at(-1)
    expect(last.headers).toContainEqual(['authorization', 'Bearer access-2'])

    // A refresh the server refuses is reported, not retried.
    shell.server.supervisorStatus = 401
    shell.server.refreshStatus = 400
    const refused = await h.call('invoke', { target: { owner: 'digest', id: page.id, surface: 'page' }, context: {} })
    expect(JSON.stringify(refused)).toMatch(/credentials/)

    // Not authorised at all.
    await h.call('deauthorize')
    const unauthorised = await h.call('invoke', { target: { owner: 'digest', id: page.id, surface: 'page' }, context: {} })
    expect(JSON.stringify(unauthorised)).toContain('not authorised to use account-wide tools')
  })

  it('passes on a supervisor failure that is not about credentials', async () => {
    const shell = fakeShell()
    shell.server.answers = { 'workspace.list': { workspaces: [] }, 'task.listAll': { tasks: [] } }
    const h = await authorised(shell)
    await h.call('installLocal', { path: join(EXAMPLES, 'digest'), grant: supervisorGrant })
    shell.server.supervisorStatus = 500
    const [page] = await h.call('entries', { surface: 'page' })
    const result = await h.call('invoke', { target: { owner: 'digest', id: page.id, surface: 'page' }, context: {} })
    expect(JSON.stringify(result)).toContain('500')
  })
})

describe('the catalogue', () => {
  const repo = { full_name: 'someone/thing', name: 'thing', owner: { login: 'someone' }, description: '', stargazers_count: 1, default_branch: 'main', pushed_at: '2026-01-01T00:00:00Z', html_url: 'https://github.com/someone/thing' }

  it('searches GitHub without the profile, and lists what it found', async () => {
    const fetchImpl = vi.fn(async (url) => {
      if (String(url).includes('/search/repositories')) {
        return new Response(JSON.stringify({ total_count: 1, items: [repo] }), { status: 200 })
      }
      return new Response('', { status: 404 })
    })
    const h = host(fakeShell(), { fetchImpl })
    const result = await h.call('refresh')
    expect(result.ok, result.reason).toBe(true)
    expect(result.index.entries).toMatchObject([{ fullName: 'someone/thing', ok: false }])
    expect(fetchImpl.mock.calls[0][1].headers).toMatchObject({ 'User-Agent': 'AgentRQ' })
    // Kept for the next run.
    expect((await h.call('state')).index.entries).toHaveLength(1)
    expect(await h.call('installFromCatalogue', { fullName: 'someone/thing' })).toMatchObject({ ok: false })
    expect(await h.call('installFromCatalogue', { fullName: 'someone/other' })).toEqual({
      ok: false,
      reason: 'That extension is no longer in the catalogue.',
    })
    expect(await h.call('installFromCatalogue', {})).toEqual({ ok: false, reason: 'No extension was chosen.' })
  })

  it('installs a release from the catalogue into the app s own folder, and removes it', async () => {
    const archive = join(dataDir, 'standup-1.0.0.tgz')
    const made = spawnSync('tar', ['-czf', archive, '-C', EXAMPLES, 'standup'])
    expect(made.status, String(made.stderr)).toBe(0)
    const bytes = await fs.readFile(archive)
    const sha256 = createHash('sha256').update(bytes).digest('hex')
    const source = JSON.parse(await fs.readFile(join(EXAMPLES, 'standup', 'agentrq-extension.json'), 'utf8'))
    source.artifact = { release: 'v1.0.0', asset: 'standup-1.0.0.tgz', sha256 }

    const fetchImpl = vi.fn(async (url) => {
      const href = String(url)
      if (href.includes('/search/repositories')) {
        return new Response(JSON.stringify({ total_count: 1, items: [{ ...repo, full_name: 'someone/standup', name: 'standup' }] }))
      }
      if (href.endsWith('/agentrq-extension.json')) return new Response(JSON.stringify(source))
      if (href === 'https://github.com/someone/standup/releases/download/v1.0.0/standup-1.0.0.tgz') return new Response(bytes)
      return new Response('', { status: 404 })
    })
    const h = host(fakeShell(), { fetchImpl })
    const found = await h.call('refresh')
    expect(found.index.entries).toMatchObject([{ fullName: 'someone/standup', ok: true, compatible: true }])

    const installed = await h.call('installFromCatalogue', { fullName: 'someone/standup', grant: workspaceGrant() })
    expect(installed.ok, installed.reason).toBe(true)
    const dir = join(dataDir, 'extensions', 'standup')
    await expect(fs.access(join(dir, 'index.js'))).resolves.toBeUndefined()
    expect((await h.call('entries', { surface: 'page' })).map((e) => e.owner)).toEqual(['standup'])

    expect((await h.call('uninstall', { name: 'standup' })).ok).toBe(true)
    await expect(fs.access(dir)).rejects.toThrow()
  })

  it('reports GitHub refusing', async () => {
    const fetchImpl = vi.fn(async () => new Response('', { status: 503 }))
    const result = await host(fakeShell(), { fetchImpl }).call('refresh')
    expect(result.ok).toBe(false)
    expect(result.index.entries).toEqual([])
  })
})

describe('tool-call review', () => {
  const guardrailGrant = { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: [], supervisor: [] }, hooks: { toolCall: 'deny' } }
  const pending = (requestId, command = 'rm -rf /tmp/x') => ({
    payload: {
      id: 't1',
      workspaceId: 'ws1',
      messages: [
        {
          metadata: {
            type: 'permission_request',
            status: 'pending',
            requestId,
            toolName: 'Bash',
            inputPreview: JSON.stringify({ command }),
          },
        },
      ],
    },
  })

  it('sends a reviewer s verdict as the person, through the profile', async () => {
    const shell = fakeShell()
    const h = host(shell)
    expect((await h.call('installLocal', { path: join(EXAMPLES, 'guardrail'), grant: guardrailGrant })).ok).toBe(true)
    expect(await h.call('reviewToolCalls', { event: pending('r1') })).toEqual({ ok: true })
    const verdict = shell.fetched.find((f) => f.url.endsWith('/permission'))
    expect(verdict.url).toBe(`${SERVER}/api/v1/workspaces/ws1/tasks/t1/permission`)
    expect(verdict.method).toBe('POST')
    expect(JSON.parse(Buffer.from(verdict.body, 'base64').toString())).toEqual({
      requestId: 'r1',
      behavior: 'deny',
      decidedBy: 'guardrail',
    })
  })

  it('says why a verdict was not delivered', async () => {
    const shell = fakeShell({ routes: { '/api/v1/workspaces/ws1/tasks/t1/permission': ({ body }) => {
      const { requestId } = JSON.parse(Buffer.from(body, 'base64').toString())
      if (requestId === 'gone') throw new Error('unreachable')
      if (requestId === 'bare') return { ok: false }
      return json({}, requestId === 'late' ? 410 : 500)
    } } })
    const h = host(shell)
    await h.call('installLocal', { path: join(EXAMPLES, 'guardrail'), grant: guardrailGrant })
    for (const [id, reason] of [
      ['late', 'already answered'],
      ['broken', 'server said 500'],
      ['gone', 'unreachable'],
      ['bare', 'The request failed.'],
    ]) {
      await h.call('reviewToolCalls', { event: pending(id) })
      expect(quiet.warn).toHaveBeenCalledWith(`[guardrail] verdict not delivered: ${reason}`)
    }
    h.notifications.context({ serverUrl: '' })
    await h.call('reviewToolCalls', { event: pending('offline') })
    expect(quiet.warn).toHaveBeenCalledWith('[guardrail] verdict not delivered: Not connected to a server.')
  })

  it('counts a reviewer that throws against its extension', async () => {
    const dir = join(dataDir, 'src', 'thrower')
    await fs.mkdir(dir, { recursive: true })
    await fs.writeFile(join(dir, 'agentrq-extension.json'), manifest('thrower', {}, { hooks: { toolCall: 'deny' } }))
    await fs.writeFile(
      join(dir, 'index.js'),
      'export const inject = ["hooks"]; export function apply(ctx) { ctx.hooks.add({ id: "x", review() { throw new Error("review broke") } }) }\n',
    )
    const h = host()
    expect((await h.call('installLocal', { path: dir, grant: { ...guardrailGrant } })).ok).toBe(true)
    for (const id of ['a', 'b', 'c']) await h.call('reviewToolCalls', { event: pending(id) })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect(h.shell.notes).toContainEqual({ method: 'changed', params: { name: 'thrower', reason: 'review broke' } })

    // Disabling it can fail too, inside the stream handler: never unhandled.
    const failing = host(fakeShell())
    const notify = failing.shell.rpc.notify
    failing.shell.rpc.notify = (method, params) => {
      if (method === 'changed') throw new Error('the shell is gone')
      notify(method, params)
    }
    await failing.call('installLocal', { path: dir, grant: { ...guardrailGrant } })
    for (const id of ['d', 'e', 'f']) await failing.call('reviewToolCalls', { event: pending(id) })
    await new Promise((resolve) => setTimeout(resolve, 10))
    expect((await failing.call('state')).installed.find((i) => i.name === 'thrower').enabled).toBe(false)
  })

  it('logs a review that throws rather than failing the call', async () => {
    const h = host()
    expect(await h.call('reviewToolCalls', { event: { get payload() { throw new Error('bad event') } } })).toEqual({ ok: true })
    expect(quiet.warn).toHaveBeenCalledWith('[extensions] tool-call review failed:', 'bad event')
    expect(await h.call('reviewToolCalls', { event: { get payload() { throw 'bare' } } })).toEqual({ ok: true })
    expect(quiet.warn).toHaveBeenCalledWith('[extensions] tool-call review failed:', 'bare')
  })
})

describe('starting and stopping', () => {
  it('reads back grants before anything loads, and starts what is enabled', async () => {
    const first = host()
    await first.call('installLocal', { path: join(EXAMPLES, 'standup'), grant: workspaceGrant() })
    await first.call('installLocal', { path: join(EXAMPLES, 'task-stats') })
    first.stop()

    const second = host()
    const results = await second.start()
    expect(results.map((r) => [r.name, r.ok])).toEqual([
      ['standup', true],
      ['task-stats', true],
    ])
    expect(second.shell.notes.at(-1)).toEqual({ method: 'running', params: { names: ['standup', 'task-stats'] } })
  })

  it('turns off what the shell says crashed it, before anything loads', async () => {
    const first = host()
    await first.call('installLocal', { path: join(EXAMPLES, 'task-stats') })
    first.stop()

    const second = host()
    const reason = 'The extension host stopped (exit code 1) three times while it ran.'
    expect(await second.start([{ name: 'task-stats', reason }])).toEqual([])
    expect(second.shell.notes).toContainEqual({ method: 'changed', params: { name: 'task-stats', reason } })
    expect(second.shell.notes.some((n) => n.method === 'loading')).toBe(false)
    expect((await second.call('state')).installed[0].enabled).toBe(false)
  })

  it('holds a call that arrives while it starts, so nothing loads twice', async () => {
    const first = host()
    await first.call('installLocal', { path: join(EXAMPLES, 'task-stats') })
    first.stop()

    const second = host()
    const started = second.start()
    const state = second.call('state')
    expect((await started).map((r) => r.name)).toEqual(['task-stats'])
    expect((await state).installed.map((i) => i.name)).toEqual(['task-stats'])
    expect(second.shell.notes.filter((n) => n.method === 'loading')).toEqual([
      { method: 'loading', params: { name: 'task-stats' } },
      { method: 'loading', params: { name: null } },
    ])

    // A start that failed holds nothing up.
    const broken = host()
    await expect(broken.start([null])).rejects.toThrow()
    expect(await broken.call('panelPages')).toEqual([])
  })

  it('starts with nothing installed and a grant file that will not parse', async () => {
    await fs.writeFile(join(dataDir, 'extensions-grants.json'), '{')
    expect(await host().start()).toEqual([])
    await fs.writeFile(join(dataDir, 'extensions-grants.json'), 'null')
    expect(await host().start()).toEqual([])
  })

  it('tells the shell an extension that failed three times was turned off', async () => {
    const dir = join(dataDir, 'src', 'broken')
    await fs.mkdir(dir, { recursive: true })
    await fs.writeFile(
      join(dir, 'agentrq-extension.json'),
      manifest('broken', { ui: ['task-menu'] }),
    )
    await fs.writeFile(join(dir, 'index.js'), 'export function apply() { throw new Error("nope") }\n')
    const h = host()
    for (let i = 0; i < 3; i++) await h.call('installLocal', { path: dir })
    expect(h.shell.notes).toContainEqual({ method: 'changed', params: { name: 'broken', reason: 'nope' } })
    expect((await h.call('state')).installed.find((i) => i.name === 'broken').enabled).toBe(false)
  })
})
