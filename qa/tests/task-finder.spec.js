// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { createTask, unique, watchTraffic } from '../lib/app.js'

test('the task finder names the workspace of each row, beside its ID', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a task found from the finder'))
  // A title search answers from the tasks this device has saved, and a task is
  // saved when it is opened.
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  await expect(page.getByText(task.title).first(), 'the opened task').toBeVisible()
  await page.goto(`/workspaces/${workspace.id}`)

  await page.keyboard.press('Control+k')
  const finder = page.getByRole('dialog')
  await expect(finder, 'the task finder').toBeVisible()
  await finder.getByRole('textbox').fill(task.title)

  const row = finder.getByRole('button', { name: task.title })
  await expect(row, 'the row of the task').toBeVisible()
  await expect(row, 'its ID and workspace').toContainText(`${task.id} · ${workspace.name}`)
  await page.screenshot({ path: test.info().outputPath('task-finder.png') })

  await row.click()
  await expect(page, 'the task the row opens').toHaveURL(new RegExp(`/workspaces/${workspace.id}/tasks/${task.id}$`))
  traffic.expectClean({ allow: ['404 /plan'] })
})
