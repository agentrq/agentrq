// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

/**
 * The browser's half of a terminal session.
 *
 * Framing, reconnection, resize debouncing and replay handling live here so
 * they can be tested without mounting a component or opening a real socket.
 * The view is a thin wrapper around an xterm instance and this.
 *
 * ## Esc gets no special handling, and that is the point
 *
 * `xterm.js`'s `onData` yields exactly the bytes the key produced: Esc is
 * `0x1b`, Enter is `\r`, an arrow is `ESC [ A`. This layer sends those bytes
 * and nothing else. Any design that enumerated "special keys" would already be
 * wrong for the next one, and a transparent byte pipe is correct for all of
 * them — including the combinations nobody has thought about.
 *
 * ## Input is never batched
 *
 * Output is coalesced on the daemon because a progress bar redraws constantly.
 * Doing the same to input would add that delay to every keypress and, worse,
 * merge a deliberate Esc with the next key into an escape sequence nobody
 * typed. So every keystroke is sent as it happens.
 */

/** Frame types, matching daemon/wire. One definition per side, same numbers. */
export const FrameType = {
  CONTROL: 0x00,
  INPUT: 0x01,
  OUTPUT: 0x02,
  RESIZE: 0x03,
  EXIT: 0x04,
  REPLAY: 0x05,
}

/** One type byte plus an eight-byte session id. */
export const HEADER_SIZE = 9

/**
 * Resizes are debounced because dragging a window emits them continuously, and
 * every one of them makes the program on the far end repaint.
 */
export const RESIZE_DEBOUNCE_MS = 150

/**
 * Reconnection backs off so a server that is down is not hammered by every
 * open terminal at once, and is capped so a tab left open overnight still
 * recovers promptly when the server returns.
 */
export const RECONNECT_BASE_MS = 500
export const RECONNECT_MAX_MS = 15000

/**
 * Encode a frame.
 *
 * Binary rather than JSON: terminal traffic is arbitrary bytes at volume, and
 * base64 would cost a third more on every frame while raising questions about
 * how `0x1b` or invalid UTF-8 survives. As bytes, they simply do.
 */
export function encodeFrame(type, sessionId, payload = new Uint8Array()) {
  // Tested for string rather than `instanceof Uint8Array`: a typed array that
  // crossed a realm boundary — Electron's preload seam, a worker — fails that
  // check and would be stringified into its own comma-separated digits.
  const body = typeof payload === 'string' ? new TextEncoder().encode(payload) : payload
  const out = new Uint8Array(HEADER_SIZE + body.length)
  out[0] = type
  // A session id is 64-bit, and JavaScript numbers are not — so it is written
  // through BigInt rather than bit-shifted, which would silently lose the high
  // bits for ids above 2^32 and deliver input to the wrong session.
  new DataView(out.buffer).setBigUint64(1, BigInt(sessionId), false)
  out.set(body, HEADER_SIZE)
  return out
}

/** Decode a frame, or null if it is too short to be one. */
export function decodeFrame(buffer) {
  const bytes = ArrayBuffer.isView(buffer) ? buffer : new Uint8Array(buffer)
  if (bytes.length < HEADER_SIZE) return null
  return {
    type: bytes[0],
    sessionId: new DataView(bytes.buffer, bytes.byteOffset).getBigUint64(1, false),
    // Copied rather than a view onto the socket's buffer, which the next
    // message may reuse.
    payload: bytes.slice(HEADER_SIZE),
  }
}

/**
 * Compute the next reconnect delay.
 *
 * Jittered, so every terminal in every tab does not retry on the same tick and
 * knock the server over again the moment it comes back.
 */
export function reconnectDelay(attempt, random = Math.random) {
  const base = Math.min(RECONNECT_BASE_MS * 2 ** Math.max(0, attempt - 1), RECONNECT_MAX_MS)
  return Math.round(base / 2 + base * random() / 2)
}

