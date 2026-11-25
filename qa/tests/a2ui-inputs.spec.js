// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, finishOngoingTasks, getTask, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

const CATALOG = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'

// An MCP tool's answer: the result's text, and whether it was refused.
function result(text) {
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  const { result } = JSON.parse(data.slice('data: '.length))
  return { text: result.content[0].text, refused: !!result.isError }
}

// The card from its top, in light, dark and at phone width.
async function shots(page, surface, name) {
  const top = () => surface.evaluate((el) => el.scrollIntoView({ block: 'start' }))
  await top()
  await page.screenshot({ path: test.info().outputPath(`${name}-light.png`), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
  await page.screenshot({ path: test.info().outputPath(`${name}-dark.png`), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'light'); document.documentElement.classList.remove('dark') })
  const size = page.viewportSize()
  await page.setViewportSize({ width: 390, height: 844 })
  await top()
  await page.screenshot({ path: test.info().outputPath(`${name}-phone.png`), animations: 'disabled' })
  await page.setViewportSize(size)
}

// A reply that asks the person to pick: chips bound to the data model, and a
// Button its check holds back until something is picked.
function choiceCard(surfaceId) {
  return [
    { version: '1.0', createSurface: { surfaceId, catalogId: CATALOG } },
    { version: '1.0', updateComponents: { surfaceId, components: [
      { id: 'root', component: 'Card', child: 'col' },
      { id: 'col', component: 'Column', children: ['title', 'envs', 'actions'] },
      { id: 'title', component: 'Text', text: '## Where should PR #1400 go?' },
      { id: 'envs', component: 'ChoicePicker', label: 'Environment', displayStyle: 'chips', value: { '@path': '/env' }, options: [
        { label: 'Staging', value: 'staging' }, { label: 'Production', value: 'production' }, { label: 'Preview', value: 'preview' },
      ] },
      { id: 'actions', component: 'Row', children: ['deploy', 'hold'] },
      { id: 'deploy', component: 'Button', variant: 'primary', child: 'deployText',
        '@checks': [{ condition: { '@call': 'required', args: { value: { '@path': '/env' } } }, message: 'Pick an environment first' }],
        action: { event: { name: 'deploy', context: { env: { '@path': '/env/0' } }, userMessage: { '@call': 'formatString', args: { value: 'Deploy to ${/env/0}' } } } } },
      { id: 'deployText', component: 'Text', text: 'Deploy' },
      { id: 'hold', component: 'Button', child: 'holdText', action: { event: { name: 'hold' } } },
      { id: 'holdText', component: 'Text', text: 'Hold' },
    ] } },
    { version: '1.0', updateDataModel: { surfaceId, path: '/', value: { env: [] } } },
  ]
}

// An agent replies with a choice card; the person picks and presses Deploy:
// the press is posted as their reply and the card turns read-only, its
// chosen Button marked.
test('a choice card in a reply: picking and pressing a Button answers it once', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await finishOngoingTasks(page, workspace.id)
  const task = await createTask(page, workspace.id, unique('a task answered with a choice card'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)

  const sent = result(await call('task', { action: 'reply', payload: {
    taskId: task.id, text: 'PR #1400 is ready. Deploy to staging, production or preview, or hold?', ui: choiceCard('qa-choice'),
  } }))
  expect(sent, "the agent's reply").toEqual({ text: 'reply sent', refused: false })

  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const surface = page.locator('[data-test="a2ui-surface"][data-surface-id="qa-choice"]')
  await expect(surface, 'the card').toBeVisible()
  const deploy = surface.getByRole('button', { name: 'Deploy' })
  await expect(deploy, 'Deploy, held back by its check').toBeDisabled()
  await expect(surface.getByText('Pick an environment first'), "the check's message").toBeVisible()

  await surface.getByRole('radio', { name: 'Staging' }).click()
  await expect(deploy, 'Deploy, once something is picked').toBeEnabled()
  await expect(surface.getByText('Pick an environment first'), "the check's message, gone").toHaveCount(0)
  await shots(page, surface, 'a2ui-choice')

  await deploy.click()
  await expect(surface, 'the card, answered').toHaveAttribute('data-answered', 'true')
  await expect(deploy, 'the chosen Button').toHaveAttribute('data-chosen', 'true')
  await expect(surface.getByRole('button', { name: 'Hold' }), 'the other Button').toBeDisabled()
  await expect(page.getByText('Deploy to staging', { exact: true }), 'the press, posted as the reply').toBeVisible()

  const messages = (await (await getTask(page, workspace.id, task.id)).json()).task.messages
  const answer = messages.find((m) => m.text === 'Deploy to staging')
  expect(answer.sender, 'who sent it').toBe('human')
  expect(answer.metadata.a2uiAction, 'the action it carries').toMatchObject({ surfaceId: 'qa-choice', name: 'deploy', context: { env: 'staging' } })
  await surface.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-choice-answered.png'), animations: 'disabled' })

  // A reload draws it answered from what the server stored.
  await page.reload()
  await expect(surface, 'the card, answered after a reload').toHaveAttribute('data-answered', 'true')
  await expect(surface.getByRole('button', { name: 'Deploy' }), 'the chosen Button, after a reload').toHaveAttribute('data-chosen', 'true')
  traffic.expectClean({ allow: ['404 /plan'] })
  await setTaskStatus(page, workspace.id, task.id, 'completed')
})

// A question asked as a form card: every input writes the card's data model,
// and the Button's press answers the waiting elicit with all of it.
function formCard(surfaceId) {
  return [
    { version: '1.0', createSurface: { surfaceId, catalogId: CATALOG } },
    { version: '1.0', updateComponents: { surfaceId, components: [
      { id: 'root', component: 'Card', child: 'col' },
      { id: 'col', component: 'Column', children: ['title', 'branch', 'checks', 'reviewers', 'effort', 'when', 'notes', 'agree', 'actions'] },
      { id: 'title', component: 'Text', text: '## Release settings' },
      { id: 'branch', component: 'TextField', label: 'Branch', placeholder: 'release/x.y', value: { '@path': '/branch' },
        '@checks': [{ condition: { '@call': 'regex', args: { value: { '@path': '/branch' }, pattern: '^release/' } }, message: 'Starts with release/' }] },
      { id: 'checks', component: 'ChoicePicker', label: 'Checks to run', variant: 'multipleSelection', value: { '@path': '/checks' }, options: [
        { label: 'Unit tests', value: 'unit' }, { label: 'Lint', value: 'lint' }, { label: 'End to end', value: 'e2e' },
      ] },
      { id: 'reviewers', component: 'ChoicePicker', label: 'Reviewer', filterable: true, value: { '@path': '/reviewer' }, options: [
        { label: 'Ada', value: 'ada' }, { label: 'Grace', value: 'grace' }, { label: 'Linus', value: 'linus' },
      ] },
      { id: 'effort', component: 'Slider', label: 'Canary share (%)', min: 0, max: 100, steps: 10, value: { '@path': '/canary' } },
      { id: 'when', component: 'DateTimeInput', label: 'Release day', enableDate: true, value: { '@path': '/day' } },
      { id: 'notes', component: 'TextField', label: 'Notes', variant: 'longText', value: { '@path': '/notes' } },
      { id: 'agree', component: 'CheckBox', label: 'Notify the channel', value: { '@path': '/notify' } },
      { id: 'actions', component: 'Row', children: ['go', 'stop'] },
      { id: 'go', component: 'Button', variant: 'primary', child: 'goText',
        '@checks': [{ condition: { '@call': 'regex', args: { value: { '@path': '/branch' }, pattern: '^release/' } }, message: 'Name a release branch' }],
        action: { event: { name: 'release', context: { branch: { '@path': '/branch' } } } } },
      { id: 'goText', component: 'Text', text: 'Release' },
      { id: 'stop', component: 'Button', variant: 'borderless', child: 'stopText', action: { event: { name: 'cancel' } } },
      { id: 'stopText', component: 'Text', text: 'Not now' },
    ] } },
    { version: '1.0', updateDataModel: { surfaceId, path: '/', value: { branch: '', checks: ['unit'], reviewer: [], canary: 10, day: '', notes: '', notify: false } } },
  ]
}

test('a question asked as a form card: what is entered answers the waiting agent', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a task with a card question'))
  const call = await agentSession(page, workspace.id)
  const asked = call('elicit', { taskId: task.id, message: 'Release settings: branch, checks, reviewer, canary share, day, notes.', mode: 'a2ui', ui: formCard('qa-form') })
  await expect.poll(async () => (await (await getTask(page, workspace.id, task.id)).json()).task.messages?.length ?? 0, 'the question, posted').toBeGreaterThan(0)

  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const surface = page.locator('[data-test="a2ui-surface"][data-surface-id="qa-form"]')
  await expect(surface, 'the question, as a card').toBeVisible()
  await expect(page.getByRole('button', { name: "I'm Done" }), "a link question's controls").toHaveCount(0)
  const release = surface.getByRole('button', { name: 'Release' })
  await expect(release, 'Release, held back').toBeDisabled()

  await surface.getByLabel('Branch').fill('main')
  await surface.getByLabel('Branch').blur()
  await expect(surface.getByText('Starts with release/'), "the field's check, once touched").toBeVisible()
  await surface.getByLabel('Branch').fill('release/1.4')
  await expect(release, 'Release, once the branch passes').toBeEnabled()
  await surface.getByRole('checkbox', { name: 'Lint' }).check()
  await surface.getByRole('searchbox', { name: 'Filter Reviewer' }).fill('gr')
  await expect(surface.getByRole('radio'), 'the reviewers left by the filter').toHaveCount(1)
  await surface.getByRole('radio', { name: 'Grace' }).check()
  await surface.getByLabel('Canary share (%)').fill('30')
  await surface.getByLabel('Release day').fill('2026-12-01')
  await surface.getByLabel('Notes').fill('Ship after the freeze.')
  await surface.getByLabel('Notify the channel').check()
  await shots(page, surface, 'a2ui-form')

  await release.click()
  const told = result(await asked)
  expect(told.refused, 'the elicit').toBe(false)
  const answer = JSON.parse(told.text.slice(told.text.indexOf('{')))
  expect(answer, 'what the agent was told').toMatchObject({
    action: 'accept',
    content: {
      name: 'release',
      context: { branch: 'release/1.4' },
      dataModel: { branch: 'release/1.4', checks: ['unit', 'lint'], reviewer: ['grace'], canary: 30, day: '2026-12-01', notes: 'Ship after the freeze.', notify: true },
    },
  })

  await expect(page.getByText('Answered', { exact: true }), 'the question, answered').toBeVisible()
  await expect(release, 'the chosen Button').toHaveAttribute('data-chosen', 'true')
  await expect(surface.getByLabel('Branch'), 'the inputs, read-only').toBeDisabled()
  await page.reload()
  await expect(surface.getByLabel('Branch'), 'what was sent, after a reload').toHaveValue('release/1.4')
  await expect(surface.getByLabel('Canary share (%)'), 'the slider, after a reload').toHaveValue('30')
  await surface.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-form-answered.png'), animations: 'disabled' })
  traffic.expectClean({ allow: ['404 /plan'] })
})
