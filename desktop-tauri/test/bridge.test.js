// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'

const tauri = vi.hoisted(() => ({
  invoke: vi.fn(),
  listen: vi.fn(),
  getTauriVersion: vi.fn(),
}))
vi.mock('@tauri-apps/api/core', () => ({ invoke: tauri.invoke }))
vi.mock('@tauri-apps/api/event', () => ({ listen: tauri.listen }))
vi.mock('@tauri-apps/api/app', () => ({ getTauriVersion: tauri.getTauriVersion }))

let bridge
beforeAll(async () => {
  globalThis.window = {}
  tauri.getTauriVersion.mockResolvedValue('2.12.5')
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
  it('is installed on window when the module loads', () => {
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
  const REDO = { id: 'redo', key: 'z', code: 'KeyZ', ctrl: true, shift: true, alt: false, meta: false }
  const CLOSE = { id: 'close', key: 'w', code: 'KeyW', ctrl: true, shift: false, alt: false, meta: false }
  const PLUS = { id: 'zoom-in', key: '+', code: null, ctrl: true, shift: true, alt: false, meta: false }
  const MINUS = { id: 'zoom-out', key: '-', code: null, ctrl: true, shift: false, alt: false, meta: false }

  /** A window that keeps its `keydown` listener, with the bindings the shell would answer. */
  async function install(platform, bindings = [CLOSE, REDO, PLUS, MINUS]) {
    let listener
    const target = { addEventListener: (type, fn) => type === 'keydown' && (listener = fn) }
    tauri.invoke.mockImplementation((command) =>
      command === 'menu_key_bindings' ? Promise.resolve(bindings) : Promise.resolve(true)
    )
    bridge.installMenuKeys(target, platform)
    await Promise.resolve()
    await Promise.resolve()
    return (init) => {
      const e = { key: 'x', ctrlKey: false, shiftKey: false, altKey: false, metaKey: false, ...init }
      e.defaultPrevented = e.defaultPrevented ?? false
      e.preventDefault = vi.fn()
      listener?.(e)
      vi.runAllTimers()
      return e
    }
  }

  const runs = () => tauri.invoke.mock.calls.filter(([c]) => c === 'menu_run').map(([, a]) => a)

  beforeEach(() => vi.useFakeTimers())
  afterEach(() => vi.useRealTimers())

  it('runs the action of a key the page left unhandled', async () => {
    const press = await install('linux')
    press({ key: 'w', ctrlKey: true })
    expect(runs()).toEqual([{ id: 'close' }])
  })

  it('leaves a key the page handled to the page', async () => {
    const press = await install('linux')
    press({ key: 'w', ctrlKey: true, defaultPrevented: true })
    press({ key: 'w', ctrlKey: true, isComposing: true })
    expect(runs()).toEqual([])
  })

  it("waits for the page's own listeners, which run after this one, before it decides", async () => {
    const listeners = []
    const target = { addEventListener: (_, fn) => listeners.push(fn) }
    tauri.invoke.mockImplementation((command) =>
      Promise.resolve(command === 'menu_key_bindings' ? [CLOSE] : true)
    )
    bridge.installMenuKeys(target, 'linux')
    await Promise.resolve()
    await Promise.resolve()
    const dispatch = (cancel) => {
      const e = { key: 'w', ctrlKey: true, shiftKey: false, altKey: false, metaKey: false, defaultPrevented: false }
      listeners.push((ev) => cancel && (ev.defaultPrevented = true))
      for (const fn of listeners) fn(e)
      listeners.pop()
      // Not before the page has had its turn.
      expect(runs()).toEqual([])
      vi.runAllTimers()
    }
    dispatch(true)
    expect(runs()).toEqual([])
    dispatch(false)
    expect(runs()).toEqual([{ id: 'close' }])
  })

  it('ignores a held key, except for zoom', async () => {
    const press = await install('linux')
    press({ key: 'w', ctrlKey: true, repeat: true })
    expect(runs()).toEqual([])
    press({ key: '-', ctrlKey: true, repeat: true })
    press({ key: '+', ctrlKey: true, shiftKey: true, repeat: true })
    expect(runs()).toEqual([{ id: 'zoom-out' }, { id: 'zoom-in' }])
  })

  it('matches a letter by its physical key when the layout gives a non-Latin one', async () => {
    const press = await install('linux')
    press({ key: 'ц', code: 'KeyW', ctrlKey: true })
    expect(runs()).toEqual([{ id: 'close' }])
    // A Latin layout that moves the letter is matched by what it types, as Electron does.
    press({ key: ',', code: 'KeyW', ctrlKey: true })
    press({ key: 'ц', code: 'KeyE', ctrlKey: true })
    press({ key: 'Dead', ctrlKey: true })
    expect(runs()).toEqual([{ id: 'close' }])
  })

  it('matches every modifier exactly, and the key whatever its case', async () => {
    const press = await install('win32')
    press({ key: 'w' })
    press({ key: 'w', ctrlKey: true, shiftKey: true })
    press({ key: 'w', ctrlKey: true, altKey: true })
    press({ key: 'w', ctrlKey: true, metaKey: true })
    press({ key: 'q', ctrlKey: true })
    expect(runs()).toEqual([])
    press({ key: 'Z', ctrlKey: true, shiftKey: true })
    press({ key: '+', ctrlKey: true, shiftKey: true })
    expect(runs()).toEqual([{ id: 'redo' }, { id: 'zoom-in' }])
  })

  it('waits for the table, and survives a shell that cannot give it', async () => {
    let listener
    const target = { addEventListener: (_, fn) => (listener = fn) }
    tauri.invoke.mockRejectedValue(new Error('denied'))
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    bridge.installMenuKeys(target, 'linux')
    await Promise.resolve()
    await Promise.resolve()
    listener({ key: 'w', ctrlKey: true, preventDefault: vi.fn() })
    vi.runAllTimers()
    expect(error).toHaveBeenCalled()
    expect(runs()).toEqual([])

    tauri.invoke.mockResolvedValue('not a list')
    bridge.installMenuKeys(target, 'linux')
    await Promise.resolve()
    await Promise.resolve()
    listener({ key: 'w', ctrlKey: true, preventDefault: vi.fn() })
    vi.runAllTimers()
    expect(runs()).toEqual([])
  })

  it('reports an action the shell could not run', async () => {
    const press = await install('linux')
    const error = vi.spyOn(console, 'error').mockImplementation(() => {})
    tauri.invoke.mockRejectedValue(new Error('no'))
    press({ key: 'w', ctrlKey: true })
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
