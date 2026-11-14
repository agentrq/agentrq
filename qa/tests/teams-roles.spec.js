// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, createWorkspace, forkWorkspace, mergeFork, nextResponse, setTaskStatus, unique } from '../lib/app.js'
import { signInAs } from '../lib/users.js'

// What teams.spec.js leaves out: a stranger, the roles, leaving, deleting, an
// archived workspace, a fork, and a teammate's skill. Seeding goes through the
// API; what a person does, and sees, through the page.

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

// A team of the owner's, with each person in it at a role: invited and
// accepted by the link's token, as the invite page does. The run's cap is two
// invitations a team a day, so a team takes at most two people.
async function teamWith(owner, members) {
  const team = await ok(await owner.request.post(`${API}/teams`, { data: { name: unique('Team') } }), 'creating the team')
  for (const [p, role] of members) {
    const sent = await ok(await owner.request.post(`${API}/teams/${team.id}/invitations`, { data: { email: p.email, role } }), `inviting ${p.name}`)
    const token = sent.link.split('#')[1]
    await ok(await p.page.request.post(`${API}/invitations/accept`, { data: { token } }), `${p.name} accepting`)
  }
  return team
}

const share = (page, workspaceId, teamId) => page.request.put(`${API}/workspaces/${workspaceId}/shares/${teamId}`, { data: { group: '' } })
const teamTitles = async (page, teamId) => (await ok(await page.request.get(`${API}/teams/${teamId}/tasks?limit=50`), 'listing the team\'s tasks')).tasks.map((t) => t.title)

