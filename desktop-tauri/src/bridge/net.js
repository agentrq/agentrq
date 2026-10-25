// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The two browser APIs the `app` scheme cannot carry as they are. The bridge
 * installs both before the renderer loads.
 *
 * - `fetch`: a Blob, File, FormData or stream body sent to a custom scheme
 *   segfaults WebKitGTK and arrives empty on WKWebView. A same-origin request
 *   with one is sent with its bytes instead (WP0 S2).
 * - `EventSource`: the scheme answers only once it has the whole response, so
 *   a stream that never ends would never answer (WP0 S1). A same-origin
 *   `EventSource` is read by Rust through the profile's client, and its events
 *   arrive over an IPC channel (`event_source.rs`). Everything else keeps the
 *   native one.
 */

/** Same scheme and host as the page. Compared by parts: a custom scheme's `origin` is opaque. */
export function sameOrigin(url, location) {
  return url.protocol === location.protocol && url.host === location.host
}

const needsBytes = (body) => body instanceof Blob || body instanceof FormData || body instanceof ReadableStream

export function installFetch(win) {
  const nativeFetch = win.fetch
  win.fetch = async function fetch(input, init) {
    const isRequest = input instanceof Request
    const body = init?.body ?? (isRequest ? input.body : null)
    const url = new URL(isRequest ? input.url : String(input), win.location.href)
    if (!needsBytes(body) || !sameOrigin(url, win.location)) {
      return nativeFetch.call(win, input, init)
    }
    // Built once so FormData gets its multipart boundary in `content-type`.
    const request = new Request(isRequest ? input : url.href, { ...init, duplex: 'half' })
    return nativeFetch.call(win, request.url, {
      method: request.method,
      headers: request.headers,
      body: await request.arrayBuffer(),
      signal: init?.signal ?? request.signal,
      credentials: request.credentials,
      cache: request.cache,
      redirect: request.redirect,
      referrerPolicy: request.referrerPolicy,
      integrity: request.integrity,
      keepalive: request.keepalive,
    })
  }
}

const CONNECTING = 0
const OPEN = 1
const CLOSED = 2

export function installEventSource(win, invoke, Channel) {
  const Native = win.EventSource

  class EventSource extends EventTarget {
    #id = null

    constructor(url, init) {
      const resolved = new URL(String(url), win.location.href)
      if (!sameOrigin(resolved, win.location)) {
        // A constructor may return another object; the page gets the real one.
        return new Native(url, init)
      }
      super()
      this.url = resolved.href
      this.withCredentials = Boolean(init?.withCredentials)
      this.readyState = CONNECTING
      this.onopen = null
      this.onmessage = null
      this.onerror = null

      const channel = new Channel()
      channel.onmessage = (frame) => this.#receive(frame)
      invoke('event_source_open', { url: resolved.pathname + resolved.search, onEvent: channel }).then(
        (id) => {
          this.#id = id
          if (this.readyState === CLOSED) invoke('event_source_close', { id })
        },
        () => this.#receive({ type: 'error', readyState: CLOSED })
      )
    }

    #fire(event) {
      this.dispatchEvent(event)
      const handler = this[`on${event.type}`]
      if (typeof handler === 'function') handler.call(this, event)
    }

    #receive(frame) {
      if (this.readyState === CLOSED) return
      if (frame.type === 'open') {
        this.readyState = OPEN
        this.#fire(new Event('open'))
      } else if (frame.type === 'message') {
        const init = { data: frame.data, lastEventId: frame.lastEventId, origin: win.location.origin }
        const event = new MessageEvent(frame.event, init)
        // Only a plain message reaches `onmessage`; a named one needs a listener.
        if (frame.event === 'message') this.#fire(event)
        else this.dispatchEvent(event)
      } else {
        // 0 while Rust reconnects after the server's retry delay, 2 when it gave up.
        this.readyState = frame.readyState
        this.#fire(new Event('error'))
      }
    }

    close() {
      if (this.readyState === CLOSED) return
      this.readyState = CLOSED
      if (this.#id !== null) invoke('event_source_close', { id: this.#id })
    }
  }

  for (const [name, value] of Object.entries({ CONNECTING, OPEN, CLOSED })) {
    Object.defineProperty(EventSource, name, { value, enumerable: true })
    Object.defineProperty(EventSource.prototype, name, { value, enumerable: true })
  }
  win.EventSource = EventSource
}
