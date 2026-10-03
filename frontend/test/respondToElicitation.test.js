// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'

import { respondToElicitation } from '../src/api'

describe('respondToElicitation', () => {
  beforeEach(() => {
    vi.unstubAllGlobals()
  })

  it('sends the answer', async () => {
    const fetchMock = vi.fn().mockResolvedValue({ ok: true, status: 200 })
    vi.stubGlobal('fetch', fetchMock)

    await respondToElicitation('ws1', 'task1', 'r1', 'accept', { a: 1 })
    expect(fetchMock).toHaveBeenCalledWith('/api/v1/workspaces/ws1/tasks/task1/elicitation', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ requestId: 'r1', action: 'accept', content: { a: 1 } }),
    })
  })

  // "Failed to send response" left the human guessing why the buttons fail.
  it('reports the reason, and whether the server closed the question', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 410,
      json: async () => ({ error: 'the question is closed now', closed: true }),
    }))
    const err = await respondToElicitation('ws1', 'task1', 'r1', 'cancel').catch((e) => e)
    expect(err.message).toBe('the question is closed now')
    expect(err.closed).toBe(true)
  })

  it('still says something when the refusal carries no reason', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({
      ok: false,
      status: 500,
      json: async () => { throw new Error('not json') },
    }))
    const err = await respondToElicitation('ws1', 'task1', 'r1', 'accept').catch((e) => e)
    expect(err.message).toBe('Failed to send response')
    expect(err.closed).toBe(false)
  })
})