test('a stranger gets nothing; admins and members get only what their role allows; leaving and deleting', async ({ page, browser, workspace }) => {
  const owner = page
  const bea = await person(browser, 'Bea')
  const cy = await person(browser, 'Cy')
  const dee = await person(browser, 'Dee')
  const team = await teamWith(owner, [[bea, 'admin'], [cy, 'member']])
  expect((await share(owner, workspace.id, team.id)).status()).toBe(200)
  const title = unique('Shared task')
  const task = await createTask(owner, workspace.id, title, 'notstarted', {
    attachments: [{ id: 'qa-dot', filename: 'dot.png', mimeType: 'image/png', data: PNG }],
  })
  expect(await teamTitles(bea.page, team.id)).toContain(title)

  // 1. Dee is in no team: every team read and write is a 404, never a 403,
  //    and the pages show her none of it.
  const t = `${API}/teams/${team.id}`
  for (const [method, path, data] of [
    ['GET', t], ['GET', `${t}/members`], ['GET', `${t}/workspaces`], ['GET', `${t}/tasks`],
    ['GET', `${t}/tasks/${task.id}`], ['GET', `${t}/tasks/${task.id}/attachments/${task.attachments[0].id}`],
    ['GET', `${t}/analytics`], ['GET', `${t}/memory`], ['GET', `${t}/memory/index`],
    ['GET', `${t}/workspaces/${workspace.id}/memory/memory.md`], ['GET', `${t}/skills`],
    ['PUT', `${t}/memory`, { content: 'mine now' }], ['POST', `${t}/tasks/${task.id}/clone`, { workspaceId: workspace.id }],
    ['PATCH', t, { name: 'mine now' }], ['DELETE', t], ['POST', `${t}/invitations`, { email: 'eve@qa.agentrq.local' }],
    ['PUT', `${API}/workspaces/${workspace.id}/shares/${team.id}`, { group: '' }],
  ]) {
    const res = await dee.page.request.fetch(path, { method, data })
    expect(res.status(), `${method} ${path} for someone not in the team`).toBe(404)
  }
  await dee.page.goto(`/tasks/active?team=${team.id}`)
  await expect(dee.page.getByText(/You're no longer in/)).toBeVisible()
  await expect(dee.page).not.toHaveURL(/team=/)
  await dee.page.goto(`/tasks/active/${workspace.id}/${task.id}?team=${team.id}`)
  await expect(dee.page.getByTestId('no-longer-shared').or(dee.page.getByText(/You're no longer in/))).toBeVisible()
  await expect(dee.page.getByText(title)).toHaveCount(0)

  // 2. The roles, as the settings page offers them and as the server holds them.
  const { members } = await ok(await owner.request.get(`${t}/members`), 'listing members')
  const ownerRow = members.find((m) => m.role === 'owner')
  const row = (p, m) => p.getByTestId('member-row').filter({ hasText: m.email })
  const roleChoices = (p, m) => row(p, m).getByTestId('member-role').locator('option').allTextContents()

  await owner.goto(`/team/settings?team=${team.id}`)
  await expect(owner.getByTestId('invite-form')).toBeVisible()
  await expect(row(owner, ownerRow).getByTestId('member-role'), 'nobody changes their own role').toHaveCount(0)
  expect(await roleChoices(owner, bea)).toEqual(['member', 'admin', 'owner'])
  await owner.getByTestId('tab-danger').click()
  await expect(owner.getByTestId('delete')).toBeVisible()

  await bea.page.goto(`/team/settings?team=${team.id}`)
  await expect(bea.page.getByTestId('invite-form')).toBeVisible()
  await expect(bea.page.getByTestId('invite-role').locator('option')).toHaveText(['member', 'admin'])
  await expect(row(bea.page, ownerRow).getByTestId('member-role'), 'an admin cannot change an owner').toHaveCount(0)
  await expect(row(bea.page, ownerRow).getByTestId('member-remove'), 'an admin cannot remove an owner').toHaveCount(0)
  expect(await roleChoices(bea.page, cy), 'an admin cannot make an owner').toEqual(['member', 'admin'])
  await bea.page.getByTestId('tab-danger').click()
  await expect(bea.page.getByTestId('leave')).toBeVisible()
  await expect(bea.page.getByTestId('delete'), 'only an owner deletes the team').toHaveCount(0)

  await cy.page.goto(`/team/settings?team=${team.id}`)
  await expect(cy.page.getByTestId('member-row')).toHaveCount(3)
  await expect(cy.page.getByTestId('invite-form')).toHaveCount(0)
  await expect(cy.page.getByTestId('member-role')).toHaveCount(0)
  await expect(cy.page.getByTestId('member-remove')).toHaveCount(0)
  await cy.page.getByTestId('tab-danger').click()
  await expect(cy.page.getByTestId('delete')).toHaveCount(0)

  for (const [p, method, path, data, what] of [
    [bea.page, 'PATCH', `${t}/members/${ownerRow.userId}`, { role: 'member' }, 'an admin demoting the owner'],
    [bea.page, 'PATCH', `${t}/members/${cy.userId}`, { role: 'owner' }, 'an admin making an owner'],
    [bea.page, 'DELETE', `${t}/members/${ownerRow.userId}`, undefined, 'an admin removing the owner'],
    [bea.page, 'DELETE', t, undefined, 'an admin deleting the team'],
    [cy.page, 'PATCH', `${t}/members/${bea.userId}`, { role: 'member' }, 'a member changing a role'],
    [cy.page, 'POST', `${t}/invitations`, { email: 'eve@qa.agentrq.local' }, 'a member inviting'],
    [cy.page, 'PATCH', t, { name: 'renamed' }, 'a member renaming the team'],
  ]) {
    expect((await p.request.fetch(path, { method, data })).status(), what).toBe(403)
  }

  // 3. Cy shares a workspace of hers, then leaves: the team leaves her
  //    switcher, and her workspace leaves the team.
  const cyWs = await createWorkspace(cy.page, unique('cy-ws'))
  expect((await share(cy.page, cyWs.id, team.id)).status()).toBe(200)
  const cyTitle = unique('Cy task')
  await createTask(cy.page, cyWs.id, cyTitle)
  expect(await teamTitles(bea.page, team.id)).toContain(cyTitle)
  await cy.page.goto(`/team/settings?team=${team.id}`)
  await cy.page.getByTestId('tab-danger').click()
  await cy.page.getByTestId('leave').click()
  const left = nextResponse(cy.page, 'POST', `/teams/${team.id}/leave`)
  await cy.page.getByRole('button', { name: 'Leave', exact: true }).click()
  expect((await left).status()).toBe(204)
  await expect(cy.page).not.toHaveURL(/team=/)
  const cyTeams = await ok(await cy.page.request.get(`${API}/teams`), 'Cy\'s teams')
  expect(cyTeams.teams.map((x) => x.id)).not.toContain(team.id)
  expect((await ok(await cy.page.request.get(`${API}/workspaces/${cyWs.id}/shares`), 'Cy\'s shares')).shares).toEqual([])
  expect(await teamTitles(bea.page, team.id)).not.toContain(cyTitle)

  // 4. The owner deletes the team: Bea's next look takes her back to Me.
  await owner.goto(`/team/settings?team=${team.id}`)
  await owner.getByTestId('tab-danger').click()
  await owner.getByTestId('delete').click()
  await owner.getByTestId('confirm-text').fill(team.name)
  const deleted = nextResponse(owner, 'DELETE', `/teams/${team.id}`)
  await owner.getByRole('button', { name: 'Delete team', exact: true }).last().click()
  expect((await deleted).status()).toBe(204)
  expect((await bea.page.request.get(t)).status()).toBe(404)
  await bea.page.goto(`/tasks/active?team=${team.id}`)
  await expect(bea.page.getByText(/You're no longer in/)).toBeVisible()
  await expect(bea.page).not.toHaveURL(/team=/)
  await expect(bea.page.getByText(title)).toHaveCount(0)

  for (const p of [bea, cy, dee]) await p.context.close()
})

test('a fork follows its parent, an archived workspace leaves the team, and a teammate\'s skill is added to mine', async ({ page, browser }) => {
  const owner = page
  const bea = await person(browser, 'Bea')
  const team = await teamWith(owner, [[bea, 'member']])

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
  expect(await teamTitles(bea.page, team.id)).toEqual(expect.arrayContaining([parentTitle, forkTitle]))
  await bea.page.goto(`/tasks/active?team=${team.id}`)
  await expect(bea.page.getByText(forkTitle)).toBeVisible()

  // 2. A skill on in the shared workspace: Bea adds it to her own, once.
  const skill = `qa-skill-${Date.now().toString(36)}`
  const call = await agentSession(owner, parent.id)
  const saved = await call('skills', { action: 'save', payload: { uri: `skill://${skill}/SKILL.md`, content: '---\ndescription: A skill the QA suite shares.\n---\n# QA\nShared with the team.\n' } })
  expect(saved).not.toMatch(/error|refused/i)
  const beaWs = await createWorkspace(bea.page, unique('bea-ws'))
  await bea.page.goto(`/skills?team=${team.id}`)
  await bea.page.getByTestId('team-skill-row').filter({ hasText: skill }).click()
  await expect(bea.page.getByTestId('team-skill-body')).toContainText('Shared with the team.')
  const add = async () => {
    await bea.page.getByTestId('team-skill-add').click()
    await bea.page.getByTestId('team-skill-workspace').and(bea.page.locator(`[value="${beaWs.id}"]`)).check()
    const added = nextResponse(bea.page, 'POST', `/teams/${team.id}/skills/${skill}/add`)
    await bea.page.getByTestId('team-skill-add-confirm').click()
    return (await added).status()
  }
  expect(await add()).toBeLessThan(300)
  await expect(bea.page.getByText(`Added ${skill} to your skills`)).toBeVisible()
  const mine = await ok(await bea.page.request.get(`${API}/workspaces/${beaWs.id}/skills`), 'Bea\'s skills')
  expect(mine.skills.map((s) => s.name)).toContain(skill)
  expect(await add(), 'adding it twice').toBe(409)
  await expect(bea.page.getByTestId('team-skill-add-error')).toBeVisible()

  // 3. A fork is merged, not archived: its tasks stay in the team, in the
  //    parent (merged once they are done). The parent archived, it leaves the team with its tasks and
  //    skills, and an open task says so; unarchived, it is back.
  const archive = async (w, how) => ok(await owner.request.post(`${API}/workspaces/${w.id}/${how}`), `${how} ${w.name}`)
  await setTaskStatus(owner, fork.id, forkTask.id, 'completed')
  await mergeFork(owner, fork.id)
  expect(await teamTitles(bea.page, team.id)).toEqual(expect.arrayContaining([parentTitle, forkTitle]))
  await bea.page.goto(`/tasks/active/${parent.id}/${forkTask.id}?team=${team.id}`)
  await expect(bea.page.getByTestId('task-title')).toHaveText(forkTitle)
  await archive(parent, 'archive')
  await bea.page.reload()
  await expect(bea.page.getByTestId('no-longer-shared')).toBeVisible()
  await expect(bea.page.getByText(forkTitle)).toHaveCount(0)
  expect(await teamTitles(bea.page, team.id)).toEqual([])
  expect((await bea.page.request.get(`${API}/teams/${team.id}/tasks/${forkTask.id}`)).status()).toBe(404)
  expect((await bea.page.request.get(`${API}/teams/${team.id}/skills/${skill}`)).status()).toBe(404)
  await archive(parent, 'unarchive')
  expect(await teamTitles(bea.page, team.id)).toEqual(expect.arrayContaining([parentTitle, forkTitle]))

  await bea.context.close()
})
