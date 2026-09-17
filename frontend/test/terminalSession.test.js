// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect, vi } from 'vitest'
import {
  useTerminalSession,
  VIEWER_SESSION,
  encodeFrame,
  decodeFrame,
  reconnectDelay,
  FrameType,
  HEADER_SIZE,
  RESIZE_DEBOUNCE_MS,
  RECONNECT_MAX_MS,
} from '../src/composables/useTerminalSession.js'

/** A socket that records what was sent and lets a test drive the callbacks. */
class FakeSocket {
  constructor() {
    this.sent = []
    this.readyState = 1
    this.closed = 0
    this.binaryType = ''
  }
  send(data) {
    this.sent.push(data)
  }
  close() {
    this.closed += 1
  }
}

/** A session wired to fakes, with handles on everything a test needs to poke. */
function harness(overrides = {}) {
  const sockets = []
  const output = []
  const replays = []
  const exits = []
  const statuses = []
  const timers = []
  let nextTimer = 1

  const session = useTerminalSession({
    sessionId: 7,
    connect: () => {
      const s = new FakeSocket()
      sockets.push(s)
      return s
    },
    onOutput: (bytes) => output.push(bytes),
    onReplay: () => replays.push(true),
    onExit: (code) => exits.push(code),
    onStatus: (s) => statuses.push(s),
    setTimer: (fn, ms) => {
      const handle = nextTimer++
      timers.push({ handle, fn, ms, cancelled: false })
      return handle
    },
    clearTimer: (handle) => {
      const t = timers.find((x) => x.handle === handle)
      if (t) t.cancelled = true
    },
    random: () => 0.5,
    ...overrides,
  })

  return {
    session,
    sockets,
    output,
    replays,
    exits,
    statuses,
    timers,
    last: () => sockets[sockets.length - 1],
    /** Run the most recently scheduled timer that has not been cancelled. */
    fire: () => {
      const t = [...timers].reverse().find((x) => !x.cancelled && !x.ran)
      t.ran = true
      t.fn()
      return t
    },
  }
}

const text = (bytes) => new TextDecoder().decode(bytes)

describe('framing', () => {
  it('writes the type, the session id and the payload', () => {
    const frame = encodeFrame(FrameType.INPUT, 7, new Uint8Array([0x1b]))
    expect(frame[0]).toBe(FrameType.INPUT)
    expect(frame.length).toBe(HEADER_SIZE + 1)
    expect(frame[HEADER_SIZE]).toBe(0x1b)
    const decoded = decodeFrame(frame)
    expect(decoded.type).toBe(FrameType.INPUT)
    expect(decoded.sessionId).toBe(7n)
    expect(Array.from(decoded.payload)).toEqual([0x1b])
  })

  it('accepts a string payload, and an empty one', () => {
    expect(text(decodeFrame(encodeFrame(FrameType.INPUT, 1, 'hi')).payload)).toBe('hi')
    expect(decodeFrame(encodeFrame(FrameType.CONTROL, 1)).payload.length).toBe(0)
  })

  // Shifting a 64-bit id with JavaScript's 32-bit bitwise operators would lose
  // the high bits and deliver a keystroke to a different session.
  it('round-trips a session id above 2^32', () => {
    const id = 9007199254740881n
    expect(decodeFrame(encodeFrame(FrameType.INPUT, id, 'x')).sessionId).toBe(id)
  })

  it('decodes an ArrayBuffer as the socket delivers it', () => {
    const frame = encodeFrame(FrameType.OUTPUT, 3, 'ok')
    expect(text(decodeFrame(frame.buffer).payload)).toBe('ok')
  })

  it('refuses a buffer too short to be a frame', () => {
    expect(decodeFrame(new Uint8Array(HEADER_SIZE - 1))).toBeNull()
  })

  // The socket reuses its read buffer, so a payload that is a view onto it is
  // overwritten by the next message before the terminal has drawn this one.
  it('copies the payload rather than viewing the source buffer', () => {
    const frame = encodeFrame(FrameType.OUTPUT, 1, 'a')
    const { payload } = decodeFrame(frame)
    frame[HEADER_SIZE] = 0x7a
    expect(text(payload)).toBe('a')
  })
})

