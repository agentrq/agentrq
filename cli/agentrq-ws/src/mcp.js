// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { ServerError } from './errors.js'
import { VERSION } from './version.js'

/**
 * The revision to negotiate with. The workspace server is deliberately
 * stateful, which means the go-sdk never offers 2026-07-28 there; it advertises
 * 2025-11-25 down to 2024-11-05. 2025-06-18 sits safely inside that range and
 * is the revision that introduced the MCP-Protocol-Version header, so it is
 * what this client speaks.
 */
export const PROTOCOL_VERSION = '2025-06-18'

const CLIENT_INFO = { name: 'agentrq-ws', version: VERSION }

/**
 * Pull the JSON-RPC payload out of a response body.
 *
 * Streamable HTTP is allowed to answer a POST with either a bare JSON object or
 * an SSE stream carrying the same object in a `data:` frame, and the workspace
 * server uses the SSE form. Handling only one of the two is the difference
 * between working and "undefined result" with nothing in the logs.
 */
export function parseResponseBody(body, contentType = '') {
  const text = String(body).trim()
  if (text === '') return null

  if (contentType.includes('text/event-stream') || text.startsWith('event:') || text.startsWith('data:')) {
    const payloads = []
    for (const line of text.split(/\r?\n/)) {
      if (!line.startsWith('data:')) continue
      const chunk = line.slice(5).trim()
      if (chunk === '' || chunk === '[DONE]') continue
      try {
        payloads.push(JSON.parse(chunk))
      } catch {
        // A frame that is not JSON is not ours; keep looking.
      }
    }
    if (payloads.length === 0) return null
    // The last frame carrying a result or error is the answer to our call;
    // earlier frames may be progress notifications.
    for (let i = payloads.length - 1; i >= 0; i -= 1) {
      if (payloads[i] && (payloads[i].result !== undefined || payloads[i].error !== undefined)) {
        return payloads[i]
      }
    }
    return payloads[payloads.length - 1]
  }

  try {
    return JSON.parse(text)
  } catch {
    throw new ServerError(`server sent a non-JSON response: ${truncate(text, 200)}`)
  }
}

function truncate(text, max) {
  return text.length > max ? `${text.slice(0, max)}…` : text
}

/** Flatten a tool result's content blocks into plain text. */
export function contentToText(result) {
  if (!result) return ''
  const blocks = Array.isArray(result.content) ? result.content : []
  return blocks
    .map((block) => {
      if (!block) return ''
      if (typeof block.text === 'string') return block.text
      if (block.type === 'resource' && block.resource && typeof block.resource.text === 'string') {
        return block.resource.text
      }
      return ''
    })
    .filter(Boolean)
    .join('\n')
}

/**
 * A minimal streamable-HTTP MCP client.
 *
 * Deliberately hand-rolled rather than pulling in the MCP SDK: this is a CLI
 * people run with `npx`, and three POSTs of handshake are cheaper to install
 * and to audit than a dependency tree.
 */
export class McpClient {
  constructor({ url, headers = {}, fetchImpl = globalThis.fetch, protocolVersion = PROTOCOL_VERSION }) {
    this.url = url
    this.extraHeaders = headers
    this.fetchImpl = fetchImpl
    this.protocolVersion = protocolVersion
    this.sessionId = null
    this.initialized = false
    this.nextId = 1
  }

  headersFor(extra = {}) {
    const headers = {
      'content-type': 'application/json',
      accept: 'application/json, text/event-stream',
      ...this.extraHeaders,
      ...extra,
    }
    if (this.sessionId) headers['mcp-session-id'] = this.sessionId
    if (this.initialized) headers['mcp-protocol-version'] = this.protocolVersion
    return headers
  }

  async post(payload) {
    let response
    try {
      response = await this.fetchImpl(this.url, {
        method: 'POST',
        headers: this.headersFor(),
        body: JSON.stringify(payload),
      })
    } catch (err) {
      throw new ServerError(`cannot reach the workspace server at ${this.url}: ${err.message}`)
    }

    const body = await response.text()
    if (!response.ok) {
      // Always name the status: a swallowed request otherwise looks exactly
      // like a successful one from the caller's side.
      throw new ServerError(
        `workspace server returned HTTP ${response.status}${body ? `: ${truncate(body.trim(), 300)}` : ''}`,
        { status: response.status },
      )
    }

    const sessionId = response.headers && response.headers.get && response.headers.get('mcp-session-id')
    if (sessionId) this.sessionId = sessionId

    return parseResponseBody(body, (response.headers && response.headers.get && response.headers.get('content-type')) || '')
  }

