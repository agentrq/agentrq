// Copyright 2026 Contextual, Inc. https://agentrq.com

/**
 * The smallest MCP client that can call a tool, for the main process.
 *
 * Until this existed, the broker's two callers threw *"AgentRQ cannot reach the
 * workspace server from the desktop app yet"* and every extension that wanted
 * anything got that sentence back. Everything else in the feature was real; this
 * was the last stub.
 *
 * ## Why hand-written rather than the SDK
 *
 * `@modelcontextprotocol/sdk` would be a runtime dependency of the packaged app
 * — externalised from the main bundle, resolved from `node_modules`, shipped and
 * updated — for three JSON-RPC calls against a server in the same repository.
 * The protocol surface used here is `initialize`, `notifications/initialized`
 * and `tools/call`, and that is the whole of it. If extensions ever need
 * sampling, roots, or to *be* a server, this stops being the right trade.
 *
 * ## Streamable HTTP, and the two shapes a reply arrives in
 *
 * The transport is one POST per message. A server may answer with
 * `application/json` or with an SSE stream carrying the same JSON-RPC envelope
 * in a `data:` line, and which one is its choice — so both are read. The Accept
 * header offers both, because a server that sees only one may refuse.
 *
 * ## Sessions, and why one is kept
 *
 * The workspace server is stateful: it holds the agent connection that receives
 * pushes, so it cannot be the stateless kind. It answers `initialize` with an
 * `Mcp-Session-Id`, and every later request has to carry it. That session can
 * end without warning — a server restart, an eviction — and the sign of it is a
 * 404 on a request that was fine a moment ago. One retry re-initialises and
 * tries again; a second failure is reported, because a client that reconnects
 * forever against a server refusing it is a client nobody can debug.
 */

const PROTOCOL_VERSION = '2025-06-18'
const ACCEPT = 'application/json, text/event-stream'

/** Identifies this client to the server, and appears in its logs. */
const CLIENT_INFO = { name: 'agentrq-desktop-extensions', version: '1.0.0' }

/**
 * Pulls the JSON-RPC envelope out of whichever shape arrived.
 *
 * An SSE body is a sequence of `data:` lines; the last complete JSON one is the
 * reply. Written to tolerate the framing rather than to parse SSE properly —
 * this is one request and one response, not a subscription.
 */
export function parseBody(contentType, text) {
  const body = String(text ?? '').trim()
  if (!body) return null

  if (!String(contentType ?? '').includes('text/event-stream')) {
    try {
      return JSON.parse(body)
    } catch {
      return null
    }
  }

  let found = null
  for (const line of body.split(/\r?\n/)) {
    if (!line.startsWith('data:')) continue
    try {
      found = JSON.parse(line.slice(5).trim())
    } catch {
      // A `data:` line that is not JSON is framing, not an answer.
    }
  }
  return found
}

/**
 * What a `tools/call` result means.
 *
 * MCP reports a *tool's* failure inside a successful response, as `isError`
 * with the reason in the content — distinct from a protocol error, which is a
 * JSON-RPC `error`. Both end up as a refusal here, because an extension has the
 * same thing to do about either, but they are told apart so the message is the
 * server's own rather than a wrapper around it.
 */
export function readToolResult(message) {
  if (message?.error) {
    return { ok: false, reason: message.error.message || 'The server refused the call.' }
  }

  const result = message?.result
  if (!result) return { ok: false, reason: 'The server answered with nothing.' }

  const text = (result.content ?? [])
    .filter((part) => part?.type === 'text')
    .map((part) => part.text)
    .join('\n')

  if (result.isError) return { ok: false, reason: text || 'The tool reported a failure.' }
  return { ok: true, result: { content: result.content ?? [] }, text }
}

/**
 * @param {object} deps
 * @param {() => string} deps.endpoint  Where to POST. Read each time, because
 *   the server a profile points at can change while the app is open.
 * @param {() => Promise<object>} [deps.headers]  Credentials, per request.
 * @param {Function} [deps.fetchImpl]  Electron's session-aware fetch in the app.
 */
