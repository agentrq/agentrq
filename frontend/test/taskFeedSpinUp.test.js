// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Spin up on the workspace's task list: the button beside Edit, on the rows
 * that can be handed off, and the row leaving the list once it has been.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ push, currentRoute: { value: { query: {} } } }),
  useRoute: () => ({ params: {}, query: {} }),
}))

const rows = {
  'ongoing,blocked': [],
  notstarted: [{ id: 't1', title: 'Fix login', status: 'notstarted', assignee: 'agent', workspaceId: 'p1' }],
  cron: [{ id: 't2', title: 'Nightly', status: 'cron', cronSchedule: '0 0 * * *', assignee: 'agent', workspaceId: 'p1' }],
  'completed,rejected': [{ id: 't3', title: 'Done thing', status: 'completed', assignee: 'agent', workspaceId: 'p1' }],
}
const forkWorkspace = vi.fn(() => Promise.resolve({ workspace: { id: 'f1', name: 'fix-login' } }))
const moveTask = vi.fn(() => Promise.resolve({}))
const launchAgent = vi.fn(() => Promise.resolve({ session: { id: 's1' } }))
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchTasks: (_ws, { status }) => Promise.resolve({ tasks: (rows[status] ?? []).map((t) => ({ ...t })) }),
  fetchTaskCounts: () => Promise.resolve({ notstarted: 1 }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [] }),
  fetchMachines: () => Promise.resolve({ machines: [{ id: 'm1', name: 'pi', enabled: true, online: true }] }),
  forkWorkspace: (...a) => forkWorkspace(...a),
  moveTask: (...a) => moveTask(...a),
  launchAgent: (...a) => launchAgent(...a),
  recordTelemetry: () => {},
}))

const { default: TaskFeed } = await import('../src/components/TaskFeed.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')

const settle = () => new Promise((r) => setTimeout(r, 30))
let app

async function mount(workspace) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const pinia = createPinia()
  app = createApp({ render: () => h(TaskFeed, { workspaceId: 'p1' }) })
  app.use(pinia)
  useWorkspaceStore(pinia).workspaces = [workspace]
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  const row = (title) => [...el.querySelectorAll('h3')].find((n) => n.textContent.trim() === title)?.closest('.group')
  return { el, row }
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
})
afterEach(() => app?.unmount())

describe('Spin up on a task row', () => {
  const PARENT = { id: 'p1', name: 'ops', workingDirectory: '/srv/ops' }

  it('sits next to Edit on unfinished work, and not on a schedule or a finished task', async () => {
    const { row } = await mount(PARENT)
    const titles = [...row('Fix login').querySelectorAll('button[title]')].map((b) => b.title)
    expect(titles.slice(titles.indexOf('Edit Task'), titles.indexOf('Edit Task') + 2)).toEqual(['Edit Task', 'Spin up in a fork'])
    expect(row('Nightly').querySelector('[title="Spin up in a fork"]')).toBe(null)
    expect(row('Done thing').querySelector('[title="Spin up in a fork"]')).toBe(null)
  })

  it('is not offered in a fork', async () => {
    const { row } = await mount({ ...PARENT, forkOfId: 'x' })
    expect(row('Fix login').querySelector('[title="Spin up in a fork"]')).toBe(null)
  })

  it('hands the task off and takes it off the list', async () => {
    const { row } = await mount(PARENT)
    row('Fix login').querySelector('[title="Spin up in a fork"]').click()
    await settle()
    ;[...document.body.querySelectorAll('[data-test=spin-up] button')].find((b) => b.textContent.trim() === 'Spin up').click()
    await settle()
    expect(forkWorkspace).toHaveBeenCalledWith('p1', { name: 'fix-login' })
    expect(moveTask).toHaveBeenCalledWith('p1', 't1', 'f1')
    expect(push).toHaveBeenCalledWith('/sessions/s1')
    expect(row('Fix login')).toBeUndefined()
  })

  it('asks Claude Code\'s model and effort on sliders in the popover and sends them', async () => {
    const { row } = await mount(PARENT)
    row('Fix login').querySelector('[title="Spin up in a fork"]').click()
    await settle()
    expect(document.body.querySelector('#spin-up-agent')).toBe(null)
    const model = document.body.querySelector('#spin-up-model')
    model.value = '4'
    model.dispatchEvent(new Event('input'))
    const effort = document.body.querySelector('#spin-up-effort')
    effort.value = '3'
    effort.dispatchEvent(new Event('input'))
    await settle()
    ;[...document.body.querySelectorAll('[data-test=spin-up] button')].find((b) => b.textContent.trim() === 'Spin up').click()
    await settle()
    expect(launchAgent).toHaveBeenCalledWith('f1', expect.objectContaining({ kind: 'claude-code', model: 'fable', effort: 'high' }))
  })

  it('keeps the row when the move is what failed', async () => {
    moveTask.mockImplementationOnce(() => Promise.reject(new Error('task not found')))
    const { row } = await mount(PARENT)
    row('Fix login').querySelector('[title="Spin up in a fork"]').click()
    await settle()
    ;[...document.body.querySelectorAll('[data-test=spin-up] button')].find((b) => b.textContent.trim() === 'Spin up').click()
    await settle()
    expect(row('Fix login')).toBeTruthy()
    document.body.querySelector('[data-test=spin-up]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await settle()
    expect(document.body.querySelector('[data-test=spin-up]')).toBe(null)
  })
})
