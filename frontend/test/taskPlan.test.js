// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, vi } from 'vitest'
import {
  PLAN_COMMENT_PREFIX,
  parsePlan,
  planSummary,
  planCommentText,
  parsePlanComment,
  planComments,
  useTaskPlan,
} from '../src/composables/useTaskPlan'

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))

describe('parsePlan', () => {
  it('reads task-list items as steps, the first unticked one current', () => {
    const plan = parsePlan('# Fix the login\n\n- [x] Read the code\n* [X] Find the bug\n- [ ] Write the fix\n+ [ ] Ship it\n\nNotes after.')
    expect(plan.steps).toEqual([
      { text: 'Read the code', done: true },
      { text: 'Find the bug', done: true },
      { text: 'Write the fix', done: false },
      { text: 'Ship it', done: false },
    ])
    expect(plan.progress).toEqual({ done: 2, total: 4 })
    expect(plan.current).toBe('Write the fix')
    expect(plan.blocks.map((b) => b.type)).toEqual(['markdown', 'step', 'step', 'step', 'step', 'markdown'])
    expect(plan.blocks[0].text).toBe('# Fix the login')
    expect(plan.blocks[5].text).toBe('Notes after.')
  })

  it('leaves task-list lines inside a code fence as code', () => {
    const plan = parsePlan('```\n- [ ] not a step\n```\n- [ ] a step')
    expect(plan.steps).toEqual([{ text: 'a step', done: false }])
    expect(plan.blocks[0]).toEqual({ type: 'markdown', text: '```\n- [ ] not a step\n```' })
  })

  it('has no progress and no current step without steps, or with all done', () => {
    expect(parsePlan('Just prose')).toMatchObject({ steps: [], progress: null, current: null })
    expect(parsePlan(undefined)).toMatchObject({ blocks: [], progress: null })
    expect(parsePlan('- [x] Done').current).toBeNull()
  })
})

describe('planSummary', () => {
  it('names the current step, says all done, or reads the first line', () => {
    expect(planSummary('- [x] A\n- [ ] B')).toBe('B')
    expect(planSummary('- [x] A')).toBe('All steps done')
    expect(planSummary('\n## Rewrite the parser\nmore')).toBe('Rewrite the parser')
    expect(planSummary('')).toBe('')
    expect(planSummary(null)).toBe('')
  })
})

describe('plan comments', () => {
  it('quote the step they are about, and read back', () => {
    const text = planCommentText('Run the tests', '  also with -race \n')
    expect(text).toBe(`${PLAN_COMMENT_PREFIX}Run the tests\n\nalso with -race`)
    expect(parsePlanComment(text)).toEqual({ step: 'Run the tests', comment: 'also with -race' })
  })

  it('are only messages that start with the prefix and name a step', () => {
    expect(parsePlanComment('> Plan step: Ship')).toEqual({ step: 'Ship', comment: '' })
    expect(parsePlanComment('Plan step: Ship')).toBeNull()
    expect(parsePlanComment(`${PLAN_COMMENT_PREFIX}   \nbody`)).toBeNull()
    expect(parsePlanComment(undefined)).toBeNull()
  })

  it('are filed under their step, or under earlier steps when it is gone', () => {
    const messages = [
      { id: 'm1', sender: 'human', text: planCommentText('Run the tests', 'with -race'), createdAt: 'a' },
      { id: 'm2', sender: 'agent', text: planCommentText('Run the tests', 'the agent quoting it'), createdAt: 'b' },
      { id: 'm3', sender: 'human', text: 'an ordinary message', createdAt: 'c' },
      { id: 'm4', sender: 'slack', text: planCommentText('Run the tests', 'and lint'), createdAt: 'd' },
      { id: 'm5', sender: 'human', text: planCommentText('Old step', 'still matters'), createdAt: 'e' },
      null,
    ]
    const { byStep, earlier } = planComments(messages, [{ text: 'Run the tests' }, { text: 'Ship' }])
    expect(byStep.get('Run the tests')).toEqual([
      { id: 'm1', comment: 'with -race', createdAt: 'a' },
      { id: 'm4', comment: 'and lint', createdAt: 'd' },
    ])
    expect(byStep.has('Ship')).toBe(false)
    expect(earlier).toEqual([{ id: 'm5', step: 'Old step', comment: 'still matters', createdAt: 'e' }])
    expect(planComments(undefined, [])).toEqual({ byStep: new Map(), earlier: [] })
  })
})

describe('useTaskPlan', () => {
  const plan = (version, updatedAt, content = 'x') => ({ id: 't1', content, version, updatedAt })

  it('reads the plan of the task on screen, and none when the read fails', async () => {
    const fetchPlan = vi.fn().mockResolvedValueOnce(plan(1, '2026-10-18T00:00:00Z')).mockRejectedValueOnce(new Error('offline'))
    const s = useTaskPlan(fetchPlan)
    await s.load('ws1', 't1')
    expect(fetchPlan).toHaveBeenCalledWith('ws1', 't1')
    expect(s.plan.value.version).toBe(1)
    await s.load('ws1', 't2')
    expect(s.plan.value).toBeNull()
  })

  it('drops a read overtaken by a later one or by an event', async () => {
    let answer
    const s = useTaskPlan(() => new Promise((resolve) => { answer = resolve }))
    s.load('ws1', 't1')
    s.apply({ taskId: 't1', plan: plan(2, '2026-10-18T00:01:00Z') }, 't1')
    answer(plan(1, '2026-10-18T00:00:00Z'))
    await settle()
    expect(s.plan.value.version).toBe(2)
  })

  it("takes the task's events only, never an older plan, and a withdrawal", () => {
    const s = useTaskPlan(vi.fn())
    s.apply(null, 't1')
    s.apply({ taskId: 'other', plan: plan(1, '2026-10-18T00:00:00Z') }, 't1')
    expect(s.plan.value).toBeNull()
    s.apply({ taskId: 't1', plan: plan(3, '2026-10-18T00:03:00Z') }, 't1')
    s.apply({ taskId: 't1', plan: plan(2, '2026-10-18T00:02:00Z') }, 't1')
    expect(s.plan.value.version).toBe(3)
    s.apply({ taskId: 't1', plan: null }, 't1')
    expect(s.plan.value).toBeNull()
    s.apply({ taskId: 't1', plan: plan(1, '2026-10-18T00:04:00Z') }, 't1')
    expect(s.plan.value.version).toBe(1)
  })

  it('reads through the api by default', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('{"error":"not found"}', { status: 404 }))))
    const s = useTaskPlan()
    await s.load('ws1', 't1')
    expect(s.plan.value).toBeNull()
    expect(fetch.mock.calls[0][0]).toBe('/api/v1/workspaces/ws1/tasks/t1/plan')
    vi.unstubAllGlobals()
  })
})
