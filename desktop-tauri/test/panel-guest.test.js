// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { createOverlayWatch, createTauriGuest, GUEST_EVENT, overlaps } from '../src/bridge/panel-guest.js'

/** Just enough of an element: a tree, a box and a computed style. */
class El {
  constructor(tag) {
    this.tagName = tag
    this.nodeType = 1
    this.children = []
    this.parent = null
    this.isConnected = true
    this.style = {}
    this.computed = { position: 'static', visibility: 'visible', opacity: '1' }
    this.box = box(0, 0, 0, 0)
  }
  get firstChild() {
    return this.children[0]
  }
  get parentElement() {
    return this.parent
  }
  append(child) {
    child.parent = this
    this.children.push(child)
  }
  remove() {
    if (!this.parent) return
    this.parent.children = this.parent.children.filter((c) => c !== this)
    this.parent = null
  }
  contains(other) {
    for (let node = other; node; node = node.parent) if (node === this) return true
    return false
  }
  querySelectorAll() {
    const found = []
    const walk = (el) => el.children.forEach((c) => (found.push(c), walk(c)))
    walk(this)
    return found
  }
  getBoundingClientRect() {
    return this.box
  }
}

function box(left, top, width, height) {
  return { left, top, width, height, right: left + width, bottom: top + height }
}

function positioned(rect, position = 'fixed') {
  const el = new El('div')
  el.computed.position = position
  el.box = rect
  return el
}

function fakeWindow() {
  const frames = []
  const listeners = new Map()
  const body = new El('body')
  let blobs = 0
  const timers = []
  const win = {
    frames,
    listeners,
    timers,
    setTimeout: (callback) => timers.push(callback),
    clearTimeout: (id) => (timers[id - 1] = null),
    decoded: () => Promise.resolve(),
    console: { warn: vi.fn() },
    document: {
      body,
      activeElement: null,
      createElement(tag) {
        const el = new El(tag)
        if (tag === 'img') el.decode = () => win.decoded()
        return el
      },
    },
    getComputedStyle: (el) => el.computed,
    requestAnimationFrame: (callback) => frames.push(callback),
    URL: { createObjectURL: vi.fn(() => `blob:${++blobs}`), revokeObjectURL: vi.fn() },
    Blob: class {
      constructor(parts, options) {
        this.parts = parts
        this.type = options.type
      }
    },
    MutationObserver: class {
      constructor(callback) {
        win.mutated = callback
      }
      observe(target, options) {
        win.observed = options
      }
      disconnect() {
        win.mutationsStopped = true
      }
    },
    ResizeObserver: class {
      constructor(callback) {
        win.resized = callback
      }
      observe() {}
      disconnect() {
        win.resizeStopped = true
      }
    },
    addEventListener: (type, listener) => listeners.set(type, listener),
    removeEventListener: (type) => listeners.delete(type),
  }
  return win
}

const settle = async () => {
  for (let i = 0; i < 10; i++) await new Promise((resolve) => setTimeout(resolve, 0))
}

/** Run the frames asked for, and what they ask for in turn, then settle. */
async function frame(win) {
  await settle()
  while (win.frames.length) {
    win.frames.shift()()
    await settle()
  }
}

function rig({ snapshot = [137, 80, 78, 71], open = 'panel-guest-1' } = {}) {
  const win = fakeWindow()
  const host = new El('div')
  host.box = box(500, 50, 400, 600)
  win.document.body.append(host)
  let handler = null
  const unlisten = vi.fn()
  const invoke = vi.fn(async (command) => {
    if (command === 'side_panel_guest_open') return typeof open === 'function' ? open() : open
    if (command === 'side_panel_guest_snapshot') return typeof snapshot === 'function' ? snapshot() : snapshot
    return undefined
  })
  const listen = vi.fn(async (event, h) => {
    handler = h
    return unlisten
  })
  const guest = createTauriGuest({ invoke, listen, win })
  const emit = (payload) => handler({ payload })
  const calls = (command) => invoke.mock.calls.filter(([c]) => c === command).map(([, args]) => args)
  return { win, host, guest, invoke, listen, unlisten, emit, calls }
}

describe('overlaps', () => {
  it('needs shared area, and an empty box covers nothing', () => {
    const b = box(0, 0, 100, 100)
    expect(overlaps(box(50, 50, 100, 100), b)).toBe(true)
    expect(overlaps(box(100, 0, 10, 10), b)).toBe(false)
    expect(overlaps(box(0, 100, 10, 10), b)).toBe(false)
    expect(overlaps(box(50, 50, 0, 0), b)).toBe(false)
    expect(overlaps(box(50, 50, 10, 0), b)).toBe(false)
  })
})