export function createMcpClient({ endpoint, headers = async () => ({}), fetchImpl = fetch }) {
  let sessionId = ''
  let initialising = null
  let nextId = 1

  async function post(message, { session = sessionId } = {}) {
    const url = endpoint()
    if (!url) throw new Error('No server is configured.')

    const response = await fetchImpl(url, {
      method: 'POST',
      headers: {
        'content-type': 'application/json',
        accept: ACCEPT,
        ...(session ? { 'mcp-session-id': session } : {}),
        // Sent from the second request on: the spec has the client echo the
        // version it agreed to, and a server may refuse a request without it.
        ...(session ? { 'mcp-protocol-version': PROTOCOL_VERSION } : {}),
        ...(await headers()),
      },
      body: JSON.stringify(message),
    })

    const text = await response.text()
    return {
      status: response.status,
      ok: response.ok,
      // Header names are case-insensitive, and Electron's Headers normalises;
      // reading it through `.get` avoids depending on which case arrived.
      session: response.headers?.get?.('mcp-session-id') ?? '',
      message: parseBody(response.headers?.get?.('content-type') ?? '', text),
      text,
    }
  }

  /**
   * Opens a session, once.
   *
   * Concurrent callers share the same attempt: two extensions asking at the
   * same moment must not open two sessions, one of which is then abandoned with
   * the server still holding it.
   */
  function initialize() {
    if (initialising) return initialising

    initialising = (async () => {
      const response = await post(
        {
          jsonrpc: '2.0',
          id: nextId++,
          method: 'initialize',
          params: {
            protocolVersion: PROTOCOL_VERSION,
            capabilities: {},
            clientInfo: CLIENT_INFO,
          },
        },
        { session: '' },
      )

      if (!response.ok) {
        throw new Error(describeFailure(response, 'could not open a session'))
      }
      if (response.message?.error) {
        throw new Error(response.message.error.message || 'The server refused the connection.')
      }

      sessionId = response.session
      // The handshake is not finished until this is sent, and a server may
      // reject `tools/call` before it. No reply is expected — it is a
      // notification — so a failure here is not fatal on its own.
      await post({ jsonrpc: '2.0', method: 'notifications/initialized' }).catch(() => {})
      return sessionId
    })()

    // Cleared either way: a failed attempt must not be the answer every later
    // caller awaits, or one bad moment disables extensions until a restart.
    initialising = initialising.finally(() => {
      initialising = null
    })
    return initialising
  }

  return {
    /** Exposed for the tests, and so a caller can tell whether one is open. */
    session: () => sessionId,

    /**
     * Call one tool.
     *
     * Answers `{ ok, result }` or `{ ok: false, reason }` and never throws: the
     * broker hands whatever comes back to extension code, and an exception
     * there surfaces as a broken promise inside somebody else's `apply`.
     */
    async callTool(name, args = {}) {
      try {
        if (!sessionId) await initialize()

        const message = { jsonrpc: '2.0', id: nextId++, method: 'tools/call', params: { name, arguments: args } }
        let response = await post(message)

        // A session the server has forgotten. One retry, then report — a client
        // that reconnects forever against a server refusing it is one nobody
        // can debug from the outside.
        if (response.status === 404 && sessionId) {
          sessionId = ''
          await initialize()
          response = await post({ ...message, id: nextId++ })
        }

        if (!response.ok) {
          return { ok: false, reason: describeFailure(response, `could not call "${name}"`) }
        }
        return readToolResult(response.message)
      } catch (error) {
        return { ok: false, reason: error?.message || `Could not call "${name}".` }
      }
    },

    /** Forget the session, so the next call opens a new one. */
    reset() {
      sessionId = ''
    },
  }
}

/**
 * A sentence about an HTTP failure, using the server's words where it gave any.
 *
 * The status alone is not enough: 401 against the workspace server means the
 * token is wrong, and that is worth saying rather than leaving somebody to look
 * up what 401 means here.
 */
function describeFailure(response, what) {
  if (response.status === 401 || response.status === 403) {
    return `AgentRQ ${what}: the server did not accept this app's credentials.`
  }

  const fromServer = response.message?.error?.message
  if (fromServer) return `AgentRQ ${what}: ${fromServer}`

  const body = String(response.text ?? '').trim().slice(0, 200)
  return body ? `AgentRQ ${what}: ${response.status} ${body}` : `AgentRQ ${what}: the server answered ${response.status}.`
}
