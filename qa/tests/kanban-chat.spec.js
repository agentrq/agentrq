// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { createTask, replyToTask, unique, watchTraffic } from '../lib/app.js'

test('a Kanban card opens its task chat beside the board, and the back arrow closes it', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a card chatted with beside the board'), 'ongoing', { assignee: 'human' })
  await replyToTask(page, workspace.id, task.id, 'Is the release plan ready?')
  await page.goto('/kanban')

  const card = page.locator('[draggable]', { hasText: task.title })
  await card.scrollIntoViewIfNeeded()
  await card.click()
  await expect(page).toHaveURL(`/kanban/chat/${workspace.id}/${task.id}`)

  const chat = page.locator('[data-pano-chat]')
  await expect(chat.getByText('Is the release plan ready?'), 'the task\'s thread in the chat column').toBeVisible()
  await expect(card, 'the board, still beside the chat').toBeVisible()
  await expect(page.getByRole('heading', { name: 'Kanban', exact: true })).toBeVisible()
  await page.screenshot({ path: test.info().outputPath('kanban-chat-light.png') })

  await page.evaluate(() => { localStorage.theme = 'dark' })
  await page.reload()
  await expect(chat.getByText('Is the release plan ready?'), 'the chat column, opened from its link').toBeVisible()
  await page.screenshot({ path: test.info().outputPath('kanban-chat-dark.png') })
  await page.evaluate(() => { localStorage.removeItem('theme') })

  await chat.locator('[data-test="task-back"]').click()
  await expect(page).toHaveURL('/kanban')
  await expect(chat, 'the chat column, closed').toHaveCount(0)
  await expect(card, 'the board, still there').toBeVisible()
  traffic.expectClean({ allow: ['404 /plan'] })
})

test('a workspace board opens a card\'s chat beside it, hiding the Scheduled column until it closes', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const task = await createTask(page, workspace.id, unique('a card chatted with on its workspace board'), 'ongoing', { assignee: 'human' })
  await replyToTask(page, workspace.id, task.id, 'Can you take the next step?')
  await page.goto(`/workspaces/${workspace.id}/board`)
  const scheduled = page.getByRole('heading', { name: 'Scheduled', exact: true })
  await expect(scheduled, 'the Scheduled column, before a chat opens').toBeVisible()

  const card = page.locator('[draggable]', { hasText: task.title })
  await card.click()
  await expect(page).toHaveURL(`/workspaces/${workspace.id}/board/chat/${task.id}`)
  const chat = page.locator('[data-pano-chat]')
  await expect(chat.getByText('Can you take the next step?'), 'the task\'s thread in the chat column').toBeVisible()
  await expect(card, 'the board, still beside the chat').toBeVisible()
  await expect(scheduled, 'the Scheduled column, hidden while the chat is open').toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('workspace-board-chat-light.png') })

  await chat.locator('[data-test="task-back"]').click()
  await expect(page).toHaveURL(`/workspaces/${workspace.id}/board`)
  await expect(chat, 'the chat column, closed').toHaveCount(0)
  await expect(scheduled, 'the Scheduled column, back').toBeVisible()
  traffic.expectClean({ allow: ['404 /plan'] })
})
