// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { agentSession, createWorkspace, nextResponse, unique, watchTraffic } from '../lib/app.js'
import { signInAs } from '../lib/users.js'

// A team workspace's own skills: one an agent saves in a shared workspace is
// the team's, every member's agent reads and improves it, a workspace opts out
// of it from its Skills tab, a person gives one of theirs to the team, and a
// member deletes it for everyone from the team's Skills page.

const API = '/api/v1'

async function ok(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.status() === 204 ? null : res.json()
}

const versionOf = async (page, teamId) => (await ok(await page.request.get(`${API}/teams/${teamId}`), 'reading the team')).version
// The next response to a call whose path holds `part`, for a route whose
// last segment is an id the page chose.
const responseTo = (page, method, part) => page.waitForResponse((res) => res.request().method() === method && new URL(res.url()).pathname.includes(part))
const share = async (page, workspaceId, teamId) => ok(await page.request.put(`${API}/workspaces/${workspaceId}/shares/${teamId}`, { data: { version: await versionOf(page, teamId) } }), 'sharing')

test('a team workspace\'s skills, from an agent\'s save to a member\'s delete', async ({ page, browser }) => {
  const owner = page
  const traffic = watchTraffic(owner)
  const beaContext = await browser.newContext()
  const bea = await beaContext.newPage()
  const beaTraffic = watchTraffic(bea)
  const who = await signInAs(beaContext, { email: `${unique('bea')}@qa.agentrq.local`, name: 'Bea Teammate' })

  // Both share a workspace of one name, so both are in one team workspace.
  const team = await ok(await owner.request.post(`${API}/teams`, { data: { name: unique('Team') } }), 'creating the team')
  const sent = await ok(await owner.request.post(`${API}/teams/${team.id}/invitations`, { data: { email: who.email, version: await versionOf(owner, team.id) } }), 'inviting Bea')
  await ok(await bea.request.post(`${API}/invitations/accept`, { data: { token: sent.link.split('#')[1] } }), 'Bea accepting')
  const name = unique('qa-tws')
  const mine = await createWorkspace(owner, name)
  const hers = await createWorkspace(bea, name)
  await share(owner, mine.id, team.id)
  await share(bea, hers.id, team.id)

  // 1. The owner's agent saves a new skill: it is the team workspace's.
  const skill = `qa-team-${Date.now().toString(36)}`
  const ownerAgent = await agentSession(owner, mine.id)
  const saved = await ownerAgent('skills', { action: 'save', payload: { uri: `skill://${skill}/SKILL.md`, content: '---\ndescription: The team\'s release steps.\n---\n# Release\nTag it.\n' } })
  expect(saved).toContain(`team workspace ${name}`)

  // 2. Bea's agent finds it, reads it, and improves it on the version it read.
  const beaAgent = await agentSession(bea, hers.id)
  expect(await beaAgent('skills', { action: 'search', payload: { q: skill } })).toContain(skill)
  const loaded = await beaAgent('skills', { action: 'load', payload: { uri: `skill://${skill}` } })
  expect(loaded).toContain('Tag it.')
  const version = Number(loaded.match(/version (\d+)/)[1])
  const improved = await beaAgent('skills', { action: 'save', payload: { uri: `skill://${skill}/SKILL.md`, version, content: '---\ndescription: The team\'s release steps.\n---\n# Release\nTag it, then announce it.\n' } })
  expect(improved).not.toMatch(/refused|error/i)

  // 3. Bea reads it on the team's Skills page, as her agent left it.
  await bea.goto(`/teams/${team.id}/skills?workspace=${encodeURIComponent(name)}`)
  const row = bea.getByTestId('tws-row').filter({ hasText: skill })
  await expect(row).toContainText('The team\'s release steps.')
  await bea.screenshot({ path: test.info().outputPath('team-workspace-skills.png'), animations: 'disabled' })
  await row.getByTestId('tws-open').click()
  await expect(bea.getByTestId('tws-skill-name')).toHaveText(skill)
  await bea.getByTestId('tws-file-open').filter({ hasText: 'SKILL.md' }).click()
  await expect(bea.getByTestId('tws-content')).toHaveValue(/then announce it/)
  await bea.screenshot({ path: test.info().outputPath('team-workspace-skill.png'), animations: 'disabled' })

  // 4. The owner opts their workspace out from its Skills tab: their agent
  //    no longer sees it, and it stays out.
  await owner.goto(`/workspaces/${mine.id}/settings`)
  await owner.getByRole('button', { name: 'Skills', exact: true }).click()
  const item = owner.getByTestId('team-skill-item').filter({ hasText: skill })
  const sw = item.getByTestId('team-skill-switch')
  await expect(sw).toHaveAttribute('aria-checked', 'true')
  const off = responseTo(owner, 'PUT', `/workspaces/${mine.id}/team-skills/`)
  await sw.click()
  expect((await off).status()).toBe(200)
  await expect(sw).toHaveAttribute('aria-checked', 'false')
  expect(await ownerAgent('skills', { action: 'search', payload: { q: skill } })).toContain('No skill\'s name or description contains')
  expect(await beaAgent('skills', { action: 'search', payload: { q: skill } }), 'Bea\'s workspace is still in').toContain(skill)

  // 5. The owner gives one of their own skills to the team: it leaves their
  //    list and Bea's agent reads it.
  const gift = `qa-gift-${Date.now().toString(36)}`
  const own = await ownerAgent('skills', { action: 'save', payload: { uri: `skill://${gift}/SKILL.md`, team: false, content: '---\ndescription: A checklist worth sharing.\n---\n# Checklist\n' } })
  expect(own).not.toContain('team workspace')
  await owner.reload()
  await owner.getByRole('button', { name: 'Skills', exact: true }).click()
  await owner.getByRole('button', { name: `Give ${gift} to the team` }).click()
  await expect(owner.getByTestId('give-skill-modal')).toBeVisible()
  await owner.screenshot({ path: test.info().outputPath('give-skill.png'), animations: 'disabled' })
  const given = nextResponse(owner, 'POST', `/workspaces/${mine.id}/skills/${gift}/give`)
  await owner.getByTestId('give-skill-confirm').click()
  expect((await given).status()).toBe(200)
  await expect(owner.getByTestId('give-skill-modal')).toHaveCount(0)
  await expect(owner.getByTestId('team-skill-item').filter({ hasText: gift })).toBeVisible()
  await owner.screenshot({ path: test.info().outputPath('workspace-team-skills.png'), animations: 'disabled' })
  expect(await beaAgent('skills', { action: 'load', payload: { uri: `skill://${gift}` } })).toContain('# Checklist')

  // 6. The owner turns auto opt-in off for their share: what their agent
  //    sees stays, and a skill added later is not on for it.
  await owner.getByRole('button', { name: 'Sharing', exact: true }).click()
  const auto = owner.getByRole('switch', { name: `Turn new team skills on in ${name}` })
  await expect(auto).toHaveAttribute('aria-checked', 'true')
  const manual = nextResponse(owner, 'PUT', `/workspaces/${mine.id}/shares/${team.id}`)
  await auto.click()
  expect((await manual).status()).toBe(200)
  expect((await manual).request().postDataJSON().autoOptInSkills).toBe(false)
  await expect(auto).toHaveAttribute('aria-checked', 'false')
  await owner.screenshot({ path: test.info().outputPath('share-auto-opt-in.png'), animations: 'disabled' })
  const later = `qa-later-${Date.now().toString(36)}`
  expect(await beaAgent('skills', { action: 'save', payload: { uri: `skill://${later}/SKILL.md`, content: '---\ndescription: Added after the switch.\n---\n# Later\n' } })).toContain(`team workspace ${name}`)
  expect(await ownerAgent('skills', { action: 'search', payload: { q: later } })).toContain('No skill\'s name or description contains')
  expect(await ownerAgent('skills', { action: 'load', payload: { uri: `skill://${gift}` } }), 'a skill on before the switch stays on').toContain('# Checklist')

  // 7. Bea deletes the first for every workspace in the team.
  await bea.getByTestId('tws-delete-skill').click()
  const deleted = responseTo(bea, 'DELETE', `/teams/${team.id}/team-workspaces/`)
  await bea.getByRole('button', { name: 'Delete', exact: true }).click()
  expect((await deleted).status()).toBe(204)
  await expect(bea.getByTestId('tws-row').filter({ hasText: skill })).toHaveCount(0)
  await expect(bea.getByTestId('tws-row').filter({ hasText: gift })).toBeVisible()
  expect(await beaAgent('skills', { action: 'search', payload: { q: skill } })).toContain('No skill\'s name or description contains')

  traffic.expectClean()
  beaTraffic.expectClean()
  await beaContext.close()
})
