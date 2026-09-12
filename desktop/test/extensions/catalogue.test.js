// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { describe, it, expect } from 'vitest'

import { decorate } from '../../src/main/extensions/catalogue.js'

/**
 * Compatibility is decided here rather than in the renderer, because the app
 * version and the live tool lists are main-process facts. The renderer is left
 * with grouping and ordering, and never learns what a semver range is.
 */

const entry = (over = {}) => ({
  fullName: 'owner/ext',
  ok: true,
  manifest: {
    name: 'ext',
    engines: { agentrq: '^1.4' },
    mcp: { workspace: ['getTask'], supervisor: [] },
  },
  ...over,
})

describe('decorate', () => {
  it('marks an entry this build can run', () => {
    const { entries } = decorate({ entries: [entry()] }, {
      appVersion: '1.4.0',
      workspaceTools: ['getTask'],
    })

    expect(entries[0].compatible).toBe(true)
    expect(entries[0].reasons).toEqual([])
  })

  it('says which version is needed and which is running', () => {
    const { entries } = decorate({ entries: [entry()] }, {
      appVersion: '1.3.0',
      workspaceTools: ['getTask'],
    })

    expect(entries[0].compatible).toBe(false)
    expect(entries[0].reasons[0]).toBe('Needs AgentRQ ^1.4; this is 1.3.0.')
  })

  it('names a tool the connected server does not offer', () => {
    // The real check for a self-hosted backend, which may be older than the
    // extension expects even when the app itself is new enough.
    const { entries } = decorate({ entries: [entry()] }, { appVersion: '1.4.0' })

    expect(entries[0].reasons).toContain('The workspace server does not offer "getTask".')
  })

  it('leaves a broken entry exactly as it was', () => {
    // There is no manifest to judge, and its parse failure is already the reason.
    const broken = entry({ ok: false, reason: '"license" is required.', manifest: undefined })

    const { entries } = decorate({ entries: [broken] }, { appVersion: '1.4.0' })

    expect(entries[0]).toEqual(broken)
  })

  it('carries the rest of the index through untouched', () => {
    const { truncated, fetchedAt } = decorate(
      { entries: [], truncated: true, fetchedAt: 42 },
      { appVersion: '1.4.0' },
    )

    expect(truncated).toBe(true)
    expect(fetchedAt).toBe(42)
  })

  it('copes with nothing at all', () => {
    expect(decorate(undefined).entries).toEqual([])
    expect(decorate({}).entries).toEqual([])
  })
})
