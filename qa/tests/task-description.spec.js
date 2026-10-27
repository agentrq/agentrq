// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTask, replyToTask, unique, watchTraffic } from '../lib/app.js'

// A task's description opens while the task has nothing else to read, and
// closes when the first reply arrives, so the reply is in view. It once stayed
// open, squeezing the conversation until a live reply landed out of sight.

const BRIEF = 'A long brief.\n\n' + '- one more point to read through\n'.repeat(60)

const description = (page) => page.locator('[data-test="task-description-body"]')

// Moves inside the app, as a click on another task in the list does.
const switchTo = (page, path) => page.evaluate((to) => document.querySelector('#app').__vue_app__.config.globalProperties.$router.push(to), path)

// Drawn, not only in the DOM: what is at the message's middle is the message.
async function drawn(page, text) {
  return page.evaluate((t) => {
    const bubble = [...document.querySelectorAll('.md-body')].find((el) => el.textContent.includes(t))
    if (!bubble) return false
    const r = bubble.getBoundingClientRect()
    return bubble.contains(document.elementFromPoint(r.x + r.width / 2, r.y + r.height / 2))
  }, text)
}

for (const size of [{ width: 1280, height: 836 }, { width: 390, height: 844 }]) {
  test(`at ${size.width}x${size.height}, the description gives way to the conversation`, async ({ page, workspace }) => {
    const traffic = watchTraffic(page)
    await page.setViewportSize(size)
    const talked = await createTask(page, workspace.id, unique('a task with a message'))
    await replyToTask(page, workspace.id, talked.id, 'An earlier message.')
    const live = await createTask(page, workspace.id, unique('a task awaiting its first reply'), 'ongoing', { body: BRIEF })
    const quiet = await createTask(page, workspace.id, unique('a task with nothing said'), 'ongoing', { body: BRIEF })
    const path = (t) => `/workspaces/${workspace.id}/tasks/${t.id}`

    // A direct load of a task with no messages opens its description.
    await page.goto(path(quiet))
    await expect(description(page), 'the description, loaded directly').toBeVisible()

    // Moving to a task with messages closes it; moving to one without opens it.
    await switchTo(page, path(talked))
    await expect(page.getByText('An earlier message.'), 'the task with a message').toBeVisible()
    await expect(description(page), 'the description of a task with messages').toHaveCount(0)
    const streaming = page.waitForResponse((res) => res.url().includes('/api/v1/events/stream'))
    await switchTo(page, path(live))
    await expect(description(page), 'the description of a task with none').toBeVisible()

    // The first reply closes it, and is drawn where it can be read.
    await streaming
    await replyToTask(page, workspace.id, live.id, 'A live reply.')
    await expect(description(page), 'the description once the first reply arrives').toHaveCount(0)
    await expect.poll(() => drawn(page, 'A live reply.'), { message: 'the live reply, drawn at its middle' }).toBe(true)

    // A task the agent has made no plan for answers 404 for it.
    traffic.expectClean({ allow: ['404 /plan'] })
  })
}
