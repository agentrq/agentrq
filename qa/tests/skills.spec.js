// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { nextResponse, watchTraffic } from '../lib/app.js'

// Skills belong to the account. A workspace's Skills tab lists every one with
// that workspace's switch; an import the server refuses shows its reason as
// the server wrote it. The tab's import starts with this workspace ticked.
test('the skills tab lists the account\'s skills and shows why an import was refused', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const listed = nextResponse(page, 'GET', `/workspaces/${workspace.id}/skills`)
  await page.goto(`/workspaces/${workspace.id}/settings`)
  await page.getByRole('button', { name: 'Skills', exact: true }).click()
  expect((await listed).status(), 'the skills list request').toBe(200)
  await expect(page.getByTestId('skill-import-workspace').filter({ hasText: workspace.name }).getByRole('checkbox'),
    'the workspace the import is started from is ticked').toBeChecked()

  await page.getByLabel('Import from GitHub').fill('https://example.com/owner/repo')
  const refused = nextResponse(page, 'POST', '/skills/import')
  await page.getByTestId('skill-import').click()
  const res = await refused
  expect(res.status(), 'importing from a link that is not GitHub').toBeGreaterThanOrEqual(400)
  expect(res.status()).toBeLessThan(500)
  expect(res.request().postDataJSON().workspaceIds, 'the import names the workspace to turn it on in').toEqual([workspace.id])
  const { error } = await res.json()
  await expect(page.getByText(error.message), 'the refusal, as the server wrote it').toBeVisible()

  traffic.expectClean({ allow: [`${res.status()} /skills/import`] })
})

// A workspace's skills, from its task list's Skills button: the sidebar has
// no Skills page of its own, so this is how the list is reached.
test('a workspace\'s Skills button opens its Skills tab', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${workspace.id}`)
  await expect(page.locator('nav a[href="/skills"]'), 'the sidebar has no Skills page').toHaveCount(0)
  const listed = nextResponse(page, 'GET', `/workspaces/${workspace.id}/skills`)
  await page.getByTestId('workspace-skills-link').click()
  expect((await listed).status(), 'the workspace\'s skills list request').toBe(200)
  await expect(page).toHaveURL(new RegExp(`/workspaces/${workspace.id}/settings\\?tab=skills$`))
  await expect(page.getByLabel('Import from GitHub'), 'skills are imported here').toBeVisible()

  traffic.expectClean()
})
