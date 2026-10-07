// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTask, unique, watchTraffic } from '../lib/app.js'

// A 1×1 transparent PNG.
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mNkYAAAAAYAAjCB0C8AAAAASUVORK5CYII='

test('Escape closes the attachment preview', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a task with an attachment'), 'notstarted', {
    attachments: [{ id: 'qa-dot', filename: 'dot.png', mimeType: 'image/png', data: PNG }],
  })
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)

  await page.getByText('dot.png', { exact: true }).first().click()
  const download = page.getByRole('link', { name: 'Download' })
  await expect(download, 'the preview').toBeVisible()
  await page.keyboard.press('Escape')
  await expect(download, 'the preview after Escape').toHaveCount(0)
  traffic.expectClean()
})

test('Escape closes the new task form, but an open palette first', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${workspace.id}/tasks/new`)
  const description = page.locator('#taskForm textarea').first()
  await expect(description, 'the focused description').toBeFocused()
  await description.fill('typed, then abandoned')

  await page.keyboard.press('Control+k')
  const finder = page.getByRole('dialog')
  await expect(finder, 'the task finder').toBeVisible()
  await page.keyboard.press('Escape')
  await expect(finder, 'the task finder after Escape').toHaveCount(0)
  await expect(page, 'the form, still open under it').toHaveURL(/\/tasks\/new$/)

  await description.focus()
  await page.keyboard.press('Escape')
  await expect(page, 'the page after Escape').toHaveURL(new RegExp(`/workspaces/${workspace.id}$`))
  traffic.expectClean()
})
