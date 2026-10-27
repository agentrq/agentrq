// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The Tauri bridge must be `desktop/src/preload/index.js`, member for member.
// Both are loaded for real, with Electron's and Tauri's sides stubbed, so a
// change to either fails here until the other follows.

import { readdirSync, readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { beforeAll, describe, expect, it, vi } from 'vitest'

const tauri = vi.hoisted(() => ({ invoke: vi.fn(), listen: vi.fn(), emit: vi.fn().mockResolvedValue(undefined), getTauriVersion: vi.fn() }))
vi.mock('@tauri-apps/api/core', () => ({ invoke: tauri.invoke, Channel: class {} }))
vi.mock('@tauri-apps/api/event', () => ({ listen: tauri.listen, emit: tauri.emit }))
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

/**
 * The bridge's members Electron has no use for: it bundles Node, so its
 * extension host never needs it downloaded.
 */
const TAURI_ONLY = {
  'extensions.downloadNode': 'extensions_download_node',
  'extensions.cancelNodeDownload': 'extensions_cancel_node_download',
  'extensions.onNodeProgress': 'agentrq:extensions:node-progress',
}
const withoutTauriOnly = (members) =>
  Object.fromEntries(Object.entries(members).filter(([path]) => !(path in TAURI_ONLY)))
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
    expect(withoutTauriOnly(tree(agentrq))).toEqual(tree(electron.exposed.agentrq))
  })

  it('adds only the Node download, which Electron bundles instead', async () => {
    const ours = tree(agentrq)
    expect(Object.keys(ours).filter((path) => !(path in tree(electron.exposed.agentrq)))).toEqual(
      Object.keys(TAURI_ONLY),
    )
    for (const path of ['extensions.downloadNode', 'extensions.cancelNodeDownload']) {
      tauri.invoke.mockReset().mockResolvedValue(undefined)
      await at(agentrq, path)()
      expect(tauri.invoke).toHaveBeenCalledWith(TAURI_ONLY[path])
    }
    const unlisten = vi.fn()
    tauri.listen.mockReset().mockResolvedValue(unlisten)
    const heard = vi.fn()
    const off = agentrq.extensions.onNodeProgress(heard)
    expect(tauri.listen.mock.calls[0][0]).toBe(TAURI_ONLY['extensions.onNodeProgress'])
    tauri.listen.mock.calls[0][1]({ payload: { percent: 7 } })
    expect(heard).toHaveBeenCalledWith({ percent: 7 })
    await tauri.listen.mock.results[0].value
    await Promise.resolve()
    off()
    expect(unlisten).toHaveBeenCalledTimes(1)
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
  const SOURCES = ['commands', 'chrome', 'app_menu', 'side_panel', 'panel_permissions', 'extensions']
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

  // The Tauri side panel guest's (bridge/panel-guest.js): Electron's guest is a
  // `<webview>` element, which needs no channel. The guest itself may call no
  // command: `agentrq:panel:send` is a POST on its own scheme.
  const GUEST_ONLY = [
    'side_panel_guest_close',
    'side_panel_guest_find',
    'side_panel_guest_focus',
    'side_panel_guest_navigate',
    'side_panel_guest_open',
    'side_panel_guest_place',
    'side_panel_guest_snapshot',
    'side_panel_guest_stop_find',
  ]
  // The bridge's own: the key listener's menu commands (Electron's menu runs in
  // the main process), `EventSource` (net.js), `navigation_ready`, and the Node
  // download (TAURI_ONLY); Electron has no channel for any of them.
  const BRIDGE_ONLY = [
    'event_source_close',
    'event_source_open',
    'extensions_cancel_node_download',
    'extensions_download_node',
    'menu_key_bindings',
    'menu_run',
    'navigation_ready',
  ]

  it('has one command per channel the preload invokes, plus the guest one and the bridge\'s own', () => {
    const preload = read('../../desktop/src/preload/index.js')
    const channels = [...preload.matchAll(/invoke\(\s*'(agentrq:[\w:-]+)'/g)].map((m) => commandOf(m[1]))
    expect(defined).toEqual([...new Set([...channels, ...GUEST_ONLY, ...BRIDGE_ONLY])].sort())
    const net = read('../src/bridge/net.js')
    expect([...new Set([...net.matchAll(/invoke\('(\w+)'/g)].map((m) => m[1]))].sort()).toEqual(['event_source_close', 'event_source_open'])
    const guest = read('../src/bridge/panel-guest.js')
    expect([...new Set([...guest.matchAll(/(?:invoke|send)\('(\w+)'/g)].map((m) => m[1]))].sort()).toEqual(GUEST_ONLY)
    expect(defined).not.toContain('panel_send')
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
    // navigation.onNavigate invokes navigation_ready when subscribed
    agentrq.navigation.onNavigate(() => {})
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
    try {
      keydown({ key: 'w', ctrlKey: true, shiftKey: false, altKey: false, metaKey: false })
      vi.runAllTimers()
    } finally {
      vi.useRealTimers()
    }
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
    tauri.invoke.mockReset().mockResolvedValue(undefined)
    for (const [path] of members(agentrq).filter(([p]) => !/\.on[A-Z]/.test(p))) {
      await at(agentrq, path)(...argsFor(path))
    }
    const bridge = tauri.invoke.mock.calls
    expect(bridge.length).toBeGreaterThan(0)
    for (const [command, payload] of bridge) {
      const taken = params.get(command)
      expect(taken, command).toBeDefined()
      expect(taken.map((p) => p.name).sort(), command).toEqual(Object.keys(payload ?? {}).sort())
    }
    // The commands take what the page always sends as required; `profiles.add()`
    // may come with no label, which numbers the profile.
    const required = {
      files_open: ['fileUrl'],
      clipboard_write: ['text'],
      view_zoom: ['direction'],
      theme_set: ['theme'],
      menu_run: ['id'],
      connection_validate: ['url'],
      connection_save: ['url'],
      profiles_switch: ['id'],
      profiles_rename: ['id', 'label'],
      profiles_remove: ['id'],
      extensions_uninstall: ['name'],
      extensions_set_enabled: ['name', 'enabled'],
      extensions_configure: ['name'],
      extensions_set_hook_consent: ['name', 'level'],
    }
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

/**
 * Why a capability would reach a side panel guest, or `null`. Guests have
 * Tauri's IPC object like every webview, so the ACL is all that refuses them:
 * a `windows` grant covers the window's child webviews (guests are `main`'s),
 * a `remote` URL any page on it, and a webview pattern their labels. Tauri
 * matches labels as globs, so only a plain label is let through.
 */
function reachesGuests(capability) {
  if (capability.windows !== undefined) return 'is scoped to windows, which covers their child webviews'
  if (capability.remote !== undefined) return 'grants remote URLs'
  if (!Array.isArray(capability.webviews) || capability.webviews.length === 0) return 'names no webview'
  const glob = capability.webviews.find((label) => typeof label !== 'string' || /[*?[\]{}!\\]/.test(label))
  if (glob !== undefined) return `names a glob, which could match the guests' labels (${glob})`
  const guest = capability.webviews.find((label) => label.startsWith('panel-guest-'))
  return guest === undefined ? null : `names a guest (${guest})`
}

/** The files Tauri merges into its config: the base, each platform's, and a script's `--config`. */
const CONFIG = /^tauri(\.[a-z]+)?\.conf\.json5?$|^Tauri(\.[a-z]+)?\.toml$|\.conf\.json5?$/i

/**
 * Each config a command line hands the Tauri CLI, which merges it too: the
 * value of a `--config`, a `-c` given inline JSON, or a `TAURI_CONFIG`, each
 * as `{ inline }` (its JSON text, unquoted) or `{ file }`. A `-c` that is not
 * JSON is another program's (`bash -c '..'`).
 */
function configsGiven(text) {
  const given = /(--config(?:=|\s+)|(?<![\w-])-c\s+|TAURI_CONFIG\s*[=:]\s*)(?:'([^']*)'|"((?:[^"\\]|\\.)*)"|(\{\S*|[^\s'"]+))/g
  return [...text.matchAll(given)].flatMap(([, flag, single, double, bare]) => {
    const value = single ?? double?.replace(/\\(.)/g, '$1') ?? bare
    if (flag.startsWith('-c')) {
      // `jq -c '{..}'` is not JSON, and the CLI would refuse one that is not.
      if (!value.trimStart().startsWith('{')) return []
      try { JSON.parse(value) } catch { return [] }
    }
    if (value.trimStart().startsWith('{')) return [{ inline: value }]
    return flag.startsWith('-c') ? [] : [{ file: value }]
  })
}

/** What a runner's shell runs: package.json's scripts are JSON strings, so read them unescaped. */
function commandLines(runner) {
  const text = read(runner)
  return runner.endsWith('package.json') ? Object.values(JSON.parse(text).scripts ?? {}).join('\n') : text
}

/** Whether `file` is one of `checked`, by path: a name alone can belong to a file elsewhere. */
function isChecked(file, checked) {
  const path = file.replace(/\\/g, '/').replace(/^(\.\.?\/)+/, '')
  return checked.some((known) => path === known || path === known.split('/').pop())
}

/** Every file under `dir`, as a path from it. */
function filesUnder(dir) {
  return readdirSync(here(dir), { recursive: true, withFileTypes: true })
    .filter((entry) => entry.isFile())
    .map((entry) => `${entry.parentPath.slice(here(dir).length)}/${entry.name}`.replace(/\\/g, '/').replace(/^\/+/, ''))
}

describe('no capability reaches a side panel guest', () => {
  it('holds for every capability file', () => {
    const files = readdirSync(here('../src-tauri/capabilities/'))
    expect(files).toContain('main.json')
    for (const file of files) {
      // Tauri reads TOML too, which this check would not.
      expect(file, 'a capability file').toMatch(/\.json$/)
      expect(reachesGuests(JSON.parse(read(`../src-tauri/capabilities/${file}`))), file).toBeNull()
    }
  })

  it('and no Tauri config adds one inline, on any platform', () => {
    const configs = [
      ...readdirSync(here('../src-tauri/')).filter((name) => CONFIG.test(name)).map((name) => `../src-tauri/${name}`),
      ...readdirSync(here('../scripts/')).filter((name) => CONFIG.test(name)).map((name) => `../scripts/${name}`),
    ]
    expect(configs).toEqual(expect.arrayContaining(['../src-tauri/tauri.conf.json', '../scripts/verify.tauri.conf.json']))
    for (const config of configs) {
      // JSON5 and TOML this check cannot read: keep to JSON.
      expect(config, 'a Tauri config').toMatch(/\.json$/)
      expect(JSON.parse(read(config)).app?.security?.capabilities, config).toBeUndefined()
    }
  })

  it('and no command line hands the CLI one inline, nor a config this check does not read', () => {
    const checked = [
      ...readdirSync(here('../src-tauri/')).filter((name) => CONFIG.test(name)).map((name) => `src-tauri/${name}`),
      ...readdirSync(here('../scripts/')).filter((name) => CONFIG.test(name)).map((name) => `scripts/${name}`),
    ]
    const runners = [
      ...readdirSync(here('../../.github/workflows/')).map((name) => `../../.github/workflows/${name}`),
      ...filesUnder('../scripts/').map((file) => `../scripts/${file}`),
      '../package.json',
      '../../Makefile',
    ]
    expect(runners).toContain('../../.github/workflows/desktop-release.yml')
    const inline = []
    for (const runner of runners) {
      for (const config of configsGiven(commandLines(runner))) {
        if (config.file !== undefined) {
          // Vite's and Vitest's are scripts; one in JSON or TOML is Tauri's.
          if (/\.(json5?|toml)$/i.test(config.file)) {
            expect(isChecked(config.file, checked), `${runner}: --config ${config.file}`).toBe(true)
          }
          continue
        }
        let parsed
        expect(() => (parsed = JSON.parse(config.inline)), `${runner}: ${config.inline}`).not.toThrow()
        expect(parsed.app?.security?.capabilities, `${runner}: ${config.inline}`).toBeUndefined()
        inline.push(config.inline)
      }
    }
    // desktop-release.yml's own, read as the CLI reads it.
    expect(inline).toContain('{"bundle":{"createUpdaterArtifacts":false}}')
  })

  it('reads each way a command line gives the CLI a config', () => {
    const capability = '{"app":{"security":{"capabilities":[{"identifier":"x","webviews":["*"],"permissions":[]}]}}}'
    const lines = [
      `args+=(--config '${capability}')`,
      `npx tauri build --config=${capability}`,
      `npx tauri build -c ${JSON.stringify(capability)}`,
      `  TAURI_CONFIG: '${capability}'`,
      `TAURI_CONFIG=${capability} npx tauri build`,
    ]
    for (const line of lines) expect(configsGiven(line), line).toEqual([{ inline: capability }])
    expect(configsGiven('tauri build --debug --config scripts/verify.tauri.conf.json')).toEqual([{ file: 'scripts/verify.tauri.conf.json' }])
    expect(configsGiven('"build": "tauri build --config scripts/other.json",')).toEqual([{ file: 'scripts/other.json' }])
    expect(configsGiven("vite build --config vite.renderer.config.mjs && bash -c 'echo -c x'")).toEqual([{ file: 'vite.renderer.config.mjs' }])
    expect(configsGiven('sudo dpkg --configure -a')).toEqual([])
    expect(configsGiven("jq -c '{name: .tag}' release.json")).toEqual([])
    // package.json's scripts, unescaped as commandLines reads them.
    const script = JSON.parse(JSON.stringify({ build: `tauri build --config "${capability.replace(/"/g, '\\"')}"` })).build
    expect(configsGiven(script)).toEqual([{ inline: capability }])
  })

  it('takes a config file by its path, not its name', () => {
    const checked = ['src-tauri/tauri.conf.json', 'scripts/verify.tauri.conf.json']
    for (const file of ['scripts/verify.tauri.conf.json', '../scripts/verify.tauri.conf.json', './tauri.conf.json', 'tauri.conf.json']) {
      expect(isChecked(file, checked), file).toBe(true)
    }
    for (const file of ['../other/tauri.conf.json', '/tmp/verify.tauri.conf.json', 'src-tauri/verify.tauri.conf.json']) {
      expect(isChecked(file, checked), file).toBe(false)
    }
  })

  it('and the shell adds none at run time, nor its build script', () => {
    const sources = filesUnder('../src-tauri/src/').map((file) => `../src-tauri/src/${file}`)
    expect(sources).toContain('../src-tauri/src/lib.rs')
    for (const source of [...sources, '../src-tauri/build.rs']) {
      expect(read(source), source).not.toMatch(/add_capability/)
    }
    // `capabilities_path_pattern(..)` would read capability files from anywhere.
    expect(read('../src-tauri/build.rs')).not.toMatch(/capabilit\w*\s*\(/i)
  })

  it('reads each platform\'s config and each script\'s', () => {
    for (const name of ['tauri.conf.json', 'tauri.linux.conf.json', 'tauri.windows.conf.json', 'tauri.macos.conf.json', 'Tauri.toml', 'Tauri.linux.toml', 'tauri.conf.json5', 'verify.tauri.conf.json']) {
      expect(CONFIG.test(name), name).toBe(true)
    }
    for (const name of ['Cargo.toml', 'package.json', 'main.json']) expect(CONFIG.test(name), name).toBe(false)
  })

  it('tells each way a capability would reach one', () => {
    expect(reachesGuests({ webviews: ['main'] })).toBeNull()
    expect(reachesGuests({ webviews: ['main', 'sign-in'] })).toBeNull()
    expect(reachesGuests({ windows: ['main'] })).toMatch(/windows/)
    expect(reachesGuests({ webviews: ['main'], remote: { urls: ['https://*'] } })).toMatch(/remote/)
    expect(reachesGuests({})).toMatch(/no webview/)
    expect(reachesGuests({ webviews: [] })).toMatch(/no webview/)
    for (const glob of ['*', 'panel-guest-*', 'panel-guest-4?', '[!x]anel-guest-*', '[p]anel-guest-1', '{main,panel-guest-1}', 'sign-in-*', 'p\\anel-guest-1']) {
      expect(reachesGuests({ webviews: ['main', glob] }), glob).toBe(`names a glob, which could match the guests' labels (${glob})`)
    }
    expect(reachesGuests({ webviews: [7] })).toMatch(/glob/)
    expect(reachesGuests({ webviews: ['panel-guest-1'] })).toMatch(/names a guest \(panel-guest-1\)/)
  })
})
