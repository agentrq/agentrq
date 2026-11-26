// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { createTask, nextResponse, watchTraffic } from '../lib/app.js'

const BODY = 'pull latest main, tag and push'
const DEPLOY = 'deploy to staging, then run the smoke tests'

const theme = (page, dark) => page.evaluate((d) => {
  localStorage.setItem('theme', d ? 'dark' : 'light')
  document.documentElement.classList.toggle('dark', d)
}, dark)

// A preset is added on the workspace's Presets tab and saved at once; typing
// #name in a new task or a reply then offers it, and choosing it puts its
// message in place of the name.
test('a preset added in settings is put in by #name in a new task and a reply', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${workspace.id}/settings?tab=presets`)
  await expect(page.getByTestId('presets-empty')).toBeVisible()

  await page.getByTestId('preset-name').fill('pull-n-push')
  await page.getByTestId('preset-body').fill(BODY)
  const saved = nextResponse(page, 'POST', `/workspaces/${workspace.id}/presets`)
  await page.getByTestId('preset-add').click()
  expect((await saved).status(), 'saving the preset').toBe(201)
  await expect(page.getByTestId('preset-row')).toHaveText(/#pull-n-push/)
  await page.getByTestId('preset-name').fill('deploy')
  await page.getByTestId('preset-body').fill('deploy to staging')
  await page.getByTestId('preset-add').click()
  await expect(page.getByTestId('preset-row')).toHaveCount(2)
  await page.screenshot({ path: test.info().outputPath('presets-settings-light.png'), animations: 'disabled' })
  await theme(page, true)
  await page.screenshot({ path: test.info().outputPath('presets-settings-dark.png'), animations: 'disabled' })
  await theme(page, false)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByTestId('presets').scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('presets-settings-phone.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 1400, height: 900 })

  // The same name again is refused, with the server's sentence.
  await page.getByTestId('preset-name').fill('deploy')
  await page.getByTestId('preset-body').fill('again')
  const refused = nextResponse(page, 'POST', `/workspaces/${workspace.id}/presets`)
  await page.getByTestId('preset-add').click()
  expect((await refused).status(), 'saving a name the workspace has').toBe(409)
  await expect(page.getByTestId('presets-error')).toHaveText('a preset named "deploy" exists')

  // A saved preset is edited in place: the pencil loads it into the form, and
  // Save keeps the new message under the same name.
  await page.getByRole('button', { name: 'Edit #deploy' }).click()
  await expect(page.getByTestId('preset-form-title')).toHaveText('Edit #deploy')
  await expect(page.getByTestId('presets-error')).toHaveCount(0)
  await expect(page.getByTestId('preset-body')).toHaveValue('deploy to staging')
  await page.getByTestId('preset-body').fill(DEPLOY)
  await page.screenshot({ path: test.info().outputPath('presets-edit-light.png'), animations: 'disabled' })
  await theme(page, true)
  await page.screenshot({ path: test.info().outputPath('presets-edit-dark.png'), animations: 'disabled' })
  await theme(page, false)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByTestId('preset-add').scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('presets-edit-phone.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 1400, height: 900 })
  const edited = nextResponse(page, 'PUT', `/workspaces/${workspace.id}/presets/deploy`)
  await page.getByTestId('preset-add').click()
  expect((await edited).status(), 'editing the preset').toBe(200)
  await expect(page.getByTestId('preset-form-title')).toHaveText('Add a Preset')
  await expect(page.getByTestId('preset-row').filter({ hasText: '#deploy' })).toContainText(DEPLOY)

  // The new task form: #pu offers pull-n-push, and Enter puts its message in.
  await page.goto(`/workspaces/${workspace.id}/tasks/new`)
  const description = page.locator('#taskForm textarea')
  await description.fill('Before the release: #pu')
  await expect(page.getByRole('listbox', { name: 'Presets' })).toContainText('#pull-n-push')
  await page.screenshot({ path: test.info().outputPath('presets-new-task-light.png'), animations: 'disabled' })
  await theme(page, true)
  await page.screenshot({ path: test.info().outputPath('presets-new-task-dark.png'), animations: 'disabled' })
  await theme(page, false)
  await description.press('Enter')
  await expect(description).toHaveValue(`Before the release: ${BODY}`)
  await expect(page.getByRole('listbox', { name: 'Presets' })).toHaveCount(0)

  // A reply: # alone lists both, and a click chooses.
  const task = await createTask(page, workspace.id, 'Ship the release', 'ongoing', { assignee: 'human' })
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const composer = page.locator('form textarea').last()
  await composer.fill('#')
  const menu = page.getByRole('listbox', { name: 'Presets' })
  await expect(menu.getByRole('option')).toHaveText([/#deploy/, /#pull-n-push/])
  await page.screenshot({ path: test.info().outputPath('presets-reply-light.png'), animations: 'disabled' })
  await menu.getByRole('option').filter({ hasText: '#deploy' }).dispatchEvent('mousedown')
  await expect(composer).toHaveValue(DEPLOY)

  // Removed from settings, it is no longer offered.
  await page.goto(`/workspaces/${workspace.id}/settings?tab=presets`)
  const removed = nextResponse(page, 'DELETE', `/workspaces/${workspace.id}/presets/deploy`)
  await page.getByRole('button', { name: 'Remove #deploy' }).click()
  expect((await removed).status(), 'removing the preset').toBe(204)
  await expect(page.getByTestId('preset-row')).toHaveCount(1)

  traffic.expectClean({ allow: ['409 /presets'] })
})
