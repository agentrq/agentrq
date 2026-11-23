// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { createTask, nextResponse, setTaskStatus, watchTraffic } from '../lib/app.js'

// The analytics page against a real backend: the stats and task-latency
// endpoints, the range control and the latency aggregate switch.
test('a workspace\'s analytics count a completed task and follow the range and aggregate', async ({ page, workspace: ws }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, ws.id, 'a task the analytics should count')
  await setTaskStatus(page, ws.id, task.id, 'ongoing')
  await setTaskStatus(page, ws.id, task.id, 'completed')

  const stats = nextResponse(page, 'GET', `/workspaces/${ws.id}/stats`)
  const latency = nextResponse(page, 'GET', `/workspaces/${ws.id}/stats/latency`)
  await page.goto(`/workspaces/${ws.id}/analytics`)
  const statsRes = await stats
  const latencyRes = await latency
  expect(statsRes.status(), 'the stats request').toBe(200)
  expect(latencyRes.status(), 'the task latency request').toBe(200)

  // The summary cards show what the server counted, whatever that is.
  const { summary } = await statsRes.json()
  const card = (label) => page.getByText(label, { exact: true }).locator('xpath=../..')
  await expect(card('Completed'), 'the Completed card').toContainText(String(summary.tasksCompleted))
  await expect(card('Messages'), 'the Messages card').toContainText(String(summary.messages))
  // Latency is read from the task rows, so the task just closed is in it.
  const closed = (await latencyRes.json()).summary.closed
  expect(closed, 'closed tasks the latency endpoint counts').toBeGreaterThanOrEqual(1)
  await expect(page.getByText(new RegExp(`${closed} closed`, 'i')), 'the latency panel\'s closed count').toBeVisible()

  // Every range asks the server again, and the server answers each one.
  for (const [label, range] of [['1d', '1d'], ['Wk', 'week'], ['30d', '30d'], ['Mo', 'month']]) {
    const again = nextResponse(page, 'GET', `/workspaces/${ws.id}/stats`, { range })
    await page.getByRole('button', { name: label, exact: true }).click()
    expect((await again).status(), `the stats request for the range ${label}`).toBe(200)
  }

  for (const [label, aggregate] of [['Min', 'min'], ['Max', 'max'], ['p50', 'p50']]) {
    const again = nextResponse(page, 'GET', `/workspaces/${ws.id}/stats/latency`, { aggregate })
    await page.getByRole('button', { name: label, exact: true }).click()
    expect((await again).status(), `the latency request for the aggregate ${label}`).toBe(200)
    await expect(page.getByRole('button', { name: label, exact: true })).toHaveAttribute('aria-pressed', 'true')
  }

  traffic.expectClean()
})

test('the account\'s performance tab loads its stats', async ({ page }) => {
  const traffic = watchTraffic(page)
  await page.goto('/')
  const stats = nextResponse(page, 'GET', '/stats')
  const latency = nextResponse(page, 'GET', '/stats/latency')
  await page.getByRole('button', { name: 'Performance', exact: true }).click()
  expect((await stats).status(), 'the account stats request').toBe(200)
  expect((await latency).status(), 'the account task latency request').toBe(200)
  await expect(page.getByRole('heading', { name: 'Task Latency' }), 'the account\'s latency panel').toBeVisible()
  traffic.expectClean()
})
