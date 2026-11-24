// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { existsSync, readFileSync } from 'node:fs'
import { join } from 'node:path'
import { apiPort } from './settings.js'

// A workspace's MCP endpoint spoken to over plain HTTP, so a test sees the
// statuses and headers an agent's client would, and a restart of the run's
// backend that a session can be carried across.

/** Reads the workspace's MCP token, as the setup tab does. */
export async function workspaceToken(page, workspaceId) {
  const res = await page.request.get(`/api/v1/workspaces/${workspaceId}/token`)
  if (!res.ok()) throw new Error(`reading the workspace token answered ${res.status()}`)
  return (await res.json()).token
}

/**
 * Posts one JSON-RPC message to a workspace's MCP endpoint on the backend at
 * origin. Resolves to the status, the session id the response names, and the
 * JSON-RPC message the body carried (SSE or plain JSON), if any.
 */
export async function mcpPost(origin, workspaceId, token, body, { session, version = '2025-06-18', headers = {} } = {}) {
  const res = await fetch(`${origin}/mcp/${workspaceId}`, {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json',
      Accept: 'application/json, text/event-stream',
      Authorization: `Bearer ${token}`,
      ...(version ? { 'MCP-Protocol-Version': version } : {}),
      ...(session ? { 'Mcp-Session-Id': session } : {}),
      ...headers,
    },
    body: JSON.stringify(body),
  })
  const text = await res.text()
  let message = null
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  try {
    message = JSON.parse(data ? data.slice(6) : text)
  } catch {
    // A plain-text refusal carries no JSON-RPC message.
  }
  return { status: res.status, session: res.headers.get('mcp-session-id'), message, text }
}

/** Initializes a session the way an agent's client does, and returns its id. */
export async function openSession(origin, workspaceId, token, version = '2025-06-18') {
  const init = await mcpPost(origin, workspaceId, token, {
    jsonrpc: '2.0', id: 1, method: 'initialize',
    params: { protocolVersion: version, capabilities: {}, clientInfo: { name: 'agentrq-qa', version: '0' } },
  }, { version })
  if (init.status !== 200 || !init.session) throw new Error(`initialize answered ${init.status}: ${init.text}`)
  await mcpPost(origin, workspaceId, token, { jsonrpc: '2.0', method: 'notifications/initialized' }, { session: init.session, version })
  return init.session
}

/**
 * Opens the session's push stream and resolves once its headers are in, to
 * the status and a function reading the next SSE data line within ms.
 */
export async function openStream(origin, workspaceId, token, session) {
  const controller = new AbortController()
  const res = await fetch(`${origin}/mcp/${workspaceId}`, {
    headers: { Accept: 'text/event-stream', Authorization: `Bearer ${token}`, 'Mcp-Session-Id': session },
    signal: controller.signal,
  })
  const reader = res.body.getReader()
  const decoder = new TextDecoder()
  let buffer = ''
  const next = async (ms) => {
    const deadline = Date.now() + ms
    for (;;) {
      const line = buffer.split('\n').find((l) => l.startsWith('data: ') || l.startsWith(': '))
      if (line) {
        buffer = buffer.slice(buffer.indexOf(line) + line.length + 1)
        return line
      }
      const left = deadline - Date.now()
      if (left <= 0) return null
      const read = await Promise.race([reader.read(), new Promise((r) => setTimeout(() => r(null), left))])
      if (!read || read.done) return null
      buffer += decoder.decode(read.value, { stream: true })
    }
  }
  return { status: res.status, next, close: () => controller.abort() }
}

const startedMarker = () => join(process.env.QA_DATA_DIR || '', 'backend.started')

/** Whether the run started its backend, and so can restart it. */
export function canRestartBackend() {
  return existsSync(startedMarker())
}

/**
 * Restarts the run's backend, as a deploy would: the server process is
 * stopped and a new one started on the same port, database and secrets.
 * Resolves once the new one answers.
 */
export async function restartBackend() {
  const marker = startedMarker()
  const before = readFileSync(marker, 'utf8')
  process.kill(Number(before.split(' ')[0]), 'SIGUSR2')
  const deadline = Date.now() + 60_000
  for (;;) {
    if (Date.now() > deadline) throw new Error('the backend did not come back after the restart')
    await new Promise((r) => setTimeout(r, 200))
    if (readFileSync(marker, 'utf8') === before) continue
    try {
      if ((await fetch(`http://localhost:${apiPort}/api/v1/auth/user`)).status === 401) return
    } catch {
      // Not listening yet.
    }
  }
}
