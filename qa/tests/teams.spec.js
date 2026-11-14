// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { createTask, createWorkspace, nextResponse, setTaskStatus, unique, watchTraffic } from '../lib/app.js'
import { signInAs } from '../lib/users.js'

const API = '/api/v1'
// A 1×1 PNG, and an SVG: one the server sends as an image, one it sends as a
// download.
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='
const SVG = Buffer.from('<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>').toString('base64')

async function ok(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.status() === 204 ? null : res.json()
}

// Two people, each in a browser of their own: the root user owns the team and
// shares a workspace; Bea joins by the invitation link and reads it.
test('a team, from the invitation to the member removed', async ({ page, browser, workspace }) => {
  const owner = page
  const traffic = watchTraffic(owner)
  const beaContext = await browser.newContext()
  const bea = await beaContext.newPage()
  const beaTraffic = watchTraffic(bea)
  const who = await signInAs(beaContext, { email: `${unique('bea')}@qa.agentrq.local`, name: 'Bea Teammate' })

  // 1. The owner creates the team.
  const teamName = unique('Core')
  await owner.goto('/team/new')
  await owner.locator('#team-name').fill(teamName)
  const created = nextResponse(owner, 'POST', '/teams')
  await owner.getByRole('button', { name: 'Create' }).click()
  expect((await created).status()).toBe(201)
  await expect(owner).toHaveURL(/\/team\/settings\?team=/)
  const teamId = new URL(owner.url()).searchParams.get('team')

  // 2. Invites Bea: no mail is sent here, so the page shows the link once.
  const invite = async (email) => {
    await owner.locator('#invite-email').fill(email)
    const sent = nextResponse(owner, 'POST', `/teams/${teamId}/invitations`)
    await owner.getByTestId('invite-form').getByRole('button', { name: 'Send' }).click()
    return sent
  }
  expect((await invite(who.email)).status()).toBe(201)
  const link = await owner.getByTestId('invite-link').inputValue()
  const token = link.split('#')[1]
  expect(token, `the invitation link ${link}`).toBeTruthy()
  await owner.keyboard.press('Escape')

  // ...and reaches the daily cap (2 in this run) on the third.
  expect((await invite(`${unique('cy')}@qa.agentrq.local`)).status()).toBe(201)
  await owner.keyboard.press('Escape')
  expect((await invite(`${unique('dee')}@qa.agentrq.local`)).status()).toBe(429)
  await expect(owner.getByTestId('invite-error')).toBeVisible()

  // 3. Bea accepts by the link, and lands in the team.
  await bea.goto(`/invite#${token}`)
  const accepted = nextResponse(bea, 'POST', '/invitations/accept')
  await bea.getByTestId('accept-button').click()
  expect((await accepted).status()).toBe(200)
  await expect(bea).toHaveURL(new RegExp(`/tasks/active\\?team=${teamId}`))

  // 4. The owner shares the run's workspace, and puts a task with two files in it.
  await owner.goto(`/workspaces/${workspace.id}/settings?tab=sharing`)
  const shared = nextResponse(owner, 'PUT', `/workspaces/${workspace.id}/shares/${teamId}`)
  await owner.getByRole('switch', { name: `Share ${workspace.name} with ${teamName}` }).click()
  expect((await shared).status()).toBe(200)
  const title = unique('Fix the login page')
  const task = await createTask(owner, workspace.id, title, 'notstarted', {
    attachments: [
      { id: 'qa-dot', filename: 'dot.png', mimeType: 'image/png', data: PNG },
      { id: 'qa-logo', filename: 'logo.svg', mimeType: 'image/svg+xml', data: SVG },
    ],
  })

  // 5. Bea sees it in team mode, owned by the owner.
  await bea.goto(`/tasks/active?team=${teamId}`)
  await expect(bea.getByText(title)).toBeVisible()

  // 6. Opens it: its files come from the team's route, as the server linked them.
  await bea.goto(`/tasks/active/${workspace.id}/${task.id}?team=${teamId}`)
  await expect(bea.getByTestId('task-title')).toHaveText(title)
  const teamFile = (att) => `${API}/teams/${teamId}/tasks/${task.id}/attachments/${att}`
  const [png, svg] = task.attachments
  await expect(bea.locator(`img[src="${teamFile(png.id)}"]`)).toHaveCount(1)
  await expect(bea.locator(`img[src="${teamFile(svg.id)}"]`)).toHaveCount(0)
  const pngRes = await bea.request.get(teamFile(png.id))
  expect(pngRes.status()).toBe(200)
  expect(pngRes.headers()['content-type']).toBe('image/png')
  const svgRes = await bea.request.get(teamFile(svg.id))
  expect(svgRes.status()).toBe(200)
  expect(svgRes.headers()['content-type']).toBe('application/octet-stream')
  expect(svgRes.headers()['content-disposition']).toMatch(/^attachment/)
  await bea.getByText('logo.svg', { exact: true }).first().click()
  await expect(bea.getByRole('link', { name: 'Download' })).toHaveAttribute('href', teamFile(svg.id))
  await expect(bea.getByRole('link', { name: 'Open in browser' })).toHaveCount(0)
  await expect(bea.getByRole('button', { name: 'Copy link' })).toHaveCount(0)
  await bea.screenshot({ path: test.info().outputPath('team-file-preview.png') })
  await bea.keyboard.press('Escape')

  // 7. The owner moves it on; Bea's open page follows, with no reload.
  await setTaskStatus(owner, workspace.id, task.id, 'ongoing')
  await expect(bea.getByTestId('team-task-status')).toContainText(/ongoing/i)

  // ...and a new task shows on her team list as it is created.
  await bea.goto(`/tasks/active?team=${teamId}`)
  await bea.waitForResponse((r) => r.url().includes('/events/stream'))
  const second = unique('Second task')
  await createTask(owner, workspace.id, second)
  await expect(bea.getByText(second)).toBeVisible()

  // 8. Bea clones the task into a workspace of her own.
  const mine = await ok(await bea.request.post(`${API}/workspaces`, { data: { workspace: { name: unique('bea-ws'), description: 'QA' } } }), 'creating Bea\'s workspace')
  await bea.goto(`/tasks/active/${workspace.id}/${task.id}?team=${teamId}`)
  await bea.getByTestId('clone-task').click()
  await bea.getByTestId('clone-workspace').selectOption(mine.workspace.id)
  const cloned = nextResponse(bea, 'POST', `/teams/${teamId}/tasks/${task.id}/clone`)
  await bea.getByTestId('clone-confirm').click()
  expect((await cloned).status()).toBe(201)
  const hers = await ok(await bea.request.get(`${API}/workspaces/${mine.workspace.id}/tasks?limit=10&offset=0`), 'listing Bea\'s tasks')
  expect(hers.tasks.map((t) => t.title)).toContain(title)

  // 9. Team memory from two tabs: the second save says the first came in between.
  const tab1 = bea
  const tab2 = await beaContext.newPage()
  for (const p of [tab1, tab2]) {
    await p.goto(`/team/memory?team=${teamId}`)
    await expect(p.getByTestId('team-memory-editor')).toBeEnabled()
  }
  const save = async (p, text) => {
    await p.getByTestId('team-memory-editor').fill(text)
    const saved = nextResponse(p, 'PUT', `/teams/${teamId}/memory`)
    await p.getByTestId('team-memory-save').click()
    return (await saved).json()
  }
  expect((await save(tab2, '# Team\nFrom tab 2')).conflict).toBe(false)
  await expect(tab2.getByTestId('team-memory-conflict')).toHaveCount(0)
  expect((await save(tab1, '# Team\nFrom tab 1')).conflict).toBe(true)
  await expect(tab1.getByTestId('team-memory-conflict')).toContainText('your save replaced theirs')
  await expect(tab1.getByTestId('team-memory-saved')).toContainText('by Bea Teammate')
  await tab1.screenshot({ path: test.info().outputPath('team-memory-conflict.png') })
  await tab2.close()

  // 10. The owner stops sharing: the task leaves Bea's list, and its page says why.
  await ok(await owner.request.delete(`${API}/workspaces/${workspace.id}/shares/${teamId}`), 'unsharing')
  await bea.goto(`/tasks/active?team=${teamId}`)
  await expect(bea.getByTestId('team-empty')).toBeVisible()
  await expect(bea.getByText(title)).toHaveCount(0)
  await bea.goto(`/tasks/active/${workspace.id}/${task.id}?team=${teamId}`)
  await expect(bea.getByTestId('no-longer-shared')).toBeVisible()

  // 11. The owner removes Bea: her next look at the team takes her back to Me.
  await ok(await owner.request.delete(`${API}/teams/${teamId}/members/${who.userId}`), 'removing Bea')
  await bea.goto(`/tasks/active?team=${teamId}`)
  // A fresh page no longer knows the team's name, so the toast may not say it.
  await expect(bea.getByText(/You're no longer in/)).toBeVisible()
  await expect(bea).not.toHaveURL(/team=/)

  traffic.expectClean({ allow: ['429 /invitations'] })
  // 404s from the team once the share and the membership are gone, as asked.
  beaTraffic.expectClean({ allow: ['404 /teams/'] })
  await beaContext.close()
})
