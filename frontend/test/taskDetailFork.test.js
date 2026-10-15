// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The fork button in a message's header, mounted for real: the coverage gate
 * does not see `.vue` files, and the promise here lives in the wiring — which
 * message is sent, and that the page then opens the new task.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: { view: 'x' } }),
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

const task = () => ({
  id: 't1',
  title: 'Ship it',
  status: 'completed',
  assignee: 'agent',
  body: '',
  messages: [
    { id: 'm1', sender: 'human', text: 'do the thing', createdAt: '2026-09-22T07:00:00Z' },
    { id: 'm2', sender: 'agent', text: 'done', createdAt: '2026-09-22T07:01:00Z' },
  ],
  toolCalls: [],
})

const forkTask = vi.fn()

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: task() }),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: (...args) => forkTask(...args),
  respondToTask: vi.fn(),
  getAttachmentUrl: () => '',
  getWorkspaceToken: vi.fn(),
  archiveWorkspace: vi.fn(),
  unarchiveWorkspace: vi.fn(),
  updateWorkspace: vi.fn(),
  updateTaskStatus: vi.fn(),
  updateTaskAssignee: vi.fn(),
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
  const forkButtons = () => [...el.querySelectorAll('button[title="Fork into a new task from here"]')]
  return { el, forkButtons }
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  push.mockClear()
  forkTask.mockReset()
  useToasts().toasts.value.splice(0)
})

describe('the fork button', () => {
  it('is on every message', async () => {
    const { forkButtons } = await mount()
    expect(forkButtons()).toHaveLength(2)
  })

  it('forks at the message it belongs to and opens the new task', async () => {
    forkTask.mockResolvedValue({ task: { id: 't9', status: 'ongoing' } })
    const { forkButtons } = await mount()

    forkButtons()[1].click()
    await settle()

    expect(forkTask).toHaveBeenCalledWith('ws1', 't1', 'm2')
    expect(push).toHaveBeenCalledWith({ path: '/workspaces/ws1/tasks/t9', query: { view: 'x' } })
    expect(useToasts().toasts.value.at(-1).message).toMatch(/starting/)
  })

  it('says so and stays put when the fork fails', async () => {
    forkTask.mockRejectedValue(new Error('Failed to fork task'))
    const { forkButtons } = await mount()

    forkButtons()[0].click()
    await settle()

    expect(push).not.toHaveBeenCalled()
    expect(useToasts().toasts.value.at(-1).message).toMatch(/Could not fork/)
    expect(forkButtons()[0].disabled).toBe(false)
  })

  it('ignores a second click while the first fork is on its way', async () => {
    let finish
    forkTask.mockReturnValue(new Promise((resolve) => { finish = resolve }))
    const { forkButtons } = await mount()

    forkButtons()[0].click()
    await settle()
    expect(forkButtons()[1].disabled).toBe(true)
    forkButtons()[1].click()
    finish({ task: { id: 't9', status: 'notstarted' } })
    await settle()

    expect(forkTask).toHaveBeenCalledTimes(1)
  })
})

describe('the back arrow', () => {
  it("goes to the workspace's task list, not back in history", async () => {
    const { el } = await mount()

    el.querySelector('[data-test="task-back"]').click()

    expect(push).toHaveBeenCalledWith({ path: '/workspaces/ws1' })
  })
})
