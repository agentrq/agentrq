// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, finishOngoingTasks, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

const OURS = 'https://app.agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json'

async function reply(call, payload) {
  const text = await call('task', { action: 'reply', payload })
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  const { result } = JSON.parse(data.slice('data: '.length))
  return { text: result.content[0].text, refused: !!result.isError }
}

const CHECKS = ['Lint', 'Unit tests', 'Postgres tests', 'Frontend coverage', 'Build']
// The card's data model once done of the checks have passed, the next running.
const model = (done) => ({
  done,
  total: CHECKS.length,
  checks: CHECKS.map((name, i) => ({ name, icon: i < done ? 'check' : i === done ? 'refresh' : 'pause' })),
})

test('an agent\'s checks card draws a progress bar that moves in place as checks pass', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await finishOngoingTasks(page, workspace.id)
  const task = await createTask(page, workspace.id, unique('a task with CI checks'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)

  const first = await reply(call, {
    taskId: task.id,
    text: 'CI: 2 of 5 checks passed.',
    ui: [
      { version: '1.0', createSurface: { surfaceId: 'qa-checks', catalogId: OURS } },
      { version: '1.0', updateComponents: { surfaceId: 'qa-checks', components: [
        { id: 'root', component: 'Card', child: 'col' },
        { id: 'col', component: 'Column', children: ['title', 'bar', 'list'] },
        { id: 'title', component: 'Text', text: '## CI checks' },
        { id: 'bar', component: 'Progress', label: { '@call': 'formatString', args: { value: '${/done} of ${/total} checks' } }, value: { '@path': '/done' }, max: { '@path': '/total' } },
        { id: 'list', component: 'List', children: { path: '/checks', componentId: 'check' } },
        { id: 'check', component: 'Row', children: ['check-icon', 'check-name'] },
        { id: 'check-icon', component: 'Icon', name: { '@path': 'icon' } },
        { id: 'check-name', component: 'Text', text: { '@path': 'name' } },
      ] } },
      { version: '1.0', updateDataModel: { surfaceId: 'qa-checks', path: '/', value: model(2) } },
    ],
  })
  expect(first, 'the checks card').toEqual({ text: 'reply sent', refused: false })

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const card = page.locator('[data-test="a2ui-surface"][data-surface-id="qa-checks"]')
  const bar = card.locator('[data-test="a2ui-progress-bar"]')
  await expect(card.locator('[data-test="a2ui-progress-value"]'), 'the percent').toHaveText('40%')
  await expect(card.getByRole('progressbar', { name: '2 of 5 checks' }), 'the bar, named for a screen reader').toHaveAttribute('aria-valuenow', '40')
  const passed = card.locator('[data-test="a2ui-icon"] path[d="M5 13l4 4L19 7"]')
  await expect(passed, 'a check mark per passed check').toHaveCount(2)
  await card.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-progress-light.png'), animations: 'disabled' })

  const drawn = await bar.elementHandle()
  const update = await reply(call, {
    taskId: task.id,
    text: 'CI: 4 of 5 checks passed.',
    ui: [{ version: '1.0', updateDataModel: { surfaceId: 'qa-checks', path: '/', value: model(4) } }],
  })
  expect(update.refused, update.text).toBe(false)
  await expect(card.locator('[data-test="a2ui-progress-value"]'), 'the percent, updated live').toHaveText('80%')
  await expect(card.getByText('4 of 5 checks'), 'the label, updated live').toBeVisible()
  expect(await drawn.evaluate((el) => el.isConnected), 'the same bar, moved in place').toBe(true)
  await expect(passed, 'the check marks, updated live').toHaveCount(4)
  await page.waitForTimeout(700) // the bar's width transition
  await page.screenshot({ path: test.info().outputPath('a2ui-progress-updated.png') })

  await page.evaluate(() => localStorage.setItem('theme', 'dark'))
  await page.reload()
  await expect(card.locator('[data-test="a2ui-progress-value"]'), 'the percent, dark').toHaveText('80%')
  await card.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-progress-dark.png'), animations: 'disabled' })
  await page.evaluate(() => localStorage.setItem('theme', 'light'))
  await page.reload()
  await page.setViewportSize({ width: 390, height: 844 })
  await card.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-progress-phone.png'), animations: 'disabled' })
  traffic.expectClean({ allow: ['404 /plan'] })
  await setTaskStatus(page, workspace.id, task.id, 'completed')
})
