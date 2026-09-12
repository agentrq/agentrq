// Copyright 2026 Contextual, Inc. https://agentrq.com

import { describe, it, expect, vi } from 'vitest'

import { createMcpClient, parseBody, readToolResult } from '../../src/main/extensions/mcp-client.js'

/**
 * The last stub in the feature, and the one that made every extension wanting
 * anything answer with "AgentRQ cannot reach the workspace server yet".
 *
 * What is worth testing here is the protocol's awkward parts rather than the
 * happy path: two reply shapes, a session that can vanish, and a tool failure
 * that arrives inside a successful HTTP response.
 */

/** A server that behaves like the real one: handshake, session id, replies. */
function fakeServer(over = {}) {
  const requests = []
  let sessions = 0

  const handlers = {
    initialize: () => ({
      status: 200,
      session: `sess-${++sessions}`,
      json: { jsonrpc: '2.0', id: 1, result: { protocolVersion: '2025-06-18', capabilities: {} } },
    }),
    'notifications/initialized': () => ({ status: 202, json: null }),
    'tools/call': () => ({
      status: 200,
      json: { jsonrpc: '2.0', id: 2, result: { content: [{ type: 'text', text: 'Workspace: Backend' }] } },
    }),
    ...over,
  }

  const fetchImpl = vi.fn(async (url, init) => {
    const body = JSON.parse(init.body)
    requests.push({ url, headers: init.headers, body })

    const answer = handlers[body.method](body, requests.length)
    const text =
      answer.sse !== undefined
        ? answer.sse
        : answer.json === null
          ? ''
          : JSON.stringify(answer.json)

    return {
      status: answer.status,
      ok: answer.status >= 200 && answer.status < 300,
      headers: {
        get: (name) => {
          if (name === 'mcp-session-id') return answer.session ?? ''
          if (name === 'content-type') return answer.sse !== undefined ? 'text/event-stream' : 'application/json'
          return ''
        },
      },
      text: async () => (answer.body !== undefined ? answer.body : text),
    }
  })

  return { fetchImpl, requests, sessionCount: () => sessions }
}

const build = (over = {}, options = {}) => {
  const server = fakeServer(over)
  const client = createMcpClient({
    endpoint: () => 'http://localhost:3000/mcp/ws1',
    headers: async () => ({ authorization: 'Bearer secret' }),
    fetchImpl: server.fetchImpl,
    ...options,
  })
  return { server, client }
}

describe('parseBody', () => {
  it('reads a plain JSON reply', () => {
    expect(parseBody('application/json', '{"jsonrpc":"2.0","id":1}')).toEqual({ jsonrpc: '2.0', id: 1 })
  })

  // A server chooses which shape to answer in, so both are read rather than
  // one being assumed.
  it('reads the envelope out of an SSE frame', () => {
    const stream = 'event: message\ndata: {"jsonrpc":"2.0","id":1,"result":{}}\n\n'

    expect(parseBody('text/event-stream', stream)).toEqual({ jsonrpc: '2.0', id: 1, result: {} })
  })

  it('takes the last complete frame, and steps over framing that is not JSON', () => {
    const stream = ': keep-alive\ndata: notjson\ndata: {"id":1}\ndata: {"id":2}\n\n'

    expect(parseBody('text/event-stream', stream)).toEqual({ id: 2 })
  })

  it('answers with nothing rather than throwing on a body it cannot read', () => {
    expect(parseBody('application/json', '<html>502</html>')).toBeNull()
    expect(parseBody('application/json', '')).toBeNull()
    expect(parseBody('text/event-stream', ': keep-alive\n\n')).toBeNull()
    expect(parseBody(undefined, undefined)).toBeNull()
  })

  it('treats a reply with no content type at all as JSON', () => {
    // Which is what a server answering 204, or a stub, tends to send.
    expect(parseBody(undefined, '{"id":1}')).toEqual({ id: 1 })
  })
})

describe('readToolResult', () => {
  it('reads the content of a successful call', () => {
    const message = { result: { content: [{ type: 'text', text: 'Workspace: Backend' }] } }

    expect(readToolResult(message)).toEqual({
      ok: true,
      result: { content: [{ type: 'text', text: 'Workspace: Backend' }] },
      text: 'Workspace: Backend',
    })
  })

  // The distinction MCP actually draws: a tool that failed answers inside a
  // successful response, and a protocol error is a JSON-RPC error.
  it('tells a tool that failed apart from a call that could not be made', () => {
    const failedTool = readToolResult({ result: { isError: true, content: [{ type: 'text', text: 'no such task' }] } })
    const refusedCall = readToolResult({ error: { code: -32602, message: 'unknown tool "nope"' } })

    expect(failedTool).toEqual({ ok: false, reason: 'no such task' })
    expect(refusedCall).toEqual({ ok: false, reason: 'unknown tool "nope"' })
  })

  it('has something to say when either arrives empty', () => {
    expect(readToolResult({ result: { isError: true, content: [] } }).reason).toBe('The tool reported a failure.')
    expect(readToolResult({ error: {} }).reason).toBe('The server refused the call.')
    expect(readToolResult({}).reason).toBe('The server answered with nothing.')
    expect(readToolResult(null).reason).toBe('The server answered with nothing.')
  })

  it('joins several text parts and ignores the rest', () => {
    const message = {
      result: { content: [{ type: 'text', text: 'one' }, { type: 'image', data: '…' }, { type: 'text', text: 'two' }] },
    }

    expect(readToolResult(message).text).toBe('one\ntwo')
  })

  it('copes with a result carrying no content at all', () => {
    expect(readToolResult({ result: {} })).toEqual({ ok: true, result: { content: [] }, text: '' })
  })
})

