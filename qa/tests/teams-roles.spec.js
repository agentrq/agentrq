// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, createWorkspace, forkWorkspace, mergeFork, nextResponse, openSidebar, setTaskStatus, unique } from '../lib/app.js'
import { signInAs } from '../lib/users.js'

// What teams.spec.js leaves out: a stranger, what any member may do (there
// are no roles), leaving, deleting, an archived workspace, a fork, and a
// teammate's skill. Seeding goes through the API; what a person does, and
// sees, through the page.

const API = '/api/v1'
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg=='

async function ok(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.status() === 204 ? null : res.json()
}

async function person(browser, name) {
  const context = await browser.newContext()
  const page = await context.newPage()
  const who = await signInAs(context, { email: `${unique(name.toLowerCase())}@qa.agentrq.local`, name: `${name} QA` })
  return { context, page, ...who }
}

// The version a change to the team must name, as the team answers it now.
const versionOf = async (page, teamId) => (await ok(await page.request.get(`${API}/teams/${teamId}`), 'reading the team')).version

// A team of the root user's, with each person in it: invited and accepted by
// the link's token, as the invite page does. The run's cap is two invitations
// a team a day, so a team takes at most two people this way.
async function teamWith(owner, people) {
  const team = await ok(await owner.request.post(`${API}/teams`, { data: { name: unique('Team') } }), 'creating the team')
  for (const p of people) {
    const sent = await ok(await owner.request.post(`${API}/teams/${team.id}/invitations`, { data: { email: p.email, version: await versionOf(owner, team.id) } }), `inviting ${p.name}`)
    const token = sent.link.split('#')[1]
    await ok(await p.page.request.post(`${API}/invitations/accept`, { data: { token } }), `${p.name} accepting`)
  }
  return team
}

const share = async (page, workspaceId, teamId) => page.request.put(`${API}/workspaces/${workspaceId}/shares/${teamId}`, { data: { version: await versionOf(page, teamId) } })
const teamTitles = async (page, teamId) => (await ok(await page.request.get(`${API}/teams/${teamId}/tasks?limit=50`), 'listing the team\'s tasks')).tasks.map((t) => t.title)

