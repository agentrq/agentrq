// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import assert from 'node:assert/strict'
import { test } from 'node:test'

import { ServerError } from '../src/errors.js'
import { McpClient, contentToText, isLostSession, parseResponseBody, pruneUndefined } from '../src/mcp.js'
import { fakeFetch, response, sse, withSession } from './fake-server.js'

const client = (fetchImpl) => new McpClient({ url: 'https://workspace.test', fetchImpl })

test('parseResponseBody reads a plain JSON body', () => {
  assert.deepEqual(parseResponseBody('{"result":1}', 'application/json'), { result: 1 })
})

test('parseResponseBody reads the SSE framing the workspace server uses', () => {
  // Streamable HTTP may answer a POST either way; handling only JSON is the
  // difference between working and an undefined result with nothing logged.
  const body = 'event: message\ndata: {"jsonrpc":"2.0","id":1,"result":{"ok":true}}\n\n'
  assert.deepEqual(parseResponseBody(body, 'text/event-stream').result, { ok: true })
})

test('parseResponseBody picks the frame carrying the result past progress frames', () => {
  const body = [
    'data: {"jsonrpc":"2.0","method":"notifications/progress"}',
    'data: {"jsonrpc":"2.0","id":1,"result":{"ok":true}}',
    '',
  ].join('\n')
  assert.deepEqual(parseResponseBody(body).result, { ok: true })
})

test('parseResponseBody falls back to the last frame when none carries a result', () => {
  const body = 'data: {"jsonrpc":"2.0","method":"a"}\ndata: {"jsonrpc":"2.0","method":"b"}\n'
  assert.deepEqual(parseResponseBody(body).method, 'b')
})

test('parseResponseBody ignores empty, sentinel and unparseable frames', () => {
  assert.equal(parseResponseBody('data: \ndata: [DONE]\ndata: not json\n'), null)
  assert.equal(parseResponseBody('   '), null)
})

test('parseResponseBody reports a non-JSON body rather than crashing', () => {
  assert.throws(() => parseResponseBody('<html>gateway error</html>', 'text/html'), /non-JSON response/)
})

test('contentToText flattens text and resource blocks', () => {
  assert.equal(contentToText(null), '')
  assert.equal(contentToText({}), '')
  assert.equal(
    contentToText({
      content: [
        { type: 'text', text: 'one' },
        null,
        { type: 'image', data: 'xx' },
        { type: 'resource', resource: { text: 'two' } },
        { type: 'resource', resource: {} },
      ],
    }),
    'one\ntwo',
  )
})

test('pruneUndefined drops unset optional arguments', () => {
  assert.deepEqual(pruneUndefined({ a: 1, b: undefined, c: null, d: '' }), { a: 1, d: '' })
  assert.deepEqual(pruneUndefined(undefined), {})
})

test('isLostSession recognises only a 404 about a session', () => {
  assert.equal(isLostSession(new ServerError('session not found', { status: 404 })), true)
  assert.equal(isLostSession(new ServerError('not found', { status: 404 })), false)
  assert.equal(isLostSession(new ServerError('session gone', { status: 500 })), false)
  assert.equal(isLostSession(null), false)
})

test('callTool completes the handshake before calling', async () => {
  const inner = fakeFetch({ onCall: () => 'done' })
  const mcpClient = client(withSession(inner))
  assert.equal((await mcpClient.callTool('getWorkspace', {})).text, 'done')

  // initialize, then the initialized notification, then the call — in order.
  assert.deepEqual(
    inner.calls.map((call) => (call.body ? call.body.method : call.method)),
    ['initialize', 'notifications/initialized', 'tools/call'],
  )
})

test('callTool sends the session and protocol headers once established', async () => {
  const inner = fakeFetch({ onCall: () => 'ok' })
  const mcpClient = client(withSession(inner, 'sess-9'))
  await mcpClient.callTool('getWorkspace', {})
  const toolCall = inner.calls.find((call) => call.body && call.body.method === 'tools/call')
  assert.equal(toolCall.headers['mcp-session-id'], 'sess-9')
  assert.equal(toolCall.headers['mcp-protocol-version'], '2025-06-18')
})

