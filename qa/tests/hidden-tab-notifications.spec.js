// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTaskAsAgent, unique, watchTraffic } from '../lib/app.js'

// A tab in the background, with notifications allowed and no Web Push: the
// system notifications it raises are recorded rather than shown. `visible`
// flips it back to the foreground.
async function backgroundTab(page) {
  await page.addInitScript(() => {
    window.__notifications = []
    window.__hidden = true
    Object.defineProperty(document, 'hidden', { get: () => window.__hidden })
    const record = (title, options) => window.__notifications.push({ title, ...options })
    window.Notification = class {
      static permission = 'granted'
      constructor(title, options) { record(title, options) }
    }
    if (navigator.serviceWorker) {
      navigator.serviceWorker.getRegistration = async () => ({ showNotification: async (title, options) => record(title, options) })
      navigator.serviceWorker.ready = Promise.resolve({ pushManager: { getSubscription: async () => null } })
    }
  })
}

test('a tab in the background raises a system notification for the news a toast carries', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await backgroundTab(page)
  const streaming = page.waitForResponse((res) => res.url().includes('/api/v1/events/stream'))
  await page.goto(`/workspaces/${workspace.id}`)
  await streaming

  const title = unique('a task the agent opened in the background')
  await createTaskAsAgent(page, workspace.id, title)

  await expect.poll(() => page.evaluate((t) => window.__notifications.filter((n) => n.title === t), title), 'the system notification')
    .toEqual([expect.objectContaining({ title, body: expect.stringContaining('The agent opened a new task.'), data: { url: expect.stringMatching(new RegExp(`^/workspaces/${workspace.id}/tasks/\\w+$`)) } })])

  // In the foreground the toast is enough.
  await page.evaluate(() => { window.__hidden = false })
  const second = unique('a task the agent opened in the foreground')
  await createTaskAsAgent(page, workspace.id, second)
  await expect(page.locator('.toast', { hasText: second }).first(), 'the toast').toBeVisible()
  expect(await page.evaluate((t) => window.__notifications.filter((n) => n.title === t).length, second), 'a notification in the foreground').toBe(0)
  traffic.expectClean()
})
