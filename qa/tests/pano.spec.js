// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { crc32, deflateSync } from 'node:zlib'
import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, nextResponse, setTaskStatus, unique, watchTraffic } from '../lib/app.js'

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

// The person assigns a checklist item to a workspace from its menu: a task is
// made there, and the item follows the task's status as it moves, ticked once
// it is completed. An @mention in a comment pings a workspace's agent, or
// one of its tasks.
test('a checklist item assigned to a workspace follows its task, and a mention pings one', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const call = await agentSession(page, workspace.id)
  const doc = await pano(call, { action: 'save', title: unique('Launch'), color: 'purple', markdown: 'Who does what:\n\n- [ ] backend\n- [ ] frontend\n- [ ] docs' })

  await page.goto(`/pano/${doc.id}`)
  const item = page.getByTestId('pano-body').locator('li').first()
  await item.hover()
  await item.locator('[data-pano-assign]').click()
  await expect(page.getByTestId('pano-item-menu')).toContainText('Assign to')
  await page.screenshot({ path: test.info().outputPath('pano-assign-light.png'), animations: 'disabled' })
  const tasked = nextResponse(page, 'POST', `/api/v1/workspaces/${workspace.id}/tasks`)
  const saved = nextResponse(page, 'PUT', `/api/v1/pano/${doc.id}`)
  await page.getByTestId('pano-item-assign').filter({ hasText: workspace.name }).click()
  const task = (await (await tasked).json()).task
  expect(task.title, 'the task is the item').toBe('backend')
  expect(task.body, 'the task names the doc').toContain(doc.id)
  expect((await saved).status(), 'the item linked to its task').toBe(200)

  const chip = item.locator('a.pano-assignee')
  await expect(chip).toContainText(`@${workspace.name}Not started`)
  await expect(chip).toHaveAttribute('data-status', 'notstarted')

  // The status moves wherever it is changed, and the open note follows.
  for (const status of ['ongoing', 'blocked']) {
    await setTaskStatus(page, workspace.id, task.id, status)
    await expect(chip, `the item shows the task ${status}`).toHaveAttribute('data-status', status)
  }
  await expect(item.locator('input[type="checkbox"]')).not.toBeChecked()
  await expect(item.locator('input[type="checkbox"]'), 'an assigned item\'s box follows its task alone').toBeDisabled()
  await setTaskStatus(page, workspace.id, task.id, 'completed')
  await expect(chip).toHaveAttribute('data-status', 'completed')
  await expect(item.locator('input[type="checkbox"]'), 'a completed task ticks its item').toBeChecked()
  await expect(page.getByTestId('pano-progress')).toContainText('1 of 3 done')
  const read = await pano(call, { action: 'read', id: doc.id })
  expect(read.markdown, 'agents read the assignee and its status').toContain(`- [x] backend → [@${workspace.name}](/workspaces/${workspace.id}/tasks/${task.id} "completed")`)

  // Completed before its work landed: unticked by hand, the item stays so,
  // and says so, until the task's status changes again.
  const box = item.locator('input[type="checkbox"]')
  const paper = page.locator('article').filter({ has: page.getByTestId('pano-body') })
  await paper.screenshot({ path: test.info().outputPath('pano-completed-light.png'), animations: 'disabled' })
  const unticked = nextResponse(page, 'PUT', `/api/v1/pano/${doc.id}`)
  await box.click()
  expect((await unticked).status(), 'the untick is saved').toBe(200)
  await expect(chip).toHaveAttribute('data-unticked', '')
  await expect(chip).toContainText('Completed · not ticked')
  await expect(box).not.toBeChecked()
  await expect(page.getByTestId('pano-progress')).toContainText('0 of 3 done')
  await paper.screenshot({ path: test.info().outputPath('pano-not-ticked-light.png'), animations: 'disabled' })
  const held = await pano(call, { action: 'read', id: doc.id })
  expect(held.markdown, 'agents read the box and the task\'s status').toContain(`- [ ] backend → [@${workspace.name}](/workspaces/${workspace.id}/tasks/${task.id} "completed")`)
  await setTaskStatus(page, workspace.id, task.id, 'ongoing')
  await expect(box, 'reopened, it follows its task again').toBeDisabled()
  await setTaskStatus(page, workspace.id, task.id, 'completed')
  await expect(box, 'completed again, it ticks').toBeChecked()
  await expect(chip).not.toHaveAttribute('data-unticked', '')

  // The menu of an assigned item names its task.
  await item.hover()
  await item.locator('[data-pano-assign]').click()
  await expect(page.getByTestId('pano-item-task')).toContainText(task.id)
  // Ticked by hand, it keeps its box whatever its task does.
  await paper.screenshot({ path: test.info().outputPath('pano-tick-mode-light.png'), animations: 'disabled' })
  const byHand = nextResponse(page, 'PUT', `/api/v1/pano/${doc.id}`)
  await page.getByTestId('pano-item-tick-mode').click()
  expect((await byHand).status()).toBe(200)
  await expect(chip).toHaveAttribute('data-manual', '')
  await setTaskStatus(page, workspace.id, task.id, 'ongoing')
  await expect(chip).toHaveAttribute('data-status', 'ongoing')
  await expect(box, 'a manual item never unticks itself').toBeChecked()
  const manual = await pano(call, { action: 'read', id: doc.id })
  expect(manual.markdown).toContain(`- [x] backend → [@${workspace.name}](/workspaces/${workspace.id}/tasks/${task.id} "ongoing manual")`)
  await setTaskStatus(page, workspace.id, task.id, 'completed')
  await expect(chip).toHaveAttribute('data-status', 'completed')
  await page.getByTestId('pano-title').click()

  // A mention, picked from the suggestions, pings the workspace's agent.
  const input = page.getByTestId('pano-comment-input')
  await input.click()
  await input.pressSequentially(`Can you take the docs, @${workspace.name.slice(0, 4)}`)
  await expect(page.getByTestId('pano-mention-option').first()).toHaveText(`@${workspace.name}`)
  await page.screenshot({ path: test.info().outputPath('pano-mention-light.png'), animations: 'disabled' })
  await input.press('Enter')
  await expect(input).toHaveValue(`Can you take the docs, @${workspace.name} `)
  // The server makes one task a second per account, and the item's task was
  // made a moment ago.
  await page.waitForTimeout(1100)
  const commented = nextResponse(page, 'POST', `/api/v1/pano/${doc.id}/comments`)
  const pinged = nextResponse(page, 'POST', `/api/v1/workspaces/${workspace.id}/tasks`)
  await input.press('ControlOrMeta+Enter')
  expect((await commented).status()).toBe(201)
  const pingRes = await pinged
  expect(pingRes.status(), await pingRes.text()).toBe(201)
  const ping = (await pingRes.json()).task
  expect(ping.title).toBe(`Pano: ${doc.title}`)
  expect(ping.body).toContain(`> Can you take the docs, @${workspace.name}`)
  await expect(page.getByTestId('pano-comments').locator('.pano-mention')).toHaveText(`@${workspace.name}`)

  // A mention naming one of the workspace's open tasks, picked after a #,
  // posts on that task instead of making another.
  await input.pressSequentially(`More on the docs, @${workspace.name} #`)
  // The workspace is shared by the run, so earlier tests' open tasks are offered too.
  await expect(page.getByTestId('pano-mention-option').first()).toHaveText(`#${ping.id} ${ping.title}`)
  await page.screenshot({ path: test.info().outputPath('pano-task-mention-light.png'), animations: 'disabled' })
  await input.press('Enter')
  await expect(input).toHaveValue(`More on the docs, @${workspace.name} #${ping.id} `)
  const responded = nextResponse(page, 'POST', `/api/v1/workspaces/${workspace.id}/tasks/${ping.id}/respond`)
  await input.press('ControlOrMeta+Enter')
  expect((await responded).status()).toBe(200)
  await expect(page.getByTestId('pano-comments').locator('.pano-mention').last()).toHaveText(`@${workspace.name} #${ping.id}`)
  const thread = await (await page.request.get(`/api/v1/workspaces/${workspace.id}/tasks/${ping.id}`)).json()
  expect(thread.task.messages.at(-1).text, 'the comment is a message on the task').toContain(`> More on the docs, @${workspace.name} #${ping.id}`)
  const open = await (await page.request.get(`/api/v1/workspaces/${workspace.id}/tasks?status=notstarted`)).json()
  expect(open.tasks.filter((t) => t.title === ping.title).map((t) => t.id), 'no task was made for it').toEqual([ping.id])

  await page.screenshot({ path: test.info().outputPath('pano-assigned-light.png'), fullPage: true, animations: 'disabled' })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await page.screenshot({ path: test.info().outputPath('pano-assigned-dark.png'), fullPage: true, animations: 'disabled' })
  await page.evaluate(() => document.documentElement.classList.remove('dark'))

  // The board shows the assignee too.
  await page.goto('/pano')
  await expect(page.getByTestId('pano-card').filter({ hasText: doc.title }).locator('.pano-assignee')).toHaveAttribute('data-status', 'completed')

  traffic.expectClean()
})

