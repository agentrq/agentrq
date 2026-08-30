import { describe, it, expect, vi } from 'vitest'

import {
  INITIAL_RECONNECT_DELAY,
  MAX_RECONNECT_DELAY,
  createEventStreamClient,
  createSSEParser,
  nextReconnectDelay,
} from '../src/main/sse.js'

/** A response whose body streams the given chunks, then ends. */
function streamingResponse(chunks, { status = 200 } = {}) {
  const encoder = new TextEncoder()
  let i = 0
  return {
    ok: status >= 200 && status < 300,
    status,
    body: {
      getReader: () => ({
        read: async () =>
          i < chunks.length ? { done: false, value: encoder.encode(chunks[i++]) } : { done: true, value: undefined },
      }),
    },
  }
}

describe('nextReconnectDelay', () => {
  it('doubles, matching useEventBus.js', () => {
    expect(nextReconnectDelay(1000)).toBe(2000)
    expect(nextReconnectDelay(2000)).toBe(4000)
    expect(nextReconnectDelay(8000)).toBe(16000)
  })

  it('caps at thirty seconds, as the renderer does', () => {
    expect(nextReconnectDelay(16000)).toBe(MAX_RECONNECT_DELAY)
    expect(nextReconnectDelay(MAX_RECONNECT_DELAY)).toBe(MAX_RECONNECT_DELAY)
  })

  it('starts from one second', () => {
    expect(INITIAL_RECONNECT_DELAY).toBe(1000)
  })
})

describe('createSSEParser', () => {
  it('reads a complete frame', () => {
    expect(createSSEParser().feed('data: {"a":1}\n\n')).toEqual(['{"a":1}'])
  })

  it('reads several frames from one chunk', () => {
    expect(createSSEParser().feed('data: one\n\ndata: two\n\n')).toEqual(['one', 'two'])
  })

  it('reassembles a frame split across chunks', () => {
    // Chunks arrive on arbitrary boundaries, including mid-line.
    const parser = createSSEParser()
    expect(parser.feed('data: {"a"')).toEqual([])
    expect(parser.feed(':1}\n')).toEqual([])
    expect(parser.feed('\n')).toEqual(['{"a":1}'])
  })

  it('holds an incomplete trailing frame until it is finished', () => {
    const parser = createSSEParser()
    expect(parser.feed('data: done\n\ndata: partial')).toEqual(['done'])
    expect(parser.feed('\n\n')).toEqual(['partial'])
  })

  it('ignores the keepalive comment the backend sends', () => {
    // ': agentrq' every 30s is what holds the connection open.
    expect(createSSEParser().feed(': agentrq\n\n')).toEqual([])
  })

  it('ignores fields other than data', () => {
    expect(createSSEParser().feed('event: ping\nid: 7\ndata: payload\n\n')).toEqual(['payload'])
  })

  it('joins a multi-line data field', () => {
    expect(createSSEParser().feed('data: line1\ndata: line2\n\n')).toEqual(['line1\nline2'])
  })

  it('tolerates a data field with no space after the colon', () => {
    expect(createSSEParser().feed('data:tight\n\n')).toEqual(['tight'])
  })

  it('emits nothing for an empty data field', () => {
    expect(createSSEParser().feed('data:\n\n')).toEqual([])
  })
})