/**
 * Drive one terminal session.
 *
 * @param {object} deps
 * @param {string|number|bigint} deps.sessionId
 * @param {() => WebSocket} deps.connect   opens a socket; called again to reconnect
 * @param {(bytes: Uint8Array) => void} deps.onOutput   write to the terminal
 * @param {() => void} deps.onReplay       clear before a redraw is written
 * @param {(payload: Uint8Array) => void} [deps.onControl]  presence, and the like
 * @param {(code: number) => void} [deps.onExit]
 * @param {(state: string) => void} [deps.onStatus]
 * @param {(fn: Function, ms: number) => any} [deps.setTimer]
 * @param {(handle: any) => void} [deps.clearTimer]
 */
export function useTerminalSession({
  sessionId,
  connect,
  onOutput,
  onReplay,
  onControl = () => {},
  onExit = () => {},
  onStatus = () => {},
  setTimer = setTimeout,
  clearTimer = clearTimeout,
  random = Math.random,
}) {
  let socket = null
  let attempts = 0
  let closed = false
  let resizeTimer = null
  let pendingSize = null

  const status = (s) => onStatus(s)

  function handleMessage(data) {
    const frame = decodeFrame(data)
    if (!frame) return
    switch (frame.type) {
      case FrameType.REPLAY:
        // A redraw of the whole screen. The terminal is cleared first, or the
        // redraw is painted on top of whatever was already there and the two
        // interleave into nonsense.
        onReplay()
        onOutput(frame.payload)
        break
      case FrameType.OUTPUT:
        onOutput(frame.payload)
        break
      case FrameType.EXIT: {
        let code = 0
        try {
          code = JSON.parse(new TextDecoder().decode(frame.payload)).code ?? 0
        } catch {
          // A malformed exit still means the session ended, which is the part
          // that matters to the person watching.
        }
        onExit(code)
        break
      }
      case FrameType.CONTROL:
        // Presence, and whatever else the backend says about the session
        // rather than through it. Left unparsed here: this layer is about
        // frames, and what a control message means is the caller's business.
        onControl(frame.payload)
        break
      default:
        // A frame type from a newer server is ignored rather than fatal.
        break
    }
  }

  function open() {
    if (closed) return
    status(attempts === 0 ? 'connecting' : 'reconnecting')

    socket = connect()
    socket.binaryType = 'arraybuffer'

    socket.onopen = () => {
      attempts = 0
      status('connected')
      // The size the terminal already has, sent on every connection: the far
      // end has no idea what shape this window is until it is told, and after
      // a reconnect it may have been told something else.
      if (pendingSize) sendResize(pendingSize.cols, pendingSize.rows, { now: true })
    }
    socket.onmessage = (event) => handleMessage(event.data)
    socket.onerror = () => {}
    socket.onclose = () => {
      if (closed) return
      attempts += 1
      status('disconnected')
      setTimer(open, reconnectDelay(attempts, random))
    }
  }

  function send(bytes) {
    if (!socket || socket.readyState !== 1) return false
    socket.send(bytes)
    return true
  }

  /** Send a keystroke. Exactly the bytes xterm gave us, immediately. */
  function sendInput(data) {
    const bytes = typeof data === 'string' ? new TextEncoder().encode(data) : data
    if (!bytes.length) return false
    return send(encodeFrame(FrameType.INPUT, sessionId, bytes))
  }

  /** Tell the far end the window changed, debounced. */
  function sendResize(cols, rows, { now = false } = {}) {
    if (!cols || !rows) return false
    pendingSize = { cols, rows }
    if (resizeTimer) clearTimer(resizeTimer)
    const fire = () => {
      resizeTimer = null
      const body = new TextEncoder().encode(JSON.stringify({ cols, rows }))
      send(encodeFrame(FrameType.RESIZE, sessionId, body))
    }
    if (now) {
      fire()
      return true
    }
    resizeTimer = setTimer(fire, RESIZE_DEBOUNCE_MS)
    return true
  }

  function close() {
    closed = true
    if (resizeTimer) clearTimer(resizeTimer)
    resizeTimer = null
    if (socket) socket.close()
    socket = null
    status('closed')
  }

  return { open, close, sendInput, sendResize, handleMessage }
}