// Two writers cannot overwrite each other. An update names the version it
// read: an agent that is stale is refused, and so is the person whose editor
// was open while an agent changed the doc, who keeps their draft and can load
// the latest. An edit changes part of a doc.
test('a write on a stale version is refused, for an agent and for the person', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const call = await agentSession(page, workspace.id)
  const created = await pano(call, { action: 'save', title: unique('Versions'), markdown: '# Plan\n\n- [ ] backend\n- [ ] docs' })
  expect(created.version, 'a new doc starts at version 1').toBe(1)

  const ticked = await pano(call, { action: 'edit', id: created.id, version: 1, ops: [
    { op: 'replace', oldString: '- [ ] backend', newString: '- [x] backend' },
    { op: 'append', text: '\n- [ ] demo' },
  ] })
  expect(ticked.version, 'an edit moves the version').toBe(2)
  expect([ticked.checklistDone, ticked.checklistTotal]).toEqual([1, 3])
  expect((await pano(call, { action: 'read', id: created.id })).markdown).toBe('# Plan\n\n- [x] backend\n- [ ] docs\n- [ ] demo')

  const stale = await pano(call, { action: 'save', id: created.id, version: 1, title: created.title, markdown: 'overwrite' })
  expect(stale.refused, 'a save on the version before the edit').toContain('changed since you read it')
  expect(stale.refused).toContain('now 2')
  const missing = await pano(call, { action: 'edit', id: created.id, ops: [{ op: 'append', text: 'x' }] })
  expect(missing.refused, 'an edit that sends no version').toContain('version')
  const twice = await pano(call, { action: 'edit', id: created.id, version: 2, ops: [{ op: 'replace', oldString: '- [ ]', newString: '- [x]' }] })
  expect(twice.refused, 'a replace that matches more than once').toContain('occurs 2 times')

  // The person opens the editor; an agent changes the doc meanwhile.
  await page.goto(`/pano/${created.id}?edit=1`)
  await page.getByTestId('pano-markdown-input').fill('# Plan\n\nMy rewrite of the plan.')
  const agent = await pano(call, { action: 'edit', id: created.id, version: 2, ops: [{ op: 'append', text: '\n- [ ] announce' }] })
  expect(agent.version).toBe(3)
  const refused = nextResponse(page, 'PUT', `/api/v1/pano/${created.id}`)
  await page.getByTestId('pano-save').click()
  expect((await refused).status(), 'the person\'s save on version 2').toBe(409)
  await expect(page.getByTestId('pano-conflict')).toContainText('changed while you were editing it')
  await expect(page.getByTestId('pano-markdown-input'), 'the draft is kept').toHaveValue('# Plan\n\nMy rewrite of the plan.')
  await page.screenshot({ path: test.info().outputPath('pano-conflict-light.png'), animations: 'disabled' })
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await page.screenshot({ path: test.info().outputPath('pano-conflict-dark.png'), animations: 'disabled' })
  await page.evaluate(() => document.documentElement.classList.remove('dark'))

  // Load latest: the agent's text, and a save on it goes through.
  await page.getByTestId('pano-conflict-reload').click()
  await expect(page.getByTestId('pano-conflict')).toHaveCount(0)
  await expect(page.getByTestId('pano-markdown-input')).toHaveValue('# Plan\n\n- [x] backend\n- [ ] docs\n- [ ] demo\n- [ ] announce')
  const saved = nextResponse(page, 'PUT', `/api/v1/pano/${created.id}`)
  await page.getByTestId('pano-save').click()
  expect((await saved).status(), 'a save on the latest version').toBe(200)
  expect((await pano(call, { action: 'read', id: created.id })).version).toBe(4)

  traffic.expectClean({ allow: ['409 /api/v1/pano/' + created.id] })
})

