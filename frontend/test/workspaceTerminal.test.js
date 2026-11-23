// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'
import { nextTick, ref } from 'vue'
import { isWatchable, useWorkspaceTerminal } from '../src/composables/useWorkspaceTerminal.js'

const RUNNING = { id: 's77', kind: 'claude-code', status: 'running', workspaceId: 'ws1' }

/**
 * A composable under test, with its one dependency stubbed.
 *
 * `settle` waits for the immediate watcher's async load, which is what every
 * assertion here depends on: the first answer arrives a microtask after the
 * composable is created, not during it.
 */
function harness(over = {}) {
  const workspaceId = ref(over.workspaceId === undefined ? 'ws1' : over.workspaceId)
  const agentConnected = ref(over.agentConnected ?? true)
  const fetchWorkspaceSession = over.fetchWorkspaceSession
    ?? vi.fn().mockResolvedValue(over.session === undefined ? { ...RUNNING } : over.session)
  const t = useWorkspaceTerminal({ workspaceId, agentConnected, fetchWorkspaceSession })
  const settle = async () => {
    await nextTick()
    await Promise.resolve()
    await Promise.resolve()
  }
  return { t, workspaceId, agentConnected, fetchWorkspaceSession, settle }
}

describe('isWatchable', () => {
  it('accepts a Claude Code session', () => {
    expect(isWatchable(RUNNING)).toBe(true)
  })

  it('accepts the gateway too, which asks its own questions on the way up', () => {
    // This used to be refused, on the grounds that the gateway is driveable
    // from the task composer. That covers its ACP turns and nothing else: the
    // process itself asks `install this version (y/n)` on stdin, which reaches
    // no composer, and the agent is stopped until somebody answers it.
    expect(isWatchable({ id: 's1', kind: 'acp-gateway', status: 'running' })).toBe(true)
  })

  it('accepts a kind nobody has added yet, because a terminal is a terminal', () => {
    expect(isWatchable({ id: 's2', kind: 'something-new', status: 'starting' })).toBe(true)
  })

  it('refuses a session with no id, which is what `{}` instead of `null` looks like here', () => {
    // A row with no id would draw a button that navigates to /sessions/undefined.
    expect(isWatchable({ kind: 'claude-code', status: 'running' })).toBe(false)
  })

  it('copes with nothing at all', () => {
    expect(isWatchable(null)).toBe(false)
    expect(isWatchable(undefined)).toBe(false)
  })
})

