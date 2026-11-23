// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Whether a task may be offered a clean context.
 *
 * The rule is narrow on purpose and each exclusion is a real case:
 *
 * - **Kind.** `/clear` is typed into a terminal, not interpreted. Offered for
 *   an ACP gateway session it would put six stray characters into whatever
 *   that agent was doing.
 * - **`starting`.** The backend writes to a *running* session's PTY, so a
 *   session still coming up would take the flag and silently drop it — and the
 *   process that would read the prompt is the thing still starting.
 *
 * Both are the sort of thing a UI gets wrong by being generous, and the cost
 * is a control that looks like it worked and did nothing.
 */
import { describe, it, expect, vi } from 'vitest'
import { ref } from 'vue'
import {
  canClearContext,
  clearContextTooltip,
  useClearContext,
  CLEAR_CONTEXT_KIND,
} from '../src/composables/useClearContext.js'

const RUNNING = { id: 's1', kind: CLEAR_CONTEXT_KIND, status: 'running' }

describe('canClearContext', () => {
  it('accepts a running Claude Code session', () => {
    expect(canClearContext(RUNNING)).toBe(true)
  })

  it.each([
    ['nothing at all', null],
    ['undefined', undefined],
    // What a backend answering `{}` instead of `null` looks like by the time
    // it reaches here.
    ['a row with no id', { kind: CLEAR_CONTEXT_KIND, status: 'running' }],
  ])('refuses %s', (_label, session) => {
    expect(canClearContext(session)).toBe(false)
  })

  it('refuses another kind of agent, because /clear is typed rather than read', () => {
    expect(canClearContext({ ...RUNNING, kind: 'acp-gateway' })).toBe(false)
  })

  it.each(['starting', 'exited', 'killed', 'failed'])('refuses a %s session', (status) => {
    // `starting` is the interesting one: there is a session row, but nothing
    // at the other end that could read a prompt yet.
    expect(canClearContext({ ...RUNNING, status })).toBe(false)
  })
})

describe('clearContextTooltip', () => {
  it('says what pressing it will do, and what it already does', () => {
    expect(clearContextTooltip(false)).toContain('clean context')
    expect(clearContextTooltip(true)).toContain('/clear')
    expect(clearContextTooltip(true)).not.toBe(clearContextTooltip(false))
  })
})

describe('useClearContext', () => {
  it('offers the option once it knows a terminal is there', async () => {
    const fetchWorkspaceSession = vi.fn().mockResolvedValue(RUNNING)
    const c = useClearContext({ workspaceId: ref('ws1'), fetchWorkspaceSession })

    // Nothing is offered before the answer arrives: not knowing is not a
    // reason to offer to type into something.
    expect(c.offered.value).toBe(false)

    await c.load()

    expect(fetchWorkspaceSession).toHaveBeenCalledWith('ws1')
    expect(c.offered.value).toBe(true)
  })

  it('takes a plain id as readily as a ref', async () => {
    const fetchWorkspaceSession = vi.fn().mockResolvedValue(RUNNING)
    const c = useClearContext({ workspaceId: 'ws1', fetchWorkspaceSession })

    await c.load()
    expect(fetchWorkspaceSession).toHaveBeenCalledWith('ws1')
    expect(c.offered.value).toBe(true)
  })

  it('hides the option when the workspace has nothing running', async () => {
    const c = useClearContext({
      workspaceId: ref('ws1'),
      fetchWorkspaceSession: vi.fn().mockResolvedValue(null),
    })

    await c.load()
    expect(c.offered.value).toBe(false)
  })

  it('hides it when the request fails, rather than guessing', async () => {
    const c = useClearContext({
      workspaceId: ref('ws1'),
      fetchWorkspaceSession: vi.fn().mockRejectedValue(new Error('offline')),
    })

    await expect(c.load()).resolves.toBeUndefined()
    expect(c.session.value).toBe(null)
    expect(c.offered.value).toBe(false)
  })

  it('asks nothing when there is no workspace to ask about', async () => {
    const fetchWorkspaceSession = vi.fn()
    const c = useClearContext({ workspaceId: ref(''), fetchWorkspaceSession })

    await c.load()
    expect(fetchWorkspaceSession).not.toHaveBeenCalled()
  })

  it('reaches for the real API when nothing is injected', async () => {
    // Exercises the default binding. The call fails in jsdom, which the
    // composable swallows — the point is that the wiring resolves at all.
    const c = useClearContext({ workspaceId: ref('ws1') })
    await c.load()
    expect(c.offered.value).toBe(false)
  })
})
