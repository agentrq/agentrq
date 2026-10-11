// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTask, forkWorkspace, getTask, mergeFork, nextResponse, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

// Opens the sidebar if it is folded to icons, so the workspace rows show.
async function openSidebar(page) {
  const expand = page.getByTitle('Expand sidebar')
  if (await expand.isVisible()) await expand.click()
}

// Opens a sidebar row's menu with its ⋯ button, the way touch and keyboard
// users reach Fork and Merge. Opening it re-reads the workspace list, and a
// click that lands while the sidebar is still settling can lose the menu, so
// it is clicked again until the menu shows.
async function openRowMenu(page, name) {
  await expect(async () => {
    if (!(await page.getByRole('menu').isVisible())) {
      await page.getByRole('button', { name: `${name} actions`, exact: true }).click()
    }
    await expect(page.getByRole('menu')).toBeVisible({ timeout: 2_000 })
  }, `the menu of ${name}`).toPass({ timeout: 20_000 })
}

// A workspace fork end to end: forked from the sidebar menu, refused a merge
// while one of its tasks is unfinished, then merged back with its task moving
// into the parent.
test('a workspace forked from the sidebar merges back into its parent, but not while a task is unfinished', async ({ page, workspace: parent }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${parent.id}`)
  await openSidebar(page)

  await openRowMenu(page, parent.name)
  await page.getByRole('menuitem', { name: 'Fork workspace' }).click()
  const name = page.getByLabel('Name')
  await expect(name, 'the fork is offered the parent\'s name with -fork').toHaveValue(`${parent.name}-fork`)
  // A fork counts against the two-workspaces-a-minute limit. Refused, the
  // dialog stays open and says so, and the fork is tried again.
  let forkRes
  for (const deadline = Date.now() + 90_000; ;) {
    const forked = nextResponse(page, 'POST', `/workspaces/${parent.id}/forks`)
    await page.getByRole('button', { name: 'Fork', exact: true }).click()
    forkRes = await forked
    if (forkRes.status() !== 429 || Date.now() > deadline) break
    await expect(page.getByText('rate limit exceeded').last(), 'a refused fork says why').toBeVisible()
    await page.waitForTimeout(5_000)
  }
  expect(forkRes.status(), 'the fork request').toBe(201)
  const fork = (await forkRes.json()).workspace
  expect(fork.forkOfId, 'the fork names its parent').toBe(parent.id)
  await expect(page.locator('[data-test="sidebar-fork"]').filter({ hasText: fork.name }), 'the fork is listed under its parent').toBeVisible()

  // An unfinished task keeps the fork from merging: the menu says why, and the
  // server refuses too when asked directly.
  const task = await createTask(page, fork.id, 'a task that must move to the parent', 'ongoing')
  await page.reload()
  await openSidebar(page)
  await openRowMenu(page, fork.name)
  const merge = page.getByRole('menuitem', { name: new RegExp(`Merge into ${parent.name}`) })
  await expect(merge, 'Merge is offered but disabled while a task is unfinished').toBeDisabled()
  await expect(merge, 'the disabled Merge says why').toContainText('1 task is not finished')
  await page.keyboard.press('Escape')
  const refused = await page.request.post(`/api/v1/workspaces/${fork.id}/merge`, { data: { deleteFolder: false } })
  expect(refused.status(), 'merging a fork with an unfinished task').toBe(409)
  expect((await refused.json()).error.message).toBe('1 task in this fork is not finished')

  // Finished, it merges, and its task is the parent's now.
  await setTaskStatus(page, fork.id, task.id, 'completed')
  await page.reload()
  await openSidebar(page)
  await openRowMenu(page, fork.name)
  await expect(merge, 'Merge is enabled once every task is finished').toBeEnabled()
  await merge.click()
  await expect(page.getByTestId('merge-message'), 'the confirmation says how many tasks move').toContainText('1 task moves')
  const merged = nextResponse(page, 'POST', `/workspaces/${fork.id}/merge`)
  await page.getByRole('button', { name: 'Merge', exact: true }).click()
  const mergeRes = await merged
  expect(mergeRes.status(), 'the merge request').toBe(200)
  expect(await mergeRes.json(), 'the merge answer').toMatchObject({ parentId: parent.id, movedTasks: 1 })
  await expect(page.locator('[data-test="sidebar-fork"]').filter({ hasText: fork.name }), 'the merged fork is gone from the sidebar').toHaveCount(0)
  expect((await getTask(page, parent.id, task.id)).status(), 'the fork\'s task, read from the parent').toBe(200)

  // Allowed: a fork refused for the rate limit, and the merged fork's own page
  // reading it once more before the app moves to the parent (the list is
  // refreshed before the navigation, so the page sees its workspace vanish).
  traffic.expectClean({ allow: ['429 /forks', `404 /workspaces/${fork.id}`] })
})

// A fork made or merged elsewhere — by the supervisor's MCP, or another tab —
// shows in the open page's sidebar without a reload.
test('a fork made and merged outside the page appears and disappears in its sidebar live', async ({ page, workspace: parent }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${parent.id}`)
  await openSidebar(page)
  await expect(page.getByRole('button', { name: `${parent.name} actions`, exact: true })).toBeVisible()

  const fork = await forkWorkspace(page, parent.id, unique('live-fork'))
  const row = page.locator('[data-test="sidebar-fork"]').filter({ hasText: fork.name })
  await expect(row, 'the fork is listed under its parent without a reload').toBeVisible()

  await mergeFork(page, fork.id)
  await expect(row, 'the merged fork leaves the sidebar without a reload').toHaveCount(0)

  traffic.expectClean()
})
