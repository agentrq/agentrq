// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it } from 'vitest'
import { spawn } from 'node:child_process'
import * as fs from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'

import { createPeer } from '../src/ext-host/rpc.js'
import { SEALED_PREFIX } from '../src/ext-host/vault.js'

/**
 * The five examples, driven through the extension host as the shell drives it:
 * a real Node process, spoken to over its stdio. `desktop/test/examples/*`
 * prove each example against the modules; this proves the process around them
 * — the wire, the stores on disk, the keychain round trips — where nothing
 * needs the side panel or the event stream.
 */

// The source, or what ships when `npm run test:sidecar` names the bundle.
const ENTRY = process.env.AGENTRQ_EXT_HOST_ENTRY || fileURLToPath(new URL('../src/ext-host/main.js', import.meta.url))
const EXAMPLES = fileURLToPath(new URL('../../examples/extensions/', import.meta.url))
const NAMES = ['task-stats', 'standup', 'digest', 'guardrail', 'panel-notes']

const GRANTS = {
  standup: { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: ['workspace.get', 'memory.load', 'task.get'], supervisor: [] } },
  digest: { scope: 'supervisor', workspaces: [], tools: { workspace: [], supervisor: ['workspace.list', 'task.listAll', 'task.create'] } },
  guardrail: { scope: 'workspace', workspaces: ['ws1'], tools: { workspace: [], supervisor: [] }, hooks: { toolCall: 'deny' } },
}

let dataDir
let child
let stderr

/** Start the host, and play the shell to it. */
function start() {
  child = spawn(process.execPath, [ENTRY, dataDir, '0.13.4'], { stdio: ['pipe', 'pipe', 'pipe'] })
  stderr = ''
  child.stderr.on('data', (chunk) => (stderr += chunk))
  const notes = []
  const strays = []
  const sealed = (text) => SEALED_PREFIX + Buffer.from(text).toString('base64')
  const peer = createPeer({
    write: (line) => child.stdin.write(line),
    handlers: {
      vault: () => ({ available: true }),
      seal: ({ texts }) => ({ ok: true, sealed: texts.map(sealed) }),
      open: ({ blobs }) => ({ opened: blobs.map((b) => Buffer.from(b.slice(SEALED_PREFIX.length), 'base64').toString()) }),
      profileFetch: () => ({ ok: false, reason: 'No server in this test.' }),
    },
    notifications: Object.fromEntries(
      ['ready', 'started', 'loading', 'running', 'changed'].map((method) => [method, (params) => notes.push({ method, params })]),
    ),
    onStray: (line) => strays.push(line),
  })
  child.stdout.setEncoding('utf8')
  child.stdout.on('data', (chunk) => peer.receive(chunk))
  const until = async (method) => {
    for (let i = 0; i < 200 && !notes.some((n) => n.method === method); i++) {
      await new Promise((resolve) => setTimeout(resolve, 25))
    }
    expect(notes.map((n) => n.method), stderr).toContain(method)
  }
  return { peer, notes, strays, until, call: (method, params = {}) => peer.request(method, params, { timeoutMs: 20_000 }) }
}

function stop() {
  return new Promise((resolve) => {
    if (!child || child.exitCode !== null) return resolve(child?.exitCode)
    child.once('exit', (code) => resolve(code))
    child.stdin.end()
  })
}

beforeEach(async () => {
  dataDir = await fs.mkdtemp(join(tmpdir(), 'ext-host-process-'))
})

afterEach(async () => {
  await stop()
  await fs.rm(dataDir, { recursive: true, force: true })
})

