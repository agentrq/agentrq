// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, getTask, unique, watchTraffic } from '../lib/app.js'

// The page's live stream never delivers anything: the card has to change on
// the server's answer to the click alone, the way it must when an event is
// lost on the way.
async function silenceLiveStream(page) {
  await page.addInitScript(() => {
    window.EventSource = class {
      constructor(url) { this.url = url; this.readyState = 0 }
      close() { this.readyState = 2 }
      addEventListener() {}
      removeEventListener() {}
    }
  })
}

test('answering a question shows it answered without a reload', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  await silenceLiveStream(page)
  const task = await createTask(page, workspace.id, unique('a task with a question'))

  const call = await agentSession(page, workspace.id)
  const asked = call('elicit', { taskId: task.id, message: 'Open the link, then say when you are done.', mode: 'url', url: 'https://example.com/' })
  await expect.poll(async () => (await (await getTask(page, workspace.id, task.id)).json()).task.messages?.length ?? 0, 'the question, posted').toBeGreaterThan(0)

  await page.goto(`/workspaces/${workspace.id}/tasks/${task.id}`)
  const done = page.getByRole('button', { name: "I'm Done" })
  await expect(done, 'the question').toBeVisible()
  await done.click()

  expect(await asked, 'what the agent was told').toContain('accept')
  await expect(page.getByText('Answered', { exact: true }), 'the card, answered').toBeVisible()
  await expect(done, 'the button, gone').toHaveCount(0)
  await page.screenshot({ path: test.info().outputPath('elicit-answered.png') })
  traffic.expectClean()
})
