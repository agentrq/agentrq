// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { test, expect } from '../lib/fixtures.js'
import { nextResponse, openSidebar, unique, watchTraffic } from '../lib/app.js'
import { signInAs } from '../lib/users.js'

// Sharing one Pano doc with a team: its owner switches the share on from the
// doc's page, a teammate reads it, and its picture, on the team's Pano, and
// stops reading it the moment the share is switched off.

const API = '/api/v1'
// A 1x1 PNG.
const PNG = 'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=='

async function ok(res, what) {
  if (!res.ok()) throw new Error(`${what} answered ${res.status()}: ${await res.text()}`)
  return res.status() === 204 ? null : res.json()
}

const versionOf = async (page, teamId) => (await ok(await page.request.get(`${API}/teams/${teamId}`), 'reading the team')).version
const dark = (page, on) => page.evaluate((d) => {
  localStorage.setItem('theme', d ? 'dark' : 'light')
  document.documentElement.classList.toggle('dark', d)
}, on)

test('a Pano doc shared with a team is read, read-only, by its members until unshared', async ({ page, browser }) => {
  const owner = page
  const traffic = watchTraffic(owner)
  const beaContext = await browser.newContext()
  const bea = await beaContext.newPage()
  const who = await signInAs(beaContext, { email: `${unique('bea')}@qa.agentrq.local`, name: 'Bea Teammate' })

  const team = await ok(await owner.request.post(`${API}/teams`, { data: { name: unique('Core') } }), 'creating the team')
  const sent = await ok(await owner.request.post(`${API}/teams/${team.id}/invitations`, { data: { email: who.email, version: await versionOf(owner, team.id) } }), 'inviting Bea')
  await ok(await bea.request.post(`${API}/invitations/accept`, { data: { token: sent.link.split('#')[1] } }), 'Bea accepting')

  const title = unique('Launch plan')
  const { doc } = await ok(await owner.request.post(`${API}/pano`, { data: {
    title, color: 'blue',
    markdown: '# Launch\n\n- [x] Write the spec\n- [ ] Ship the backend\n\n![chart](chart.png)',
    files: [{ filename: 'chart.png', mimeType: 'image/png', data: PNG }],
  } }), 'saving the doc')
  const { doc: mine } = await ok(await owner.request.post(`${API}/pano`, { data: { title: unique('Private notes'), markdown: 'mine' } }), 'saving a private doc')

  // Before any share, Bea's team Pano is empty and the doc is not hers.
  expect((await ok(await bea.request.get(`${API}/teams/${team.id}/pano`), 'Bea listing')).docs).toEqual([])
  expect((await bea.request.get(`${API}/teams/${team.id}/pano/${doc.id}`)).status()).toBe(404)

  // The owner switches the share on from the doc's page.
  await owner.goto(`/pano/${doc.id}`)
  await openSidebar(owner)
  const row = owner.getByTestId('pano-share-row').filter({ hasText: team.name })
  await expect(row).toBeVisible()
  const sharing = nextResponse(owner, 'PUT', `/pano/${doc.id}/shares/${team.id}`)
  await row.getByTestId('pano-share-switch').click()
  expect((await sharing).status()).toBe(200)
  await expect(row.getByTestId('pano-share-switch')).toHaveAttribute('aria-checked', 'true')
  await owner.getByTestId('pano-share').scrollIntoViewIfNeeded()
  await owner.screenshot({ path: test.info().outputPath('owner-share.png'), animations: 'disabled' })
  await dark(owner, true)
  await owner.screenshot({ path: test.info().outputPath('owner-share-dark.png'), animations: 'disabled' })
  await dark(owner, false)

  // Bea finds it on the team's Pano, from the team sidebar.
  await bea.goto(`/tasks/active?team=${team.id}`)
  await openSidebar(bea)
  await bea.getByRole('link', { name: 'Pano' }).click()
  await expect(bea).toHaveURL(new RegExp(`/team/pano\\?team=${team.id}`))
  const card = bea.getByTestId('team-pano-card').filter({ hasText: title })
  await expect(card).toBeVisible()
  await expect(bea.getByTestId('team-pano-card').filter({ hasText: mine.title })).toHaveCount(0)
  await bea.screenshot({ path: test.info().outputPath('teammate-board.png'), animations: 'disabled' })
  await dark(bea, true)
  await bea.screenshot({ path: test.info().outputPath('teammate-board-dark.png'), animations: 'disabled' })
  await dark(bea, false)

  // The doc, read-only, with its picture on the team's route.
  const chart = bea.waitForResponse((r) => r.url().includes(`/teams/${team.id}/pano/${doc.id}/files/`))
  await card.click()
  await expect(bea.getByTestId('team-pano-title')).toHaveText(title)
  expect((await chart).status()).toBe(200)
  await expect(bea.getByTestId('team-pano-body').locator('img')).toBeVisible()
  await expect(bea.getByRole('button', { name: /edit/i })).toHaveCount(0)
  await bea.screenshot({ path: test.info().outputPath('teammate-doc.png'), animations: 'disabled' })
  await dark(bea, true)
  await bea.screenshot({ path: test.info().outputPath('teammate-doc-dark.png'), animations: 'disabled' })
  await dark(bea, false)
  await bea.setViewportSize({ width: 390, height: 844 })
  await bea.reload()
  await expect(bea.getByTestId('team-pano-title')).toHaveText(title)
  await bea.screenshot({ path: test.info().outputPath('teammate-doc-phone.png'), animations: 'disabled' })

  // Nothing of the owner's other docs, or her shares, reaches Bea.
  expect((await bea.request.get(`${API}/teams/${team.id}/pano/${mine.id}`)).status()).toBe(404)
  expect((await bea.request.get(`${API}/pano/${doc.id}/shares`)).status()).toBe(404)
  expect((await bea.request.put(`${API}/pano/${doc.id}/shares/${team.id}`, { data: { version: await versionOf(bea, team.id) } })).status()).toBe(404)

  // The owner switches it off; Bea's next read is refused.
  await owner.goto(`/pano/${doc.id}`)
  const unsharing = nextResponse(owner, 'DELETE', `/pano/${doc.id}/shares/${team.id}`)
  await row.getByTestId('pano-share-switch').click()
  await owner.getByRole('button', { name: 'Stop sharing' }).click()
  expect((await unsharing).status()).toBe(200)
  await expect(row.getByTestId('pano-share-switch')).toHaveAttribute('aria-checked', 'false')
  expect((await bea.request.get(`${API}/teams/${team.id}/pano/${doc.id}`)).status()).toBe(404)
  const files = (await ok(await owner.request.get(`${API}/pano/${doc.id}`), 'reading the doc')).doc.files
  expect((await bea.request.get(`${API}/teams/${team.id}/pano/${doc.id}/files/${files[0].id}`)).status()).toBe(404)

  traffic.expectClean()
  await beaContext.close()
})