  async request(method, params, { allowReconnect = true } = {}) {
    const id = this.nextId
    this.nextId += 1
    let message
    try {
      message = await this.post({ jsonrpc: '2.0', id, method, params })
    } catch (err) {
      // The workspace server keeps its sessions alive by pinging them over the
      // SSE GET stream, and a CLI never opens that stream — so a session can be
      // dropped underneath a command that is merely a little slow, and a
      // two-call command like `attachment get` is exactly long enough to hit
      // it. Re-establishing is invisible and correct; surfacing "session not
      // found" to somebody who never asked for a session is not.
      if (allowReconnect && isLostSession(err)) {
        this.sessionId = null
        this.initialized = false
        await this.connect()
        return this.request(method, params, { allowReconnect: false })
      }
      throw err
    }
    if (!message) throw new ServerError(`the server sent no response to ${method}`)
    if (message.error) {
      const { code, message: text } = message.error
      throw new ServerError(`${method} failed${code !== undefined ? ` (${code})` : ''}: ${text || 'unknown error'}`)
    }
    return message.result
  }

  /**
   * End the session rather than letting the server hold it until it decides the
   * client is gone. Failure here is never interesting — the command already
   * did its work — so it is swallowed.
   */
  async close() {
    if (!this.sessionId) return
    const headers = this.headersFor()
    this.sessionId = null
    this.initialized = false
    try {
      await this.fetchImpl(this.url, { method: 'DELETE', headers })
    } catch {
      // Nothing to do: the session expires on its own.
    }
  }

  async notify(method, params) {
    await this.post({ jsonrpc: '2.0', method, params })
  }

  async connect() {
    if (this.initialized) return
    await this.request('initialize', {
      protocolVersion: this.protocolVersion,
      capabilities: {},
      clientInfo: CLIENT_INFO,
    })
    this.initialized = true
    await this.notify('notifications/initialized', {})
  }

  async listTools() {
    await this.connect()
    const result = await this.request('tools/list', {})
    return (result && result.tools) || []
  }

  /**
   * Call one action of an action tool — `task`, `pano` — which takes two
   * inputs, the action and its payload. A server from before those tools took
   * them gets the call it knows instead: `legacy` is the tool and arguments it
   * had for this, `[name, args]`.
   */
  async callAction(tool, action, payload, legacy) {
    if (!(await this.takesPayload(tool))) return this.callTool(...legacy)
    return this.callTool(tool, { action, payload: pruneUndefined(payload) })
  }

  /** Whether the server's `tool` takes a payload: asked once per client. */
  async takesPayload(tool) {
    if (!this.actionTools) {
      const tools = await this.listTools()
      this.actionTools = new Set(tools.filter((t) => t.inputSchema?.properties?.payload).map((t) => t.name))
    }
    return this.actionTools.has(tool)
  }

  /**
   * Call a tool and return its text.
   *
   * A tool that answers with `isError` is a refusal by the workspace (a bad
   * status, a memory name that does not match the required shape), not a
   * transport failure — it still becomes a non-zero exit, but with the
   * server's own wording rather than a stack.
   */
  async callTool(name, args = {}) {
    await this.connect()
    const result = await this.request('tools/call', { name, arguments: pruneUndefined(args) })
    const text = contentToText(result)
    if (result && result.isError) {
      throw new ServerError(text || `${name} failed`)
    }
    return { text, result }
  }
}

/** True when an error says the server no longer knows our session. */
export function isLostSession(err) {
  return Boolean(err) && err.status === 404 && /session/i.test(err.message || '')
}

/** Drop keys the caller left unset so optional tool arguments stay absent. */
export function pruneUndefined(object) {
  const out = {}
  for (const [key, value] of Object.entries(object || {})) {
    if (value !== undefined && value !== null) out[key] = value
  }
  return out
}
