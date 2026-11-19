// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTask, deleteTask, getTask, nextResponse, unique, watchTraffic } from '../lib/app.js'

// A task row in a task list: its title, and the row around it holding its
// buttons, the same way the app's own unit tests find one.
function taskRow(page, title) {
  return page.locator('h3.truncate', { hasText: title }).locator('xpath=ancestor::*[contains(concat(" ", @class, " "), " group ")][1]')
}

// The cross-workspace list is in queue order, ten at a time, after the tasks
// earlier specs left: Load More until the row is there.
async function loadRow(page, title) {
  await expect(async () => {
    if (await taskRow(page, title).count() === 0) {
      await page.getByRole('button', { name: 'Load More' }).click({ timeout: 2_000 })
    }
    await expect(taskRow(page, title)).toHaveCount(1, { timeout: 2_000 })
  }, `the row of ${title}, paged in`).toPass()
}

test('a card dragged into Done on the board completes its task', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a card to drag into Done'), 'ongoing')
  await page.goto(`/workspaces/${workspace.id}/board`)

  const card = page.locator('[draggable="true"]', { hasText: task.title })
  await expect(card).toBeVisible()
  const moved = nextResponse(page, 'PATCH', `/tasks/${task.id}/status`)
  await card.dragTo(page.getByRole('heading', { name: 'Done', exact: true }))
  const res = await moved
  expect(res.status(), 'the status change the drop made').toBe(200)
  expect((await res.json()).task.status, 'the task\'s new status').toBe('completed')

  const saved = await (await getTask(page, workspace.id, task.id)).json()
  expect(saved.task.status, 'the status the server kept').toBe('completed')
  traffic.expectClean()
})

test('deleting a task from the list removes it, and a delete the server refuses says so', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const kept = await createTask(page, workspace.id, unique('a task deleted from the list'))
  const gone = await createTask(page, workspace.id, unique('a task deleted while its dialog is open'))
  await page.goto('/tasks/notstarted')
  await loadRow(page, gone.title)

  await taskRow(page, kept.title).getByTitle('Delete Task').click()
  const deleted = nextResponse(page, 'DELETE', `/tasks/${kept.id}`)
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  expect((await deleted).status(), 'the delete request').toBe(204)
  await expect(page.getByText('Task deleted', { exact: true }), 'the success toast').toBeVisible()
  await expect(taskRow(page, kept.title), 'the deleted row').toHaveCount(0)
  expect((await getTask(page, workspace.id, kept.id)).status(), 'reading the deleted task back').toBe(404)

  // Deleted elsewhere while its dialog is open: the list drops the row as soon
  // as the server says so, and the confirmed delete is refused and reported.
  // The delete read the list again, from its first page.
  await loadRow(page, gone.title)
  await taskRow(page, gone.title).getByTitle('Delete Task').click()
  await deleteTask(page, workspace.id, gone.id)
  await expect(taskRow(page, gone.title), 'the row of a task deleted elsewhere, once the server says so').toHaveCount(0)
  const refused = nextResponse(page, 'DELETE', `/tasks/${gone.id}`)
  await page.getByRole('button', { name: 'Delete', exact: true }).click()
  expect((await refused).status(), 'deleting a task that is already gone').toBe(404)
  await expect(page.getByText(/^Delete Error:/), 'the error toast').toBeVisible()

  traffic.expectClean({ allow: [`404 /tasks/${gone.id}`] })
})
