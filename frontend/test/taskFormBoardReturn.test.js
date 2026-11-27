// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A task form opened from a Kanban board goes back to that board when it is
 * sent or closed; opened from anywhere else, to the workspace as before.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'
import { boardReturnPath } from '../src/utils/boardReturn'

const route = { params: { id: 'ws1' }, query: {} }
const push = vi.fn()
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push }) }))

vi.mock('../src/composables/useSpeechToText', () => ({
  useSpeechToText: () => ({
    isRecording: ref(false),
    isTranscribing: ref(false),
    isModelLoading: ref(false),
    modelProgress: ref(0),
    error: ref(null),
    isSupported: ref(false),
    toggleRecording: vi.fn(),
  }),
}))
vi.mock('../src/composables/useUntitledTaskTitles', () => ({ nameUntitledTask: vi.fn() }))

const createTask = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'ops' } }),
  fetchEvents: () => Promise.resolve({ events: [] }),
  fetchWorkflows: () => Promise.resolve({ workflows: [] }),
  fetchWorkspaceSession: () => Promise.resolve(null),
  createTask: (...a) => createTask(...a),
}))

const { default: TaskFormView } = await import('../src/views/TaskFormView.vue')

const settle = () => new Promise((r) => setTimeout(r, 30))
let app

async function mountFrom(back) {
  window.history.replaceState({ back }, '')
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(TaskFormView) })
  app.use(createPinia())
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  return el
}

async function send(el) {
  const body = el.querySelector('#taskForm textarea')
  body.value = 'Fix the login page redirect'
  body.dispatchEvent(new Event('input'))
  await settle()
  el.querySelector('#taskForm').dispatchEvent(new Event('submit', { cancelable: true }))
  await settle()
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  createTask.mockResolvedValue({ task: { id: 't9', title: 'Fix' } })
})
afterEach(() => {
  app?.unmount()
  window.history.replaceState(null, '')
})

describe('the task form opened from a board', () => {
  it('goes back to every workspace\'s board once sent', async () => {
    await send(await mountFrom('/kanban'))
    expect(push).toHaveBeenCalledWith('/kanban')
  })

  it('goes back to the workspace\'s board, its open chat and filters too, on Escape', async () => {
    await mountFrom('/workspaces/ws1/board/chat/t1?q=x')
    window.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }))
    await settle()
    expect(push).toHaveBeenCalledWith('/workspaces/ws1/board/chat/t1?q=x')
  })

  it('goes to the workspace when opened from anywhere else', async () => {
    await send(await mountFrom('/tasks/active'))
    expect(push).toHaveBeenCalledWith('/workspaces/ws1')
  })
})

describe('boardReturnPath', () => {
  it.each([
    '/kanban',
    '/kanban?workspace=ws1',
    '/kanban/chat/ws1/t1',
    '/teams/tm1/kanban',
    '/teams/tm1/kanban/chat/ws1/t1',
    '/workspaces/ws1/board',
    '/workspaces/ws1/board?q=x',
    '/workspaces/ws1/board/chat/t1',
  ])('is %s for a board', (back) => {
    expect(boardReturnPath(back)).toBe(back)
  })

  it.each([
    undefined,
    null,
    42,
    '',
    '/',
    '/kanbanx',
    '/workspaces/ws1',
    '/workspaces/ws1/boards',
    '/workspaces/ws1/tasks/t1',
    '/tasks/active',
    'https://evil.example/kanban',
  ])('is empty for %s', (back) => {
    expect(boardReturnPath(back)).toBe('')
  })
})
