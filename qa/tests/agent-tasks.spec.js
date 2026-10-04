// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTaskAsAgent, unique, watchTraffic } from '../lib/app.js'

test('a task an agent creates is announced once', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  // The toast comes off the account-wide stream, so it must be open before
  // the agent creates anything.
  const streaming = page.waitForResponse((res) => res.url().includes('/api/v1/events/stream'))
  await page.goto(`/workspaces/${workspace.id}`)
  await streaming

  const title = unique('a task the agent opened')
  await createTaskAsAgent(page, workspace.id, title)

  await expect(page.locator('h3.line-clamp-2', { hasText: title }), 'the new row').toBeVisible()
  const toasts = page.locator('.toast', { hasText: title })
  await expect(toasts.first(), 'the toast').toBeVisible()
  // The stream delivers each event within moments; give any duplicate the time
  // to arrive before counting.
  await page.waitForTimeout(1_500)
  await page.screenshot({ path: test.info().outputPath('agent-task-toast.png') })
  // Counted once rather than retried: the toasts expire, so a retrying count
  // would pass on its way down from four to none.
  expect(await toasts.count(), 'toasts for the one task the agent created').toBe(1)
  traffic.expectClean()
})
