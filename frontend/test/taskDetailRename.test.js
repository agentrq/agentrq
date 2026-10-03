// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Renaming a task from its page, mounted for real: the coverage gate does not
 * see `.vue` files, and what matters here is the wiring — the title that
 * opens an input when clicked, and the keys that save or abandon it.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h, ref, nextTick } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} }),
  useRouter: () => ({ push, back: vi.fn() }),
}))

vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]) }),
}))

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

vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTask: vi.fn(),
  cacheTaskUpdate: (_cache, current, payload) => ({ ...current, ...payload }),
  sharedCache: () => null,
}))

let createdAt = new Date().toISOString()
const task = () => ({
  id: 't1',
  title: 'Untitled',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Fix the login page redirect',
  createdAt,
  messages: [],
  toolCalls: [],
})

const updateTaskTitle = vi.fn()

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: task() }),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: vi.fn(),
  respondToTask: vi.fn(),
  getAttachmentUrl: () => '',
  getWorkspaceToken: vi.fn(),
  archiveWorkspace: vi.fn(),
  unarchiveWorkspace: vi.fn(),
  updateWorkspace: vi.fn(),
  updateTaskStatus: vi.fn(),
  updateTaskAssignee: vi.fn(),
  updateTaskTitle: (...args) => updateTaskTitle(...args),
  sendPermissionVerdict: vi.fn(),
  respondToElicitation: vi.fn(),
  stopTask: vi.fn(),
  updateTaskAllowAllCommands: vi.fn(),
  TELEMETRY_UI_COPY_MARKDOWN: 'ui.copy_markdown',
  TELEMETRY_UI_SHORTCUT_USE: 'ui.shortcut_use',
  TELEMETRY_UI_TRAJECTORY_VIEW: 'ui.trajectory_view',
  API_BASE_URL: '/api/v1',
}))

const { default: TaskDetailView } = await import('../src/views/TaskDetailView.vue')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
const mounted = []

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(TaskDetailView) })
  mounted.push(app)
  app.use(createPinia())
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  return {
    el,
    heading: () => el.querySelector('[data-test="task-title"]').textContent.trim(),
    title: () => el.querySelector('[data-test="task-title"]'),
    input: () => el.querySelector('[data-test="task-title-input"]'),
  }
}

function type(input, value, key) {
  input.value = value
  input.dispatchEvent(new Event('input'))
  input.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true }))
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  createdAt = new Date().toISOString()
  updateTaskTitle.mockReset()
  useToasts().toasts.value.splice(0)
})

describe('renaming a task from its page', () => {
  it('opens the title for editing, selected, and saves it on Enter', async () => {
    updateTaskTitle.mockImplementation((_ws, _id, title) => Promise.resolve({ task: { ...task(), title } }))
    const page = await mount()

    page.title().click()
    await nextTick()
    await nextTick()
    const input = page.input()
    expect(input.value).toBe('Untitled')
    expect(document.activeElement).toBe(input)

    type(input, 'Fix the login redirect', 'Enter')
    await settle()

    expect(updateTaskTitle).toHaveBeenCalledTimes(1)
    expect(updateTaskTitle).toHaveBeenCalledWith('ws1', 't1', 'Fix the login redirect')
    expect(page.input()).toBeNull()
    expect(page.heading()).toBe('Fix the login redirect')
  })

  it('abandons the edit on Escape', async () => {
    const page = await mount()
    page.title().click()
    await nextTick()

    type(page.input(), 'Something else', 'Escape')
    await settle()

    expect(updateTaskTitle).not.toHaveBeenCalled()
    expect(page.heading()).toBe('Untitled')
  })

  it('saves when the input loses focus', async () => {
    updateTaskTitle.mockImplementation((_ws, _id, title) => Promise.resolve({ task: { ...task(), title } }))
    const page = await mount()
    page.title().click()
    await nextTick()

    const input = page.input()
    input.value = 'Fix the login redirect'
    input.dispatchEvent(new Event('input'))
    input.dispatchEvent(new Event('blur'))
    await settle()

    expect(updateTaskTitle).toHaveBeenCalledWith('ws1', 't1', 'Fix the login redirect')
  })

  it('says why when the server refuses', async () => {
    updateTaskTitle.mockRejectedValue(new Error('a task can only be renamed in the first 7 days after it was created'))
    const page = await mount()
    page.title().click()
    await nextTick()

    type(page.input(), 'Fix the login redirect', 'Enter')
    await settle()

    expect(useToasts().toasts.value.at(-1).message).toMatch(/Could not rename the task: a task can only be renamed/)
  })

  it('opens the editor from the keyboard too', async () => {
    const page = await mount()
    expect(page.title().getAttribute('role')).toBe('button')
    page.title().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await nextTick()
    expect(page.input()).not.toBeNull()
  })

  it('carries its own hint, in the title row', async () => {
    const page = await mount()
    expect(page.el.querySelector('[data-test="task-title-hint"]').textContent).toBe('Click to rename')
  })

  it('leaves the title of a task older than 7 days as plain text', async () => {
    createdAt = new Date(Date.now() - 8 * 24 * 60 * 60 * 1000).toISOString()
    const page = await mount()
    expect(page.title().hasAttribute('role')).toBe(false)
    expect(page.el.querySelector('[data-test="task-title-hint"]')).toBeNull()
    page.title().click()
    page.title().dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await nextTick()
    expect(page.input()).toBeNull()
  })
})
