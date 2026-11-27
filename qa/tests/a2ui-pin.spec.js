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

const STEPS = ['Fact-check guidelines', 'Script', 'Voice track', 'Scenes and stills review', '4K render', 'Share link and poster', 'PR, tests, merge']
// The card's data model once done of the steps are done, the next running.
const model = (done) => ({
  done,
  total: STEPS.length,
  steps: STEPS.map((name, i) => ({ name: `**${name}**: ${i < done ? 'done' : i === done ? 'running' : 'queued'}`, icon: i < done ? 'check' : i === done ? 'refresh' : 'moreHoriz' })),
})

// A progress card like the skill teaches: a title, a bar, and a row per step.
const progressCard = (surfaceId, title, done) => [
  { version: '1.0', createSurface: { surfaceId, catalogId: OURS } },
  { version: '1.0', updateComponents: { surfaceId, components: [
    { id: 'root', component: 'Card', child: 'col' },
    { id: 'col', component: 'Column', children: ['title', 'bar', 'list'] },
    { id: 'title', component: 'Text', text: `### ${title}` },
    { id: 'bar', component: 'Progress', label: { '@call': 'formatString', args: { value: '${/done} of ${/total} steps' } }, value: { '@path': '/done' }, max: { '@path': '/total' } },
    { id: 'list', component: 'List', children: { path: '/steps', componentId: 'step' } },
    { id: 'step', component: 'Row', align: 'center', children: ['step-icon', 'step-name'] },
    { id: 'step-icon', component: 'Icon', name: { '@path': 'icon' } },
    { id: 'step-name', component: 'Text', text: { '@path': 'name' } },
  ] } },
  { version: '1.0', updateDataModel: { surfaceId, path: '/', value: model(done) } },
]

// The pinned progress card stays over the conversation under the timeline,
// follows the agent's updates in place, folds to its top there and in the
// thread, is still pinned and folded after a reload, and only one is pinned.
test('a progress card pins under the timeline and folds to its title and bar', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await finishOngoingTasks(page, workspace.id)
  const task = await createTask(page, workspace.id, unique('a task with a progress card'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)

  const sent = await reply(call, { taskId: task.id, text: 'Video: 4 of 7 steps done.', ui: progressCard('qa-video', 'Task board video: progress', 4) })
  expect(sent, 'the progress card').toEqual({ text: 'reply sent', refused: false })
  for (let i = 1; i <= 4; i++) {
    await reply(call, { taskId: task.id, text: `Note ${i}: ${'a longer line of progress notes to fill the thread. '.repeat(4)}` })
  }
  const other = await reply(call, { taskId: task.id, text: 'Docs: 1 of 7 steps done.', ui: progressCard('qa-docs', 'Docs: progress', 1) })
  expect(other, 'a second progress card').toEqual({ text: 'reply sent', refused: false })

  await page.setViewportSize({ width: 1280, height: 900 })
  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const pinned = page.locator('[data-test="pinned-card"]')
  const inThread = (id) => page.locator(`[data-test="a2ui-surface"][data-surface-id="${id}"]:not([data-test="pinned-card"] *)`)
  const bubble = (id) => page.locator('div.group').filter({ has: inThread(id) })
  const pin = async (id) => {
    await bubble(id).hover()
    await bubble(id).locator('[data-test="card-pin"]').click()
  }

  await pin('qa-video')
  await expect(pinned, 'the pinned card').toHaveCount(1)
  await expect(pinned.getByRole('progressbar', { name: '4 of 7 steps' }), 'its bar').toBeVisible()
  await expect(pinned.getByText('Voice track'), 'its steps').toBeVisible()

  // Scrolled to the newest message, the card is still in view.
  await page.getByText('Docs: progress').scrollIntoViewIfNeeded()
  await expect(pinned, 'the pinned card, with the thread scrolled').toBeInViewport()
  await page.screenshot({ path: test.info().outputPath('a2ui-pin-light.png'), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
  await page.screenshot({ path: test.info().outputPath('a2ui-pin-dark.png'), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'light'); document.documentElement.classList.remove('dark') })

  // The agent updates the card in place: the pinned copy follows.
  const updated = await reply(call, {
    taskId: task.id,
    text: 'Video: 5 of 7 steps done.',
    ui: [{ version: '1.0', updateDataModel: { surfaceId: 'qa-video', path: '/', value: model(5) } }],
  })
  expect(updated.refused, `the update: ${updated.text}`).toBe(false)
  await expect(pinned.getByRole('progressbar', { name: '5 of 7 steps' }), 'the update, pinned').toBeVisible()

  // Folded where pinned: the title and bar stay, the steps fade out.
  await pinned.locator('[data-test="pinned-card-toggle"]').click()
  await expect(pinned, 'the pinned card, folded').toHaveAttribute('data-collapsed', 'true')
  await expect(pinned.getByRole('progressbar'), 'its bar, still shown').toBeInViewport()
  expect((await pinned.boundingBox()).height, 'the folded card\'s height').toBeLessThanOrEqual(96)
  // And in the thread, on its own.
  await expect(inThread('qa-video'), 'the thread\'s card, open').not.toHaveAttribute('data-collapsed', 'true')
  await bubble('qa-docs').locator('[data-test="card-collapse"]').click()
  await expect(inThread('qa-docs'), 'the thread\'s card, folded').toHaveAttribute('data-collapsed', 'true')
  await page.screenshot({ path: test.info().outputPath('a2ui-pin-collapsed.png'), animations: 'disabled' })

  await page.reload()
  await expect(pinned, 'still pinned and folded after a reload').toHaveAttribute('data-collapsed', 'true')
  await expect(inThread('qa-docs'), 'the thread\'s card, still folded').toHaveAttribute('data-collapsed', 'true')

  // Only one is pinned: pinning another replaces it.
  await pin('qa-docs')
  await expect(pinned, 'one pinned card').toHaveCount(1)
  await expect(pinned.getByText('Docs: progress'), 'the one pinned last').toBeVisible()

  await page.setViewportSize({ width: 390, height: 844 })
  await expect(pinned, 'the new pin, open').not.toHaveAttribute('data-collapsed', 'true')
  await page.screenshot({ path: test.info().outputPath('a2ui-pin-phone.png'), animations: 'disabled' })

  await pinned.locator('[data-test="pinned-card-unpin"]').click()
  await expect(pinned, 'unpinned').toHaveCount(0)
  traffic.expectClean({ allow: ['404 /plan'] })
  await setTaskStatus(page, workspace.id, task.id, 'completed')
})
