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
vi.mock('@tauri-apps/api/core', () => ({ invoke: tauri.invoke, Channel: class {} }))
vi.mock('@tauri-apps/api/event', () => ({ listen: tauri.listen }))
vi.mock('@tauri-apps/api/app', () => ({ getTauriVersion: tauri.getTauriVersion }))

/** A `#[tauri::command]` function, however it is declared. */
const COMMAND_PATTERN = /#\[tauri::command[^\]]*\]\s*(?:#\[[^\]]*\]\s*)*pub\s+(?:async\s+)?fn\s+(\w+)/g

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


/** A function's named parameters; a destructured one is an options object and has none. */
function paramNames(fn) {
  const source = fn.toString()
  const start = source.indexOf('(')
  let depth = 0
  let end = -1
  for (let i = start; i < source.length; i++) {
    if ('([{'.includes(source[i])) depth++
    else if (')]}'.includes(source[i]) && --depth === 0) {
      end = i
      break
    }
  }
  const names = []
  let part = ''
  depth = 0
  for (const ch of source.slice(start + 1, end) + ',') {
    if ('([{'.includes(ch)) depth++
    else if (')]}'.includes(ch)) depth--
    if (ch === ',' && depth === 0) {
      const name = part.split('=')[0].trim()
      if (name && !/^[{[]/.test(name)) names.push(name)
      part = ''
    } else part += ch
  }
  return names
}

/**
 * What Electron's call carries, keyed as Tauri's payload must be: its own
 * object when it sends one, else the preload's parameter names zipped onto
 * the positional values.
 */
function keyed(fn, args) {
  const [first] = args
  if (args.length === 1 && first && typeof first === 'object') return first
  return Object.fromEntries(paramNames(fn).map((name, i) => [name, args[i]]))
}

/** Arguments that name each parameter uniquely; the install calls take an options object. */
const OPTIONS_CALLS = new Set(['extensions.installLocal', 'extensions.installFromCatalogue'])
const argsFor = (path) => (OPTIONS_CALLS.has(path) ? ['s1', { grant: 's2', config: 's3' }] : ['s1', 's2'])

const members = (api) => Object.entries(tree(api)).filter(([, kind]) => kind.startsWith('function'))
const at = (api, path) => path.split('.').reduce((o, k) => o[k], api)

let electron
let agentrq
beforeAll(async () => {
  // The bridge wraps fetch and EventSource on the page's window first (net.js).
  globalThis.window = Object.assign(new EventTarget(), {
    location: { href: 'app://localhost/' },
    fetch: () => {},
    EventSource: class {},
  })
  tauri.getTauriVersion.mockResolvedValue('2.12.5')
  // The menu-key listener asks for its table as the bridge loads (WP5).
  tauri.invoke.mockResolvedValue([])
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
      expect(payload ?? {}).toEqual(keyed(at(electron.exposed.agentrq, path), electron.invoked[0].args))
    })
  })

  describe('every listener', () => {
    const listeners = members(loadPreload().exposed.agentrq).filter(([path]) => /\.on[A-Z]/.test(path))

    it.each(listeners.map(([path]) => [path]))('%s listens to the same channel, and unsubscribes', async (path) => {
      electron.listened.length = 0
      const unlisten = vi.fn()
      tauri.invoke.mockReset().mockResolvedValue(undefined)
      tauri.listen.mockReset().mockResolvedValue(unlisten)
      const off = at(electron.exposed.agentrq, path)(() => {})
      const ours = at(agentrq, path)(() => {})
      expect(electron.listened).toHaveLength(1)
      expect(tauri.listen).toHaveBeenCalledTimes(1)
      expect(tauri.listen.mock.calls[0][0]).toBe(electron.listened[0])
      expect(typeof off).toBe(typeof ours)
      expect(typeof ours).toBe('function')
      await tauri.listen.mock.results[0].value
      await Promise.resolve()
      expect(unlisten).not.toHaveBeenCalled()
      ours()
      expect(unlisten).toHaveBeenCalledTimes(1)
    })
  })
})