test('a stranger gets nothing; any member can invite, rename, remove, leave and delete', async ({ page, browser, workspace }) => {
  const owner = page
  const bea = await person(browser, 'Bea')
  const cy = await person(browser, 'Cy')
  const dee = await person(browser, 'Dee')
  const team = await teamWith(owner, [bea])
  expect((await share(owner, workspace.id, team.id)).status()).toBe(200)
  const title = unique('Shared task')
  const task = await createTask(owner, workspace.id, title, 'notstarted', {
    attachments: [{ id: 'qa-dot', filename: 'dot.png', mimeType: 'image/png', data: PNG }],
  })
  expect(await teamTitles(bea.page, team.id)).toContain(title)

  // 1. Dee is in no team: every team read and write is a 404, never a 403,
  //    and the pages show her none of it.
  const t = `${API}/teams/${team.id}`
  const v = await versionOf(owner, team.id)
  for (const [method, path, data] of [
    ['GET', t], ['GET', `${t}/members`], ['GET', `${t}/invitations`], ['GET', `${t}/activities`],
    ['GET', `${t}/workspaces`], ['GET', `${t}/tasks`],
    ['GET', `${t}/tasks/${task.id}`], ['GET', `${t}/tasks/${task.id}/attachments/${task.attachments[0].id}`], ['GET', `${t}/skills`],
    ['POST', `${t}/tasks/${task.id}/clone`, { workspaceId: workspace.id, assignee: 'human' }],
    ['PATCH', t, { name: 'mine now', version: v }], ['DELETE', `${t}?version=${v}`],
    ['POST', `${t}/invitations`, { email: 'eve@qa.agentrq.local', version: v }],
    ['DELETE', `${t}/members/${bea.userId}?version=${v}`],
    ['PUT', `${API}/workspaces/${workspace.id}/shares/${team.id}`, { version: v }],
  ]) {
    const res = await dee.page.request.fetch(path, { method, data })
    expect(res.status(), `${method} ${path} for someone not in the team`).toBe(404)
  }
  await dee.page.goto(`/teams/${team.id}/tasks/active`)
  await expect(dee.page.getByText(/You're no longer in/)).toBeVisible()
  await expect(dee.page).not.toHaveURL(/\/teams\//)
  await dee.page.goto(`/teams/${team.id}/tasks/active/${workspace.id}/${task.id}`)
  await expect(dee.page.getByTestId('no-longer-shared').or(dee.page.getByText(/You're no longer in/)).first()).toBeVisible()
  await expect(dee.page.getByText(title)).toHaveCount(0)

  // 2. Any member can invite: Bea, who did not make the team, invites Cy.
  await bea.page.goto(`/teams/${team.id}/settings?tab=invitations`)
  await bea.page.locator('#invite-email').fill(cy.email)
  const invited = nextResponse(bea.page, 'POST', `/teams/${team.id}/invitations`)
  await bea.page.getByTestId('invite-form').getByRole('button', { name: 'Send' }).click()
  expect((await invited).status()).toBe(201)
  const token = (await bea.page.getByTestId('invite-link').inputValue()).split('#')[1]
  await ok(await cy.page.request.post(`${API}/invitations/accept`, { data: { token } }), 'Cy accepting')

  // 3. Every member sees the same settings: no role anywhere, Remove on
  //    everyone else, and Leave and Delete for themselves.
  await cy.page.goto(`/teams/${team.id}/settings?tab=members`)
  const dialog = cy.page.getByTestId('team-settings')
  await expect(dialog.getByTestId('member-row')).toHaveCount(3)
  await expect(dialog.getByTestId('member-remove')).toHaveCount(2)
  await expect(dialog.getByTestId('member-row').filter({ hasText: '(you)' }).getByTestId('member-remove')).toHaveCount(0)
  // Whole words: the root user is called Root Administrator.
  await expect(dialog).not.toContainText(/\b(owner|admin)\b/i)
  await dialog.getByTestId('settings-tab-danger').click()
  await expect(dialog.getByTestId('leave')).toBeVisible()
  await expect(dialog.getByTestId('delete')).toBeVisible()
  await dialog.getByTestId('settings-tab-members').click()
  const { members } = await ok(await cy.page.request.get(`${t}/members`), 'listing members')
  for (const m of members) expect(m.role, 'a member has no role').toBeUndefined()

  // 4. Any member can rename it, in place.
  const renamedTo = unique('Renamed')
  await dialog.getByTestId('team-rename').click()
  await cy.page.getByTestId('team-rename-input').fill(renamedTo)
  const renamed = nextResponse(cy.page, 'PATCH', `/teams/${team.id}`)
  await cy.page.getByTestId('team-rename-input').press('Enter')
  expect((await renamed).status()).toBe(200)
  expect((await ok(await owner.request.get(t), 'reading the team')).name).toBe(renamedTo)
  await expect(cy.page.getByTestId('team-mode-bar')).toContainText(renamedTo)

  // 5. Cy shares a workspace of hers, then leaves: the team leaves her
  //    switcher, and her workspace leaves the team.
  const cyWs = await createWorkspace(cy.page, unique('cy-ws'))
  expect((await share(cy.page, cyWs.id, team.id)).status()).toBe(200)
  const cyTitle = unique('Cy task')
  await createTask(cy.page, cyWs.id, cyTitle)
  expect(await teamTitles(bea.page, team.id)).toContain(cyTitle)
  await cy.page.goto(`/teams/${team.id}/settings?tab=danger`)
  await cy.page.getByTestId('leave').click()
  const left = nextResponse(cy.page, 'POST', `/teams/${team.id}/leave`)
  await cy.page.getByRole('button', { name: 'Leave', exact: true }).click()
  expect((await left).status()).toBe(204)
  await expect(cy.page).not.toHaveURL(/\/teams\//)
  const cyTeams = await ok(await cy.page.request.get(`${API}/teams`), 'Cy\'s teams')
  expect(cyTeams.teams.map((x) => x.id)).not.toContain(team.id)
  expect((await ok(await cy.page.request.get(`${API}/workspaces/${cyWs.id}/shares`), 'Cy\'s shares')).shares).toEqual([])
  expect(await teamTitles(bea.page, team.id)).not.toContain(cyTitle)

  // 6. Any member can remove another, the one who made the team included:
  //    Bea removes the root user, after asking, and the log says so.
  const { members: now } = await ok(await bea.page.request.get(`${t}/members`), 'listing members')
  const maker = now.find((m) => String(m.userId) !== String(bea.userId))
  await bea.page.goto(`/teams/${team.id}/settings?tab=members`)
  const beaDialog = bea.page.getByTestId('team-settings')
  await beaDialog.getByTestId('member-row').filter({ hasText: maker.email }).getByTestId('member-remove').click()
  const removed = nextResponse(bea.page, 'DELETE', `/teams/${team.id}/members/${maker.userId}`)
  await bea.page.getByRole('button', { name: 'Remove', exact: true }).last().click()
  expect((await removed).status()).toBe(200)
  await expect(beaDialog.getByTestId('member-row')).toHaveCount(1)
  expect((await owner.request.get(t)).status()).toBe(404)
  await beaDialog.getByTestId('settings-tab-activity').click()
  await expect(beaDialog.getByTestId('activity-row').first()).toContainText('Bea QA removed')

  // 7. Bea deletes the team, once its name is typed. The root user's next
  //    look takes them back to Me.
  await beaDialog.getByTestId('settings-tab-danger').click()
  await beaDialog.getByTestId('delete').click()
  await bea.page.getByTestId('confirm-text').fill(renamedTo)
  const deleted = nextResponse(bea.page, 'DELETE', `/teams/${team.id}`)
  await bea.page.getByRole('button', { name: 'Delete team', exact: true }).last().click()
  expect((await deleted).status()).toBe(204)
  await expect(bea.page).not.toHaveURL(/\/teams\//)
  expect((await bea.page.request.get(t)).status()).toBe(404)
  await owner.goto(`/teams/${team.id}/tasks/active`)
  await expect(owner.getByText(/You're no longer in/)).toBeVisible()
  await expect(owner).not.toHaveURL(/\/teams\//)
  await expect(owner.getByText(title)).toHaveCount(0)

  for (const p of [bea, cy, dee]) await p.context.close()
})

test('a fork follows its parent, an archived workspace leaves the team, and a teammate\'s own skill stays theirs', async ({ page, browser }) => {
  const owner = page
  const bea = await person(browser, 'Bea')
  const team = await teamWith(owner, [bea])

  // 1. A fork is never shared on its own; it is seen through its parent.
  const parent = await createWorkspace(owner, unique('qa-shared'))
  const fork = await forkWorkspace(owner, parent.id, unique('qa-fork'))
  expect((await share(owner, parent.id, team.id)).status()).toBe(200)
  expect((await share(owner, fork.id, team.id)).status(), 'sharing a fork').toBe(422)
  const parentTitle = unique('Parent task')
  const forkTitle = unique('Fork task')
  await createTask(owner, parent.id, parentTitle)
  const forkTask = await createTask(owner, fork.id, forkTitle)
  const { workspaces } = await ok(await bea.page.request.get(`${API}/teams/${team.id}/workspaces`), 'the team\'s workspaces')
  expect(workspaces.find((w) => w.id === fork.id)?.forkOfId).toBe(parent.id)
  // The fork is in its parent's team workspace, under the parent's name.
  expect(workspaces.find((w) => w.id === fork.id)?.sharedName).toBe(parent.name)
  expect(await teamTitles(bea.page, team.id)).toEqual(expect.arrayContaining([parentTitle, forkTitle]))
  await bea.page.goto(`/teams/${team.id}/tasks/active`)
  await expect(bea.page.getByText(forkTitle)).toBeVisible()
  await openSidebar(bea.page)
  await expect(bea.page.getByTestId('team-workspace').filter({ hasText: parent.name })).toHaveCount(1)
  await expect(bea.page.getByTestId('team-workspace').filter({ hasText: fork.name })).toHaveCount(0)

  // 2. A teammate's own skill, on in the shared workspace, stays theirs:
  //    the team workspace's Skills page lists only the skills it owns, and
  //    nothing links a teammate's skill into Bea's account any more.
  const skill = `qa-skill-${Date.now().toString(36)}`
  const call = await agentSession(owner, parent.id)
  const saved = await call('skills', { action: 'save', payload: { uri: `skill://${skill}/SKILL.md`, team: false, content: '---\ndescription: A skill the QA suite keeps personal.\n---\n# QA\nNot the team\'s.\n' } })
  expect(saved).not.toMatch(/error|refused/i)
  await bea.page.goto(`/teams/${team.id}/tasks/active?workspace=${encodeURIComponent(parent.name)}`)
  await expect(bea.page.getByTestId('team-skills-link')).toBeVisible()
  await bea.page.screenshot({ path: test.info().outputPath('team-workspace-tasks.png'), animations: 'disabled' })
  await bea.page.getByTestId('team-skills-link').click()
  await expect(bea.page).toHaveURL(/\/skills\?/)
  await expect(bea.page.getByTestId('team-workspace-skills')).toBeVisible()
  await expect(bea.page.getByTestId('teammates-own-skills')).toHaveCount(0)
  await expect(bea.page.getByText(skill)).toHaveCount(0)
  await bea.page.screenshot({ path: test.info().outputPath('team-workspace-skills.png'), animations: 'disabled' })
  expect((await bea.page.request.get(`${API}/teams/${team.id}/skills`)).status(), 'the retired list').toBe(404)
  // The server's catch-all answers a POST it has no route for with 405.
  expect((await bea.page.request.post(`${API}/teams/${team.id}/skills/${skill}/add`, { data: { workspaceIds: [] } })).status(), 'the retired add').toBe(405)

  // 3. A fork is merged, not archived: its tasks stay in the team, in the
  //    parent (merged once they are done). The parent archived, it leaves the team with its tasks and
  //    skills, and an open task says so; unarchived, it is back.
  const archive = async (w, how) => ok(await owner.request.post(`${API}/workspaces/${w.id}/${how}`), `${how} ${w.name}`)
  await setTaskStatus(owner, fork.id, forkTask.id, 'completed')
  await mergeFork(owner, fork.id)
  expect(await teamTitles(bea.page, team.id)).toEqual(expect.arrayContaining([parentTitle, forkTitle]))
  await bea.page.goto(`/teams/${team.id}/tasks/active/${parent.id}/${forkTask.id}`)
  await expect(bea.page.getByTestId('task-title')).toHaveText(forkTitle)
  await archive(parent, 'archive')
  await bea.page.reload()
  await expect(bea.page.getByTestId('no-longer-shared')).toBeVisible()
  await expect(bea.page.getByText(forkTitle)).toHaveCount(0)
  expect(await teamTitles(bea.page, team.id)).toEqual([])
  expect((await bea.page.request.get(`${API}/teams/${team.id}/tasks/${forkTask.id}`)).status()).toBe(404)
  await archive(parent, 'unarchive')
  expect(await teamTitles(bea.page, team.id)).toEqual(expect.arrayContaining([parentTitle, forkTitle]))

  await bea.context.close()
})
