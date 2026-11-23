// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { createTask, createWorkspace, unique } from '../lib/app.js'
import { canRestartBackend, mcpPost, openSession, openStream, restartBackend, workspaceToken } from '../lib/mcp.js'
import { apiPort } from '../lib/settings.js'

// A workspace's MCP session is a signed record of its handshake, so it
// outlives the server process that opened it. These run against the run's
// real backend, which one test restarts.

const origin = `http://localhost:${apiPort}`

const ping = { jsonrpc: '2.0', id: 9, method: 'ping' }
const getWorkspace = { jsonrpc: '2.0', id: 2, method: 'tools/call', params: { name: 'workspace', arguments: { action: 'get' } } }

function claimsOf(session) {
  const parts = session.split('.')
  expect(parts, 'the session id is a JWT').toHaveLength(3)
  return JSON.parse(Buffer.from(parts[1], 'base64url').toString())
}

test('a session id records the handshake and nothing that grants access', async ({ page, workspace }) => {
  const token = await workspaceToken(page, workspace.id)
  const session = await openSession(origin, workspace.id, token)

  const claims = claimsOf(session)
  expect(Object.keys(claims).sort(), 'the claims').toEqual(['aud', 'cn', 'cv', 'exp', 'iat', 'jti', 'pv', 'sub'])
  expect(claims.aud, 'bound to the workspace').toEqual([workspace.id])
  expect(claims.pv, 'the revision asked for').toBe('2025-06-18')
  expect([claims.cn, claims.cv], 'the client').toEqual(['agentrq-qa', '0'])
  expect(claims.exp - claims.iat, 'honoured for 30 days').toBe(30 * 24 * 3600)

  // It opens nothing on its own: the token is still required.
  const res = await fetch(`${origin}/mcp/${workspace.id}`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream', 'Mcp-Session-Id': session },
    body: JSON.stringify(ping),
  })
  expect(res.status, 'a session id without the token').toBe(401)
})

test('a session outlives a restart of the server', async ({ page }) => {
  test.skip(!canRestartBackend(), 'the run is reusing a backend it did not start, so it cannot restart it')
  // A workspace of its own: a push is held back while the agent has a task
  // running, and the shared one may have one.
  const workspace = await createWorkspace(page, unique('qa-mcp-session'))
  const token = await workspaceToken(page, workspace.id)
  const session = await openSession(origin, workspace.id, token)
  expect((await mcpPost(origin, workspace.id, token, getWorkspace, { session })).status).toBe(200)

  // A new server process, which has never seen the session.
  await restartBackend()

  const call = await mcpPost(origin, workspace.id, token, getWorkspace, { session })
  expect(call.status, 'tools/call after the restart').toBe(200)
  expect(call.message?.result?.content?.[0]?.text, 'its answer').toContain(`Workspace: ${workspace.name}`)

  // The push stream reopens on it too, and a task for the agent reaches it.
  const stream = await openStream(origin, workspace.id, token, session)
  expect(stream.status, 'the push stream after the restart').toBe(200)
  expect(await stream.next(5_000), 'the stream opens').toBe(': ok')
  const title = unique('pushed after the restart')
  await createTask(page, workspace.id, title)
  const push = await stream.next(10_000)
  expect(push, 'the task pushed to the session').toContain('"method":"notifications/claude/channel"')
  expect(push, 'the task pushed to the session').toContain(`Created by the QA suite: ${title}`)
  stream.close()
})

test('a session id is honoured only where it was opened, and not once deleted', async ({ page, workspace }) => {
  const token = await workspaceToken(page, workspace.id)
  const session = await openSession(origin, workspace.id, token)

  const forged = session.slice(0, -4) + (session.endsWith('AAAA') ? 'BBBB' : 'AAAA')
  for (const [what, id] of [['a forged id', forged], ['the access token', token], ['a random id', 'KIM5LKXVX26J5HDTTTXT3IL5T2']]) {
    expect((await mcpPost(origin, workspace.id, token, ping, { session: id })).status, what).toBe(404)
  }

  const deleted = await fetch(`${origin}/mcp/${workspace.id}`, {
    method: 'DELETE',
    headers: { Authorization: `Bearer ${token}`, 'Mcp-Session-Id': session },
  })
  expect(deleted.status, 'DELETE').toBe(204)
  expect((await mcpPost(origin, workspace.id, token, ping, { session })).status, 'the deleted session').toBe(404)
})

test('the workspace server speaks 2025-11-25 and 2025-06-18', async ({ page, workspace }) => {
  const token = await workspaceToken(page, workspace.id)
  const handshake = (version) => mcpPost(origin, workspace.id, token, {
    jsonrpc: '2.0', id: 1, method: 'initialize',
    params: { protocolVersion: version, capabilities: {}, clientInfo: { name: 'agentrq-qa', version: '0' } },
  }, { version: '' })

  for (const version of ['2025-11-25', '2025-06-18']) {
    expect((await handshake(version)).message?.result?.protocolVersion, `${version} is spoken`).toBe(version)
  }
  for (const version of ['2025-03-26', '2024-11-05']) {
    expect((await handshake(version)).message?.result?.protocolVersion, `${version} is answered with the newest`).toBe('2025-11-25')
    expect((await mcpPost(origin, workspace.id, token, ping, { version })).status, `a ${version} header`).toBe(400)
  }

  const discover = await mcpPost(origin, workspace.id, token, {
    jsonrpc: '2.0', id: 1, method: 'server/discover',
    params: { _meta: { 'io.modelcontextprotocol/protocolVersion': '2026-07-28', 'io.modelcontextprotocol/clientCapabilities': {} } },
  }, { version: '2026-07-28', headers: { 'Mcp-Method': 'server/discover' } })
  expect(discover.message?.result?.supportedVersions, 'what server/discover advertises').toEqual(['2025-11-25', '2025-06-18'])

  // Sessions need a stream for pushes, so the sessionless revision is refused,
  // with the ones to fall back to.
  const modern = await mcpPost(origin, workspace.id, token, {
    jsonrpc: '2.0', id: 3, method: 'tools/list',
    params: { _meta: { 'io.modelcontextprotocol/protocolVersion': '2026-07-28', 'io.modelcontextprotocol/clientCapabilities': {} } },
  }, { version: '2026-07-28', headers: { 'Mcp-Method': 'tools/list' } })
  expect(modern.status).toBe(400)
  expect(modern.message?.error?.code, 'unsupported protocol version').toBe(-32022)
  expect(modern.message?.error?.data?.supported).toEqual(['2025-11-25', '2025-06-18'])

  // The page's own work is untouched by any of it.
  const task = await createTask(page, workspace.id, unique('after the MCP checks'))
  expect(task.id).toBeTruthy()
})
