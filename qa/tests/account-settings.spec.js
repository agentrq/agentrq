// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { readFileSync } from 'node:fs'
import { gunzipSync } from 'node:zlib'

import { test, expect } from '../lib/fixtures.js'
import { createTask, nextResponse, openSidebar, watchTraffic } from '../lib/app.js'

const theme = (page, dark) => page.evaluate((d) => {
  localStorage.setItem('theme', d ? 'dark' : 'light')
  document.documentElement.classList.toggle('dark', d)
}, dark)

async function shots(page, name, scrollTo) {
  await page.screenshot({ path: test.info().outputPath(`${name}-light.png`), animations: 'disabled' })
  await theme(page, true)
  await page.screenshot({ path: test.info().outputPath(`${name}-dark.png`), animations: 'disabled' })
  await theme(page, false)
  await page.setViewportSize({ width: 390, height: 844 })
  if (scrollTo) await scrollTo.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath(`${name}-phone.png`), animations: 'disabled' })
  await page.setViewportSize({ width: 1400, height: 900 })
}

const CLOCK_12H = /\d{1,2}:\d{2}\s?[AP]M/
const CLOCK_24H = /^(?!.*[AP]M).*\d{2}:\d{2}/

// The account menu opens the account's settings. The clock picked there is
// saved, kept after a reload, and used for every time in the app; a preset
// added there is offered in a workspace's new task form.
test('account settings pick the clock for every time and keep presets for every workspace', async ({ page, workspace }) => {
  const task = await createTask(page, workspace.id, 'Clock check')
  await page.goto(`/workspaces/${workspace.id}`)
  await expect(page.getByText('Clock check').first()).toBeVisible()
  const userMenu = page.locator('button[aria-expanded]:has(> div.rounded-full)')
  const link = page.getByTestId('account-settings-link')
  const openMenu = async () => {
    if (!(await link.isVisible())) await userMenu.click()
    await expect(link).toBeVisible()
  }
  await openSidebar(page)
  await openMenu()
  await page.screenshot({ path: test.info().outputPath('account-menu-light.png'), animations: 'disabled' })
  await theme(page, true)
  await openMenu()
  await page.screenshot({ path: test.info().outputPath('account-menu-dark.png'), animations: 'disabled' })
  await theme(page, false)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.getByRole('button', { name: /Menu/ }).last().click()
  await openMenu()
  await page.screenshot({ path: test.info().outputPath('account-menu-phone.png'), animations: 'disabled' })
  await page.setViewportSize({ width: 1400, height: 900 })
  await openMenu()
  await link.click()
  await expect(page).toHaveURL(/\/settings$/)
  await expect(page.getByTestId('account-settings')).toBeVisible()

  let saved = nextResponse(page, 'PUT', '/account/settings')
  await page.getByTestId('time-format-12h').click()
  expect((await saved).status(), 'saving 12h').toBe(200)
  await expect(page.getByTestId('time-format-example')).toHaveText(CLOCK_12H)
  await shots(page, 'account-settings-general-12h', page.getByTestId('time-format'))

  await page.reload()
  await expect(page.getByTestId('time-format-example')).toHaveText(CLOCK_12H)
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  await expect(page.getByText(CLOCK_12H).first()).toBeVisible()
  await shots(page, 'task-12h')

  await page.goto('/settings')
  saved = nextResponse(page, 'PUT', '/account/settings')
  await page.getByTestId('time-format-24h').click()
  expect((await saved).status(), 'saving 24h').toBe(200)
  await expect(page.getByTestId('time-format-example')).toHaveText(CLOCK_24H)
  await shots(page, 'account-settings-general-24h', page.getByTestId('time-format'))
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  await expect(page.getByText(CLOCK_12H)).toHaveCount(0)
  await shots(page, 'task-24h')

  await page.goto('/settings?tab=presets')
  await expect(page.getByTestId('presets-account-note')).toBeVisible()
  await page.getByTestId('preset-name').fill('ship-it')
  await page.getByTestId('preset-body').fill('tag the release and push it')
  const added = nextResponse(page, 'POST', '/account/presets')
  await page.getByTestId('preset-add').click()
  expect((await added).status(), 'saving the account preset').toBe(201)
  await expect(page.getByTestId('preset-row')).toHaveText(/#ship-it/)
  await shots(page, 'account-settings-presets', page.getByTestId('presets'))

  // Beside a workspace's own preset, each marked with where it is kept.
  const own = await page.request.post(`/api/v1/workspaces/${workspace.id}/presets`, { data: { name: 'ship-docs', body: 'publish the docs site' } })
  expect(own.status(), 'saving the workspace preset').toBe(201)
  await page.goto(`/workspaces/${workspace.id}/tasks/new`)
  await page.locator('#taskForm textarea').fill('Now: #sh')
  const menu = page.getByRole('listbox', { name: 'Presets' })
  await expect(menu.getByRole('option', { name: /#ship-docs/ }).getByRole('img', { name: 'Workspace preset' })).toBeVisible()
  await expect(menu.getByRole('option', { name: /#ship-it/ }).getByRole('img', { name: 'Account preset' })).toBeVisible()
  await shots(page, 'account-preset-new-task')

  // The schedule's time fields are drawn on the account's clock too, not the
  // browser's: 24h now, then 12h.
  for (const format of ['24h', '12h']) {
    const res = await page.request.put('/api/v1/account/settings', { data: { display: { timeFormat: format } } })
    expect(res.status(), `saving ${format}`).toBe(200)
    await page.goto(`/workspaces/${workspace.id}/tasks/new`)
    await page.getByTestId('task-schedule').click()
    await page.getByTestId('task-schedule-repeated').click()
    await page.getByTestId('task-repeat-preset').selectOption('daily')
    const time = page.getByTestId('task-repeat-time')
    await expect(time.getByTestId('clock-period')).toHaveCount(format === '12h' ? 1 : 0)
    await time.getByTestId('clock-hour').selectOption(format === '12h' ? '6' : '18')
    if (format === '12h') await time.getByTestId('clock-period').selectOption('PM')
    await expect(page.getByText(/^\d+ \d+ \* \* \*$/)).toBeVisible()
    await shots(page, `schedule-repeat-${format}`)
    await page.getByTestId('task-schedule-onetime').click()
    const when = page.getByTestId('task-onetime-date')
    await when.getByTestId('datetime-date').fill('2030-03-04')
    await when.getByTestId('clock-hour').selectOption(format === '12h' ? '9' : '21')
    await shots(page, `schedule-onetime-${format}`)
  }
})

// Connections is a tab because the run's GitHub App is set up; Profile shows
// who is signed in and downloads everything the account holds as a gzipped
// JSON file, the workspace's task among it.
test('account settings connect GitHub and download the account\'s data', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, 'Export check')

  await page.goto('/settings?tab=connections')
  await expect(page.getByTestId('settings-tab-connections')).toHaveAttribute('aria-selected', 'true')
  await expect(page.getByTestId('github-connection')).toBeVisible()
  await expect(page.getByTestId('gitlab-connection')).toHaveCount(0)
  await shots(page, 'account-settings-connections', page.getByTestId('settings-connections'))

  await page.getByTestId('settings-tab-profile').click()
  await expect(page).toHaveURL(/tab=profile/)
  const me = await (await page.request.get('/api/v1/auth/user')).json()
  await expect(page.getByTestId('profile-email')).toHaveValue(me.email)
  await expect(page.getByTestId('profile-name')).toHaveValue(me.name)
  await expect(page.getByTestId('profile-email')).toHaveAttribute('readonly', '')
  await shots(page, 'account-settings-profile', page.getByTestId('export-data'))

  const download = page.waitForEvent('download')
  const exported = nextResponse(page, 'GET', '/account/export')
  await page.getByTestId('export-data').click()
  expect((await exported).status(), 'the export').toBe(200)
  const file = await download
  expect(file.suggestedFilename()).toMatch(/^agentrq-export-\d{4}-\d{2}-\d{2}\.json\.gz$/)
  const data = JSON.parse(gunzipSync(readFileSync(await file.path())).toString('utf-8'))
  expect(data.version).toBe(1)
  expect(data.profile.email).toBe(me.email)
  expect(data.workspaces.map((w) => w.id)).toContain(workspace.id)
  expect(data.tasks.find((t) => t.id === task.id)?.title).toBe('Export check')
  traffic.expectClean()
})
