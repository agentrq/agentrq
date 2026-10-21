// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { watchTraffic } from '../lib/app.js'
import { apiPort, rootToken } from '../lib/settings.js'

const STATS = '/admin/v1/stats'
const LOGIN = '/auth/admin/login'

// Holds an agent's MCP stream open on the workspace, as a connected agent
// does, until the returned function closes it. The body is read as it comes:
// a response nobody reads can be collected, and its stream closed with it.
async function openAgentStream(page, workspaceId) {
  const { token } = await (await page.request.get(`/api/v1/workspaces/${workspaceId}/token`)).json()
  const url = `http://localhost:${apiPort}/mcp/${workspaceId}?token=${encodeURIComponent(token)}`
  const headers = { 'Content-Type': 'application/json', Accept: 'application/json, text/event-stream' }
  const init = await fetch(url, {
    method: 'POST', headers,
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-06-18', capabilities: {}, clientInfo: { name: 'agentrq-qa', version: '0' } } }),
  })
  const session = init.headers.get('mcp-session-id')
  await init.text()
  await (await fetch(url, { method: 'POST', headers: { ...headers, 'mcp-session-id': session }, body: JSON.stringify({ jsonrpc: '2.0', method: 'notifications/initialized' }) })).text()
  const stop = new AbortController()
  const stream = await fetch(url, { headers: { Accept: 'text/event-stream', 'mcp-session-id': session }, signal: stop.signal })
  expect(stream.status, 'the agent stream opens').toBe(200)
  const reader = stream.body.getReader()
  const drained = (async () => { try { while (!(await reader.read()).done); } catch {} })()
  return async () => { stop.abort(); await drained }
}

test('the operator signs in with the root token and sees the open agent streams', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  // One sign-in attempt per client every ten minutes: a client of its own, so
  // a rerun against a reused server is not refused for the last run's.
  await page.setExtraHTTPHeaders({ 'X-Forwarded-For': `qa-${Date.now()}` })

  // A signed-in user, even the root user, is not the operator.
  expect((await page.request.get(STATS)).status(), 'the admin API with only a user session').toBe(401)
  await page.goto('/admin/stats')
  await expect(page, 'a signed-out operator is sent to sign in').toHaveURL(/\/admin\/login$/)
  await expect(page.getByRole('heading', { name: 'Admin sign in' })).toBeVisible()

  const { id: userId } = await (await page.request.get('/api/v1/auth/user')).json()
  const row = page.getByTestId('admin-row').filter({ hasText: userId })
  const close = await openAgentStream(page, workspace.id)
  try {
    await page.getByPlaceholder('Paste access token...').fill(rootToken)
    await page.getByRole('button', { name: 'Sign in' }).click()
    await expect(page).toHaveURL(/\/admin\/stats$/)

    await expect(row.locator(':scope > span').nth(1), "the root user's open stream").toHaveText('1')
    await expect(page.getByTestId('admin-total').first()).not.toContainText(/\b0$/)
    await page.screenshot({ path: test.info().outputPath('admin-stats.png') })
  } finally {
    await close()
  }

  // The stream's close reaches the count by the next read.
  await expect(async () => {
    await page.getByTestId('admin-refresh').click()
    await expect(row).toHaveCount(0, { timeout: 1_000 })
  }).toPass()

  await page.getByTestId('admin-logout').click()
  await expect(page).toHaveURL(/\/admin\/login$/)
  expect((await page.request.get(STATS)).status(), 'the admin API after signing out').toBe(401)

  // That was this client's one try for ten minutes, right token or not.
  await page.getByPlaceholder('Paste access token...').fill(rootToken)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page.getByText('Too many attempts. Try again in 10 minutes.')).toBeVisible()
  await expect(page).toHaveURL(/\/admin\/login$/)

  // Another client still gets its try, and a wrong token is refused.
  const wrong = await page.request.post(LOGIN, { headers: { 'X-Forwarded-For': `qa-other-${Date.now()}` }, data: { rootToken: 'not-the-token' } })
  expect(wrong.status(), 'a wrong root token').toBe(401)
  traffic.expectClean()
})
