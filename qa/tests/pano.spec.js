// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { crc32, deflateSync } from 'node:zlib'
import { test, expect } from '../lib/fixtures.js'
import { agentSession, nextResponse, unique, watchTraffic } from '../lib/app.js'

// A solid PNG, so a linked image has something to show.
function png(width, height, [r, g, b]) {
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
  const row = Buffer.concat([Buffer.from([0]), Buffer.from(Array.from({ length: width }, () => [r, g, b]).flat())])
  const pixels = Buffer.concat(Array.from({ length: height }, () => row))
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', header),
    chunk('IDAT', deflateSync(pixels)),
    chunk('IEND', Buffer.alloc(0)),
  ]).toString('base64')
}

// Calls the pano tool as an agent, and reads its answer: the doc as JSON, or
// the refusal's text.
async function pano(call, args) {
  const text = await call('pano', args)
  const data = text.split('\n').find((line) => line.startsWith('data: '))
  const { result } = JSON.parse(data.slice('data: '.length))
  const body = result.content[0].text
  return result.isError ? { refused: body } : JSON.parse(body)
}

// An agent posts a doc with a linked image; one with a file it does not link
// is refused. Another write comments and attaches. The board shows each as a
// post-it, filters by title, and the doc page shows the image, the
// attachments and the comments, and takes a comment from the person.
test('agents share docs on the Pano, and the person reads and comments on them', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const call = await agentSession(page, workspace.id)

  const refused = await pano(call, {
    action: 'save', title: 'Not linked', markdown: 'No link here',
    files: [{ filename: 'chart.png', mimeType: 'image/png', data: png(4, 4, [0, 0, 0]) }],
  })
  expect(refused.refused, 'a file the markdown does not link').toContain('chart.png is not linked from the markdown')

  const title = unique('Release plan')
  const plan = await pano(call, {
    action: 'save', title, color: 'yellow', ttlDays: 14,
    markdown: `Ship the Pano on **Friday**. Every workspace posts here.\n\n- [x] backend\n- [ ] frontend\n- [ ] docs\n\n![chart](chart.png)`,
    files: [{ filename: 'chart.png', mimeType: 'image/png', data: png(240, 80, [79, 70, 229]) }],
  })
  expect(plan.createdBy, 'the agent\'s workspace is the author').toBe(workspace.id)
  expect(plan.ttlDays).toBe(14)
  expect(plan.files.map((f) => f.filename)).toEqual(['chart.png'])
  expect([plan.checklistDone, plan.checklistTotal], 'the checklist, counted at save').toEqual([1, 3])

  const notes = await pano(call, { action: 'save', title: unique('API notes'), color: 'blue', markdown: '`POST /api/v1/pano` creates a doc.\n\n> Every file must be linked from the markdown.' })
  await pano(call, { action: 'save', title: unique('Bug triage'), color: 'pink', markdown: 'Three open bugs:\n1. a\n2. b\n3. c' })
  await pano(call, { action: 'save', title: unique('Ideas'), color: 'green', markdown: 'Tags on docs, maybe. Pinned docs.' })
  await pano(call, { action: 'comment', id: plan.id, comment: 'Backend tests pass; frontend next.' })
  await pano(call, { action: 'attach', id: plan.id, files: [{ filename: 'notes.txt', mimeType: 'text/plain', data: Buffer.from('release notes').toString('base64') }] })
  const read = await pano(call, { action: 'read', id: plan.id, includeComments: true, includeAttachments: true })
  expect(read.comments.map((c) => c.body)).toEqual(['Backend tests pass; frontend next.'])
  expect(read.attachments.map((a) => a.filename)).toEqual(['notes.txt'])

  // The board, from the sidebar's link, above Skills.
  await page.goto(`/workspaces/${workspace.id}`)
  const listed = nextResponse(page, 'GET', '/api/v1/pano')
  await page.locator('nav a[href="/pano"]').first().click()
  expect((await listed).status(), 'the Pano list request').toBe(200)
  await expect(page.getByRole('heading', { level: 1, name: 'Pano' })).toBeVisible()
  const planCard = page.getByTestId('pano-card').filter({ hasText: title })
  await expect(planCard).toBeVisible()
  await expect(planCard.getByTestId('pano-progress')).toContainText('1 of 3 done')

  // A box ticked on the card is saved, and the progress follows.
  const ticked = nextResponse(page, 'PUT', `/api/v1/pano/${plan.id}`)
  await planCard.getByTestId('pano-excerpt').locator('input[type="checkbox"]').nth(1).click()
  expect((await ticked).status(), 'ticking a box').toBe(200)
  await expect(planCard.getByTestId('pano-progress')).toContainText('2 of 3 done')
  expect((await pano(call, { action: 'read', id: plan.id })).markdown, 'the tick, in the markdown').toContain('- [x] frontend')
  await expect(page).toHaveURL(/\/pano$/)
  await page.screenshot({ path: test.info().outputPath('pano-board-light.png'), animations: 'disabled' })

  // A doc an agent posts while the board is open appears without a reload.
  const late = unique('Posted while you watched')
  await pano(call, { action: 'save', title: late, color: 'orange', markdown: 'Live update check.' })
  await expect(page.getByTestId('pano-card').filter({ hasText: late })).toBeVisible()

  await page.getByTestId('pano-search').fill('API notes')
  await expect(page.getByTestId('pano-card')).toHaveCount(1)
  await page.getByTestId('pano-search').fill('')

  await page.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
  await page.screenshot({ path: test.info().outputPath('pano-board-dark.png'), animations: 'disabled' })
  await page.evaluate(() => { localStorage.setItem('theme', 'light'); document.documentElement.classList.remove('dark') })

  // The doc, with its linked image drawn from its public link.
  await planCard.getByRole('heading').click()
  await expect(page).toHaveURL(new RegExp(`/pano/${plan.id}$`))
  const image = page.getByTestId('pano-body').locator('img')
  await expect(image).toHaveAttribute('src', plan.files[0].url)
  await expect.poll(() => image.evaluate((img) => img.naturalWidth), { message: 'the linked image loads' }).toBe(240)
  await expect(page.getByTestId('pano-attachments')).toContainText('notes.txt')

  await page.getByTestId('pano-comment-input').fill('Looks good to me.')
  const commented = nextResponse(page, 'POST', `/api/v1/pano/${plan.id}/comments`)
  await page.getByTestId('pano-comment').click()
  expect((await commented).status(), 'the person\'s comment').toBe(201)
  await expect(page.getByTestId('pano-comments')).toContainText('You')
  await expect(page.getByTestId('pano-comments')).toContainText('Looks good to me.')
  await page.screenshot({ path: test.info().outputPath('pano-doc-light.png'), fullPage: true, animations: 'disabled' })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await page.screenshot({ path: test.info().outputPath('pano-doc-dark.png'), fullPage: true, animations: 'disabled' })
  await page.evaluate(() => document.documentElement.classList.remove('dark'))

  // The person edits a doc: its new color and markdown are saved as theirs.
  await page.goto(`/pano/${notes.id}`)
  await page.getByTestId('pano-edit').click()
  await page.getByTestId('pano-color-purple').click()
  await page.getByTestId('pano-markdown-input').fill('Rewritten by the person.\n\nship it')
  // A formatting button starts the selected line as a checklist item.
  await page.getByTestId('pano-markdown-input').evaluate((el) => el.setSelectionRange(el.value.length - 2, el.value.length))
  await page.getByTestId('pano-format-checkbox').click()
  await expect(page.getByTestId('pano-markdown-input')).toHaveValue('Rewritten by the person.\n\n- [ ] ship it')
  await page.screenshot({ path: test.info().outputPath('pano-editor-light.png'), animations: 'disabled' })
  const saved = nextResponse(page, 'PUT', `/api/v1/pano/${notes.id}`)
  await page.getByTestId('pano-save').click()
  expect((await saved).status(), 'saving the edit').toBe(200)
  await expect(page.getByTestId('pano-body')).toContainText('Rewritten by the person.')
  const again = await pano(call, { action: 'read', id: notes.id })
  expect(again.updatedBy, 'the person is the last writer').toBe('human')

  // And deletes it; agents can no longer read it.
  await page.getByTestId('pano-delete').click()
  const deleted = nextResponse(page, 'DELETE', `/api/v1/pano/${notes.id}`)
  await page.getByRole('button', { name: /delete/i }).last().click()
  expect((await deleted).status()).toBe(204)
  await expect(page).toHaveURL(/\/pano$/)
  expect((await pano(call, { action: 'read', id: notes.id })).refused).toBe('Pano doc not found')

  // The person posts a doc of their own. It leads the board, ahead of the
  // agents' newer ones, and becomes a task in a workspace from its card.
  const mine = unique('My checklist')
  await page.goto('/pano')
  await page.getByTestId('pano-new').click()
  await page.getByTestId('pano-title-input').fill(mine)
  await page.getByTestId('pano-color-green').click()
  await page.getByTestId('pano-markdown-input').fill('- [x] write the docs\n- [ ] record a demo\n- [ ] post the video')
  const posted = nextResponse(page, 'POST', '/api/v1/pano')
  await page.getByTestId('pano-save').click()
  expect((await posted).status(), 'posting the doc').toBe(201)
  await expect(page).toHaveURL(/\/pano\/[0-9A-Za-z]+$/)
  await pano(call, { action: 'save', title: unique('Newer agent doc'), markdown: 'Posted after the person\'s.' })
  await page.goto('/pano')
  await expect(page.getByTestId('pano-card').first(), 'the person\'s doc comes first').toContainText(mine)
  await expect(page.getByTestId('pano-card').nth(1).getByTestId('pano-to-task'), 'an agent\'s doc has no task button').toHaveCount(0)
  await page.getByTestId('pano-card').first().getByTestId('pano-to-task').click()
  await page.screenshot({ path: test.info().outputPath('pano-to-task-light.png'), animations: 'disabled' })
  const tasked = nextResponse(page, 'POST', `/api/v1/workspaces/${workspace.id}/tasks`)
  await page.getByTestId('pano-to-task-workspace').filter({ hasText: workspace.name }).click()
  const taskRes = await tasked
  expect(taskRes.status(), 'the task made from the doc').toBe(201)
  expect(taskRes.request().postDataJSON().task.title).toBe(mine)
  await expect(page, 'the click made a task, not a visit to the doc').toHaveURL(/\/pano$/)

  // The board on a phone.
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/pano')
  await expect(page.getByTestId('pano-card').first()).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('pano-board-phone.png'), animations: 'disabled' })

  traffic.expectClean()
})
