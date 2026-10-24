// The design-A EventSource shim (WP0 S2): same-origin URLs are read by Rust
// through the profile's jar and arrive over an IPC channel; anything else uses
// the browser's own EventSource. Never merge.
;(function () {
  const Native = window.EventSource
  const core = window.__TAURI__.core
  class ShimEventSource extends EventTarget {
    constructor(url, init) {
      const u = new URL(url, location.href)
      if (u.origin !== location.origin) return new Native(url, init)
      super()
      this.url = u.href
      this.withCredentials = false
      this.readyState = 0
      this.onopen = this.onmessage = this.onerror = null
      const ch = new core.Channel()
      ch.onmessage = (m) => this._on(m)
      core.invoke('sse_open', { url: u.pathname + u.search, onEvent: ch }).then((id) => {
        this._id = id
        if (this.readyState === 2) core.invoke('sse_close', { id })
      }, (e) => this._on({ type: 'error', detail: String(e) }))
    }
    _fire(type, ev) {
      this.dispatchEvent(ev)
      const h = this['on' + type]
      if (typeof h === 'function') h.call(this, ev)
    }
    _on(m) {
      if (this.readyState === 2) return
      if (m.type === 'open') { this.readyState = 1; this._fire('open', new Event('open')) }
      else if (m.type === 'message') {
        const ev = new MessageEvent(m.event || 'message', { data: m.data, lastEventId: m.lastEventId, origin: location.origin })
        if ((m.event || 'message') === 'message') this._fire('message', ev); else this.dispatchEvent(ev)
      } else if (m.type === 'error') {
        // Spike simplification: no automatic reconnect. useEventBus closes and
        // reconnects on its own; WP2 must port the spec's retry for parity.
        this.readyState = 2
        this._fire('error', new Event('error'))
      }
    }
    close() {
      if (this.readyState === 2) return
      this.readyState = 2
      if (this._id) core.invoke('sse_close', { id: this._id })
    }
  }
  ShimEventSource.CONNECTING = 0; ShimEventSource.OPEN = 1; ShimEventSource.CLOSED = 2
  window.EventSource = ShimEventSource
})()

// WebKitGTK crashes in webkit_uri_scheme_request_get_http_body when a request
// to a custom scheme carries a Blob or a File (FormData with a file included).
// Flatten such bodies to bytes before they reach the scheme. Never merge.
;(function () {
  const nativeFetch = window.fetch
  const needsFlatten = (b) => b instanceof Blob || b instanceof FormData || b instanceof ReadableStream
  window.fetch = async function (input, init) {
    const body = init?.body ?? (input instanceof Request ? input.body : undefined)
    if (body == null || !needsFlatten(body)) return nativeFetch.call(this, input, init)
    const req = new Request(input, init)
    if (new URL(req.url).origin !== location.origin) return nativeFetch.call(this, input, init)
    const bytes = await req.arrayBuffer()
    return nativeFetch.call(this, req.url, {
      method: req.method, headers: req.headers, body: bytes, signal: init?.signal,
      cache: req.cache, redirect: req.redirect, referrerPolicy: req.referrerPolicy,
    })
  }
})()