describe('useWorkspaceTerminal', () => {
  it('offers the terminal of the session running here', async () => {
    const { t, fetchWorkspaceSession, settle } = harness()
    await settle()

    expect(fetchWorkspaceSession).toHaveBeenCalledWith('ws1')
    expect(t.offered.value).toBe(true)
    expect(t.to.value).toBe('/sessions/s77')
    expect(t.label.value).toBe('Agent terminal')
    expect(t.loading.value).toBe(false)
  })

  it('says a starting session is starting, because that is when it might fail', async () => {
    const { t, settle } = harness({ session: { ...RUNNING, status: 'starting' } })
    await settle()

    expect(t.offered.value).toBe(true)
    expect(t.label.value).toBe('Agent terminal (starting)')
  })

  it('offers a starting session even with no agent connected yet', async () => {
    // The gate is a live session, not a live connection: a session in
    // `starting` has nothing attached, and watching it boot is the point.
    const { t, settle } = harness({
      agentConnected: false,
      session: { ...RUNNING, status: 'starting' },
    })
    await settle()

    expect(t.offered.value).toBe(true)
  })

  it('offers nothing when no agent is running here', async () => {
    const { t, settle } = harness({ session: null })
    await settle()

    expect(t.offered.value).toBe(false)
    expect(t.to.value).toBe('')
    expect(t.label.value).toBe('')
  })

  it('offers a gateway session its terminal, same as any other', async () => {
    const { t, settle } = harness({
      session: { id: 's1', kind: 'acp-gateway', status: 'starting' },
    })
    await settle()

    expect(t.offered.value).toBe(true)
    expect(t.to.value).toBe('/sessions/s1')
    // Still starting is exactly when it is worth opening: a gateway that wants
    // `install this version (y/n)` answered never gets past it on its own.
    expect(t.label.value).toBe('Agent terminal (starting)')
  })

  it('draws no button when the lookup fails, and says nothing about it', async () => {
    // Nobody asked for this, so there is no news to report — and the header
    // dot still says whether an agent is there.
    const fetchWorkspaceSession = vi.fn().mockRejectedValue(new Error('offline'))
    const { t, settle } = harness({ fetchWorkspaceSession })
    await settle()

    expect(t.offered.value).toBe(false)
    expect(t.loading.value).toBe(false)
  })

  it('asks for nothing without a workspace', async () => {
    const { t, fetchWorkspaceSession, settle } = harness({ workspaceId: '' })
    await settle()

    expect(fetchWorkspaceSession).not.toHaveBeenCalled()
    expect(t.offered.value).toBe(false)
  })

  it('forgets the old session when the workspace changes', async () => {
    const fetchWorkspaceSession = vi
      .fn()
      .mockResolvedValueOnce({ ...RUNNING })
      .mockResolvedValueOnce(null)
    const { t, workspaceId, settle } = harness({ fetchWorkspaceSession })
    await settle()
    expect(t.offered.value).toBe(true)

    workspaceId.value = 'ws2'
    await settle()

    expect(fetchWorkspaceSession).toHaveBeenLastCalledWith('ws2')
    expect(t.offered.value).toBe(false)
  })

  it('re-asks when an agent connects, which is what brings the button', async () => {
    const fetchWorkspaceSession = vi
      .fn()
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({ ...RUNNING })
    const { t, agentConnected, settle } = harness({
      agentConnected: false,
      fetchWorkspaceSession,
    })
    await settle()
    expect(t.offered.value).toBe(false)

    agentConnected.value = true
    await settle()

    expect(t.offered.value).toBe(true)
  })

  it('re-asks when an agent goes, which is what takes the button away', async () => {
    const fetchWorkspaceSession = vi
      .fn()
      .mockResolvedValueOnce({ ...RUNNING })
      .mockResolvedValueOnce(null)
    const { t, agentConnected, settle } = harness({ fetchWorkspaceSession })
    await settle()
    expect(t.offered.value).toBe(true)

    agentConnected.value = false
    await settle()

    expect(t.offered.value).toBe(false)
    expect(fetchWorkspaceSession).toHaveBeenCalledTimes(2)
  })

  it('reports that it is asking', async () => {
    let release
    const fetchWorkspaceSession = vi.fn(
      () => new Promise((resolve) => { release = resolve })
    )
    const { t, settle } = harness({ fetchWorkspaceSession })
    await nextTick()

    expect(t.loading.value).toBe(true)
    release({ ...RUNNING })
    await settle()
    expect(t.loading.value).toBe(false)
  })

  it('works with no refs handed to it at all', async () => {
    // Defends the optional chaining: a caller that passes neither ref should
    // get a quiet no-op rather than a crash on page load.
    const fetchWorkspaceSession = vi.fn()
    const t = useWorkspaceTerminal({ fetchWorkspaceSession })
    await nextTick()

    expect(fetchWorkspaceSession).not.toHaveBeenCalled()
    expect(t.offered.value).toBe(false)
  })

  it('can be re-asked directly', async () => {
    const fetchWorkspaceSession = vi
      .fn()
      .mockResolvedValueOnce(null)
      .mockResolvedValueOnce({ ...RUNNING })
    const { t, settle } = harness({ fetchWorkspaceSession })
    await settle()
    expect(t.offered.value).toBe(false)

    await t.load()

    expect(t.offered.value).toBe(true)
  })
})
