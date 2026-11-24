// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { crc32, deflateSync } from 'node:zlib'
import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, getTask, unique, watchTraffic } from '../lib/app.js'

const CATALOG = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'

// A small bar chart as a PNG, so the card's image has something to show.
function chart(width, height, bars) {
  const chunk = (type, data) => {
    const body = Buffer.concat([Buffer.from(type), data])
    const len = Buffer.alloc(4)
    len.writeUInt32BE(data.length)
    const crc = Buffer.alloc(4)
    crc.writeUInt32BE(crc32(body))
    return Buffer.concat([len, body, crc])
  }
  const header = Buffer.alloc(13)
  header.writeUInt32BE(width, 0)
  header.writeUInt32BE(height, 4)
  header.set([8, 2, 0, 0, 0], 8)
  const slot = width / bars.length
  const rows = Array.from({ length: height }, (_, y) => Buffer.from([0, ...Array.from({ length: width }, (_, x) => {
    const i = Math.floor(x / slot)
    const inBar = x - i * slot > slot * 0.2 && x - i * slot < slot * 0.8 && height - y <= bars[i] * height
    return inBar ? [79, 70, 229] : [238, 242, 255]
  }).flat()]))
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(Buffer.concat(rows))),
    chunk('IEND', Buffer.alloc(0)),
  ]).toString('base64')
}

// Calls task reply as an agent, and reads its answer: the result's text, and
// whether it was refused.
async function reply(call, payload) {
  const text = await call('task', { action: 'reply', payload })
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  const { result } = JSON.parse(data.slice('data: '.length))
  return { text: result.content[0].text, refused: !!result.isError }
}

async function messageCount(page, workspaceId, taskId) {
  return (await (await getTask(page, workspaceId, taskId)).json()).task.messages?.length ?? 0
}

// The three patterns the agent skill teaches, in one card: a result with a
// chart, a status summary, and a comparison list drawn from the data model.
function buildCard(surfaceId) {
  return [
    { version: '1.0', createSurface: { surfaceId, catalogId: CATALOG } },
    { version: '1.0', updateComponents: { surfaceId, components: [
      { id: 'root', component: 'Card', child: 'col' },
      { id: 'col', component: 'Column', children: ['title', 'summary', 'chart', 'divider', 'checks', 'packages'] },
      { id: 'title', component: 'Text', text: '## Build passed' },
      { id: 'summary', component: 'Text', text: { '@path': '/summary' } },
      { id: 'chart', component: 'Image', url: 'attachment:chart.png', description: 'Test time per package' },
      { id: 'divider', component: 'Divider' },
      { id: 'checks', component: 'Column', children: ['unit', 'lint'] },
      { id: 'unit', component: 'Row', align: 'center', children: ['unitIcon', 'unitText'] },
      { id: 'unitIcon', component: 'Icon', name: 'check' },
      { id: 'unitText', component: 'Text', text: 'Unit tests: 412 passed' },
      { id: 'lint', component: 'Row', align: 'center', children: ['lintIcon', 'lintText'] },
      { id: 'lintIcon', component: 'Icon', name: 'warning' },
      { id: 'lintText', component: 'Text', text: 'Lint: 2 warnings' },
      { id: 'packages', component: 'List', children: { path: '/packages', componentId: 'package' } },
      { id: 'package', component: 'Row', justify: 'spaceBetween', children: ['packageName', 'packageTime'] },
      { id: 'packageName', component: 'Text', text: { '@path': 'name' } },
      { id: 'packageTime', component: 'Text', text: { '@path': 'time' }, variant: 'caption' },
    ] } },
    { version: '1.0', updateDataModel: { surfaceId, path: '/', value: {
      summary: 'All **412** tests pass on `dev`.',
      packages: [{ name: 'controller/mcp', time: '3.2 s' }, { name: 'service/a2ui', time: '1.1 s' }, { name: 'handler/api', time: '0.8 s' }],
    } } },
  ]
}