// The bug this file did not catch, kept as a test because the reason it got
// through is that every fixture here passed a number and the application
// passes a base62 string.
describe('a session id the browser actually holds', () => {
  const REAL_ID = '0is9t9UOwO9'

  it('does not throw on one', () => {
    expect(() => encodeFrame(FrameType.INPUT, REAL_ID, 'y')).not.toThrow()
  })

  // Thrown over, this happened inside a keystroke handler where nothing was
  // watching: output kept arriving and the keyboard did nothing.
  it('sends every keystroke rather than throwing on each one', () => {
    const h = harness({ sessionId: REAL_ID })
    h.session.open()
    expect(h.session.sendInput('y')).toBe(true)
    expect(h.session.sendInput('\r')).toBe(true)
    expect(text(decodeFrame(h.last().sent[0]).payload)).toBe('y')
  })

  it('resizes rather than throwing', () => {
    const h = harness({ sessionId: REAL_ID })
    h.session.open()
    h.session.sendResize(80, 24)
    h.fire()
    expect(h.last().sent).toHaveLength(1)
  })

  // A viewer names no session, because the backend decides which one this
  // socket may drive and overwrites whatever arrives.
  it('writes no session for a viewer', () => {
    expect(VIEWER_SESSION).toBe(0)
    expect(decodeFrame(encodeFrame(FrameType.INPUT, VIEWER_SESSION, 'y')).sessionId).toBe(0n)
  })

  it('still writes a real number when it is given one', () => {
    expect(decodeFrame(encodeFrame(FrameType.INPUT, 9007199254740881n, 'y')).sessionId).toBe(
      9007199254740881n
    )
    expect(decodeFrame(encodeFrame(FrameType.INPUT, '42', 'y')).sessionId).toBe(42n)
  })

  it('treats a missing id as none rather than as a crash', () => {
    expect(decodeFrame(encodeFrame(FrameType.INPUT, undefined, 'y')).sessionId).toBe(0n)
    expect(decodeFrame(encodeFrame(FrameType.INPUT, null, 'y')).sessionId).toBe(0n)
  })
})

describe('reconnectDelay', () => {
  it('grows with each attempt and stays within the jitter window', () => {
    expect(reconnectDelay(1, () => 0)).toBe(250)
    expect(reconnectDelay(1, () => 1)).toBe(500)
    expect(reconnectDelay(3, () => 0)).toBe(1000)
  })

  it('caps so a tab left open overnight still recovers promptly', () => {
    expect(reconnectDelay(40, () => 1)).toBe(RECONNECT_MAX_MS)
  })

  it('treats a zeroth attempt as the first', () => {
    expect(reconnectDelay(0, () => 1)).toBe(500)
  })

  it('jitters by default so every tab does not retry on the same tick', () => {
    const delays = new Set(Array.from({ length: 40 }, () => reconnectDelay(5)))
    expect(delays.size).toBeGreaterThan(1)
  })
})

describe('input', () => {
  // The whole point of this layer: the bytes xterm produced are the bytes that
  // reach the far end, whatever they are.
  it.each([
    ['escape', '\x1b'],
    ['escape then a key', '\x1bb'],
    ['ctrl-c', '\x03'],
    ['enter as carriage return', '\r'],
    ['an arrow key', '\x1b[A'],
    ['a NUL', '\x00'],
  ])('passes %s through untouched', (_name, keys) => {
    const h = harness()
    h.session.open()
    h.session.sendInput(keys)
    const payload = decodeFrame(h.last().sent[0]).payload
    expect(Array.from(payload)).toEqual(Array.from(new TextEncoder().encode(keys)))
  })

  it('sends raw bytes that are not valid UTF-8 unchanged', () => {
    const h = harness()
    h.session.open()
    h.session.sendInput(new Uint8Array([0xff, 0xfe, 0x80]))
    expect(Array.from(decodeFrame(h.last().sent[0]).payload)).toEqual([0xff, 0xfe, 0x80])
  })

  it('sends each keystroke on its own rather than batching', () => {
    const h = harness()
    h.session.open()
    h.session.sendInput('a')
    h.session.sendInput('b')
    expect(h.last().sent).toHaveLength(2)
  })

  it('ignores an empty keystroke', () => {
    const h = harness()
    h.session.open()
    expect(h.session.sendInput('')).toBe(false)
    expect(h.last().sent).toHaveLength(0)
  })

  it('drops input while the socket is not open', () => {
    const h = harness()
    expect(h.session.sendInput('a')).toBe(false)
    h.session.open()
    h.last().readyState = 0
    expect(h.session.sendInput('a')).toBe(false)
  })
})

