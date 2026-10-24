// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The Tauri bridge must be `desktop/src/preload/index.js`, member for member.
// Both are loaded for real, with Electron's and Tauri's sides stubbed, so a
// change to either fails here until the other follows.

import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { beforeAll, describe, expect, it, vi } from 'vitest'

const tauri = vi.hoisted(() => ({ invoke: vi.fn(), listen: vi.fn(), getTauriVersion: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => ({ invoke: tauri.invoke }))
vi.mock('@tauri-apps/api/event', () => ({ listen: tauri.listen }))
vi.mock('@tauri-apps/api/app', () => ({ getTauriVersion: tauri.getTauriVersion }))

const here = (p) => fileURLToPath(new URL(p, import.meta.url))
const read = (p) => readFileSync(here(p), 'utf8')

/** Run the preload as Electron would, and keep what it exposes and calls. */
function loadPreload() {
  const electron = { invoked: [], listened: [], exposed: {} }
  const ipcRenderer = {
    invoke: (channel, ...args) => {
      electron.invoked.push({ channel, args })
      return Promise.resolve()
    },
    on: (channel) => electron.listened.push(channel),
    off: () => {},
  }
  const contextBridge = { exposeInMainWorld: (name, api) => (electron.exposed[name] = api) }
  const fakeRequire = (name) => {
    if (name !== 'electron') throw new Error(`the preload requires ${name}`)
    return { contextBridge, ipcRenderer }
  }
  new Function('require', 'process', read('../../desktop/src/preload/index.js'))(fakeRequire, process)
  return electron
}

/** Every member path of an object: its kind, and a function's arity. */
function tree(obj, prefix = '', out = {}) {
  for (const [key, value] of Object.entries(obj)) {
    const path = prefix ? `${prefix}.${key}` : key
    if (typeof value === 'function') out[path] = `function/${value.length}`
    else if (value && typeof value === 'object' && path !== 'versions') tree(value, path, out)
    else out[path] = path === 'versions' ? 'object' : typeof value
  }
  return out
}

/** Electron's channel as the Rust command that serves it. */
const commandOf = (channel) =>
  channel
    .replace(/^agentrq:/, '')
    .replace(/[:-]/g, '_')
    .replace(/([a-z])([A-Z])/g, '$1_$2')
    .toLowerCase()

/** What a call sends, ignoring how it is packed: the leaf values, sorted. */
function leaves(value) {
  if (value && typeof value === 'object') return Object.values(value).flatMap(leaves)
  return value === undefined ? [] : [value]
}
const sorted = (list) => list.map(String).sort()

/** Arguments that name each parameter uniquely; the install calls take an options object. */
const OPTIONS_CALLS = new Set(['extensions.installLocal', 'extensions.installFromCatalogue'])
const argsFor = (path) => (OPTIONS_CALLS.has(path) ? ['s1', { grant: 's2', config: 's3' }] : ['s1', 's2'])

const members = (api) => Object.entries(tree(api)).filter(([, kind]) => kind.startsWith('function'))
const at = (api, path) => path.split('.').reduce((o, k) => o[k], api)

let electron
let agentrq
beforeAll(async () => {
  globalThis.window = {}
  tauri.getTauriVersion.mockResolvedValue('2.12.5')
  electron = loadPreload()
  agentrq = (await import('../src/bridge/index.js')).createBridge('Mozilla/5.0 (X11; Linux x86_64)')
})

describe('the bridge mirrors the Electron preload', () => {
  it('exposes the same members, of the same kind and arity', () => {
    expect(electron.exposed.agentrq).toBeDefined()
    expect(tree(agentrq)).toEqual(tree(electron.exposed.agentrq))
  })

  it('differs in the versions it reports, and nothing else', () => {
    expect(Object.keys(electron.exposed.agentrq.versions).sort()).toEqual(['chrome', 'electron', 'node'])
    expect(Object.keys(agentrq.versions).sort()).toEqual(['tauri', 'webview'])
  })

  it('names a Tauri side-panel guest, where Electron names its own', () => {
    expect(electron.exposed.agentrq.sidePanel.guest).toBe('electron')
    expect(agentrq.sidePanel.guest).toBe('tauri')
  })

  describe('every call', () => {
    const calls = members(loadPreload().exposed.agentrq).filter(([path]) => !/\.on[A-Z]/.test(path))

    it('covers the 46 calls of the 47 channels', () => {
      // `panel:send` is the other one: it lives in the guest preload, not here.
      expect(calls).toHaveLength(46)
    })

    it.each(calls.map(([path]) => [path]))('%s sends the same values to the command for its channel', async (path) => {
      electron.invoked.length = 0
      tauri.invoke.mockReset().mockResolvedValue(undefined)
      const args = argsFor(path)
      await at(electron.exposed.agentrq, path)(...args)
      await at(agentrq, path)(...args)

      expect(electron.invoked).toHaveLength(1)
      expect(tauri.invoke).toHaveBeenCalledTimes(1)
      const [command, payload] = tauri.invoke.mock.calls[0]
      expect(command).toBe(commandOf(electron.invoked[0].channel))
      expect(sorted(leaves(payload))).toEqual(sorted(leaves(electron.invoked[0].args)))
    })
  })

  describe('every listener', () => {
    const listeners = members(loadPreload().exposed.agentrq).filter(([path]) => /\.on[A-Z]/.test(path))

    it.each(listeners.map(([path]) => [path]))('%s listens to the same channel, and unsubscribes', async (path) => {
      electron.listened.length = 0
      tauri.listen.mockReset().mockResolvedValue(() => {})
      const off = at(electron.exposed.agentrq, path)(() => {})
      const ours = at(agentrq, path)(() => {})
      expect(electron.listened).toHaveLength(1)
      expect(tauri.listen).toHaveBeenCalledTimes(1)
      expect(tauri.listen.mock.calls[0][0]).toBe(electron.listened[0])
      expect(typeof off).toBe(typeof ours)
      expect(typeof ours).toBe('function')
    })
  })
})

describe('the Rust side serves what the bridge calls', () => {
  const rust = read('../src-tauri/src/commands.rs')
  const defined = [...rust.matchAll(/#\[tauri::command\]\s*pub fn (\w+)/g)].map((m) => m[1]).sort()
  const handler = rust.match(/generate_handler!\[([^\]]*)\]/)[1].split(',').map((s) => s.trim()).filter(Boolean).sort()
  const manifest = read('../src-tauri/build.rs').match(/COMMANDS: &\[&str\] = &\[([^\]]*)\]/)[1]
  const named = [...manifest.matchAll(/"(\w+)"/g)].map((m) => m[1]).sort()
  const capability = JSON.parse(read('../src-tauri/capabilities/main.json'))
  const allowed = capability.permissions
    .filter((p) => p.startsWith('allow-'))
    .map((p) => p.slice('allow-'.length).replace(/-/g, '_'))
    .sort()

  // The guest preload's, which WP7b gives the `agentrq-ext:` page's own webview.
  const GUEST_ONLY = ['panel_send']

  it('has one command per channel the preload invokes, plus the guest one', () => {
    const preload = read('../../desktop/src/preload/index.js')
    const channels = [...preload.matchAll(/invoke\(\s*'(agentrq:[\w:-]+)'/g)].map((m) => commandOf(m[1]))
    expect(defined).toEqual([...new Set([...channels, ...GUEST_ONLY])].sort())
  })

  it('registers every command in the handler, the manifest and the capability', () => {
    expect(handler).toEqual(defined)
    expect(named).toEqual(defined)
    expect(allowed).toEqual(defined)
  })

  it('invokes only commands that exist', async () => {
    tauri.invoke.mockReset().mockResolvedValue(undefined)
    for (const [path] of members(agentrq).filter(([p]) => !/\.on[A-Z]/.test(p))) {
      await at(agentrq, path)(...argsFor(path))
    }
    const invoked = new Set(tauri.invoke.mock.calls.map(([name]) => name))
    expect([...invoked].filter((name) => !defined.includes(name))).toEqual([])
    expect(defined.filter((name) => !invoked.has(name))).toEqual(GUEST_ONLY)
  })

  it('grants the main window events and the title-bar drag, and nothing broader', () => {
    const core = capability.permissions.filter((p) => !p.startsWith('allow-'))
    expect(core).toEqual([
      'core:event:allow-listen',
      'core:event:allow-unlisten',
      'core:app:allow-tauri-version',
      'core:window:allow-start-dragging',
      'core:window:allow-toggle-maximize',
    ])
    expect(capability.windows).toEqual(['main'])
    expect(capability.remote).toBeUndefined()
  })
})
