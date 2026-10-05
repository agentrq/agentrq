// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { withRequestAnswered } from '../src/composables/useRequestAnswer'

const task = () => ({
  id: 't1',
  messages: [
    { id: 'm1', sender: 'human', text: 'go' },
    { id: 'm2', sender: 'agent', metadata: { type: 'elicitation_request', mode: 'url', status: 'pending', requestId: 'r1' } },
    { id: 'm3', sender: 'agent', metadata: { type: 'permission_request', status: 'pending', request_id: 'r2' } },
  ],
})

describe('withRequestAnswered', () => {
  it('resolves the card the request names, and leaves the rest alone', () => {
    const before = task()
    const after = withRequestAnswered(before, 'r1', { status: 'accept', content: { choice: 'B' } })
    expect(after.messages[1].metadata).toEqual({ type: 'elicitation_request', mode: 'url', status: 'accept', requestId: 'r1', content: { choice: 'B' } })
    expect(after.messages[0]).toBe(before.messages[0])
    expect(after.messages[2]).toBe(before.messages[2])
  })

  it('returns a new task rather than changing the one it was given', () => {
    const before = task()
    withRequestAnswered(before, 'r1', { status: 'accept' })
    expect(before.messages[1].metadata.status).toBe('pending')
  })

  // Permission requests written before the API went camelCase.
  it('finds a request by its snake_case id', () => {
    expect(withRequestAnswered(task(), 'r2', { status: 'deny' }).messages[2].metadata.status).toBe('deny')
  })

  it('returns the task as it was when nothing matches', () => {
    const before = task()
    expect(withRequestAnswered(before, 'r9', { status: 'accept' })).toBe(before)
    expect(withRequestAnswered(before, '', { status: 'accept' })).toBe(before)
    expect(withRequestAnswered(null, 'r1', { status: 'accept' })).toBe(null)
    expect(withRequestAnswered({ id: 't1' }, 'r1', { status: 'accept' })).toEqual({ id: 't1' })
  })
})