describe('createOverlayWatch', () => {
  function setup() {
    const win = fakeWindow()
    const host = new El('div')
    host.box = box(500, 0, 400, 600)
    win.document.body.append(host)
    return { win, host, watch: createOverlayWatch(win, host) }
  }

  it('finds a positioned element over the host, and nothing else', () => {
    const { win, host, watch } = setup()
    const toast = positioned(box(800, 500, 200, 80))
    const aside = positioned(box(0, 0, 100, 100), 'absolute')
    const flow = new El('p')
    flow.box = box(500, 0, 100, 100)
    win.document.body.append(aside)
    win.document.body.append(flow)
    watch.scan(win.document.body)
    expect(watch.occluded()).toBe(false)

    win.document.body.append(toast)
    watch.mutated([{ type: 'childList', addedNodes: [toast, { nodeType: 3 }] }])
    expect(watch.occluded()).toBe(true)

    // Restyled out of the way, hidden, transparent.
    toast.computed.visibility = 'hidden'
    expect(watch.occluded()).toBe(false)
    toast.computed.visibility = 'visible'
    toast.computed.opacity = '0'
    expect(watch.occluded()).toBe(false)
    toast.computed.opacity = '1'
    // Inside a transparent wrapper (a dialog closing).
    const wrapper = new El('div')
    wrapper.computed.opacity = '0'
    win.document.body.append(wrapper)
    wrapper.append(toast)
    expect(watch.occluded()).toBe(false)
    wrapper.computed.opacity = '1'
    expect(watch.occluded()).toBe(true)
    toast.computed.position = 'static'
    expect(watch.occluded()).toBe(false)

    // Gone from the page: forgotten.
    toast.computed.position = 'fixed'
    toast.isConnected = false
    expect(watch.occluded()).toBe(false)
    toast.isConnected = true
    expect(watch.occluded()).toBe(false)

    // A class change that positions an element already there.
    flow.computed.position = 'absolute'
    watch.mutated([{ type: 'attributes', target: flow }])
    expect(watch.occluded()).toBe(true)
    expect(host).toBeTruthy()
  })

  it('never counts the host, what holds it, or what it holds', () => {
    const { win, host, watch } = setup()
    const panel = positioned(box(0, 0, 1000, 1000))
    const inner = positioned(box(500, 0, 400, 600), 'absolute')
    win.document.body.children = []
    win.document.body.append(panel)
    panel.append(host)
    host.append(inner)
    watch.scan(win.document.body)
    expect(watch.occluded()).toBe(false)
  })
})

