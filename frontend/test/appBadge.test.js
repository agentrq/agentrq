// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'

import { refreshBadge, setBadge, waitingOnYou } from '../src/composables/useAppBadge'

function badgeNav() {
  return { setAppBadge: vi.fn(async () => {}), clearAppBadge: vi.fn(async () => {}) }
}

function respond(body, ok = true) {
  return vi.fn(async () => ({ ok, json: async () => body }))
}

describe('waitingOnYou', () => {
  it('adds the pending and blocked tasks of every workspace', () => {
    const overview = new Map([
      ['1', { counts: { pending: 2, blocked: 1, ongoing: 5, queued: 3 } }],
      ['2', { counts: { blocked: 4 } }],
      ['3', { counts: {} }],
    ])
    expect(waitingOnYou(overview.values())).toBe(7)
  })

  it('is zero with no overview', () => {
    expect(waitingOnYou(undefined)).toBe(0)
    expect(waitingOnYou([null, {}])).toBe(0)
  })
})

describe('setBadge', () => {
  it('shows a count above zero', async () => {
    const nav = badgeNav()
    await setBadge(nav, 3)
    expect(nav.setAppBadge).toHaveBeenCalledWith(3)
    expect(nav.clearAppBadge).not.toHaveBeenCalled()
  })

  it('clears the badge at zero', async () => {
    const nav = badgeNav()
    await setBadge(nav, 0)
    expect(nav.clearAppBadge).toHaveBeenCalled()
    expect(nav.setAppBadge).not.toHaveBeenCalled()
  })

  it('does nothing where the platform has no badge', async () => {
    await expect(setBadge({}, 3)).resolves.toBeUndefined()
    await expect(setBadge(undefined, 3)).resolves.toBeUndefined()
  })

  it('ignores a refusal from an app that is not installed', async () => {
    const nav = { setAppBadge: vi.fn(async () => { throw new Error('NotAllowedError') }) }
    await expect(setBadge(nav, 1)).resolves.toBeUndefined()
  })
})

describe('refreshBadge', () => {
  it('reads the overview under the worker scope and shows its count', async () => {
    const nav = badgeNav()
    const fetchImpl = respond({ workspaces: [{ counts: { pending: 1, blocked: 1 } }] })
    await refreshBadge(fetchImpl, 'https://h/base/', nav)
    expect(String(fetchImpl.mock.calls[0][0])).toBe('https://h/base/api/v1/tasks/overview')
    expect(nav.setAppBadge).toHaveBeenCalledWith(2)
  })

  it('clears the badge once nothing waits', async () => {
    const nav = badgeNav()
    await refreshBadge(respond({ workspaces: [] }), 'https://h/', nav)
    expect(nav.clearAppBadge).toHaveBeenCalled()
  })

  it('keeps the badge when the read is refused', async () => {
    const nav = badgeNav()
    await refreshBadge(respond({}, false), 'https://h/', nav)
    expect(nav.setAppBadge).not.toHaveBeenCalled()
    expect(nav.clearAppBadge).not.toHaveBeenCalled()
  })

  it('keeps the badge when offline', async () => {
    const nav = badgeNav()
    await refreshBadge(vi.fn(async () => { throw new TypeError('offline') }), 'https://h/', nav)
    expect(nav.setAppBadge).not.toHaveBeenCalled()
    expect(nav.clearAppBadge).not.toHaveBeenCalled()
  })

  it('makes no request where the platform has no badge', async () => {
    const fetchImpl = respond({ workspaces: [] })
    await refreshBadge(fetchImpl, 'https://h/', {})
    expect(fetchImpl).not.toHaveBeenCalled()
  })
})
