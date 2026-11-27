// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { nextResponse, watchTraffic } from '../lib/app.js'

// Skills belong to the account. A workspace's Skills tab lists every one with
// that workspace's switch; a link the importer cannot take is said under its
// field when Discover is pressed, and nothing is sent. The tab's import turns
// skills on in this workspace alone, so it offers no choice of workspaces.
test('the skills tab lists the account\'s skills and says why a link cannot be imported', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const listed = nextResponse(page, 'GET', `/workspaces/${workspace.id}/skills`)
  await page.goto(`/workspaces/${workspace.id}/settings`)
  await page.getByRole('button', { name: 'Skills', exact: true }).click()
  expect((await listed).status(), 'the skills list request').toBe(200)
  await expect(page.getByLabel('Import from GitHub'), 'the import form').toBeVisible()
  await expect(page.getByTestId('skill-import-workspaces'), 'a choice of workspaces to import into').toHaveCount(0)

  let sent = 0
  page.on('request', (r) => { if (r.url().includes('/skills/import')) sent += 1 })
  await page.getByLabel('Import from GitHub').fill('https://example.com/owner/repo')
  await page.getByTestId('skill-import').click()
  await expect(page.getByTestId('field-error'), 'what a link must look like, under the field').toContainText('Expected https://github.com/owner/repo')
  await expect(page.getByLabel('Import from GitHub'), 'the field is marked invalid').toHaveAttribute('aria-invalid', 'true')
  await expect(page.getByLabel('Import from GitHub'), 'and has the focus back').toBeFocused()
  expect(sent, 'a link the importer cannot take is not sent').toBe(0)

  traffic.expectClean()
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
