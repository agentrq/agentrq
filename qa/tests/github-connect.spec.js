// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test, expect } from '../lib/fixtures.js'
import { nextResponse, watchTraffic } from '../lib/app.js'
import { githubAppSlug } from '../lib/settings.js'

// With the server's GitHub App set up, a workspace's Skills tab offers to
// connect GitHub for private imports: a link to install the app, carrying a
// state made for this account. GitHub's return with a state that is not the
// account's is refused, and the page says so rather than connecting.
test('the skills tab offers to connect GitHub, and a return not made for the account is refused', async ({ page, workspace }) => {
  const traffic = watchTraffic(page)
  const connection = nextResponse(page, 'GET', '/github')
  await page.goto(`/workspaces/${workspace.id}/settings?tab=skills`)
  expect((await connection).status(), 'the GitHub connection request').toBe(200)

  const row = page.getByTestId('github-connection')
  await expect(row, 'the private repositories row').toBeVisible()
  await expect(row.getByTestId('github-empty')).toHaveText('Connect GitHub to import from repositories you choose.')
  const connect = row.getByTestId('github-connect')
  await expect(connect).toHaveText('Connect GitHub')
  const href = new URL(await connect.getAttribute('href'))
  expect(href.origin + href.pathname, 'the app\'s install page').toBe(`https://github.com/apps/${githubAppSlug}/installations/new`)
  expect(href.searchParams.get('state'), 'a state for the return').toMatch(/^\d+\.\d+\.[\w-]+$/)

  // GitHub returning with a state forged for another account.
  const forged = href.searchParams.get('state').replace(/^\d+/, '1')
  const refused = nextResponse(page, 'POST', '/github/installations')
  await page.goto(`/github/callback?code=abc&installation_id=9&setup_action=install&state=${encodeURIComponent(forged)}`)
  const res = await refused
  expect(res.status(), 'a connect whose state is not the account\'s').toBe(422)
  await expect(page.getByTestId('github-callback')).toHaveAttribute('data-state', 'failed')
  await expect(page.getByTestId('github-callback-message')).toContainText('has expired or was started by another account')
  await page.getByTestId('github-callback-back').click()
  await expect(page).toHaveURL(/\/$/)

  traffic.expectClean({ allow: ['422 /github/installations'] })
})
