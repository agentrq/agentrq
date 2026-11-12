// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { execFileSync } from 'node:child_process'
import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { fileURLToPath } from 'node:url'
import { gzipSync } from 'node:zlib'
import { test, expect } from '../lib/fixtures.js'
import { clientAddress, nextResponse, unique, watchTraffic } from '../lib/app.js'
import { rootToken } from '../lib/settings.js'

const repo = fileURLToPath(new URL('../..', import.meta.url))

// The generic hook is built-in: the operator adds it, and admin-hooks.spec.js
// deletes it again. So the test adds it when it is missing.
test.beforeAll(async ({ browser }) => {
  execFileSync('make', ['wasm-hooks'], { cwd: repo, stdio: 'ignore' })
  const page = await browser.newPage({ extraHTTPHeaders: { 'X-Real-IP': clientAddress() } })
  expect((await page.request.post('/auth/admin/login', { data: { rootToken } })).status(), 'admin sign-in').toBe(200)
  const { hooks } = await (await page.request.get('/admin/v1/hooks')).json()
  if (!hooks.some((h) => h.name === 'generic' && h.enabled)) {
    const res = await page.request.post('/admin/v1/hooks', {
      headers: { 'Content-Type': 'application/wasm', 'Content-Encoding': 'gzip' },
      data: gzipSync(readFileSync(join(repo, 'wasm-hooks', 'dist', 'generic.wasm'))),
    })
    expect(res.status(), `adding the generic hook: ${await res.text()}`).toBe(201)
  }
  await page.close()
})

test('a request to a hook URL becomes a task, and a wrong token or secret does not', async ({ page, workspace, playwright }) => {
  test.setTimeout(90_000)
  const traffic = watchTraffic(page)

  // An event, and a trigger making a task from it in the run's workspace.
  // Event names are [a-z][a-z0-9_]*: unique()'s dash is refused.
  const name = unique('hooked').replace('-', '_')
  const created = await page.request.post('/api/v1/events', { data: { name, payloadGuidelines: 'The CI run' } })
  expect(created.status(), await created.text()).toBe(201)
  const { event } = await created.json()
  const title = `Hook fired ${name}`
  const trigger = await page.request.post(`/api/v1/events/${event.id}/triggers`, {
    data: { workspaceId: workspace.id, title, body: 'Payload: {{EVENT_PAYLOAD}}', assignee: 'agent', cronSchedule: '', allowAllCommands: false },
  })
  expect(trigger.status()).toBe(201)

  // Register the generic hook from the Events page, routed to that event.
  await page.goto('/events')
  await page.getByTestId('tab-hooks').click()
  await expect(page).toHaveURL(/\/events\/hooks$/)
  await page.getByTestId('reg-new').click()
  await expect(page).toHaveURL(/\/events\/hooks\/new$/)
  await page.getByTestId('reg-name').fill(`qa ${name}`)
  await page.getByTestId('reg-hook').filter({ hasText: 'generic' }).click()
  const secret = `s-${name}`
  await page.getByTestId('reg-input-token').fill(secret)
  await page.getByTestId('reg-route-request.received').selectOption({ label: name })
  const registered = nextResponse(page, 'POST', '/hooks/registrations')
  await page.getByTestId('reg-submit').click()
  expect((await registered).status()).toBe(201)
  await expect(page).toHaveURL(/\/events\/hooks\/[A-Za-z0-9]+$/)
  await expect(page.getByTestId('reg-token-once')).toBeVisible()
  const url = (await page.getByTestId('reg-url').textContent()).trim()
  expect(url).toContain('agentrq_hook_token=')
  await expect(page.getByTestId('reg-routes')).toContainText(name)

  // The provider has no session: its requests go out without the page's cookie.
  const provider = await playwright.request.newContext()
  // The URL without its token, or with another, is not found: the id alone
  // is no credential.
  const bare = url.split('?')[0]
  const auth = { Authorization: `Bearer ${secret}` }
  expect((await provider.post(bare, { headers: auth, data: {} })).status(), 'no token').toBe(404)
  expect((await provider.post(`${bare}?agentrq_hook_token=00000000`, { headers: auth, data: {} })).status(), 'wrong token').toBe(404)

  // The right token but the wrong secret is taken, then rejected by the hook.
  expect((await provider.post(url, { headers: { Authorization: 'Bearer nope' }, data: { run: 'denied' } })).status()).toBe(202)

  // The right token and secret: answered at once, then the task appears.
  const sent = await provider.post(url, { headers: { ...auth, 'Idempotency-Key': name }, data: { run: name } })
  expect(sent.status()).toBe(202)
  await expect.poll(async () => {
    const { tasks } = await (await page.request.get(`/api/v1/workspaces/${workspace.id}/tasks?status=notstarted`)).json()
    return (tasks || []).filter((t) => t.title === title).map((t) => t.body)
  }, { message: 'the task the hook fired', timeout: 30_000 }).toEqual([expect.stringContaining(name)])

  // The same delivery again is a duplicate, and makes no second task.
  expect((await provider.post(url, { headers: { ...auth, 'Idempotency-Key': name }, data: { run: name } })).status()).toBe(202)

  // The delivery log tells the three apart.
  const rows = page.getByTestId('reg-delivery')
  await expect.poll(async () => {
    await page.reload()
    await expect(rows.first()).toBeVisible()
    return (await rows.evaluateAll((els) => els.map((e) => e.firstElementChild.textContent.trim()))).sort()
  }, { timeout: 30_000 }).toEqual(['duplicate', 'published', 'rejected'])
  await expect(rows.filter({ hasText: 'rejected' })).toContainText('missing or wrong token in Authorization')
  await expect(page.getByTestId('reg-token-once'), 'the token is shown once').toHaveCount(0)
  const { tasks } = await (await page.request.get(`/api/v1/workspaces/${workspace.id}/tasks?status=notstarted`)).json()
  expect(tasks.filter((t) => t.title === title), 'one task only').toHaveLength(1)

  // Deleted, the URL is not found. It also frees the hook for
  // admin-hooks.spec.js, which deletes every hook on a reused server.
  await page.getByTestId('reg-delete').click()
  const deleted = nextResponse(page, 'DELETE', `/hooks/registrations/${bare.split('/').pop()}`)
  await page.getByRole('button', { name: 'Delete', exact: true }).last().click()
  expect((await deleted).status()).toBe(204)
  await expect(page).toHaveURL(/\/events\/hooks$/)
  expect((await provider.post(url, { headers: auth, data: {} })).status(), 'a deleted registration').toBe(404)
  await provider.dispose()

  traffic.expectClean()
})
