// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The Tauri side panel guest, behind `usePanelGuest`'s interface.
 *
 * The page is a native webview laid over the window at the host element's
 * box, not an element in this page, so two things Electron's `<webview>` gets
 * for free are done here:
 *
 * - **Place**: the host's box is sent to Rust whenever it may have moved.
 * - **Overlays**: a native view is drawn over everything in the page, so the
 *   app's dialogs, menus and toasts would vanish behind it. While anything
 *   positioned overlaps the host, and while the panel's edge is dragged, the
 *   guest is swapped for a picture of itself in the host and hidden (WP0 S6,
 *   mitigation a). The picture is in the page, so the overlay draws over it.
 *
 * Find in page is the native webview's own; Rust says what it found.
 */

/** What Rust tells the window about a guest. */
export const GUEST_EVENT = 'agentrq:side-panel:guest'

/**
 * How long a freeze waits for the guest's picture before hiding it anyway: an
 * overlay drawn under the guest, its clicks landing in the guest, is worse
 * than a blank host for the moment the picture still takes.
 */
export const PICTURE_WAIT_MS = 100

function emitter() {
  const listeners = new Map()
  return {
    on(event, listener) {
      if (!listeners.has(event)) listeners.set(event, new Set())
      listeners.get(event).add(listener)
      return () => listeners.get(event).delete(listener)
    },
    emit(event, payload) {
      for (const listener of listeners.get(event) ?? []) listener(payload)
    },
  }
}

/** Whether two boxes share any area. An empty box, such as an idle toast stack, covers nothing. */
export function overlaps(a, b) {
  if (a.width === 0 || a.height === 0) return false
  return a.left < b.right && b.left < a.right && a.top < b.bottom && b.top < a.bottom
}

/**
 * The elements that could be drawn over the guest: everything with a fixed or
 * absolute position. Kept as a set, updated from the mutations that add or
 * restyle elements, so a check never walks the whole page.
 */
export function createOverlayWatch(win, host) {
  const candidates = new Set()
  const positioned = (el) => /^(fixed|absolute)$/.test(win.getComputedStyle(el).position)

  function scan(root) {
    for (const el of [root, ...root.querySelectorAll('*')]) {
      if (positioned(el)) candidates.add(el)
    }
  }

  // Opacity is not inherited: a closing dialog's wrapper at 0 hides what it holds.
  function transparent(el) {
    for (let node = el; node; node = node.parentElement) {
      if (win.getComputedStyle(node).opacity === '0') return true
    }
    return false
  }

  function covers(el, box) {
    if (el.contains(host) || host.contains(el) || !positioned(el)) return false
    if (win.getComputedStyle(el).visibility === 'hidden' || transparent(el)) return false
    return overlaps(el.getBoundingClientRect(), box)
  }

  return {
    scan,
    /** Feed a `MutationObserver`'s records. */
    mutated(records) {
      for (const record of records) {
        const added = record.type === 'childList' ? [...record.addedNodes] : [record.target]
        for (const node of added) if (node.nodeType === 1) scan(node)
      }
    },
    /** Whether anything is over the host now. */
    occluded() {
      const box = host.getBoundingClientRect()
      for (const el of candidates) {
        if (!el.isConnected) candidates.delete(el)
        else if (covers(el, box)) return true
      }
      return false
    },
  }
}

/**
 * @param {object} deps
 * @param {(command: string, args?: object) => Promise<unknown>} deps.invoke
 * @param {(event: string, handler: (e: { payload: object }) => void) => Promise<() => void>} deps.listen
 * @param {Window} [deps.win]
 */
