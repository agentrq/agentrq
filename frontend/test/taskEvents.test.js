// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'

import { mergeTaskUpdate, upsertToolCall } from '../src/composables/useTaskEvents'

const task = (overrides = {}) => ({
  id: 'task-1',
  status: 'ongoing',
  messages: [{ id: 'm1', text: 'hello' }],
  toolCalls: [{ id: 't1', toolName: 'Read' }],
  ...overrides,
})

describe('mergeTaskUpdate', () => {
  it('takes the scalar fields from the event', () => {
    const merged = mergeTaskUpdate(task(), task({ status: 'completed', title: 'Renamed' }))
    expect(merged.status).toBe('completed')
    expect(merged.title).toBe('Renamed')
  })

  // The bug this exists for: the SSE forwarder published a task loaded without
  // its tool calls, and the trajectory's tool lane emptied on every new
  // message.
  it('keeps a relation the event did not carry', () => {
    const current = task()
    const incoming = task({ toolCalls: [], messages: [{ id: 'm1' }, { id: 'm2' }] })

    const merged = mergeTaskUpdate(current, incoming)

    expect(merged.toolCalls).toEqual(current.toolCalls)
    expect(merged.messages).toHaveLength(2)
  })

  it('keeps a relation the event omitted entirely', () => {
    const { toolCalls, ...withoutToolCalls } = task({ status: 'completed' })
    expect(toolCalls).toHaveLength(1)

    const merged = mergeTaskUpdate(task(), withoutToolCalls)

    expect(merged.toolCalls).toHaveLength(1)
    expect(merged.status).toBe('completed')
  })

  it('takes the relation from the event whenever it carries one', () => {
    const incoming = task({ toolCalls: [{ id: 't1' }, { id: 't2' }] })

    expect(mergeTaskUpdate(task(), incoming).toolCalls).toHaveLength(2)
  })

  it('leaves an empty relation empty when there is nothing to keep', () => {
    const merged = mergeTaskUpdate(task({ toolCalls: [] }), task({ toolCalls: [] }))

    expect(merged.toolCalls).toEqual([])
  })

  it('does not mutate the task on screen', () => {
    const current = task()

    mergeTaskUpdate(current, task({ toolCalls: [] }))

    expect(current.toolCalls).toHaveLength(1)
  })

  it('takes the event whole when there is no task yet', () => {
    const incoming = task()

    expect(mergeTaskUpdate(null, incoming)).toBe(incoming)
  })

  // Merging across tasks would splice one task's history onto another.
  it('takes the event whole when it describes a different task', () => {
    const incoming = task({ id: 'task-2', toolCalls: [] })

    expect(mergeTaskUpdate(task(), incoming)).toBe(incoming)
  })

  it('holds on to the current task when the event carried no payload', () => {
    const current = task()

    expect(mergeTaskUpdate(current, null)).toBe(current)
    expect(mergeTaskUpdate(undefined, undefined)).toBeNull()
  })
})

describe('upsertToolCall', () => {
  const call = (overrides = {}) => ({ id: 't2', taskId: 'task-1', toolName: 'Bash', status: 'pending', ...overrides })

  it('appends a tool call the task does not list yet', () => {
    const current = task()
    const updated = upsertToolCall(current, call())

    expect(updated.toolCalls.map((c) => c.id)).toEqual(['t1', 't2'])
    expect(updated).not.toBe(current)
    expect(current.toolCalls).toHaveLength(1)
  })

  it('replaces a tool call the task already lists, where it stands', () => {
    const current = task({ toolCalls: [call({ id: 't1' }), call(), call({ id: 't3' })] })
    const updated = upsertToolCall(current, call({ status: 'allowed' }))

    expect(updated.toolCalls.map((c) => c.id)).toEqual(['t1', 't2', 't3'])
    expect(updated.toolCalls[1].status).toBe('allowed')
  })

  it('starts the list for a task that has none', () => {
    expect(upsertToolCall(task({ toolCalls: undefined }), call()).toolCalls).toEqual([call()])
  })

  it('leaves the task alone for a call that is not its own', () => {
    const current = task()

    expect(upsertToolCall(current, call({ taskId: 'task-2' }))).toBe(current)
    expect(upsertToolCall(current, call({ id: '' }))).toBe(current)
    expect(upsertToolCall(current, null)).toBe(current)
    expect(upsertToolCall(null, call())).toBeNull()
    expect(upsertToolCall(undefined, call())).toBeNull()
  })
})
