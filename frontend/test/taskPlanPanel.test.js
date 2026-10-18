// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, afterEach } from 'vitest'
import { createApp, h, reactive } from 'vue'
import TaskPlanPanel from '../src/components/TaskPlanPanel.vue'
import { planCommentText } from '../src/composables/useTaskPlan'

const settle = () => new Promise((resolve) => setTimeout(resolve, 0))
const mounted = []

function mount(props) {
  const state = reactive({ ...props, comments: [] })
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({
    render: () => h(TaskPlanPanel, {
      plan: state.plan,
      messages: state.messages,
      disabled: state.disabled,
      onComment: (step, text) => state.comments.push([step, text]),
    }),
  })
  mounted.push(app)
  app.mount(el)
  const q = (id) => el.querySelector(`[data-testid="${id}"]`)
  const all = (id) => [...el.querySelectorAll(`[data-testid="${id}"]`)]
  return { el, state, q, all }
}

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
})

const plan = { id: 't1', version: 2, content: '## Fix login\n- [x] Read the code\n- [ ] Write the fix\n- [ ] Ship it' }

describe('TaskPlanPanel', () => {
  it('shows a one-line bar with the progress and the current step', () => {
    const { q } = mount({ plan, messages: [] })
    expect(q('task-plan-progress').textContent.trim()).toBe('1 / 3')
    expect(q('task-plan-summary').textContent).toContain('Write the fix')
    expect(q('task-plan-body')).toBeNull()
  })

  it('opens to the whole plan, its markdown and its steps', async () => {
    const { q, all } = mount({ plan, messages: [] })
    q('task-plan-toggle').click()
    await settle()
    expect(q('task-plan-toggle').getAttribute('aria-expanded')).toBe('true')
    expect(q('task-plan-body').querySelector('h2').textContent).toBe('Fix login')
    expect(all('task-plan-step').map((s) => s.textContent.trim().replace(/\s*💬.*$/, ''))).toEqual(['Read the code', 'Write the fix', 'Ship it'])
    q('task-plan-toggle').click()
    await settle()
    expect(q('task-plan-body')).toBeNull()
  })

  it('sends a comment on a step, and cancels one', async () => {
    const { q, all, state } = mount({ plan, messages: [] })
    q('task-plan-toggle').click()
    await settle()
    const commentOn = (i) => all('task-plan-step')[i].querySelector('[data-testid="task-plan-comment"]')

    commentOn(1).click()
    await settle()
    const textarea = q('task-plan-comment-form').querySelector('textarea')
    expect(document.activeElement).toBe(textarea)
    q('task-plan-comment-form').dispatchEvent(new Event('submit'))
    await settle()
    expect(state.comments).toEqual([])

    textarea.value = '  add a test first '
    textarea.dispatchEvent(new Event('input'))
    textarea.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }))
    await settle()
    expect(state.comments).toEqual([['Write the fix', 'add a test first']])
    expect(q('task-plan-comment-form')).toBeNull()

    commentOn(2).click()
    await settle()
    q('task-plan-comment-form').querySelector('textarea').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await settle()
    expect(q('task-plan-comment-form')).toBeNull()

    commentOn(2).click()
    await settle()
    ;[...q('task-plan-comment-form').querySelectorAll('button')].find((b) => b.textContent === 'Cancel').click()
    await settle()
    expect(q('task-plan-comment-form')).toBeNull()
  })

  it('sends nothing while disabled', async () => {
    const { q, all, state } = mount({ plan, messages: [], disabled: true })
    q('task-plan-toggle').click()
    await settle()
    const button = all('task-plan-step')[1].querySelector('[data-testid="task-plan-comment"]')
    expect(button.disabled).toBe(true)
    // Disabled after a form was opened: the form still refuses to send.
    state.disabled = false
    await settle()
    button.click()
    await settle()
    const textarea = q('task-plan-comment-form').querySelector('textarea')
    textarea.value = 'hold on'
    textarea.dispatchEvent(new Event('input'))
    state.disabled = true
    await settle()
    q('task-plan-comment-form').dispatchEvent(new Event('submit'))
    await settle()
    expect(state.comments).toEqual([])
  })

  it('lists comments under their step, and those of a step that is gone as earlier', async () => {
    const messages = [
      { id: 'm1', sender: 'human', text: planCommentText('Write the fix', 'add a test first') },
      { id: 'm2', sender: 'human', text: planCommentText('Write the fix', 'and keep it small') },
      { id: 'm3', sender: 'human', text: planCommentText('Old step', 'still matters') },
    ]
    const { q, all, el } = mount({ plan, messages })
    expect(el.textContent).toContain('💬 3')
    q('task-plan-toggle').click()
    await settle()
    const fix = all('task-plan-step')[1]
    expect([...fix.querySelectorAll('li')].map((li) => li.textContent)).toEqual(['add a test first', 'and keep it small'])
    expect(fix.querySelector('[data-testid="task-plan-comment"]').textContent).toContain('2')
    expect(q('task-plan-earlier').textContent).toContain('Old step')
    expect(q('task-plan-earlier').textContent).toContain('still matters')
  })

  it('counts a single comment in the singular, and shows a plan without steps', () => {
    const messages = [{ id: 'm1', sender: 'human', text: planCommentText('Gone', 'x') }]
    const { el, q } = mount({ plan: { id: 't1', version: 1, content: 'Investigate first.' }, messages })
    expect(el.querySelector('[title="1 comment"]')).not.toBeNull()
    expect(q('task-plan-progress')).toBeNull()
    expect(q('task-plan-summary').textContent).toContain('Investigate first.')
  })

  it('has no summary for an empty plan', () => {
    const { q } = mount({ plan: { id: 't1', version: 1, content: '' } })
    expect(q('task-plan-summary')).toBeNull()
  })
})
