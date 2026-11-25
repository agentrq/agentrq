// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, finishOngoingTasks, getTask, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

const CATALOG = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'
const SURFACE = 'qa-progress'

// Calls task reply as an agent, and reads its answer: the result's text, and
// whether it was refused.
async function reply(call, payload) {
  const text = await call('task', { action: 'reply', payload })
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  const { result } = JSON.parse(data.slice('data: '.length))
  return { text: result.content[0].text, refused: !!result.isError }
}

const steps = (done) => ['Build', 'Test', 'Deploy'].map((name, i) => ({ name, state: i < done ? 'done' : i === done ? 'running' : 'waiting' }))

// A progress card: a count, one line per step drawn from the data model, and
// a note the person may type while it runs.
function progressCard() {
  return [
    { version: '1.0', createSurface: { surfaceId: SURFACE, catalogId: CATALOG } },
    { version: '1.0', updateComponents: { surfaceId: SURFACE, components: [
      { id: 'root', component: 'Card', child: 'col' },
      { id: 'col', component: 'Column', children: ['title', 'count', 'steps', 'note'] },
      { id: 'title', component: 'Text', text: '## Releasing 0.9.4' },
      { id: 'count', component: 'Text', text: { '@call': 'formatString', args: { value: '**${/done}/3** steps done' } } },
      { id: 'steps', component: 'List', children: { path: '/steps', componentId: 'step' } },
      { id: 'step', component: 'Text', variant: 'caption', text: { '@call': 'formatString', args: { value: '${name} · ${state}' } } },
      { id: 'note', component: 'TextField', label: 'Note for the release', value: { '@path': '/note' } },
    ] } },
    { version: '1.0', updateDataModel: { surfaceId: SURFACE, path: '/', value: { done: 1, steps: steps(1), note: '' } } },
  ]
}

const progress = (done) => [
  { version: '1.0', updateDataModel: { surfaceId: SURFACE, path: '/done', value: done } },
  { version: '1.0', updateDataModel: { surfaceId: SURFACE, path: '/steps', value: steps(done) } },
]

// The card from its top, in light, dark and at phone width.
async function shots(page, surface, name) {
  const top = () => surface.evaluate((el) => el.scrollIntoView({ block: 'start' }))
  await top()
  await page.screenshot({ path: test.info().outputPath(`${name}-light.png`) })
  await page.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
  await page.screenshot({ path: test.info().outputPath(`${name}-dark.png`) })
  await page.evaluate(() => { localStorage.setItem('theme', 'light'); document.documentElement.classList.remove('dark') })
  const size = page.viewportSize()
  await page.setViewportSize({ width: 390, height: 844 })
  await top()
  await page.screenshot({ path: test.info().outputPath(`${name}-phone.png`) })
  await page.setViewportSize(size)
}

// An agent's progress card goes 1/3 → 2/3 → 3/3 by replies that update it in
// place: the open page patches the same card, live, marks what changed, and
// keeps the note the person is typing; no new message appears.
test('a progress card updated in place: 1/3 to 3/3, live, keeping what the person typed', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await finishOngoingTasks(page, workspace.id)
  const task = await createTask(page, workspace.id, unique('a task with a live progress card'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)

  expect(await reply(call, { taskId: task.id, text: 'Releasing 0.9.4: 1/3 steps done.', ui: progressCard() }), 'the card').toEqual({ text: 'reply sent', refused: false })

  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const surface = page.locator(`[data-test="a2ui-surface"][data-surface-id="${SURFACE}"]`)
  const count = surface.locator('[data-test="a2ui-text"]').filter({ hasText: 'steps done' })
  await expect(count, 'the count').toHaveText('1/3 steps done')
  await expect(surface.getByText('Test · running'), 'the running step').toBeVisible()
  const messages = async () => (await (await getTask(page, workspace.id, task.id)).json()).task.messages
  const before = (await messages()).length

  // Marked, to tell the same element from one drawn again.
  await count.evaluate((el) => { el.dataset.qaMark = 'kept' })
  const note = surface.getByLabel('Note for the release')
  await note.fill('Ping #releases once it is out')

  expect(await reply(call, { taskId: task.id, text: 'Releasing 0.9.4: 2/3 steps done.', ui: progress(2) }), 'the first update').toMatchObject({ text: expect.stringMatching(/^card "qa-progress" updated/), refused: false })
  await expect(count, 'the count, updated live').toHaveText('2/3 steps done')
  await expect(count, 'the count, marked as changed').toHaveAttribute('data-changed', 'true')
  await expect(surface.getByText('Deploy · running'), 'the next step').toBeVisible()
  await expect(note, 'what the person typed').toHaveValue('Ping #releases once it is out')
  await expect(surface.locator('[data-test="a2ui-textfield"]'), 'the note, which the agent left alone').not.toHaveAttribute('data-changed', 'true')
  await surface.evaluate((el) => el.scrollIntoView({ block: 'start' }))
  await page.screenshot({ path: test.info().outputPath('a2ui-live-progress-changed.png') })

  expect(await reply(call, { taskId: task.id, text: 'Released 0.9.4: 3/3 steps done.', ui: progress(3) }), 'the last update').toMatchObject({ text: expect.stringMatching(/^card "qa-progress" updated/), refused: false })
  await expect(count, 'the count, finished').toHaveText('3/3 steps done')
  await expect(surface.getByText('Deploy · done'), 'the last step').toBeVisible()
  await expect(count, 'the same element, never drawn again').toHaveAttribute('data-qa-mark', 'kept')
  await expect(note, 'what the person typed, still').toHaveValue('Ping #releases once it is out')
  await expect(count, 'the mark, gone after a moment').not.toHaveAttribute('data-changed', 'true')
  await shots(page, surface, 'a2ui-live-progress')

  const after = await messages()
  expect(after.length, 'no new message: the card changed in place').toBe(before)
  expect(after.find((m) => m.metadata?.surfaceId === SURFACE)?.text, "the card's text, replaced").toBe('Released 0.9.4: 3/3 steps done.')

  // A reload draws it finished from what the server stored.
  await page.reload()
  await expect(count, 'the count, after a reload').toHaveText('3/3 steps done')
  traffic.expectClean({ allow: ['404 /plan'] })
  await setTaskStatus(page, workspace.id, task.id, 'completed')
})
