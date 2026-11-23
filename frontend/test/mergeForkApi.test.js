// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mergeFork } from '../src/api.js'

describe('mergeFork', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue({ ok: true, status: 200, json: async () => ({ parentId: 'p1', movedTasks: 2 }) }))
  })
  afterEach(() => vi.unstubAllGlobals())

  const sent = () => {
    const [url, init] = fetch.mock.calls[0]
    return { url, init, body: JSON.parse(init.body) }
  }

  it('keeps the folder unless asked', async () => {
    expect(await mergeFork('f1')).toEqual({ parentId: 'p1', movedTasks: 2 })
    const { url, init, body } = sent()
    expect(url).toBe('/api/v1/workspaces/f1/merge')
    expect(init.method).toBe('POST')
    expect(body).toEqual({ deleteFolder: false })
  })

  it('asks for the folder to be deleted', async () => {
    await mergeFork('f1', { deleteFolder: true })
    expect(sent().body).toEqual({ deleteFolder: true })
  })

  it('throws the server refusal', async () => {
    fetch.mockResolvedValueOnce({ ok: false, status: 409, json: async () => ({ error: 'laptop is offline' }), text: async () => '' })
    await expect(mergeFork('f1', { deleteFolder: true })).rejects.toThrow()
  })
})