// A task a doc points at opens in the chat column beside the doc on a large
// screen, from an item's chip and from the related tasks above the comments.
test('a task linked from a doc opens beside it', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const call = await agentSession(page, workspace.id)
  const task = await createTask(page, workspace.id, unique('a task for the side column'), 'ongoing')
  const doc = await pano(call, {
    action: 'save',
    payload: { title: unique('related'), markdown: `- [ ] ship it → [@qa](/workspaces/${workspace.id}/tasks/${task.id} "ongoing")\n\nSee also [this task](/workspaces/${workspace.id}/tasks/${task.id}).` },
  })
  await page.goto(`/pano/${doc.id}`)

  await expect(page.getByTestId('pano-related-task')).toHaveText(task.title)
  await page.getByTestId('pano-related-task').click()
  await expect(page).toHaveURL(`/pano/${doc.id}/chat/${workspace.id}/${task.id}`, { timeout: 60_000 })
  await expect(page.locator('[data-pano-chat]')).toBeVisible()
  await expect(page.getByTestId('pano-body')).toBeVisible()
  if (process.env.QA_SCREENSHOT) await page.screenshot({ path: process.env.QA_SCREENSHOT })
  // A task with no agent plan answers 404 for it.
  traffic.expectClean({ allow: ['404 /plan'] })
})

