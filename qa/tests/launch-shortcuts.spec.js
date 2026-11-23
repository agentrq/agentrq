// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { watchTraffic } from '../lib/app.js'

// The installed app's icon shortcuts open these URLs (the manifest's
// `shortcuts`); the shell opens the overlay and drops the query.

test('the Find task shortcut opens the task finder', async ({ page }) => {
  const traffic = watchTraffic(page)
  await page.goto('/?shortcut=find-task')
  await expect(page.getByRole('dialog', { name: 'Find task' })).toBeVisible()
  await expect(page).toHaveURL((url) => !url.searchParams.has('shortcut'))
  traffic.expectClean()
})

test('the New task shortcut asks which workspace', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  // The account has its supervisor workspace and this run's one, so the
  // switcher asks rather than going straight to a form.
  expect(workspace.id).toBeTruthy()
  await page.goto('/?shortcut=new-task')
  await expect(page.getByRole('dialog', { name: 'New task in workspace' })).toBeVisible()
  await expect(page).toHaveURL((url) => !url.searchParams.has('shortcut'))
  traffic.expectClean()
})
