// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it } from 'vitest'

import config from '../vite.config.js'

// A path the dev server does not proxy gets the app, which sends a signed-out
// visitor to the sign-in page instead of the backend's answer.
describe('the dev server proxy', () => {
  it('sends the health probes to the backend', () => {
    const { proxy } = config.server
    expect(proxy['/healthz']).toBe(proxy['/mcp'])
    expect(proxy['/readyz']).toBe(proxy['/mcp'])
  })
})
