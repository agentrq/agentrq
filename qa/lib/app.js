// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { expect } from '@playwright/test'
import { rootToken } from './settings.js'

const API = '/api/v1'

// Signs the page's browser context in as the root user. The cookie it sets is
// the same one the app itself uses, so everything after runs signed in.
export async function signIn(page) {
  const res = await page.request.post(`${API}/auth/root/login`, { data: { rootToken } })
  expect(res.status(), 'root login should succeed; is QA_ROOT_TOKEN the server\'s token?').toBe(200)
}

// The server allows one user a few workspaces and tasks a minute and answers
// 429 past that. Seeding waits its turn rather than failing the test.
async function send(page, method, path, data) {
  const deadline = Date.now() + 90_000
  for (;;) {
    const res = await page.request.fetch(`${API}${path}`, { method, data })
    if (res.status() !== 429 || Date.now() > deadline) return res
    await page.waitForTimeout(2_000)
  }
}

async function expectOk(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.json()
}

export async function createWorkspace(page, name) {
  const res = await send(page, 'POST', '/workspaces', { workspace: { name, description: `Created by the QA suite: ${name}` } })
  return (await expectOk(res, `creating the workspace ${name}`)).workspace
}

export async function createTask(page, workspaceId, title, status = 'notstarted') {
  const task = { title, body: `Created by the QA suite: ${title}`, createdBy: 'human', assignee: 'agent', status }
  const res = await send(page, 'POST', `/workspaces/${workspaceId}/tasks`, { task })
  return (await expectOk(res, `creating the task ${title}`)).task
}

export async function setTaskStatus(page, workspaceId, taskId, value) {
  const res = await send(page, 'PATCH', `/workspaces/${workspaceId}/tasks/${taskId}/status`, { status: { value } })
  return (await expectOk(res, `moving task ${taskId} to ${value}`)).task
}

export async function deleteTask(page, workspaceId, taskId) {
  const res = await send(page, 'DELETE', `/workspaces/${workspaceId}/tasks/${taskId}`)
  if (!res.ok()) throw new Error(`deleting task ${taskId} answered ${res.status()}`)
}

export async function getTask(page, workspaceId, taskId) {
  return page.request.get(`${API}/workspaces/${workspaceId}/tasks/${taskId}`)
}

// Records every API call the page makes that fails, and every console error,
// so a test can end by saying the page talked to the server cleanly. `allow`
// lists the failures a test causes on purpose, as "<status> <path fragment>".
export function watchTraffic(page) {
  const failures = []
  const consoleErrors = []
  page.on('response', (res) => {
    const url = new URL(res.url())
    if (url.pathname.startsWith(API) && res.status() >= 400) failures.push(`${res.status()} ${res.request().method()} ${url.pathname}`)
  })
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text())
  })
  return {
    failures,
    expectClean({ allow = [] } = {}) {
      const unexpected = failures.filter((f) => !allow.some((a) => f.startsWith(a.split(' ')[0]) && f.includes(a.split(' ').slice(1).join(' '))))
      expect(unexpected, 'API calls the page made that failed').toEqual([])
      // The browser logs every failed request as a console error of its own;
      // those are covered above, so only the rest count here.
      const real = consoleErrors.filter((e) => !e.startsWith('Failed to load resource'))
      expect(real, 'console errors the page logged').toEqual([])
    },
  }
}

// Waits for the next API response whose path ends with `path`, made with
// `method` and carrying every query parameter in `params`, so a test can
// assert on the call a click made rather than one still in flight from before.
export function nextResponse(page, method, path, params = {}) {
  return page.waitForResponse((res) => {
    const url = new URL(res.url())
    return res.request().method() === method && url.pathname.startsWith(API) && url.pathname.endsWith(path) &&
      Object.entries(params).every(([key, value]) => url.searchParams.get(key) === value)
  })
}

// A name no earlier run has used, since a reused server keeps its database.
export function unique(prefix) {
  return `${prefix}-${Date.now().toString(36)}`
}

// Opens an MCP session on the workspace's server the way an agent does: with
// the workspace token, and a session of its own. Returns a function calling one
// of its tools, which resolves to the response's text.
export async function agentSession(page, workspaceId) {
  const { token } = await expectOk(await page.request.get(`${API}/workspaces/${workspaceId}/token`), 'reading the workspace token')
  const url = `/mcp/${workspaceId}?token=${encodeURIComponent(token)}`
  const headers = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' }
  const rpc = async (body, session, timeout) => {
    const res = await page.request.post(url, { headers: session ? { ...headers, 'mcp-session-id': session } : headers, data: body, timeout })
    if (!res.ok()) throw new Error(`MCP ${body.method} answered ${res.status()}: ${await res.text()}`)
    return res
  }
  const init = await rpc({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'agentrq-qa', version: '0' } } })
  const session = init.headers()['mcp-session-id']
  await rpc({ jsonrpc: '2.0', method: 'notifications/initialized' }, session)
  let id = 1
  // A tool that waits on the human (elicit) holds the request open until they
  // answer, so it gets the test's own deadline rather than the request's.
  return async (name, args) => (await rpc({ jsonrpc: '2.0', id: ++id, method: 'tools/call', params: { name, arguments: args } }, session, 0)).text()
}

// Creates a task the way an agent does, over the workspace's MCP server.
export async function createTaskAsAgent(page, workspaceId, title) {
  const call = await agentSession(page, workspaceId)
  const text = await call('createTask', { title, body: `Created by the QA suite: ${title}` })
  if (!text.includes('task created with id=')) throw new Error(`createTask over MCP answered: ${text}`)
}
