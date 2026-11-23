// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { test as base } from '@playwright/test'
import { createWorkspace, signIn, unique } from './app.js'

// One workspace for the whole run. The server lets a user create only two
// workspaces a minute, and a fork counts as one, so every test works in this
// one and the fork test's fork is the only other.
export const test = base.extend({
  workspace: [async ({ browser }, use) => {
    const page = await browser.newPage()
    await signIn(page)
    const workspace = await createWorkspace(page, unique('qa'))
    await page.close()
    await use(workspace)
  }, { scope: 'worker' }],

  // Every test's page starts signed in.
  page: async ({ page }, use) => {
    await signIn(page)
    await use(page)
  },
})

export { expect } from '@playwright/test'