describe('createTauriGuest', () => {
  it('opens a guest at the host, and places it as the host moves', async () => {
    const { win, host, guest, calls, listen } = rig()
    guest.mount(host, 'https://a.example/')
    expect(listen).toHaveBeenCalledWith(GUEST_EVENT, expect.any(Function))
    expect(calls('side_panel_guest_open')).toEqual([
      { url: 'https://a.example/', rect: { x: 500, y: 50, width: 400, height: 600 }, visible: true },
    ])
    expect(win.observed.attributeFilter).toEqual(['class', 'style', 'hidden', 'open'])
    await frame(win)
    // Where it was opened: nothing to send.
    expect(calls('side_panel_guest_place')).toEqual([])

    host.box = box(600, 50, 300, 600)
    win.resized()
    win.listeners.get('resize')()
    win.listeners.get('scroll')()
    expect(win.frames.length).toBe(1)
    await frame(win)
    expect(calls('side_panel_guest_place')).toEqual([
      { label: 'panel-guest-1', rect: { x: 600, y: 50, width: 300, height: 600 }, visible: true },
    ])

    guest.setVisible(false)
    await frame(win)
    expect(calls('side_panel_guest_place').at(-1).visible).toBe(false)

    // Zoomed: the same CSS box is another place in logical pixels.
    const placed = calls('side_panel_guest_place').length
    win.resized()
    await frame(win)
    expect(calls('side_panel_guest_place').length).toBe(placed)
    win.devicePixelRatio = 1.25
    win.resized()
    await frame(win)
    expect(calls('side_panel_guest_place').length).toBe(placed + 1)
    expect(calls('side_panel_guest_place').at(-1)).toEqual({
      label: 'panel-guest-1',
      rect: { x: 600, y: 50, width: 300, height: 600 },
      visible: false,
    })
  })

  it('hides the guest before a slow picture arrives, and shows the picture when it does', async () => {
    let shot
    const { win, host, guest, calls } = rig({ snapshot: () => new Promise((resolve) => (shot = resolve)) })
    guest.mount(host, 'https://a.example/')
    await frame(win)
    guest.setDragging(true)
    await frame(win)
    expect(calls('side_panel_guest_place')).toEqual([])
    // The wait is up: hidden, the host blank for now.
    win.timers.at(-1)()
    await settle()
    expect(calls('side_panel_guest_place').at(-1).visible).toBe(false)
    expect(host.children).toEqual([])
    shot([1, 2])
    await frame(win)
    expect(host.children.length).toBe(1)

    // A wait that ends after the thaw changes nothing.
    const never = rig({ snapshot: () => new Promise(() => {}) })
    never.guest.mount(never.host, 'https://a.example/')
    await frame(never.win)
    never.guest.setDragging(true)
    await frame(never.win)
    const wait = never.win.timers.at(-1)
    never.guest.setDragging(false)
    await frame(never.win)
    const placed = never.calls('side_panel_guest_place').length
    wait()
    await settle()
    expect(never.calls('side_panel_guest_place').length).toBe(placed)
  })

  it('hears its own guest, including what came before it had a name', async () => {
    const { win, host, guest, emit } = rig()
    const loading = vi.fn()
    const navigate = vi.fn()
    const fail = vi.fn()
    guest.on('loading', loading)
    const off = guest.on('navigate', navigate)
    guest.on('fail', fail)
    guest.mount(host, 'https://a.example/')
    await settle()
    // `listen` answered before `side_panel_guest_open` did? Then it is early.
    expect(guest.canGoBack()).toBe(false)

    emit({ kind: 'navigate', label: 'panel-guest-1', url: 'https://b.example/', inPage: true, canGoBack: true, canGoForward: true })
    emit({ kind: 'navigate', label: 'panel-guest-2', url: 'https://other.example/', inPage: false, canGoBack: false, canGoForward: false })
    emit({ kind: 'loading', label: 'panel-guest-1', loading: true, canGoBack: true, canGoForward: false })
    emit({ kind: 'fail', label: 'panel-guest-1', code: -2, description: 'Gone' })
    emit({ kind: 'unknown', label: 'panel-guest-1' })
    emit(undefined)
    expect(navigate).toHaveBeenCalledWith({ url: 'https://b.example/', inPage: true })
    expect(navigate).toHaveBeenCalledTimes(1)
    expect(loading).toHaveBeenCalledWith(true)
    expect(fail).toHaveBeenCalledWith({ code: -2, description: 'Gone' })
    expect(guest.canGoBack()).toBe(true)
    expect(guest.canGoForward()).toBe(false)

    const again = vi.fn()
    guest.on('navigate', again)
    off()
    emit({ kind: 'navigate', label: 'panel-guest-1', url: 'https://c.example/', inPage: false, canGoBack: true, canGoForward: false })
    expect(navigate).toHaveBeenCalledTimes(1)
    expect(again).toHaveBeenCalledWith({ url: 'https://c.example/', inPage: false })
    await frame(win)
  })

  it('keeps the events that arrive before its name', async () => {
    let answer
    const { host, guest, emit } = rig({ open: () => new Promise((resolve) => (answer = resolve)) })
    const navigate = vi.fn()
    guest.on('navigate', navigate)
    guest.mount(host, 'https://a.example/')
    await settle()
    emit({ kind: 'navigate', label: 'panel-guest-1', url: 'https://a.example/', inPage: false, canGoBack: false, canGoForward: false })
    expect(navigate).not.toHaveBeenCalled()
    // Heard by nobody.
    emit({ kind: 'loading', label: 'panel-guest-1', loading: true, canGoBack: false, canGoForward: false })
    answer('panel-guest-1')
    await settle()
    expect(navigate).toHaveBeenCalledWith({ url: 'https://a.example/', inPage: false })
  })

  it('takes the focus away from the page when the guest takes the keyboard', async () => {
    const { win, host, guest, emit } = rig()
    guest.mount(host, 'https://a.example/')
    await settle()
    emit({ kind: 'focus', label: 'panel-guest-1' })
    const address = new El('input')
    address.blur = vi.fn()
    win.document.activeElement = address
    emit({ kind: 'focus', label: 'panel-guest-1' })
    expect(address.blur).toHaveBeenCalled()
    win.document.activeElement = new El('div')
    expect(() => emit({ kind: 'focus', label: 'panel-guest-1' })).not.toThrow()
  })

  it('names its guest in every command, and none before it has one', async () => {
    const { host, guest, calls } = rig()
    expect(guest.reload()).toBeUndefined()
    guest.mount(host, 'https://a.example/')
    guest.loadURL('https://b.example/')
    guest.goBack()
    guest.goForward()
    guest.reload()
    guest.stop()
    guest.focus()
    guest.findInPage('x', {})
    guest.stopFindInPage('clearSelection')
    await settle()
    expect(calls('side_panel_guest_navigate')).toEqual([
      { label: 'panel-guest-1', action: 'load', url: 'https://b.example/' },
      { label: 'panel-guest-1', action: 'back' },
      { label: 'panel-guest-1', action: 'forward' },
      { label: 'panel-guest-1', action: 'reload' },
      { label: 'panel-guest-1', action: 'stop' },
    ])
    expect(calls('side_panel_guest_focus')).toEqual([{ label: 'panel-guest-1' }])
    expect(calls('side_panel_guest_find')).toEqual([{ label: 'panel-guest-1', text: 'x', forward: true, findNext: false }])
    expect(calls('side_panel_guest_stop_find')).toEqual([{ label: 'panel-guest-1', action: 'clearSelection' }])
  })

  it('finds as Electron does: a new search, then steps either way, and says what it found', async () => {
    const { host, guest, calls, emit } = rig()
    const found = vi.fn()
    guest.on('found', found)
    guest.mount(host, 'https://a.example/')
    guest.findInPage('cat', { findNext: true })
    guest.findInPage('cat', { forward: false, findNext: false })
    guest.findInPage('cat')
    await settle()
    expect(calls('side_panel_guest_find')).toEqual([
      { label: 'panel-guest-1', text: 'cat', forward: true, findNext: true },
      { label: 'panel-guest-1', text: 'cat', forward: false, findNext: false },
      { label: 'panel-guest-1', text: 'cat', forward: true, findNext: false },
    ])
    emit({ kind: 'found', label: 'panel-guest-1', active: 2, total: 5 })
    emit({ kind: 'found', label: 'panel-guest-2', active: 1, total: 1 })
    expect(found.mock.calls).toEqual([[{ active: 2, total: 5 }]])
  })

  it('swaps the guest for its picture while something is over it, and back', async () => {
    const { win, host, guest, calls } = rig()
    guest.mount(host, 'https://a.example/')
    await frame(win)
    const dialog = positioned(box(0, 0, 1000, 1000))
    win.document.body.append(dialog)
    win.mutated([{ type: 'childList', addedNodes: [dialog] }])
    await frame(win)

    expect(calls('side_panel_guest_snapshot')).toEqual([{ label: 'panel-guest-1' }])
    const picture = host.children[0]
    expect(picture.style.cssText).toContain('overflow:hidden')
    const img = picture.firstChild
    expect(img.alt).toBe('')
    expect(img.style.cssText).toContain('width:400px;height:600px;max-width:none')
    expect(win.URL.createObjectURL.mock.calls[0][0].parts[0]).toEqual(new Uint8Array([137, 80, 78, 71]))
    expect(calls('side_panel_guest_place').at(-1).visible).toBe(false)

    dialog.remove()
    dialog.isConnected = false
    win.mutated([])
    await frame(win)
    expect(calls('side_panel_guest_place').at(-1).visible).toBe(true)
    expect(host.children).toEqual([])
    expect(win.URL.revokeObjectURL).toHaveBeenCalledWith(img.src)
  })

  it('freezes while the panel edge is dragged', async () => {
    const { win, host, guest, calls } = rig({ snapshot: new ArrayBuffer(4) })
    guest.mount(host, 'https://a.example/')
    await frame(win)
    guest.setDragging(true)
    await frame(win)
    expect(host.children.length).toBe(1)
    // Still dragging: the guest stays hidden as the host moves.
    host.box = box(450, 50, 450, 600)
    win.resized()
    await frame(win)
    expect(calls('side_panel_guest_place').at(-1)).toEqual({
      label: 'panel-guest-1',
      rect: { x: 450, y: 50, width: 450, height: 600 },
      visible: false,
    })
    guest.setDragging(false)
    await frame(win)
    expect(host.children.length).toBe(0)
  })

  it('drops a picture that comes too late, or none at all', async () => {
    const late = rig()
    let release
    late.win.decoded = () => new Promise((resolve) => (release = resolve))
    late.guest.mount(late.host, 'https://a.example/')
    await frame(late.win)
    late.guest.setDragging(true)
    await frame(late.win)
    // Thawed while the picture was decoding: it never goes up.
    late.guest.setDragging(false)
    await frame(late.win)
    release()
    await frame(late.win)
    expect(late.host.children).toEqual([])
    expect(late.win.URL.revokeObjectURL).toHaveBeenCalled()

    // Thawed before the snapshot came back.
    let shot
    const slow = rig({ snapshot: () => new Promise((resolve) => (shot = resolve)) })
    slow.guest.mount(slow.host, 'https://a.example/')
    await frame(slow.win)
    slow.guest.setDragging(true)
    await frame(slow.win)
    slow.guest.setDragging(false)
    await frame(slow.win)
    shot([1, 2, 3])
    await frame(slow.win)
    expect(slow.host.children).toEqual([])
    expect(slow.win.URL.createObjectURL).not.toHaveBeenCalled()

    const failing = rig()
    failing.win.decoded = () => Promise.reject(new Error('not an image'))
    failing.guest.mount(failing.host, 'https://a.example/')
    await frame(failing.win)
    failing.guest.setDragging(true)
    await frame(failing.win)
    expect(failing.host.children.length).toBe(1)

    // No picture: the guest still hides, and nothing goes up.
    const none = rig({ snapshot: null })
    none.guest.mount(none.host, 'https://a.example/')
    await frame(none.win)
    none.guest.setDragging(true)
    await frame(none.win)
    expect(none.host.children).toEqual([])
    none.guest.setDragging(false)
    await frame(none.win)
    expect(none.calls('side_panel_guest_place').at(-1).visible).toBe(true)
  })

  it('keeps a newer picture when an older thaw lands late', async () => {
    const { win, host, guest } = rig()
    guest.mount(host, 'https://a.example/')
    await frame(win)
    guest.setDragging(true)
    await frame(win)
    guest.setDragging(false)
    win.frames.shift()()
    await settle()
    // The thaw's frame is waiting; the guest is torn down first.
    guest.destroy()
    while (win.frames.length) win.frames.shift()()
    expect(host.children).toEqual([])
  })

  it('closes its guest, and stops listening, however late the answers come', async () => {
    const { win, host, guest, calls, unlisten } = rig()
    guest.destroy()
    guest.mount(host, 'https://a.example/')
    guest.destroy()
    await settle()
    expect(calls('side_panel_guest_close')).toEqual([{ label: 'panel-guest-1' }])
    expect(unlisten).toHaveBeenCalled()
    expect(win.mutationsStopped && win.resizeStopped).toBe(true)
    expect(win.listeners.size).toBe(0)
    // Nothing more is sent for it.
    guest.reload()
    guest.setVisible(true)
    await frame(win)
    expect(calls('side_panel_guest_navigate')).toEqual([])
  })

  it('stops listening when the guest is closed before listening started', async () => {
    const { win, host, guest, unlisten } = rig()
    guest.mount(host, 'https://a.example/')
    await settle()
    guest.destroy()
    await settle()
    expect(unlisten).toHaveBeenCalledTimes(1)
    await frame(win)
  })

  it('stops a frame that lands after the guest closed', async () => {
    const { win, host, guest, calls } = rig()
    guest.mount(host, 'https://a.example/')
    await settle()
    guest.setVisible(false)
    guest.destroy()
    await frame(win)
    expect(calls('side_panel_guest_place')).toEqual([])
  })

  it('warns when the shell refuses, where the window can', async () => {
    const win = fakeWindow()
    const host = new El('div')
    win.document.body.append(host)
    const invoke = vi.fn(async () => {
      throw new Error('no')
    })
    const listen = vi.fn(async () => {
      throw new Error('no events')
    })
    createTauriGuest({ invoke, listen, win }).mount(host, 'https://a.example/')
    await settle()
    expect(win.console.warn).toHaveBeenCalledWith('[side-panel]', expect.any(Error))

    for (const console of [undefined, {}]) {
      win.console = console
      expect(() => createTauriGuest({ invoke, listen, win }).mount(host, 'x')).not.toThrow()
      await settle()
    }
  })

  it('uses the page window when given none', () => {
    const guest = createTauriGuest({ invoke: vi.fn(), listen: vi.fn() })
    expect(guest.canGoBack()).toBe(false)
  })
})
