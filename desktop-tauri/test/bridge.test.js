// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const tauri = vi.hoisted(() => ({
  invoke: vi.fn(),
  listen: vi.fn(),
  getTauriVersion: vi.fn(),
}))
vi.mock('@tauri-apps/api/core', () => ({ invoke: tauri.invoke, Channel: class {} }))
vi.mock('@tauri-apps/api/event', () => ({ listen: tauri.listen }))
vi.mock('@tauri-apps/api/app', () => ({ getTauriVersion: tauri.getTauriVersion }))

let bridge
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
  bridge = await import('../src/bridge/index.js')
})

/** A `listen` whose listeners the test can fire, and whose unlisten it can see. */
function fakeListen() {
  const handlers = new Map()
  const unlisten = vi.fn()
  tauri.listen.mockImplementation((event, handler) => {
    handlers.set(event, handler)
    return Promise.resolve(unlisten)
  })
  return { emit: (event, payload) => handlers.get(event)({ payload }), unlisten }
}

beforeEach(() => {
  tauri.invoke.mockReset().mockResolvedValue(undefined)
  tauri.listen.mockReset()
  tauri.getTauriVersion.mockReset().mockResolvedValue('2.12.5')
})

afterEach(() => vi.restoreAllMocks())

describe('platformOf', () => {
  it('names the OS the way process.platform does', () => {
    expect(bridge.platformOf('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit')).toBe('darwin')
    expect(bridge.platformOf('Mozilla/5.0 (Windows NT 10.0; Win64; x64) Edg/141')).toBe('win32')
    expect(bridge.platformOf('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit')).toBe('linux')
    expect(bridge.platformOf(undefined)).toBe('linux')
  })
})

describe('window.agentrq', () => {
  it('is installed on window when the module loads, after the fetch and EventSource the scheme needs', () => {
    expect(window.EventSource.CLOSED).toBe(2)
    expect(window.agentrq.isDesktop).toBe(true)
    expect(window.agentrq.platform).toBe(bridge.platformOf(navigator.userAgent))
  })

  it('cannot be replaced by page code, nor can its members', () => {
    const descriptor = Object.getOwnPropertyDescriptor(window, 'agentrq')
    expect(descriptor).toMatchObject({ writable: false, configurable: false, enumerable: true })
    const original = window.agentrq
    const save = original.connection.save
    expect(() => {
      'use strict'
      window.agentrq = {}
    }).toThrow(TypeError)
    expect(() => {
      'use strict'
      original.connection.save = () => 'hijacked'
    }).toThrow(TypeError)
    expect(window.agentrq).toBe(original)
    expect(window.agentrq.connection.save).toBe(save)
    expect(Object.isFrozen(original)).toBe(true)
    expect(Object.isFrozen(original.connection)).toBe(true)
    expect(Object.isFrozen(original.sidePanel.guest)).toBe(true)
  })

  it('carries the webview, and the Tauri version once it is known', async () => {
    const agentrq = bridge.createBridge('Macintosh')
    expect(agentrq.versions).toEqual({ tauri: '', webview: 'Macintosh' })
    await vi.waitFor(() => expect(agentrq.versions.tauri).toBe('2.12.5'))
    expect(bridge.createBridge(undefined).versions.webview).toBe('')
  })

  it('still boots when the Tauri version cannot be read', async () => {
    tauri.getTauriVersion.mockRejectedValue(new Error('denied'))
    const agentrq = bridge.createBridge('Linux')
    await Promise.resolve()
    expect(agentrq.versions.tauri).toBe('')
  })

  it('sends a call as one command with an object keyed by the preload parameter names', async () => {
    const agentrq = bridge.createBridge('Linux')
    await agentrq.attachments.forgetTask('w1', 't1')
    expect(tauri.invoke).toHaveBeenCalledWith('attachments_forget_task', { workspaceId: 'w1', taskId: 't1' })
    await agentrq.sidePanel.answerPermission(7, 'allow')
    expect(tauri.invoke).toHaveBeenCalledWith('side_panel_permission_answer', { id: 7, decision: 'allow' })
  })

  it('answers with what the command answers, and rejects with what it rejects', async () => {
    const agentrq = bridge.createBridge('Linux')
    tauri.invoke.mockResolvedValueOnce({ configured: true })
    await expect(agentrq.connection.get()).resolves.toEqual({ configured: true })
    tauri.invoke.mockRejectedValueOnce(new Error('not implemented yet'))
    await expect(agentrq.profiles.get()).rejects.toThrow('not implemented yet')
  })

  it('defaults the options of an install and the context of an entry, as the preload does', async () => {
    const agentrq = bridge.createBridge('Linux')
    await agentrq.extensions.installLocal('/x')
    expect(tauri.invoke).toHaveBeenLastCalledWith('extensions_install_local', { path: '/x', grant: null, config: null })
    await agentrq.extensions.installFromCatalogue('o/r', { grant: 'g' })
    expect(tauri.invoke).toHaveBeenLastCalledWith('extensions_install_catalogue', { fullName: 'o/r', grant: 'g', config: null })
    await agentrq.extensions.entries('page')
    expect(tauri.invoke).toHaveBeenLastCalledWith('extensions_entries', { surface: 'page', context: {} })
    await agentrq.extensions.invoke('t')
    expect(tauri.invoke).toHaveBeenLastCalledWith('extensions_invoke', { target: 't', context: {} })
  })

  it('declares the Tauri side-panel guest', () => {
    expect(bridge.createBridge('Linux').sidePanel.guest).toBe('tauri')
  })
})

