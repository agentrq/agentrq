// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { execFileSync } from 'node:child_process'
import { fileURLToPath } from 'node:url'
import { join } from 'node:path'
import { test, expect } from '../lib/fixtures.js'
import { clientAddress, watchTraffic } from '../lib/app.js'
import { rootToken } from '../lib/settings.js'

const HOOKS = '/admin/v1/hooks'
const repo = fileURLToPath(new URL('../..', import.meta.url))
const dist = join(repo, 'wasm-hooks', 'dist')

// The real built-in hooks, built as the release builds them. A Go hook is
// over the server's 4 MB request limit, so this also proves the page's upload
// is gzipped.
test.beforeAll(() => {
  execFileSync('make', ['wasm-hooks'], { cwd: repo, stdio: 'ignore' })
})

test('the operator adds, upgrades, turns off and deletes a built-in hook', async ({ page }) => {
  test.setTimeout(60_000)
  const traffic = watchTraffic(page)
  // One sign-in attempt per client every ten minutes: a client of its own.
  await page.setExtraHTTPHeaders({ 'X-Real-IP': clientAddress() })
  await page.goto('/admin/login')
  await page.getByPlaceholder('Paste access token...').fill(rootToken)
  await page.getByRole('button', { name: 'Sign in' }).click()
  await expect(page).toHaveURL(/\/admin\/stats$/)

  // A reused server keeps the last run's hooks; nothing registers them.
  for (const { id } of (await (await page.request.get(HOOKS)).json()).hooks) {
    expect((await page.request.delete(`${HOOKS}/${id}`)).status()).toBe(204)
  }

  await page.getByRole('link', { name: 'Hooks' }).click()
  await expect(page).toHaveURL(/\/admin\/hooks$/)
  await expect(page.getByTestId('hook-empty')).toBeVisible()

  const file = page.getByTestId('hook-file')
  const rows = page.getByTestId('hook-row')
  for (const [name, events] of [['github', '7 events'], ['generic', '1 event']]) {
    const added = page.waitForResponse((r) => r.url().endsWith(HOOKS) && r.request().method() === 'POST')
    await file.setInputFiles(join(dist, `${name}.wasm`))
    const res = await added
    expect(res.status(), `adding ${name}`).toBe(201)
    expect(res.request().headers()['content-encoding']).toBe('gzip')
    await expect(page.getByText(`Added ${name} `)).toBeVisible()
    await expect(rows.filter({ hasText: name }).getByTestId('hook-facts')).toContainText(events)
  }
  await expect(rows).toHaveCount(2)
  await expect(rows.first()).toContainText('generic')

  // The same module again is an upgrade to the version it already is.
  const github = rows.filter({ hasText: 'github' })
  await github.getByTestId('hook-upgrade').click()
  const upgraded = page.waitForResponse((r) => r.request().method() === 'PUT')
  await file.setInputFiles(join(dist, 'github.wasm'))
  expect((await upgraded).status()).toBe(200)
  await expect(page.getByText('Upgraded github to')).toBeVisible()

  // Generic's module is another hook: refused, with the server's reason.
  await github.getByTestId('hook-upgrade').click()
  const refused = page.waitForResponse((r) => r.request().method() === 'PUT')
  await file.setInputFiles(join(dist, 'generic.wasm'))
  expect((await refused).status()).toBe(422)
  await expect(page.getByTestId('hook-error')).toContainText('this module is the hook "generic", not "github"')

  await github.getByTestId('hook-enabled').click()
  await expect(github.getByTestId('hook-enabled')).toHaveAttribute('aria-checked', 'false')
  await expect(github).toContainText('Off')
  await page.reload()
  await expect(rows.filter({ hasText: 'github' }).getByTestId('hook-enabled'), 'off after a reload').toHaveAttribute('aria-checked', 'false')
  await rows.filter({ hasText: 'github' }).getByTestId('hook-enabled').click()
  await expect(rows.filter({ hasText: 'github' }).getByTestId('hook-enabled')).toHaveAttribute('aria-checked', 'true')

  await expect(page.getByTestId('hook-add'), 'nothing in flight').toBeEnabled()
  for (const [scheme, width, height] of [['light', 1280, 800], ['dark', 1280, 800], ['light', 390, 844]]) {
    await page.emulateMedia({ colorScheme: scheme })
    await page.setViewportSize({ width, height })
    await page.screenshot({ path: test.info().outputPath(`admin-hooks-${scheme}-${width}.png`), fullPage: true, animations: 'disabled' })
  }
  await page.setViewportSize({ width: 1280, height: 800 })

  await rows.filter({ hasText: 'generic' }).getByTestId('hook-delete').click()
  await expect(page.getByText('Delete the hook generic?')).toBeVisible()
  const deleted = page.waitForResponse((r) => r.request().method() === 'DELETE')
  await page.getByRole('button', { name: 'Delete', exact: true }).last().click()
  expect((await deleted).status()).toBe(204)
  await expect(rows).toHaveCount(1)
  await expect(rows.first()).toContainText('github')

  traffic.expectClean()
})