describe('resize', () => {
  it('debounces, so dragging a window sends one size and not a hundred', () => {
    const h = harness()
    h.session.open()
    h.session.sendResize(80, 24)
    h.session.sendResize(100, 30)
    h.session.sendResize(120, 40)
    expect(h.last().sent).toHaveLength(0)
    const fired = h.fire()
    expect(fired.ms).toBe(RESIZE_DEBOUNCE_MS)
    expect(h.last().sent).toHaveLength(1)
    const frame = decodeFrame(h.last().sent[0])
    expect(frame.type).toBe(FrameType.RESIZE)
    expect(JSON.parse(text(frame.payload))).toEqual({ cols: 120, rows: 40 })
  })

  it('ignores a size with no dimensions', () => {
    const h = harness()
    h.session.open()
    expect(h.session.sendResize(0, 24)).toBe(false)
    expect(h.session.sendResize(80, 0)).toBe(false)
    expect(h.timers).toHaveLength(0)
  })

  // Nothing on the far end knows the shape of this window until it is told,
  // and after a reconnect it may have been told something else.
  it('resends the known size on every connection', () => {
    const h = harness()
    h.session.open()
    h.session.sendResize(90, 20)
    h.fire()
    h.last().onclose()
    h.fire() // the reconnect timer
    h.last().onopen()
    const frame = decodeFrame(h.last().sent[0])
    expect(frame.type).toBe(FrameType.RESIZE)
    expect(JSON.parse(text(frame.payload))).toEqual({ cols: 90, rows: 20 })
  })

  it('sends nothing on connect when the size is not known yet', () => {
    const h = harness()
    h.session.open()
    h.last().onopen()
    expect(h.last().sent).toHaveLength(0)
  })
})

describe('output and replay', () => {
  it('writes output straight to the terminal', () => {
    const h = harness()
    h.session.open()
    h.last().onmessage({ data: encodeFrame(FrameType.OUTPUT, 7, 'hello') })
    expect(text(h.output[0])).toBe('hello')
    expect(h.replays).toHaveLength(0)
  })

  // A redraw painted on top of what was already there interleaves into
  // nonsense, so the terminal is cleared first.
  it('clears before writing a replay', () => {
    const h = harness()
    h.session.open()
    h.last().onmessage({ data: encodeFrame(FrameType.REPLAY, 7, 'screen') })
    expect(h.replays).toHaveLength(1)
    expect(text(h.output[0])).toBe('screen')
  })

  it('reports the exit code', () => {
    const h = harness()
    h.session.open()
    h.last().onmessage({ data: encodeFrame(FrameType.EXIT, 7, JSON.stringify({ code: 130 })) })
    expect(h.exits).toEqual([130])
  })

  // A malformed or code-less exit still means the session ended, which is the
  // part the person watching needs to see.
  it('still reports an exit whose body cannot be read', () => {
    const h = harness()
    h.session.open()
    h.last().onmessage({ data: encodeFrame(FrameType.EXIT, 7, 'not json') })
    h.last().onmessage({ data: encodeFrame(FrameType.EXIT, 7, '{}') })
    expect(h.exits).toEqual([0, 0])
  })

  it('ignores a frame type it does not know, and a runt frame', () => {
    const h = harness()
    h.session.open()
    h.last().onmessage({ data: encodeFrame(0x7f, 7, 'x') })
    h.last().onmessage({ data: new Uint8Array(2) })
    expect(h.output).toHaveLength(0)
    expect(h.exits).toHaveLength(0)
  })

  // Presence and the like: about the session rather than through it. Handed
  // on unparsed, because what a control message means is the caller's
  // business, not the framing layer's.
  it('hands a control frame to the caller unparsed', () => {
    const controls = []
    const h = harness({ onControl: (p) => controls.push(p) })
    h.session.open()
    h.last().onmessage({ data: encodeFrame(FrameType.CONTROL, 7, '{"op":"presence"}') })
    expect(controls).toHaveLength(1)
    expect(text(controls[0])).toBe('{"op":"presence"}')
    expect(h.output).toHaveLength(0)
  })
})

