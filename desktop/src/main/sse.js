/**
 * Server-sent events client for the main process.
 *
 * The renderer already consumes this stream through `useEventBus.js` for live
 * UI updates. The main process needs its own connection for a different reason:
 * notifications have to keep arriving when the window is in the background, and
 * on some platforms a backgrounded renderer is throttled. This client is
 * deliberately separate from the renderer's and does not replace it.
 *
 * Reconnect behaviour matches `useEventBus.js` exactly — one second, doubling to
 * a thirty second ceiling, and a hard stop on 401 rather than a reconnect loop
 * against a server that has already said no.
 *
 * Everything is injected, so the parsing and backoff rules are testable in plain
 * Node with no Electron and no network.
 */

export const INITIAL_RECONNECT_DELAY = 1000
export const MAX_RECONNECT_DELAY = 30000

/** The delay after a failure at `current`. Mirrors useEventBus.js. */
export function nextReconnectDelay(current) {
  return Math.min(current * 2, MAX_RECONNECT_DELAY)
}

/**
 * Incremental parser for the SSE wire format.
 *
 * Chunks arrive on arbitrary boundaries, so a frame can be split anywhere —
 * including mid-line. State is kept in the closure and only complete frames are
 * emitted.
 */
export function createSSEParser() {
  let buffer = ''

  return {
    /**
     * @returns {string[]} the `data` payload of each complete frame in this
     *          chunk. Keepalive comments and frames with no data yield nothing.
     */
    feed(chunk) {
      buffer += chunk
      const out = []

      // Frames are separated by a blank line. Anything after the last separator
      // is an incomplete frame and stays buffered.
      let separator
      while ((separator = buffer.indexOf('\n\n')) !== -1) {
        const frame = buffer.slice(0, separator)
        buffer = buffer.slice(separator + 2)

        const data = frame
          .split('\n')
          // A line starting with ':' is a comment — the backend sends
          // ': agentrq' as its keepalive.
          .filter((line) => line.startsWith('data:'))
          .map((line) => line.slice('data:'.length).trimStart())
          .join('\n')

        if (data) out.push(data)
      }

      return out
    },
  }
}

/**
 * Hold a connection to the event stream, reconnecting until stopped.
 *
 * @param {object} deps
 * @param {() => string} deps.streamUrl     resolved per attempt, so a server
 *                                          switch is picked up on reconnect
 * @param {typeof fetch} deps.netFetch
 * @param {(event: object) => void} deps.onEvent
 * @param {(connected: boolean) => void} [deps.onStatus]
 * @param {(ms: number) => Promise<void>} deps.delay
 * @param {() => void} [deps.onUnauthorized] called instead of reconnecting when
 *                                           the server rejects the credentials
 * @returns {{ start: () => void, stop: () => void, isRunning: () => boolean }}
 */
export function createEventStreamClient({
  streamUrl,
  netFetch,
  onEvent,
  onStatus = () => {},
  delay,
  onUnauthorized = () => {},
}) {
  let running = false
  let controller = null

  async function readStream(response) {
    const parser = createSSEParser()
    const decoder = new TextDecoder()
    const reader = response.body.getReader()

    while (running) {
      const { done, value } = await reader.read()
      if (done) return

      for (const data of parser.feed(decoder.decode(value, { stream: true }))) {
        try {
          onEvent(JSON.parse(data))
        } catch {
          // A frame we cannot parse is not a reason to drop the connection;
          // the next one is probably fine.
        }
      }
    }
  }

  async function loop() {
    let reconnectDelay = INITIAL_RECONNECT_DELAY

    while (running) {
      controller = new AbortController()
      try {
        const response = await netFetch(streamUrl(), {
          headers: { Accept: 'text/event-stream' },
          signal: controller.signal,
        })

        if (response.status === 401) {
          // Reconnecting would just be rejected again. The renderer's own
          // stream handles sending the user to the login screen.
          //
          // `running` is cleared before returning so the client is genuinely
          // stopped rather than merely idle: leaving it set would make a later
          // start() — after a successful sign-in — a silent no-op.
          running = false
          onStatus(false)
          onUnauthorized()
          return
        }

        if (!response.ok || !response.body) {
          throw new Error(`stream responded ${response.status}`)
        }

        onStatus(true)
        // A connection that lasted is not suspect, so the next failure starts
        // its backoff from the bottom again.
        reconnectDelay = INITIAL_RECONNECT_DELAY
        await readStream(response)
      } catch {
        // Network error, or stop() aborting the in-flight request.
      }

      onStatus(false)
      if (!running) return

      await delay(reconnectDelay)
      reconnectDelay = nextReconnectDelay(reconnectDelay)
    }
  }

  return {
    start() {
      if (running) return
      running = true
      loop()
    },
    stop() {
      running = false
      controller?.abort()
      controller = null
    },
    isRunning: () => running,
  }
}