export function createTauriGuest({ invoke, listen, win = globalThis.window }) {
  const events = emitter()
  const nav = { canGoBack: false, canGoForward: false }
  const state = { visible: true, dragging: false }
  const stops = []
  let host = null
  let label = null
  let ready = null
  let destroyed = false
  let watch = null
  let frameRequested = false
  let lastPlace = ''
  // The picture shown while the guest is hidden behind an overlay, and which
  // freeze it belongs to, so a late snapshot never lands after a thaw.
  let picture = null
  let frozen = 0
  let freezes = 0
  // Events for a guest whose label has not come back yet: its first page can
  // start loading before `side_panel_guest_open` answers.
  let early = []

  const warn = (error) => win.console?.warn?.('[side-panel]', error)

  /** A command for this guest, once it exists. */
  function send(command, args = {}) {
    return ready
      ?.then(() => (destroyed ? undefined : invoke(command, { label, ...args })))
      .catch(warn)
  }

  function onEvent(event) {
    const payload = event.payload
    if (label === null) return early.push(event)
    if (payload?.label !== label) return
    if ('canGoBack' in payload) {
      nav.canGoBack = payload.canGoBack
      nav.canGoForward = payload.canGoForward
    }
    if (payload.kind === 'loading') events.emit('loading', payload.loading)
    else if (payload.kind === 'navigate') events.emit('navigate', { url: payload.url, inPage: payload.inPage })
    else if (payload.kind === 'fail') events.emit('fail', { code: payload.code, description: payload.description })
    else if (payload.kind === 'found') events.emit('found', { active: payload.active, total: payload.total })
    // Electron moves focus to the `<webview>`; here the page would still think
    // its address field has it, and keep that field from following the guest.
    else if (payload.kind === 'focus') win.document.activeElement?.blur?.()
  }

  function rectOf() {
    const r = host.getBoundingClientRect()
    return { x: r.left, y: r.top, width: r.width, height: r.height }
  }

  // The guest is placed in logical pixels, from the CSS box and the zoom: a
  // zoom that leaves the box alone still moves it.
  const placeKey = (place) => JSON.stringify({ ...place, ratio: win.devicePixelRatio ?? 1 })

  function place() {
    const next = { rect: rectOf(), visible: state.visible && !frozen }
    const key = placeKey(next)
    if (key === lastPlace) return Promise.resolve()
    lastPlace = key
    return send('side_panel_guest_place', next)
  }

  function dropPicture() {
    if (!picture) return
    win.URL.revokeObjectURL(picture.firstChild.src)
    picture.remove()
    picture = null
  }

  async function freeze() {
    const freeze = ++freezes
    frozen = freeze
    const box = rectOf()
    const hide = win.setTimeout(() => frozen === freeze && place(), PICTURE_WAIT_MS)
    const bytes = await send('side_panel_guest_snapshot')
    win.clearTimeout(hide)
    if (frozen !== freeze) return
    // With no picture the host shows blank, which still beats an overlay
    // drawn behind the guest.
    if (bytes) {
      const img = win.document.createElement('img')
      // An `ArrayBuffer`, or an array of numbers where the IPC carries JSON.
      img.src = win.URL.createObjectURL(new win.Blob([new Uint8Array(bytes)], { type: 'image/png' }))
      img.alt = ''
      // Its own size, cropped rather than squeezed as the panel narrows: the
      // app's stylesheet caps an image at its box's width.
      img.style.cssText = `display:block;width:${box.width}px;height:${box.height}px;max-width:none;max-height:none`
      await img.decode().catch(() => {})
      if (frozen !== freeze) return win.URL.revokeObjectURL(img.src)
      const frame = win.document.createElement('div')
      frame.style.cssText = 'position:absolute;inset:0;overflow:hidden;pointer-events:none'
      frame.append(img)
      dropPicture()
      picture = frame
      host.append(frame)
    }
    place()
  }

  /** Show the guest again, and take its picture down only once it is back. */
  function thaw() {
    frozen = 0
    const shown = picture
    place().then(() =>
      win.requestAnimationFrame(() => {
        if (picture === shown) dropPicture()
      }),
    )
  }

  function update() {
    frameRequested = false
    if (!host || destroyed) return
    const wanted = state.visible && (state.dragging || watch.occluded())
    if (wanted && !frozen) freeze()
    else if (!wanted && frozen) thaw()
    else place()
  }

  function schedule() {
    if (frameRequested || !host) return
    frameRequested = true
    win.requestAnimationFrame(update)
  }

  return {
    on: events.on,

    mount(element, url) {
      host = element
      watch = createOverlayWatch(win, host)
      watch.scan(win.document.body)
      const mutations = new win.MutationObserver((records) => {
        watch.mutated(records)
        schedule()
      })
      mutations.observe(win.document.body, {
        childList: true,
        subtree: true,
        attributes: true,
        attributeFilter: ['class', 'style', 'hidden', 'open'],
      })
      const resized = new win.ResizeObserver(schedule)
      resized.observe(host)
      win.addEventListener('resize', schedule)
      win.addEventListener('scroll', schedule, true)
      stops.push(
        () => mutations.disconnect(),
        () => resized.disconnect(),
        () => win.removeEventListener('resize', schedule),
        () => win.removeEventListener('scroll', schedule, true),
      )
      listen(GUEST_EVENT, onEvent).then((unlisten) => (destroyed ? unlisten() : stops.push(unlisten)), warn)

      const rect = rectOf()
      lastPlace = placeKey({ rect, visible: state.visible })
      ready = invoke('side_panel_guest_open', { url, rect, visible: state.visible }).then((opened) => {
        label = opened
        for (const event of early) onEvent(event)
        early = []
      })
      ready.then(schedule, warn)
    },

    loadURL: (url) => send('side_panel_guest_navigate', { action: 'load', url }),
    goBack: () => send('side_panel_guest_navigate', { action: 'back' }),
    goForward: () => send('side_panel_guest_navigate', { action: 'forward' }),
    reload: () => send('side_panel_guest_navigate', { action: 'reload' }),
    stop: () => send('side_panel_guest_navigate', { action: 'stop' }),
    canGoBack: () => nav.canGoBack,
    canGoForward: () => nav.canGoForward,
    focus: () => send('side_panel_guest_focus'),
    // Electron's defaults: forward, and a step within the session.
    findInPage: (text, { forward = true, findNext = false } = {}) =>
      send('side_panel_guest_find', { text, forward, findNext }),
    stopFindInPage: (action) => send('side_panel_guest_stop_find', { action }),

    setVisible(visible) {
      state.visible = visible
      schedule()
    },
    setDragging(dragging) {
      state.dragging = dragging
      schedule()
    },

    destroy() {
      if (!host) return
      destroyed = true
      ready.then(() => invoke('side_panel_guest_close', { label })).catch(warn)
      for (const stop of stops) stop()
      dropPicture()
      host = null
    },
  }
}