test('callTool passes custom headers from .mcp.json through', async () => {
  const inner = fakeFetch({})
  const mcpClient = new McpClient({
    url: 'https://workspace.test',
    headers: { authorization: 'Bearer tok' },
    fetchImpl: withSession(inner),
  })
  await mcpClient.callTool('getWorkspace', {})
  assert.equal(inner.calls[0].headers.authorization, 'Bearer tok')
})

test('connect is idempotent', async () => {
  const inner = fakeFetch({})
  const mcpClient = client(withSession(inner))
  await mcpClient.connect()
  await mcpClient.connect()
  assert.equal(inner.calls.filter((call) => call.body && call.body.method === 'initialize').length, 1)
})

test('a tool answering isError becomes the server error, with its own wording', async () => {
  const mcpClient = client(withSession(fakeFetch({ onCall: () => ({ text: 'invalid taskId format', isError: true }) })))
  await assert.rejects(() => mcpClient.callTool('getTask', { taskId: 'x' }), /invalid taskId format/)
})

test('an isError result with no text still fails', async () => {
  const mcpClient = client(withSession(fakeFetch({ onCall: () => ({ text: '', isError: true }) })))
  await assert.rejects(() => mcpClient.callTool('getTask', {}), /getTask failed/)
})

test('a JSON-RPC error becomes a readable failure', async () => {
  const mcpClient = client(withSession(fakeFetch({})))
  await assert.rejects(() => mcpClient.request('tasks/unknown', {}), /no such method tasks\/unknown/)
})

test('a JSON-RPC error with no message still names the method', async () => {
  const fetchImpl = async () => sse({ jsonrpc: '2.0', id: 1, error: {} })
  await assert.rejects(() => client(fetchImpl).request('thing', {}), /thing failed: unknown error/)
})

test('an empty response is reported rather than returning undefined', async () => {
  const fetchImpl = async () => response('', { status: 200 })
  await assert.rejects(() => client(fetchImpl).request('initialize', {}), /sent no response/)
})

test('an HTTP failure names the status, because a swallowed call looks like success', async () => {
  const fetchImpl = async () => response('workspace not found', { status: 403 })
  await assert.rejects(() => client(fetchImpl).callTool('getWorkspace'), /HTTP 403: workspace not found/)
})

test('an HTTP failure with no body still names the status', async () => {
  const fetchImpl = async () => response('', { status: 500 })
  await assert.rejects(() => client(fetchImpl).callTool('getWorkspace'), /HTTP 500/)
})

test('a long error body is truncated', async () => {
  const fetchImpl = async () => response('x'.repeat(500), { status: 500 })
  await assert.rejects(() => client(fetchImpl).callTool('getWorkspace'), /…/)
})

test('an unreachable server says so, with the URL', async () => {
  const fetchImpl = async () => {
    throw new Error('ECONNREFUSED')
  }
  await assert.rejects(() => client(fetchImpl).callTool('getWorkspace'), /cannot reach the workspace server at https:\/\/workspace.test/)
})

test('a dropped session is re-established and the call retried', async () => {
  // The server pings sessions over the SSE GET stream and a CLI never opens
  // one, so a session can vanish underneath a slow command. That must not
  // reach the user as "session not found".
  let toolCalls = 0
  const inner = fakeFetch({
    onCall: () => 'recovered',
    onRequest: (record) => {
      if (record.body && record.body.method === 'tools/call') {
        toolCalls += 1
        if (toolCalls === 1) return response('session not found', { status: 404 })
      }
      return null
    },
  })
  const mcpClient = client(withSession(inner))
  assert.equal((await mcpClient.callTool('getTask', {})).text, 'recovered')
  assert.equal(toolCalls, 2)
  assert.equal(inner.calls.filter((call) => call.body && call.body.method === 'initialize').length, 2)
})