describe('an on* listener', () => {
  it('hands the callback the payload, never the event', async () => {
    const { emit } = fakeListen()
    const agentrq = bridge.createBridge('Linux')
    const cb = vi.fn()
    agentrq.navigation.onNavigate(cb)
    emit('agentrq:navigate', '/workspaces/w')
    expect(cb).toHaveBeenCalledExactlyOnceWith('/workspaces/w')
  })

  it('calls a no-argument callback with nothing', () => {
    const { emit } = fakeListen()
    const agentrq = bridge.createBridge('Linux')
    const cb = vi.fn()
    agentrq.sidePanel.onToggle(cb)
    agentrq.sidePanel.onFind(cb)
    emit('agentrq:side-panel:toggle', { leaked: true })
    emit('agentrq:side-panel:find', { leaked: true })
    expect(cb).toHaveBeenCalledTimes(2)
    expect(cb.mock.calls).toEqual([[], []])
  })

  it.each([
    ['extensions', 'onChanged', 'agentrq:extensions:changed'],
    ['sidePanel', 'onOpen', 'agentrq:side-panel:open'],
    ['sidePanel', 'onPermissionRequest', 'agentrq:side-panel:permission-request'],
    ['sidePanel', 'onPermissionSettled', 'agentrq:side-panel:permission-settled'],
  ])('%s.%s hands the callback its payload', (namespace, member, event) => {
    const { emit } = fakeListen()
    const cb = vi.fn()
    bridge.createBridge('Linux')[namespace][member](cb)
    emit(event, { id: 1 })
    expect(cb).toHaveBeenCalledExactlyOnceWith({ id: 1 })
  })

  it('unsubscribes with the function it returns', async () => {
    const { unlisten } = fakeListen()
    const off = bridge.createBridge('Linux').extensions.onChanged(() => {})
    await Promise.resolve()
    await Promise.resolve()
    expect(off()).toBeUndefined()
    expect(unlisten).toHaveBeenCalledTimes(1)
    off()
    expect(unlisten).toHaveBeenCalledTimes(1)
  })

  it('applies an unsubscribe that comes before the listener is ready', async () => {
    const { unlisten } = fakeListen()
    const off = bridge.createBridge('Linux').sidePanel.onOpen(() => {})
    off()
    expect(unlisten).not.toHaveBeenCalled()
    await vi.waitFor(() => expect(unlisten).toHaveBeenCalledTimes(1))
  })

  it('drops an event that arrives after an early unsubscribe', async () => {
    const { emit, unlisten } = fakeListen()
    const cb = vi.fn()
    const off = bridge.createBridge('Linux').sidePanel.onOpen(cb)
    off()
    emit('agentrq:side-panel:open', '/late')
    await vi.waitFor(() => expect(unlisten).toHaveBeenCalledTimes(1))
    expect(cb).not.toHaveBeenCalled()
  })

  it('reports a listener that could not be attached, and still returns an unsubscribe', async () => {
    const err = vi.spyOn(console, 'error').mockImplementation(() => {})
    tauri.listen.mockRejectedValue(new Error('not allowed'))
    const off = bridge.createBridge('Linux').sidePanel.onPermissionSettled(() => {})
    await vi.waitFor(() => expect(err).toHaveBeenCalled())
    expect(err.mock.calls[0][0]).toContain('agentrq:side-panel:permission-settled')
    expect(() => off()).not.toThrow()
  })

  it('delivers the update state now as well as on every change', async () => {
    const { emit } = fakeListen()
    tauri.invoke.mockResolvedValue({ status: 'idle' })
    const cb = vi.fn()
    bridge.createBridge('Linux').updates.onStatus(cb)
    await vi.waitFor(() => expect(cb).toHaveBeenCalledWith({ status: 'idle' }))
    expect(tauri.invoke).toHaveBeenCalledWith('update_get')
    emit('agentrq:update:status', { status: 'downloaded' })
    expect(cb).toHaveBeenLastCalledWith({ status: 'downloaded' })
  })

  it('ignores a current state that cannot be read', async () => {
    fakeListen()
    tauri.invoke.mockRejectedValue(new Error('todo'))
    const cb = vi.fn()
    bridge.createBridge('Linux').updates.onStatus(cb)
    await Promise.resolve()
    await Promise.resolve()
    expect(cb).not.toHaveBeenCalled()
  })
})