describe('the extension host, as a process', () => {
  it('installs, lists, configures, disables and uninstalls the five examples', async () => {
    const host = start()
    await host.until('started')
    host.peer.notify('context', { serverUrl: 'http://127.0.0.1:9' })

    for (const name of NAMES) {
      const installed = await host.call('installLocal', { path: join(EXAMPLES, name), grant: GRANTS[name] ?? null })
      expect(installed.ok, `${name}: ${installed.reason}`).toBe(true)
    }
    const state = await host.call('state')
    expect(state.installed.map((i) => i.name).sort()).toEqual([...NAMES].sort())
    expect(host.notes.filter((n) => n.method === 'running').at(-1).params.names.sort()).toEqual([...NAMES].sort())

    // A page: task-stats' menu item, run and drawn without a server.
    const task = { title: 'Ship it', body: 'three words here', status: 'ongoing', createdAt: new Date().toISOString(), messages: [] }
    const menu = await host.call('entries', { surface: 'task-menu', context: task })
    expect(menu.map((e) => e.owner)).toEqual(expect.arrayContaining(['task-stats', 'standup', 'panel-notes']))
    const ran = await host.call('invoke', { target: { owner: 'task-stats', id: 'stats', surface: 'task-menu' }, context: task })
    expect(ran.ok, ran.reason).toBe(true)

    // Settings, with a secret that reaches the disk sealed.
    const configured = await host.call('configure', {
      name: 'digest',
      values: { workspaceId: 'ws1', cron: '0 6 * * *', webhookToken: 'hunter2' },
    })
    expect(configured.ok, configured.reason).toBe(true)
    const config = await fs.readFile(join(dataDir, 'extensions-config.json'), 'utf8')
    expect(config).not.toContain('hunter2')
    expect(config).toContain(SEALED_PREFIX)

    // Off, and on again.
    expect((await host.call('setEnabled', { name: 'standup', enabled: false })).ok).toBe(true)
    expect((await host.call('entries', { surface: 'page' })).map((e) => e.owner)).not.toContain('standup')
    expect((await host.call('setEnabled', { name: 'standup', enabled: true })).ok).toBe(true)
    expect((await host.call('entries', { surface: 'page' })).map((e) => e.owner)).toContain('standup')

    // The panel page is listed, and answers what its page sends.
    expect(await host.call('panelPages')).toMatchObject([{ owner: 'panel-notes', url: 'agentrq-ext://panel-notes/panel/index.html' }])
    expect(await host.call('panelSend', { senderUrl: 'agentrq-ext://panel-notes/', message: { type: 'load' } })).toEqual({ ok: true, reply: { text: '' } })

    for (const name of NAMES) {
      const removed = await host.call('uninstall', { name })
      expect(removed.ok, `${name}: ${removed.reason}`).toBe(true)
    }
    expect((await host.call('state')).installed).toEqual([])
    expect(host.strays).toEqual([])
    expect(await stop()).toBe(0)
  }, 60_000)

  it('starts what was enabled last time, and keeps the wire clean of an extension s output', async () => {
    const noisy = join(dataDir, 'src', 'noisy')
    await fs.mkdir(noisy, { recursive: true })
    await fs.cp(join(EXAMPLES, 'task-stats', 'agentrq-extension.json'), join(noisy, 'agentrq-extension.json'))
    const manifest = JSON.parse(await fs.readFile(join(noisy, 'agentrq-extension.json'), 'utf8'))
    await fs.writeFile(join(noisy, 'agentrq-extension.json'), JSON.stringify({ ...manifest, name: 'noisy', provides: { ui: [] } }))
    await fs.writeFile(
      join(noisy, 'index.js'),
      'export function apply() { console.log("hello"); process.stdout.write("raw\\n"); console.info("info") }\n',
    )

    const first = start()
    await first.until('started')
    expect((await first.call('installLocal', { path: noisy })).ok).toBe(true)
    expect(first.strays).toEqual([])
    await stop()

    const second = start()
    await second.until('started')
    expect(second.notes.filter((n) => n.method === 'loading').map((n) => n.params.name)).toEqual(['noisy', null])
    expect(second.notes.filter((n) => n.method === 'running').at(-1).params.names).toEqual(['noisy'])
    expect(second.strays).toEqual([])
    expect(stderr).toContain('hello')
    expect(stderr).toContain('raw')
  }, 60_000)
})
