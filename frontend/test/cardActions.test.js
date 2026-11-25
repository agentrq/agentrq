// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, expect, it, vi } from 'vitest'
import { ref } from 'vue'

import { cardReadOnly, useCardActions } from '../src/composables/useCardActions'

const question = (expiresAt) => ({ id: 'm1', metadata: { type: 'elicitation_request', mode: 'a2ui', status: 'pending', requestId: 'r1', expiresAt } })
const reply = { id: 'm2', metadata: { type: 'a2ui', surfaceId: 's' } }

describe('cardReadOnly', () => {
  const now = Date.parse('2026-11-25T00:00:00Z')
  it('is read-only in a closed workspace, on a teammate\'s task, or past a question\'s deadline', () => {
    const open = { archived: false, teamMode: false, now }
    expect(cardReadOnly(reply, open)).toBe(false)
    expect(cardReadOnly(question('2026-11-26T00:00:00Z'), open)).toBe(false)
    expect(cardReadOnly(question('2026-11-24T00:00:00Z'), open)).toBe(true)
    expect(cardReadOnly(reply, { ...open, archived: true })).toBe(true)
    expect(cardReadOnly(reply, { ...open, teamMode: true })).toBe(true)
    expect(cardReadOnly({ id: 'x' }, open)).toBe(false)
  })
})

describe('useCardActions', () => {
  function setup(api) {
    const deps = {
      api,
      ids: () => ({ workspaceId: 'w', taskId: 't' }),
      task: ref({ id: 't', messages: [reply] }),
      applyTaskUpdate: (cur, inc) => ({ ...cur, ...inc }),
      markAnswered: vi.fn(),
      notifySuccess: vi.fn(),
      notifyError: vi.fn(),
    }
    return { deps, ...useCardActions(deps) }
  }
  const action = { surfaceId: 's', name: 'go', sourceComponentId: 'b', context: {}, userMessage: '', dataModel: {} }

  it('sends one click per card at a time, and frees it after', async () => {
    let resolve
    const sendA2uiAction = vi.fn(() => new Promise((r) => { resolve = r }))
    const { cardsSending, sendCardAction } = setup({ sendA2uiAction })
    const first = sendCardAction(reply, action)
    expect(cardsSending.value.has('m2')).toBe(true)
    await sendCardAction(reply, action)
    expect(sendA2uiAction).toHaveBeenCalledTimes(1)
    resolve({})
    await first
    expect(cardsSending.value.has('m2')).toBe(false)
  })

  it("never closes a reply's card on a closed-question refusal, nor a question on a 409", async () => {
    const r = setup({ sendA2uiAction: vi.fn().mockRejectedValue(Object.assign(new Error('x'), { closed: true })) })
    await r.sendCardAction(reply, action)
    expect(r.deps.markAnswered).not.toHaveBeenCalled()
    expect(r.deps.task.value.messages[0].metadata.answered).toBeUndefined()
    const q = setup({ respondToElicitation: vi.fn().mockRejectedValue(Object.assign(new Error('x'), { status: 409 })) })
    q.deps.task.value = { id: 't', messages: [question('2999-01-01T00:00:00Z')] }
    await q.sendCardAction(question('2999-01-01T00:00:00Z'), action)
    expect(q.deps.markAnswered).not.toHaveBeenCalled()
    expect(q.deps.task.value.messages[0].metadata.answered).toBeUndefined()
    expect(q.deps.notifyError).toHaveBeenCalledWith('Failed to send the answer: x')
  })
})
