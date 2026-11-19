// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createTask, createWorkspace, nextResponse, openSidebar, unique, watchTraffic } from '../lib/app.js'
import { signInAs } from '../lib/users.js'

// A team's analytics: the Analytics button on the team's tasks opens the
// panels a workspace has, summed over every member's workspace in the team
// workspace on screen, or over the whole team.

const API = '/api/v1'

async function ok(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.status() === 204 ? null : res.json()
}

const versionOf = async (page, teamId) => (await ok(await page.request.get(`${API}/teams/${teamId}`), 'reading the team')).version
const share = async (page, workspaceId, teamId) =>
  ok(await page.request.put(`${API}/workspaces/${workspaceId}/shares/${teamId}`, { data: { version: await versionOf(page, teamId) } }), 'sharing')
const summaryOf = async (page, path) => (await ok(await page.request.get(`${API}${path}`), path)).summary

// Completes a task the way an agent does, so it counts as a completion.
async function completeAsAgent(page, workspaceId, title) {
  const task = await createTask(page, workspaceId, title)
  const call = await agentSession(page, workspaceId)
  await call('task', { action: 'updateStatus', payload: { taskId: task.id, status: 'ongoing' } })
  await call('task', { action: 'reply', payload: { taskId: task.id, text: 'done' } })
  await call('task', { action: 'updateStatus', payload: { taskId: task.id, status: 'completed' } })
}

