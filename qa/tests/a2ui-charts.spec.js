// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, finishOngoingTasks, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

const OURS = 'https://agentrq.com/a2ui/v1_0/catalogs/agentrq/catalog.json'
const BASIC = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'

async function reply(call, payload) {
  const text = await call('task', { action: 'reply', payload })
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  const { result } = JSON.parse(data.slice('data: '.length))
  return { text: result.content[0].text, refused: !!result.isError }
}

const ui = (surfaceId, components, catalogId = OURS) => [
  { version: '1.0', createSurface: { surfaceId, catalogId } },
  { version: '1.0', updateComponents: { surfaceId, components } },
]

// A day's label, n days before 2026-11-24.
function day(n) {
  const d = new Date(Date.UTC(2026, 10, 24 - n))
  return d.toISOString().slice(0, 10)
}

const velocity = Array.from({ length: 14 }, (_, i) => ({ label: day(13 - i), value: [3, 5, 4, 8, 6, 2, 1, 7, 9, 6, 10, 8, 4, 11][i] }))
const byRepo = [{ label: 'backend', value: 18 }, { label: 'frontend', value: 12 }, { label: 'daemon', value: 5 }, { label: 'qa', value: 3 }]
const activity = Array.from({ length: 84 }, (_, i) => ({ label: day(83 - i), value: (i * 7) % 11 > 6 ? 0 : (i * 5) % 9 }))

test('an agent\'s card draws line, bar and heatmap charts as the analytics pages do', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await finishOngoingTasks(page, workspace.id)
  const task = await createTask(page, workspace.id, unique('a task answered with charts'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)

  const trends = await reply(call, {
    taskId: task.id,
    text: '72 tasks done in two weeks, 11 yesterday; 38 PRs, most in backend.',
    ui: ui('qa-trends', [
      { id: 'root', component: 'Card', child: 'col' },
      { id: 'col', component: 'Column', children: ['title', 'row'] },
      { id: 'title', component: 'Text', text: '## Two weeks of work' },
      { id: 'row', component: 'Row', children: ['line', 'bar'] },
      { id: 'line', component: 'Chart', label: 'Tasks completed', data: velocity, weight: 1 },
      { id: 'bar', component: 'Chart', variant: 'bar', label: 'PRs by repo', color: 'messages', data: byRepo, weight: 1 },
    ]),
  })
  expect(trends, 'the line and bar card').toEqual({ text: 'reply sent', refused: false })
  const heat = await reply(call, {
    taskId: task.id,
    text: 'Activity over the last 12 weeks.',
    ui: ui('qa-heat', [
      { id: 'root', component: 'Card', child: 'chart' },
      { id: 'chart', component: 'Chart', variant: 'heatmap', label: 'Tasks per day', data: activity },
    ]),
  })
  expect(heat, 'the heatmap card').toEqual({ text: 'reply sent', refused: false })

  const basic = await reply(call, { taskId: task.id, text: 'x', ui: ui('qa-basic', [{ id: 'root', component: 'Chart', data: byRepo }], BASIC) })
  expect(basic.refused, 'a Chart on a basic-catalog card').toBe(true)
  expect(basic.text, 'the reason').toContain('Chart is in our catalog')

  await page.setViewportSize({ width: 1280, height: 1000 })
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const trendsCard = page.locator('[data-test="a2ui-surface"][data-surface-id="qa-trends"]')
  const heatCard = page.locator('[data-test="a2ui-surface"][data-surface-id="qa-heat"]')
  await expect(trendsCard.locator('[data-test="a2ui-chart"][data-variant="line"]'), 'the line chart').toBeVisible()
  await expect(trendsCard.locator('[data-test="a2ui-chart"][data-variant="bar"] [data-test="bar"]'), 'a bar per repo').toHaveCount(4)
  await expect(trendsCard.getByRole('img', { name: /^Tasks completed\. / }), 'the line chart, named for a screen reader').toBeVisible()
  await expect(heatCard.locator('[data-test="a2ui-chart"][data-variant="heatmap"]'), 'the heatmap').toBeVisible()
  await expect(trendsCard.getByText('PRs by repo'), 'the bar chart\'s label').toBeVisible()

  await trendsCard.locator('[data-test="bar"]').first().hover()
  await expect(trendsCard.getByText('backend: 18'), 'the hovered bar\'s tooltip').toBeVisible()
  await page.mouse.move(0, 0)

  await heatCard.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-charts-light.png'), animations: 'disabled' })
  // The charts read the theme from its store, so the page reloads in dark.
  await page.evaluate(() => localStorage.setItem('theme', 'dark'))
  await page.reload()
  await expect(heatCard.locator('[data-test="a2ui-chart"]'), 'the heatmap, dark').toBeVisible()
  await heatCard.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-charts-dark.png'), animations: 'disabled' })
  await page.evaluate(() => localStorage.setItem('theme', 'light'))
  await page.reload()
  await page.setViewportSize({ width: 390, height: 844 })
  await trendsCard.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-charts-phone.png'), animations: 'disabled' })
  traffic.expectClean({ allow: ['404 /plan'] })
  await setTaskStatus(page, workspace.id, task.id, 'completed')
})