test('a session that cannot be re-established fails rather than looping', async () => {
  const inner = fakeFetch({
    onRequest: (record) =>
      record.body && record.body.method === 'tools/call'
        ? response('session not found', { status: 404 })
        : null,
  })
  await assert.rejects(() => client(withSession(inner)).callTool('getTask', {}), /session not found/)
})

test('a non-session failure is not retried', async () => {
  let attempts = 0
  const inner = fakeFetch({
    onRequest: (record) => {
      if (record.body && record.body.method === 'tools/call') {
        attempts += 1
        return response('internal server error', { status: 500 })
      }
      return null
    },
  })
  await assert.rejects(() => client(withSession(inner)).callTool('getTask', {}), /HTTP 500/)
  assert.equal(attempts, 1)
})

test('listTools returns the advertised tools, and copes with none', async () => {
  const tools = [{ name: 'getWorkspace', description: 'x' }]
  assert.deepEqual(await client(withSession(fakeFetch({ tools }))).listTools(), tools)

  const empty = async (url, options) => {
    const body = JSON.parse(options.body)
    if (body.method === 'initialize') return sse({ jsonrpc: '2.0', id: body.id, result: {} })
    return sse({ jsonrpc: '2.0', id: body.id, result: {} })
  }
  assert.deepEqual(await client(empty).listTools(), [])
})

test('close ends the session and is safe to call twice', async () => {
  const inner = fakeFetch({})
  const mcpClient = client(withSession(inner, 'sess-close'))
  await mcpClient.callTool('getWorkspace', {})
  await mcpClient.close()
  await mcpClient.close()
  const deletes = inner.calls.filter((call) => call.method === 'DELETE')
  assert.equal(deletes.length, 1)
  assert.equal(deletes[0].headers['mcp-session-id'], 'sess-close')
})

test('close never turns a completed command into a failure', async () => {
  const inner = fakeFetch({})
  const mcpClient = client(async (url, options) => {
    if ((options.method || 'POST') === 'DELETE') throw new Error('network gone')
    return withSession(inner)(url, options)
  })
  await mcpClient.callTool('getWorkspace', {})
  await mcpClient.close()
})

test('close with no session does nothing', async () => {
  const inner = fakeFetch({})
  await client(inner).close()
  assert.equal(inner.calls.length, 0)
})

test('callAction sends action and payload to a tool that takes a payload', async () => {
  const seen = []
  const fetchImpl = fakeFetch({
    tools: [{ name: 'task', inputSchema: { properties: { action: {}, payload: {} } } }, { name: 'reply' }],
    onCall: (name, args) => {
      seen.push([name, args])
      return 'ok'
    },
  })
  const c = client(fetchImpl)
  await c.callAction('task', 'reply', { taskId: 't1', text: 'hi', attachments: undefined }, ['reply', { chatId: 't1', text: 'hi' }])
  await c.callAction('task', 'get', {}, ['getTask', {}])
  assert.deepEqual(seen, [
    ['task', { action: 'reply', payload: { taskId: 't1', text: 'hi' } }],
    ['task', { action: 'get', payload: {} }],
  ])
  // The server's tools are asked for once, not per call.
  assert.equal(fetchImpl.calls.filter((r) => r.body?.method === 'tools/list').length, 1)
})

test('callAction makes the older call on a server whose tool takes no payload', async () => {
  const seen = []
  const c = client(fakeFetch({
    tools: [{ name: 'pano', inputSchema: { properties: { action: {}, id: {} } } }],
    onCall: (name, args) => {
      seen.push([name, args])
      return 'ok'
    },
  }))
  await c.callAction('task', 'reply', { taskId: 't1', text: 'hi' }, ['reply', { chatId: 't1', text: 'hi' }])
  await c.callAction('pano', 'delete', { id: 'd1' }, ['pano', { action: 'delete', id: 'd1' }])
  assert.deepEqual(seen, [
    ['reply', { chatId: 't1', text: 'hi' }],
    ['pano', { action: 'delete', id: 'd1' }],
  ])
})
