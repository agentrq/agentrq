// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createWorkspace, unique, watchTraffic } from '../lib/app.js'

// A workspace created, renamed and deleted outside the page — by the
// supervisor's MCP, or another tab — changes the open page's sidebar without a
// reload.
test('a workspace changed outside the page shows in its sidebar live', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${workspace.id}`)
  const expand = page.getByTitle('Expand sidebar')
  if (await expand.isVisible()) await expand.click()
  await expect(page.getByRole('button', { name: `${workspace.name} actions`, exact: true })).toBeVisible()
  const row = (name) => page.getByRole('button', { name: `${name} actions`, exact: true })

  const created = await createWorkspace(page, unique('live-workspace'))
  await expect(row(created.name), 'a created workspace is listed without a reload').toBeVisible()

  const renamed = `${created.name}-renamed`
  const res = await page.request.patch(`/api/v1/workspaces/${created.id}`, { data: { workspace: { name: renamed, description: created.description } } })
  expect(res.status(), 'the rename').toBe(200)
  await expect(row(renamed), 'a renamed workspace shows its new name without a reload').toBeVisible()
  await expect(row(created.name)).toHaveCount(0)

  const deleted = await page.request.delete(`/api/v1/workspaces/${created.id}`)
  expect(deleted.ok(), 'the delete').toBe(true)
  await expect(row(renamed), 'a deleted workspace leaves the sidebar without a reload').toHaveCount(0)

  traffic.expectClean()
})