test('a team workspace\'s analytics sum each member\'s workspace in it', async ({ page, browser, workspace }) => {
  const owner = page
  const traffic = watchTraffic(owner)
  const beaContext = await browser.newContext()
  const bea = await beaContext.newPage()
  const who = await signInAs(beaContext, { email: `${unique('bea')}@qa.agentrq.local`, name: 'Bea Teammate' })

  const team = await ok(await owner.request.post(`${API}/teams`, { data: { name: unique('Core') } }), 'creating the team')
  const sent = await ok(await owner.request.post(`${API}/teams/${team.id}/invitations`, { data: { email: who.email, version: await versionOf(owner, team.id) } }), 'inviting Bea')
  await ok(await bea.request.post(`${API}/invitations/accept`, { data: { token: sent.link.split('#')[1] } }), 'Bea accepting')

  // Both share a workspace of the same name: one team workspace. The root
  // user shares a second one, which only the whole team's numbers include.
  const beaWs = await createWorkspace(bea, workspace.name)
  const other = await createWorkspace(owner, unique('Other'))
  await share(owner, workspace.id, team.id)
  await share(bea, beaWs.id, team.id)
  await share(owner, other.id, team.id)
  // The run's one workspace carries what earlier specs completed in it, and
  // their telemetry may still be landing: wait for more than before. The two
  // made here start from none.
  const before = (await summaryOf(owner, `/workspaces/${workspace.id}/stats`)).tasksCompleted
  await completeAsAgent(owner, workspace.id, unique('Ship the owner\'s part'))
  await completeAsAgent(bea, beaWs.id, unique('Ship Bea\'s part'))
  await completeAsAgent(bea, beaWs.id, unique('Ship Bea\'s second part'))
  await completeAsAgent(owner, other.id, unique('Ship the other part'))

  // Telemetry is written after the call answers: wait for each completion.
  const completions = (p, id) => expect.poll(async () => (await summaryOf(p, `/workspaces/${id}/stats`)).tasksCompleted, { message: `completions in ${id}` })
  await completions(owner, workspace.id).toBeGreaterThan(before)
  await completions(bea, beaWs.id).toBe(2)
  await completions(owner, other.id).toBe(1)
  const mine = await summaryOf(owner, `/workspaces/${workspace.id}/stats`)
  const hers = await summaryOf(bea, `/workspaces/${beaWs.id}/stats`)
  const third = await summaryOf(owner, `/workspaces/${other.id}/stats`)

  // The workspace page's own Analytics button, which the team's copies.
  await owner.goto(`/workspaces/${workspace.id}`)
  await openSidebar(owner)
  await owner.getByTitle('Analytics').waitFor()
  await owner.screenshot({ path: test.info().outputPath('workspace-header.png'), animations: 'disabled' })

  // From the team workspace's tasks, the Analytics button.
  await owner.goto(`/teams/${team.id}/tasks/active?workspace=${encodeURIComponent(workspace.name)}`)
  await openSidebar(owner)
  const stats = nextResponse(owner, 'GET', `/teams/${team.id}/analytics`, { workspace: workspace.name })
  await owner.getByTestId('team-analytics').click()
  const res = await stats
  expect(res.status()).toBe(200)
  const { summary } = await res.json()
  expect(summary.tasksCompleted, 'the team workspace\'s completions').toBe(mine.tasksCompleted + hers.tasksCompleted)
  expect(summary.messages, 'the team workspace\'s messages').toBe(mine.messages + hers.messages)
  await expect(owner).toHaveURL(new RegExp(`/teams/[^/]+/analytics\\?.*workspace=`))
  await expect(owner.getByTestId('team-analytics-scope')).toHaveText(workspace.name)
  const card = (label) => owner.getByText(label, { exact: true }).locator('xpath=../..')
  await expect(card('Completed')).toContainText(String(summary.tasksCompleted))
  await owner.screenshot({ path: test.info().outputPath('team-analytics-workspace.png'), animations: 'disabled' })
  await owner.evaluate(() => { localStorage.setItem('theme', 'dark'); document.documentElement.classList.add('dark') })
  await owner.screenshot({ path: test.info().outputPath('team-analytics-workspace-dark.png'), animations: 'disabled' })
  await owner.evaluate(() => { localStorage.setItem('theme', 'light'); document.documentElement.classList.remove('dark') })

  // Back to the team workspace's tasks, then the whole team's numbers.
  await owner.getByTestId('team-actions-status-active').click()
  await expect(owner).toHaveURL(new RegExp(`/tasks/active\\?.*workspace=`))
  await owner.screenshot({ path: test.info().outputPath('team-tasks-button.png'), animations: 'disabled' })
  // As high as the pickers beside it.
  const height = async (locator) => (await locator.boundingBox()).height
  expect(await height(owner.getByTestId('team-analytics'))).toBe(await height(owner.getByTestId('team-filter-workspace')))
  await owner.goto(`/teams/${team.id}/tasks/active`)
  const all = nextResponse(owner, 'GET', `/teams/${team.id}/analytics`)
  await owner.getByTestId('team-analytics').click()
  const whole = (await (await all).json()).summary
  expect(whole.tasksCompleted, 'the whole team\'s completions').toBe(mine.tasksCompleted + hers.tasksCompleted + third.tasksCompleted)
  await expect(owner.getByTestId('team-analytics-scope')).toHaveText('All workspaces')

  // Bea reads the same; a phone gets the same page.
  await bea.setViewportSize({ width: 390, height: 844 })
  const beas = nextResponse(bea, 'GET', `/teams/${team.id}/analytics`)
  await bea.goto(`/teams/${team.id}/analytics?workspace=${encodeURIComponent(workspace.name)}`)
  expect((await (await beas).json()).summary.tasksCompleted).toBe(summary.tasksCompleted)
  await bea.screenshot({ path: test.info().outputPath('team-analytics-phone.png'), animations: 'disabled' })
  await bea.goto(`/teams/${team.id}/tasks/active?workspace=${encodeURIComponent(workspace.name)}`)
  await expect(bea.getByTestId('team-analytics')).toBeVisible()
  expect(await height(bea.getByTestId('team-analytics'))).toBe(await height(bea.getByTestId('team-filter-workspace')))
  await bea.screenshot({ path: test.info().outputPath('team-tasks-phone.png'), animations: 'disabled' })

  traffic.expectClean()
  await beaContext.close()
})