// A doc is linked to the tasks that worked on it without the agent writing a
// link: a task the Pano chat made about it, and the one task its agent was on
// when it wrote it. Both show under Related tasks with their status now, and
// a status change moves no version.
test('a doc is linked to the chat about it and to the task its agent was on', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const call = await agentSession(page, workspace.id)
  // The agent is on one task only, so that is the task a doc it writes is for.
  const open = await (await page.request.get(`/api/v1/workspaces/${workspace.id}/tasks?status=ongoing`)).json()
  for (const t of open.tasks || []) await setTaskStatus(page, workspace.id, t.id, 'completed')
  const onIt = await createTask(page, workspace.id, unique('the agent is on this'), 'ongoing')
  const doc = await pano(call, { action: 'save', payload: { title: unique('written on a task'), markdown: '- [ ] my own step' } })

  // The Pano chat sends the doc's id with the task it makes.
  await page.waitForTimeout(1100)
  const res = await page.request.post(`/api/v1/workspaces/${workspace.id}/tasks`, {
    data: { task: { title: unique('a chat about it'), body: 'from the chat', createdBy: 'human', assignee: 'human', status: 'notstarted' }, panoDocId: doc.id },
  })
  expect(res.status(), 'the chat\'s task, linked to the doc').toBe(201)
  const chat = (await res.json()).task

  await page.goto(`/pano/${doc.id}`)
  await expect(page.getByTestId('pano-related-task')).toHaveText([onIt.title, chat.title])
  await expect(page.getByTestId('pano-related-status').first()).toHaveAttribute('aria-label', 'Ongoing')
  await expect(page.getByTestId('pano-body').locator('input[type="checkbox"]'), 'the agent\'s own step stays its to tick').toBeEnabled()

  await setTaskStatus(page, workspace.id, onIt.id, 'completed')
  await expect(page.getByTestId('pano-related-status').first()).toHaveAttribute('aria-label', 'Completed')
  const read = await pano(call, { action: 'read', payload: { id: doc.id } })
  expect(read.version, 'a task\'s status change is no write').toBe(doc.version)

  // A doc that is not the account's gets no task.
  const refused = await page.request.post(`/api/v1/workspaces/${workspace.id}/tasks`, {
    data: { task: { title: 'never', createdBy: 'human', assignee: 'human' }, panoDocId: 'nosuchdoc1' },
  })
  expect(refused.status(), 'a task about a doc that is not there').toBe(404)
  await page.screenshot({ path: test.info().outputPath('pano-linked-tasks-light.png'), animations: 'disabled' })
  traffic.expectClean()
})

// P opens the Pano from anywhere, but a P typed into a field stays text.
test('P opens the Pano, except while typing', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await page.goto(`/workspaces/${workspace.id}/tasks/new`)
  const description = page.locator('#taskForm textarea').first()
  await expect(description, 'the focused description').toBeFocused()
  await page.keyboard.press('p')
  await expect(description, 'the description after typing P').toHaveValue('p')
  await expect(page, 'the form, still open').toHaveURL(/\/tasks\/new$/)

  await page.goto(`/workspaces/${workspace.id}`)
  const listed = nextResponse(page, 'GET', '/api/v1/pano')
  await page.keyboard.press('p')
  expect((await listed).status(), 'the Pano list request').toBe(200)
  await expect(page).toHaveURL(/\/pano$/)
  await expect(page.getByRole('heading', { level: 1, name: 'Pano' })).toBeVisible()

  await page.keyboard.press('?')
  const sheet = page.getByRole('dialog', { name: 'Keyboard shortcuts' })
  await expect(sheet.getByRole('listitem').filter({ hasText: 'Open the Pano' }), 'the help sheet row').toContainText('P')
  await sheet.screenshot({ path: test.info().outputPath('shortcuts-help.png'), animations: 'disabled' })
  traffic.expectClean()
})