// An agent replies over MCP with a card and the chart it shows: the thread
// draws the card, with the chart loaded from the reply's stored file.
test('an agent\'s reply with an A2UI card and an attached image is drawn as a card', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a task answered with a card'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)

  const sent = await reply(call, {
    taskId: task.id,
    text: 'Build passed: 412 tests, 2 lint warnings.',
    attachments: [{ filename: 'chart.png', mimeType: 'image/png', data: chart(240, 96, [0.9, 0.35, 0.25]) }],
    ui: buildCard('qa-build'),
  })
  expect(sent, 'the agent\'s reply').toEqual({ text: 'reply sent', refused: false })

  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const surface = page.locator('[data-test="a2ui-surface"][data-surface-id="qa-build"]')
  await expect(surface, 'the card').toBeVisible()
  await expect(surface.getByRole('heading', { name: 'Build passed' }), 'its title').toBeVisible()
  await expect(surface.getByText('All 412 tests pass on dev.'), 'its bound summary').toBeVisible()
  await expect(surface.getByText('Unit tests: 412 passed'), 'its status rows').toBeVisible()
  await expect(surface.locator('[data-test="a2ui-icon"]'), 'its status icons').toHaveCount(2)
  await expect(surface.getByRole('listitem'), 'one row per package in its data').toHaveCount(3)
  await expect(surface.getByRole('listitem').nth(1), 'a templated row').toContainText('service/a2ui')

  const image = surface.locator('[data-test="a2ui-image"]')
  await expect(image, 'the attached chart').toBeVisible()
  const src = await image.getAttribute('src')
  expect(src, 'the chart\'s link: our stored file, never the agent\'s reference').not.toContain('attachment:')
  expect(src, 'the chart\'s link').toMatch(/\/storage\/|\/api\/v1\/workspaces\//)
  await expect.poll(() => image.evaluate((img) => img.complete && img.naturalWidth), 'the chart, loaded').toBe(240)
  await expect(page.locator('[data-test="a2ui-media-refused"]'), 'media the card refused').toHaveCount(0)

  await surface.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-card-light.png'), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
  await page.screenshot({ path: test.info().outputPath('a2ui-card-dark.png'), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'light'); document.documentElement.classList.remove('dark') })
  await page.setViewportSize({ width: 390, height: 844 })
  await surface.scrollIntoViewIfNeeded()
  await page.screenshot({ path: test.info().outputPath('a2ui-card-phone.png'), animations: 'disabled' })
  traffic.expectClean({ allow: ['404 /plan'] })
})

// A card the validator refuses posts nothing, and the agent is told why in
// the validator's own words, so it can fix the card and send it again.
test('an invalid A2UI card is refused with the validator\'s message and posts nothing', async ({ page, workspace }) => {
  const task = await createTask(page, workspace.id, unique('a task with a refused card'), 'ongoing', { assignee: 'human' })
  const call = await agentSession(page, workspace.id)
  const before = await messageCount(page, workspace.id, task.id)

  const card = buildCard('qa-refused')
  const components = card[1].updateComponents.components
  components.find((c) => c.id === 'chart').url = 'https://example.com/chart.png'
  components.find((c) => c.id === 'title').component = 'Headline'

  const sent = await reply(call, {
    taskId: task.id,
    text: 'Build passed.',
    attachments: [{ filename: 'chart.png', mimeType: 'image/png', data: chart(8, 8, [1]) }],
    ui: card,
  })
  expect(sent.refused, `the reply, refused: ${sent.text}`).toBe(true)
  expect(sent.text, 'the validator\'s message').toMatch(/^ui is not valid A2UI v1\.0: /)
  expect(sent.text, 'the linked image it names').toContain('"chart": url must be attachment:<filename> of a file attached to this reply: media cannot come from a link')
  expect(sent.text, 'the unknown component it names').toContain('Headline')
  expect(await messageCount(page, workspace.id, task.id), 'messages in the task').toBe(before)

  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  await expect(page.getByText(task.title).first(), 'the task').toBeVisible()
  await expect(page.locator('[data-test="a2ui-surface"]'), 'a card in the thread').toHaveCount(0)
  await expect(page.getByText('Build passed.'), 'the refused reply\'s text').toHaveCount(0)
})
