// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { createTask, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

// The installed app's icon badge, recorded rather than shown: the last call
// lands in `window.__badge`, a number, or 0 for a clear.
async function recordBadge(page) {
  await page.addInitScript(() => {
    window.__badge = null
    navigator.setAppBadge = async (n) => { window.__badge = n }
    navigator.clearAppBadge = async () => { window.__badge = 0 }
  })
}

test('the app icon counts the tasks waiting on the person, live', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await recordBadge(page)
  const streaming = page.waitForResponse((res) => res.url().includes('/api/v1/events/stream'))
  await page.goto(`/workspaces/${workspace.id}`)
  await streaming

  // Other specs share the account, so count from what is already there.
  await expect.poll(() => page.evaluate(() => window.__badge), 'the first badge').not.toBeNull()
  const before = await page.evaluate(() => window.__badge)

  const task = await createTask(page, workspace.id, unique('a task waiting on you'), 'blocked')
  await expect.poll(() => page.evaluate(() => window.__badge), 'the badge once blocked').toBe(before + 1)

  await setTaskStatus(page, workspace.id, task.id, 'completed')
  await expect.poll(() => page.evaluate(() => window.__badge), 'the badge once done').toBe(before)
  traffic.expectClean()
})
