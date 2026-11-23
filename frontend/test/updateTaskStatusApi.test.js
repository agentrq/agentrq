// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, afterEach } from 'vitest'
import { updateTaskStatus } from '../src/api.js'

describe('updateTaskStatus', () => {
  afterEach(() => vi.unstubAllGlobals())

  it('throws the reason the server refused the move with', async () => {
    const reason = 'another task is already ongoing in this workspace'
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 409, json: async () => ({ error: { message: reason, code: 409 } }) }))
    await expect(updateTaskStatus('w1', 't1', 'ongoing')).rejects.toThrow(reason)
  })

  it('falls back to its own words when the server gave none', async () => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: false, status: 502, json: async () => { throw new Error('not json') } }))
    await expect(updateTaskStatus('w1', 't1', 'ongoing')).rejects.toThrow('Failed to update task status')
  })
})
