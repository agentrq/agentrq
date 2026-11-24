// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { sendA2uiAction } from '../src/api'

describe('sendA2uiAction', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  const action = { messageId: 'm1', surfaceId: 'deploy', name: 'approve', sourceComponentId: 'ok', context: { env: 'prod' } }

  it('posts the click and returns the task', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ task: { id: 't1' } }) })
    vi.stubGlobal('fetch', fetchMock)

    await expect(sendA2uiAction('ws1', 't1', action)).resolves.toEqual({ task: { id: 't1' } })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/workspaces/ws1/tasks/t1/a2ui/actions', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(action),
    })
  })

  // A card answered in another tab is refused with 409, which the page tells
  // apart from a failure.
  it('reports the reason and the status of a refusal', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 409,
      json: async () => ({ error: { code: 409, message: 'this card is already answered' } }),
    }))
    const err = await sendA2uiAction('ws1', 't1', action).catch((e) => e)
    expect(err.message).toBe('this card is already answered')
    expect(err.status).toBe(409)
  })

  it('says something of its own for a server error', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 500, json: async () => ({}) }))
    const err = await sendA2uiAction('ws1', 't1', action).catch((e) => e)
    expect(err.message).toBe('Failed to send the answer')
    expect(err.status).toBe(500)
  })
})