describe('createEventStreamClient', () => {
  const setup = ({ stopAfterDelays = 1, ...overrides } = {}) => {
    const onEvent = vi.fn()
    const onStatus = vi.fn()
    const onUnauthorized = vi.fn()

    // The reconnect loop is unbounded by design — it retries until stopped. A
    // delay that resolves instantly would therefore spin the loop as fast as
    // the event loop allows and exhaust memory before the test could assert,
    // so this stands in for the passage of time *and* ends the run.
    let handle
    const delay = vi.fn(async () => {
      if (delay.mock.calls.length >= stopAfterDelays) handle.stop()
      await new Promise((resolve) => setTimeout(resolve, 0))
    })

    handle = createEventStreamClient({
      streamUrl: () => 'https://example.com/api/v1/events/stream',
      netFetch: overrides.netFetch ?? vi.fn(async () => streamingResponse([])),
      onEvent,
      onStatus,
      onUnauthorized,
      delay,
      ...overrides,
    })
    return { client: handle, onEvent, onStatus, onUnauthorized, delay }
  }

  /** Let the client's async loop advance. */
  const settle = () => new Promise((r) => setTimeout(r, 5))

  it('asks for an event stream', async () => {
    const netFetch = vi.fn(async () => streamingResponse([]))
    const { client } = setup({ netFetch })

    client.start()
    await settle()
    client.stop()

    expect(netFetch.mock.calls[0][0]).toBe('https://example.com/api/v1/events/stream')
    expect(netFetch.mock.calls[0][1].headers).toEqual({ Accept: 'text/event-stream' })
  })

  it('delivers parsed events', async () => {
    const netFetch = vi.fn(async () => streamingResponse(['data: {"type":"task.created"}\n\n']))
    const { client, onEvent } = setup({ netFetch })

    client.start()
    await settle()
    client.stop()

    expect(onEvent).toHaveBeenCalledWith({ type: 'task.created' })
  })

  it('survives a frame it cannot parse', async () => {
    // One malformed frame is not a reason to drop a working connection.
    const netFetch = vi.fn(async () => streamingResponse(['data: not json\n\ndata: {"type":"ok"}\n\n']))
    const { client, onEvent } = setup({ netFetch })

    client.start()
    await settle()
    client.stop()

    expect(onEvent).toHaveBeenCalledOnce()
    expect(onEvent).toHaveBeenCalledWith({ type: 'ok' })
  })

  it('reports connected and disconnected', async () => {
    const { client, onStatus } = setup()

    client.start()
    await settle()
    client.stop()

    expect(onStatus).toHaveBeenCalledWith(true)
    expect(onStatus).toHaveBeenCalledWith(false)
  })

  it('stops on 401 instead of hammering a server that said no', async () => {
    const netFetch = vi.fn(async () => ({ ok: false, status: 401 }))
    const { client, onUnauthorized, delay } = setup({ netFetch })

    client.start()
    await settle()

    expect(onUnauthorized).toHaveBeenCalledOnce()
    expect(netFetch).toHaveBeenCalledOnce()
    expect(delay).not.toHaveBeenCalled()
  })

  it('backs off after a failure, doubling each time', async () => {
    const netFetch = vi.fn(async () => {
      throw new Error('ECONNREFUSED')
    })
    const { client, delay } = setup({ netFetch, stopAfterDelays: 3 })

    client.start()
    await settle()
    client.stop()

    expect(delay.mock.calls[0][0]).toBe(1000)
    expect(delay.mock.calls[1][0]).toBe(2000)
    expect(delay.mock.calls[2][0]).toBe(4000)
  })

  it('treats a non-OK, non-401 response as a failure and retries', async () => {
    const netFetch = vi.fn(async () => ({ ok: false, status: 503 }))
    const { client, delay } = setup({ netFetch })

    client.start()
    await settle()
    client.stop()

    expect(delay).toHaveBeenCalled()
  })

  it('resets the backoff after a connection that worked', async () => {
    // A connection that lasted is not suspect: the next failure should retry
    // promptly rather than inherit a long delay.
    let attempt = 0
    const netFetch = vi.fn(async () => {
      attempt += 1
      if (attempt === 1) throw new Error('down')
      if (attempt === 2) return streamingResponse(['data: {"type":"ok"}\n\n'])
      throw new Error('down again')
    })
    const { client, delay } = setup({ netFetch, stopAfterDelays: 2 })

    client.start()
    await settle()
    client.stop()

    expect(delay.mock.calls[0][0]).toBe(1000)
    // Third attempt failed after a good connection, so back to the floor.
    expect(delay.mock.calls[1][0]).toBe(1000)
  })

  it('re-resolves the URL each attempt, so a server switch is picked up', async () => {
    let url = 'https://first.example.com/api/v1/events/stream'
    const netFetch = vi.fn(async () => {
      throw new Error('down')
    })
    const { client } = setup({ netFetch, streamUrl: () => url, stopAfterDelays: 1 })

    client.start()
    await settle()
    url = 'https://second.example.com/api/v1/events/stream'
    client.start()
    await settle()
    client.stop()

    expect(netFetch.mock.calls.at(-1)[0]).toBe('https://second.example.com/api/v1/events/stream')
  })

  it('reports whether it is running, and starting twice is a no-op', async () => {
    const netFetch = vi.fn(async () => streamingResponse([]))
    const { client } = setup({ netFetch })

    expect(client.isRunning()).toBe(false)
    client.start()
    client.start()
    expect(client.isRunning()).toBe(true)

    await settle()
    client.stop()
    expect(client.isRunning()).toBe(false)
  })

  it('does not schedule a retry when stopped mid-attempt', async () => {
    // stop() during an in-flight request must end the loop then and there,
    // rather than falling through into a reconnect delay.
    let handle
    const delay = vi.fn(async () => {})
    handle = createEventStreamClient({
      streamUrl: () => 'https://example.com/api/v1/events/stream',
      netFetch: async () => {
        handle.stop()
        throw new Error('aborted')
      },
      onEvent: () => {},
      delay,
    })

    handle.start()
    await new Promise((r) => setTimeout(r, 10))

    expect(delay).not.toHaveBeenCalled()
    expect(handle.isRunning()).toBe(false)
  })

  it('treats a 200 with no body as a failure', async () => {
    const netFetch = vi.fn(async () => ({ ok: true, status: 200, body: null }))
    const { client, delay } = setup({ netFetch })

    client.start()
    await settle()
    client.stop()

    expect(delay).toHaveBeenCalled()
  })

  it('works without optional callbacks', async () => {
    // onStatus and onUnauthorized are optional; omitting them must not throw
    // on either the connected path or the 401 path.
    let attempt = 0
    const client = createEventStreamClient({
      streamUrl: () => 'https://example.com/api/v1/events/stream',
      netFetch: async () => {
        attempt += 1
        return attempt === 1 ? streamingResponse(['data: {"type":"ok"}\n\n']) : { ok: false, status: 401 }
      },
      onEvent: () => {},
      delay: async () => {},
    })

    client.start()
    await new Promise((r) => setTimeout(r, 10))

    expect(client.isRunning()).toBe(false)
  })

  it('aborts the in-flight request when stopped', async () => {
    let signal
    const netFetch = vi.fn(async (_url, init) => {
      signal = init.signal
      return streamingResponse([])
    })
    const { client } = setup({ netFetch })

    client.start()
    await settle()
    client.stop()

    expect(signal.aborted).toBe(true)
  })
})