describe('connection lifecycle', () => {
  it('reports connecting, connected, disconnected and reconnecting', () => {
    const h = harness()
    h.session.open()
    h.last().onopen()
    h.last().onerror(new Error('transport'))
    h.last().onclose()
    h.fire()
    expect(h.statuses).toEqual(['connecting', 'connected', 'disconnected', 'reconnecting'])
  })

  it('asks for binary frames rather than text', () => {
    const h = harness()
    h.session.open()
    expect(h.last().binaryType).toBe('arraybuffer')
  })

  it('backs off further with each failed attempt', () => {
    const h = harness()
    h.session.open()
    h.last().onclose()
    const first = h.fire().ms
    h.last().onclose()
    const second = h.fire().ms
    expect(second).toBeGreaterThan(first)
  })

  it('starts the backoff over after a successful connection', () => {
    const h = harness()
    h.session.open()
    h.last().onclose()
    const first = h.fire().ms
    h.last().onopen()
    h.last().onclose()
    expect(h.fire().ms).toBe(first)
  })

  it('does not reconnect a session the viewer closed', () => {
    const h = harness()
    h.session.open()
    const socket = h.last()
    h.session.close()
    expect(socket.closed).toBe(1)
    socket.onclose()
    expect(h.timers.filter((t) => !t.cancelled)).toHaveLength(0)
    expect(h.statuses).toContain('closed')
    expect(h.sockets).toHaveLength(1)
  })

  it('cancels a pending resize when the session closes', () => {
    const h = harness()
    h.session.open()
    h.session.sendResize(80, 24)
    h.session.close()
    expect(h.timers[0].cancelled).toBe(true)
  })

  it('closes cleanly when it never connected', () => {
    const h = harness()
    h.session.close()
    expect(h.statuses).toEqual(['closed'])
  })

  it('ignores a reconnect timer that fires after the viewer closed', () => {
    const h = harness()
    h.session.open()
    h.last().onclose()
    h.session.close()
    h.timers.forEach((t) => t.fn())
    expect(h.sockets).toHaveLength(1)
  })
})

describe('defaults', () => {
  // Every dependency has a default so a caller that only wants to watch output
    // does not have to supply the rest.
  it('runs with only the required dependencies supplied', () => {
    vi.useFakeTimers()
    const socket = new FakeSocket()
    const output = []
    const session = useTerminalSession({
      sessionId: 1,
      connect: () => socket,
      onOutput: (b) => output.push(b),
      onReplay: () => {},
    })
    session.open()
    socket.onopen()
    socket.onmessage({ data: encodeFrame(FrameType.CONTROL, 1, '{}') })
    socket.onmessage({ data: encodeFrame(FrameType.OUTPUT, 1, 'hi') })
    socket.onmessage({ data: encodeFrame(FrameType.EXIT, 1, '{"code":0}') })
    session.sendResize(80, 24)
    vi.advanceTimersByTime(RESIZE_DEBOUNCE_MS)
    expect(text(output[0])).toBe('hi')
    expect(socket.sent).toHaveLength(1)
    socket.onclose()
    vi.advanceTimersByTime(RECONNECT_MAX_MS)
    session.close()
    vi.useRealTimers()
  })
})