describe('callTool', () => {
  it('opens a session, finishes the handshake, then calls', async () => {
    const { server, client } = build()

    const answer = await client.callTool('getWorkspace', { workspaceId: 'ws1' })

    expect(server.requests.map((r) => r.body.method)).toEqual([
      'initialize',
      'notifications/initialized',
      'tools/call',
    ])
    expect(answer.ok).toBe(true)
    expect(answer.text).toBe('Workspace: Backend')
  })

  it('sends the credentials it was given, and the session on every request after the first', async () => {
    const { server, client } = build()

    await client.callTool('getWorkspace', {})

    const [handshake, , call] = server.requests
    expect(handshake.headers.authorization).toBe('Bearer secret')
    // No session on the way in — that is what initialize is for.
    expect(handshake.headers['mcp-session-id']).toBeUndefined()
    expect(call.headers['mcp-session-id']).toBe('sess-1')
    expect(call.headers['mcp-protocol-version']).toBe('2025-06-18')
    expect(call.headers.accept).toContain('text/event-stream')
  })

  it('passes the tool name and arguments through untouched', async () => {
    const { server, client } = build()

    await client.callTool('getTask', { taskId: 't1', includeConversation: true })

    expect(server.requests.at(-1).body.params).toEqual({
      name: 'getTask',
      arguments: { taskId: 't1', includeConversation: true },
    })
  })

  it('reuses the session it already has', async () => {
    const { server, client } = build()

    await client.callTool('getWorkspace', {})
    await client.callTool('getTask', {})

    expect(server.sessionCount()).toBe(1)
    expect(server.requests.filter((r) => r.body.method === 'initialize')).toHaveLength(1)
  })

  // Two extensions asking at once must not open two sessions, one of which is
  // then abandoned with the server still holding it.
  it('opens one session for callers that arrive together', async () => {
    const { server, client } = build()

    await Promise.all([client.callTool('getWorkspace', {}), client.callTool('getTask', {})])

    expect(server.sessionCount()).toBe(1)
  })

  // A session the server has forgotten — a restart, an eviction.
  it('re-opens a session the server has dropped, once', async () => {
    let calls = 0
    const { server, client } = build({
      'tools/call': () => {
        calls += 1
        return calls === 1
          ? { status: 404, json: { error: { message: 'session not found' } } }
          : { status: 200, json: { result: { content: [{ type: 'text', text: 'back' }] } } }
      },
    })
    await client.callTool('getWorkspace', {})

    const answer = await client.callTool('getWorkspace', {})

    expect(answer.ok).toBe(true)
    expect(answer.text).toBe('back')
    expect(server.sessionCount()).toBe(2)
  })

  it('gives up after the second refusal rather than reconnecting forever', async () => {
    const { server, client } = build({ 'tools/call': () => ({ status: 404, json: { error: { message: 'gone' } } }) })

    const answer = await client.callTool('getWorkspace', {})

    expect(answer.ok).toBe(false)
    expect(server.requests.filter((r) => r.body.method === 'tools/call')).toHaveLength(2)
  })

  it('says plainly when the server did not accept the credentials', async () => {
    const { client } = build({ initialize: () => ({ status: 401, json: null, body: 'Unauthorized' }) })

    const answer = await client.callTool('getWorkspace', {})

    expect(answer.reason).toContain("did not accept this app's credentials")
  })

  it('uses the words the server gave, when it gave any', async () => {
    const { client } = build({
      'tools/call': () => ({ status: 400, json: { error: { message: 'workspaceId is required' } } }),
    })

    expect((await client.callTool('getTask', {})).reason).toContain('workspaceId is required')
  })

  it('falls back to the status and the body when it gave neither', async () => {
    const { client } = build({ 'tools/call': () => ({ status: 502, json: null, body: 'bad gateway' }) })

    const answer = await client.callTool('getTask', {})

    expect(answer.reason).toContain('502')
    expect(answer.reason).toContain('bad gateway')
  })

  it('says something about a status with no body at all', async () => {
    const empty = build({ 'tools/call': () => ({ status: 500, json: null, body: '' }) })
    expect((await empty.client.callTool('getTask', {})).reason).toContain('answered 500')

    // And when there is no body to read at all, rather than an empty one.
    const missing = createMcpClient({
      endpoint: () => 'http://localhost:3000/mcp',
      fetchImpl: async () => ({
        status: 500,
        ok: false,
        headers: { get: () => '' },
        text: async () => undefined,
      }),
    })
    expect((await missing.callTool('getTask', {})).reason).toContain('answered 500')
  })

  it('reports a refused handshake with the reason the server gave', async () => {
    const { client } = build({
      initialize: () => ({ status: 200, json: { error: { message: 'unsupported protocol version' } } }),
    })

    expect((await client.callTool('getWorkspace', {})).reason).toContain('unsupported protocol version')
  })

  it('has something to say about a handshake refused without one', async () => {
    const { client } = build({ initialize: () => ({ status: 200, json: { error: {} } }) })

    expect((await client.callTool('getWorkspace', {})).reason).toBe('The server refused the connection.')
  })

  // Never throws: the broker hands whatever comes back to extension code, and
  // an exception surfaces as a broken promise inside somebody else's apply.
  it('answers with a refusal when the network itself fails', async () => {
    const client = createMcpClient({
      endpoint: () => 'http://localhost:3000/mcp/ws1',
      fetchImpl: async () => {
        throw new Error('ECONNREFUSED')
      },
    })

    expect(await client.callTool('getWorkspace', {})).toEqual({ ok: false, reason: 'ECONNREFUSED' })
  })

  it('says so when nothing is configured to reach', async () => {
    const client = createMcpClient({ endpoint: () => '', fetchImpl: vi.fn() })

    expect((await client.callTool('getWorkspace', {})).reason).toBe('No server is configured.')
  })

  it('has something to say about a failure that carried no message', async () => {
    const client = createMcpClient({
      endpoint: () => 'http://localhost:3000/mcp/ws1',
      fetchImpl: async () => {
        throw new Error('')
      },
    })

    expect((await client.callTool('getWorkspace', {})).reason).toBe('Could not call "getWorkspace".')
  })

  // A failed handshake must not become the answer every later caller awaits.
  it('tries again after a handshake that failed', async () => {
    let attempts = 0
    const { client } = build({
      initialize: () => {
        attempts += 1
        return attempts === 1
          ? { status: 503, json: null, body: 'starting up' }
          : { status: 200, session: 'sess-later', json: { result: {} } }
      },
    })

    expect((await client.callTool('getWorkspace', {})).ok).toBe(false)
    expect((await client.callTool('getWorkspace', {})).ok).toBe(true)
  })

  it('carries on when the initialized notification is refused', async () => {
    // It is a notification: nothing waits on it, and a server that answers
    // badly has still opened the session.
    const { client } = build({
      'notifications/initialized': () => {
        throw new Error('nope')
      },
    })

    expect((await client.callTool('getWorkspace', {})).ok).toBe(true)
  })

  it('reads a reply that arrived as an SSE frame', async () => {
    const { client } = build({
      'tools/call': () => ({
        status: 200,
        sse: 'event: message\ndata: {"jsonrpc":"2.0","id":2,"result":{"content":[{"type":"text","text":"streamed"}]}}\n\n',
      }),
    })

    expect((await client.callTool('getWorkspace', {})).text).toBe('streamed')
  })

  it('forgets its session when told to', async () => {
    const { server, client } = build()
    await client.callTool('getWorkspace', {})
    expect(client.session()).toBe('sess-1')

    client.reset()
    await client.callTool('getWorkspace', {})

    expect(server.sessionCount()).toBe(2)
  })

  it('works with no credentials, which is what a cookie-authenticated server needs', async () => {
    const server = fakeServer()
    const client = createMcpClient({ endpoint: () => 'http://localhost:3000/mcp', fetchImpl: server.fetchImpl })

    expect((await client.callTool('listWorkspaces', {})).ok).toBe(true)
    expect(server.requests[0].headers.authorization).toBeUndefined()
  })

  it('reads the endpoint each time, because a profile can change it', async () => {
    const server = fakeServer()
    let url = 'http://a/mcp'
    const client = createMcpClient({ endpoint: () => url, fetchImpl: server.fetchImpl })

    await client.callTool('getWorkspace', {})
    url = 'http://b/mcp'
    client.reset()
    await client.callTool('getWorkspace', {})

    expect(server.requests.at(-1).url).toBe('http://b/mcp')
  })

  it('holds up against a response with no headers to read', async () => {
    const client = createMcpClient({
      endpoint: () => 'http://localhost:3000/mcp',
      fetchImpl: async () => ({
        status: 200,
        ok: true,
        headers: {},
        text: async () => JSON.stringify({ result: { content: [] } }),
      }),
    })

    expect((await client.callTool('getWorkspace', {})).ok).toBe(true)
  })
})
