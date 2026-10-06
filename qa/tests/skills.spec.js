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

// The Skills page, from the sidebar: the account's list, read from the
// account's own route.
test('the skills page lists the account\'s skills', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${workspace.id}`)
  const listed = nextResponse(page, 'GET', '/api/v1/skills')
  // By its address: the sidebar starts collapsed, where a link shows only its icon.
  await page.locator('nav a[href="/skills"]').first().click()
  expect((await listed).status(), 'the account\'s skills list request').toBe(200)
  await expect(page).toHaveURL(/\/skills$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Skills' })).toBeVisible()
  await expect(page.getByLabel('Import from GitHub'), 'skills can be imported here too').toBeVisible()

  traffic.expectClean()
})