describe('the command pattern', () => {
  const names = (source) => [...source.matchAll(COMMAND_PATTERN)].map((m) => m[1])

  it('finds sync, async and attribute-laden commands, and nothing else', () => {
    const source = `
      #[tauri::command]
      pub fn plain() {}
      #[tauri::command]
      pub async fn asynchronous() {}
      #[tauri::command(rename_all = "snake_case")]
      #[allow(clippy::too_many_arguments)]
      pub async fn decorated() {}
      #[tauri::command]
      fn private() {}
      pub fn unmarked() {}
    `
    expect(names(source)).toEqual(['plain', 'asynchronous', 'decorated'])
  })
})

describe('the Rust side serves what the bridge calls', () => {
  // The handler lives in commands.rs; the commands of a work package beside it.
  const SOURCES = ['commands', 'chrome', 'app_menu']
  const sources = SOURCES.map((name) => read(`../src-tauri/src/${name}.rs`))
  const rust = sources[0]
  const defined = sources.flatMap((source) => [...source.matchAll(COMMAND_PATTERN)].map((m) => m[1])).sort()
  const handler = rust
    .match(/generate_handler!\[([^\]]*)\]/)[1]
    .split(',')
    .map((s) => s.trim().replace(/^crate::\w+::/, ''))
    .filter(Boolean)
    .sort()
  const manifest = read('../src-tauri/build.rs').match(/COMMANDS: &\[&str\] = &\[([^\]]*)\]/)[1]
  const named = [...manifest.matchAll(/"(\w+)"/g)].map((m) => m[1]).sort()
  const capability = JSON.parse(read('../src-tauri/capabilities/main.json'))
  const allowed = capability.permissions
    .filter((p) => p.startsWith('allow-'))
    .map((p) => p.slice('allow-'.length).replace(/-/g, '_'))
    .sort()

  // The guest preload's, which WP7b gives the `agentrq-ext:` page's own webview.
  const GUEST_ONLY = ['panel_send']
  // The bridge's own: the key listener's menu commands (Electron's menu runs in
  // the main process) and `EventSource` (net.js); Electron has no channel for either.
  const BRIDGE_ONLY = ['event_source_close', 'event_source_open', 'menu_key_bindings', 'menu_run']

  it('has one command per channel the preload invokes, plus the guest one and the bridge\'s own', () => {
    const preload = read('../../desktop/src/preload/index.js')
    const channels = [...preload.matchAll(/invoke\(\s*'(agentrq:[\w:-]+)'/g)].map((m) => commandOf(m[1]))
    expect(defined).toEqual([...new Set([...channels, ...GUEST_ONLY, ...BRIDGE_ONLY])].sort())
    const net = read('../src/bridge/net.js')
    expect([...new Set([...net.matchAll(/invoke\('(\w+)'/g)].map((m) => m[1]))].sort()).toEqual(['event_source_close', 'event_source_open'])
  })

  it('registers every command in the handler, the manifest and the capability', () => {
    expect(handler).toEqual(defined)
    expect(named).toEqual(defined)
    expect(allowed).toEqual(defined.filter((name) => !GUEST_ONLY.includes(name)))
  })

  it('invokes only commands that exist', async () => {
    tauri.invoke.mockReset().mockResolvedValue(undefined)
    for (const [path] of members(agentrq).filter(([p]) => !/\.on[A-Z]/.test(p))) {
      await at(agentrq, path)(...argsFor(path))
    }
    // The key listener calls its two on a key, as a page would.
    let keydown
    const target = { addEventListener: (_, fn) => (keydown = fn) }
    tauri.invoke.mockImplementation((name) =>
      Promise.resolve(name === 'menu_key_bindings' ? [{ id: 'close', key: 'w', code: 'KeyW', ctrl: true, shift: false, alt: false, meta: false }] : undefined)
    )
    const { installMenuKeys } = await import('../src/bridge/index.js')
    installMenuKeys(target, 'linux')
    await Promise.resolve()
    await Promise.resolve()
    vi.useFakeTimers()
    keydown({ key: 'w', ctrlKey: true, shiftKey: false, altKey: false, metaKey: false })
    vi.runAllTimers()
    vi.useRealTimers()
    const invoked = new Set(tauri.invoke.mock.calls.map(([name]) => name))
    expect([...invoked].filter((name) => !defined.includes(name))).toEqual([])
    // Left over: the guest's own, and net.js's EventSource, which this walk of
    // `window.agentrq` does not reach.
    expect(defined.filter((name) => !invoked.has(name))).toEqual(
      [...BRIDGE_ONLY.filter((name) => name.startsWith('event_source_')), ...GUEST_ONLY].sort()
    )
  })

  // A mismatch must reject, not pass `None`: Tauri maps a payload key onto the
  // command's parameter (`fileUrl` onto `file_url`), and an `Option` parameter
  // turns a key that is not there into `None` without a word.
  it('names each argument the way the command takes it, and requires what the page always sends', async () => {
    const camel = (name) => name.replace(/_([a-z])/g, (_, c) => c.toUpperCase())
    const INJECTED = /^(?:tauri::)?(?:AppHandle|WebviewWindow|Window|Webview|State)\b/
    const params = new Map()
    for (const source of sources) {
      for (const m of source.matchAll(
        /#\[tauri::command[^\]]*\]\s*(?:#\[[^\]]*\]\s*)*pub\s+(?:async\s+)?fn\s+(\w+)(?:<[^>]*>)?\(([^)]*)\)/g
      )) {
        const list = m[2]
          .split(/,\s*(?=\w+\s*:)/)
          .map((p) => p.trim())
          .filter(Boolean)
          .map((p) => ({ name: camel(p.slice(0, p.indexOf(':')).trim()), type: p.slice(p.indexOf(':') + 1).trim() }))
          .filter((p) => !INJECTED.test(p.type))
        params.set(m[1], list)
      }
    }
    // A work package's stub takes no arguments until the package replaces it.
    const stubs = new Set([...rust.matchAll(/pub fn (\w+)\(\)[^{]*\{\s*todo\(/g)].map((m) => m[1]))
    tauri.invoke.mockReset().mockResolvedValue(undefined)
    for (const [path] of members(agentrq).filter(([p]) => !/\.on[A-Z]/.test(p))) {
      await at(agentrq, path)(...argsFor(path))
    }
    const bridge = tauri.invoke.mock.calls.filter(([n]) => !stubs.has(n))
    expect(stubs.size).toBeGreaterThan(0)
    expect(bridge.length).toBeGreaterThan(0)
    for (const [command, payload] of bridge) {
      const taken = params.get(command)
      expect(taken, command).toBeDefined()
      expect(taken.map((p) => p.name).sort(), command).toEqual(Object.keys(payload ?? {}).sort())
    }
    // The commands of this package take what the page always sends as required.
    const required = { files_open: ['fileUrl'], clipboard_write: ['text'], view_zoom: ['direction'], theme_set: ['theme'], menu_run: ['id'] }
    for (const [command, names] of Object.entries(required)) {
      for (const name of names) {
        const param = params.get(command).find((p) => p.name === name)
        expect(param?.type, `${command}.${name}`).not.toMatch(/^Option</)
      }
    }
  })

  it('grants the main window events and the title-bar drag, and nothing broader', () => {
    const core = capability.permissions.filter((p) => !p.startsWith('allow-'))
    expect(core).toEqual([
      'core:event:allow-listen',
      'core:event:allow-unlisten',
      'core:app:allow-tauri-version',
      'core:window:allow-start-dragging',
      'core:window:allow-internal-toggle-maximize',
    ])
    expect(capability.webviews).toEqual(['main'])
    expect(capability.windows).toBeUndefined()
    expect(capability.remote).toBeUndefined()
  })
})
