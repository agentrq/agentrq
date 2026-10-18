// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi, afterEach } from 'vitest'

import { getTaskPlan } from '../src/api'

function serve(body, status = 200) {
  const seen = []
  vi.stubGlobal('fetch', vi.fn((url) => {
    seen.push(url)
    return Promise.resolve(new Response(JSON.stringify(body), { status }))
  }))
  return seen
}

afterEach(() => vi.unstubAllGlobals())

// Relative, so the desktop app's proxy carries it.
describe('getTaskPlan', () => {
  it("reads the task's plan", async () => {
    const seen = serve({ id: 't1', content: '- [ ] A', version: 1 })
    expect(await getTaskPlan('ws1', 't1')).toEqual({ id: 't1', content: '- [ ] A', version: 1 })
    expect(seen).toEqual(['/api/v1/workspaces/ws1/tasks/t1/plan'])
  })

  it('is null for a task with no plan', async () => {
    serve({ error: 'not found' }, 404)
    expect(await getTaskPlan('ws1', 't1')).toBeNull()
  })

  it('throws on any other refusal', async () => {
    serve({ error: 'boom' }, 500)
    await expect(getTaskPlan('ws1', 't1')).rejects.toThrow('Failed to fetch plan')
  })
})