describe('the menu keys', () => {
  const key = (id, k, code, shift = false) => ({
    id, key: k, code, ctrl: true, shift, alt: false, meta: false,
  })
  const CLOSE = key('close', 'w', 'KeyW')
  const PLUS = { ...key('zoom-in', '+', 'Equal', true), repeat: true }
  const MINUS = { ...key('zoom-out', '-', 'Minus'), repeat: true }
  const RELOAD = key('reload', 'r', 'KeyR')

  /** A real event target, with the bindings the shell would answer. */
  async function install(platform, bindings = [CLOSE, PLUS, MINUS, RELOAD], before) {
    const target = new EventTarget()
    before?.(target)
    tauri.invoke.mockImplementation((command) =>
      command === 'menu_key_bindings' ? Promise.resolve(bindings) : Promise.resolve(true)
    )
    bridge.installMenuKeys(target, platform)
    await Promise.resolve()
    await Promise.resolve()
    return Object.assign(
      (init) => {
        const e = Object.assign(new Event('keydown', { cancelable: true }), {
          key: 'x', code: '', ctrlKey: false, shiftKey: false, altKey: false, metaKey: false,
          ...init,
        })
        target.dispatchEvent(e)
        return e
      },
      { target }
    )
  }

  const runs = () => tauri.invoke.mock.calls.filter(([c]) => c === 'menu_run').map(([, a]) => a)
  const later = () => vi.runAllTimers()

  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    vi.useRealTimers()
    vi.restoreAllMocks()
  })

  it('runs the action of a key the page left unhandled', async () => {
    const press = await install('linux')
    press({ key: 'w', ctrlKey: true })
    expect(runs()).toEqual([])
    later()
    expect(runs()).toEqual([{ id: 'close' }])
  })

  it('leaves a key the page cancelled before us to the page', async () => {
    const press = await install('linux', undefined, (t) =>
      t.addEventListener('keydown', (e) => e.preventDefault())
    )
    press({ key: 'w', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('leaves a key the page cancels after us to the page', async () => {
    const press = await install('linux')
    // A page listener on window, registered after ours, runs after it.
    press.target.addEventListener('keydown', (e) => e.preventDefault())
    const e = press({ key: 'w', ctrlKey: true })
    expect(e.defaultPrevented).toBe(true)
    later()
    expect(runs()).toEqual([])
  })

  it('leaves a key typed into an IME composition to the IME', async () => {
    const press = await install('linux')
    press({ key: 'w', ctrlKey: true, isComposing: true })
    later()
    expect(runs()).toEqual([])
  })

  it('ignores a held key that does not repeat', async () => {
    const press = await install('linux')
    press({ key: 'r', ctrlKey: true, repeat: true })
    later()
    expect(runs()).toEqual([])
  })

  it('leaves undo and redo to the webview, held or not: the table has no key for them', async () => {
    const press = await install('linux')
    press({ key: 'z', ctrlKey: true })
    press({ key: 'z', ctrlKey: true, repeat: true })
    press({ key: 'Z', ctrlKey: true, shiftKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('repeats a held key that says it does', async () => {
    const press = await install('linux')
    press({ key: '+', ctrlKey: true, shiftKey: true, repeat: true })
    press({ key: '-', ctrlKey: true, repeat: true })
    later()
    expect(runs()).toEqual([{ id: 'zoom-in' }, { id: 'zoom-out' }])
  })

  it('matches every modifier exactly, and the key whatever its case', async () => {
    const press = await install('win32')
    press({ key: 'w' })
    press({ key: 'w', ctrlKey: true, shiftKey: true })
    press({ key: 'w', ctrlKey: true, altKey: true })
    press({ key: 'w', ctrlKey: true, metaKey: true })
    press({ key: 'q', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
    press({ key: 'W', ctrlKey: true })
    press({ key: '+', ctrlKey: true, shiftKey: true })
    later()
    expect(runs()).toEqual([{ id: 'close' }, { id: 'zoom-in' }])
  })

  it('falls back on the physical key for a letter of another script', async () => {
    const press = await install('linux')
    for (const key of ['ц', 'ω', 'ש', 'ض', '中']) press({ key, code: 'KeyW', ctrlKey: true })
    later()
    expect(runs()).toHaveLength(5)
    expect(runs().every((r) => r.id === 'close')).toBe(true)
  })

  it('types a Latin letter by the letter, not the place: AZERTY\'s z is QWERTY\'s KeyW', async () => {
    const press = await install('linux')
    press({ key: 'z', code: 'KeyW', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('does not zoom on a Latin layout\'s own punctuation or a dead key', async () => {
    const press = await install('linux')
    // German ß is on Minus, Spanish ¡ on Equal, Swedish ´ is a dead key on Equal.
    press({ key: 'ß', code: 'Minus', ctrlKey: true })
    press({ key: '¡', code: 'Equal', ctrlKey: true })
    press({ key: '´', code: 'Equal', ctrlKey: true })
    press({ key: 'Dead', code: 'Equal', ctrlKey: true })
    press({ key: 'Dead', code: 'Minus', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('has no place for a key typed on no place', async () => {
    const press = await install('linux')
    press({ key: 'ц', code: 'KeyQ', ctrlKey: true })
    press({ key: 'ц', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('has no physical fallback for a binding with no code', async () => {
    const press = await install('linux', [{ ...CLOSE, code: null }])
    press({ key: 'ц', code: 'KeyW', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('waits for the table, and survives a shell that cannot give it', async () => {
    let listener
    const target = { addEventListener: (_, fn) => (listener = fn) }
    tauri.invoke.mockRejectedValue(new Error('denied'))
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    bridge.installMenuKeys(target, 'linux')
    await Promise.resolve()
    await Promise.resolve()
    listener({ key: 'w', ctrlKey: true })
    later()
    expect(error).toHaveBeenCalled()
    expect(runs()).toEqual([])

    tauri.invoke.mockResolvedValue('not a list')
    bridge.installMenuKeys(target, 'linux')
    await Promise.resolve()
    await Promise.resolve()
    listener({ key: 'w', ctrlKey: true })
    later()
    expect(runs()).toEqual([])
  })

  it('reports an action the shell could not run', async () => {
    const press = await install('linux')
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    tauri.invoke.mockRejectedValue(new Error('no'))
    press({ key: 'w', ctrlKey: true })
    later()
    await Promise.resolve()
    await Promise.resolve()
    expect(error).toHaveBeenCalled()
  })

  it('does nothing on macOS, whose menu bar takes its own keys', async () => {
    const target = { addEventListener: vi.fn() }
    bridge.installMenuKeys(target, 'darwin')
    expect(target.addEventListener).not.toHaveBeenCalled()
    expect(tauri.invoke).not.toHaveBeenCalled()
  })
})

describe('installing the bridge in a page', () => {
  it('listens for the menu keys when the page can listen, and keeps window.agentrq read-only', async () => {
    vi.resetModules()
    const addEventListener = vi.fn()
    globalThis.window = { addEventListener }
    await import('../src/bridge/index.js')
    expect(addEventListener).toHaveBeenCalledWith('keydown', expect.any(Function))
    expect(tauri.invoke).toHaveBeenCalledWith('menu_key_bindings')
    expect(Object.getOwnPropertyDescriptor(window, 'agentrq').writable).toBe(false)
  })
})
