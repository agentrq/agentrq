// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A `toolCall.updated` event, mounted for real: the open task takes the one
 * call it carries, and a call for another task changes nothing.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() }),
}))

const events = ref([])
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events }),
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

const cacheTaskUpdate = vi.fn((_cache, current, payload) => ({ ...current, ...payload }))
vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTask: vi.fn(),
  cacheTaskUpdate: (...args) => cacheTaskUpdate(...args),
  sharedCache: () => null,
}))

const createdAt = new Date().toISOString()
const call = (overrides = {}) => ({ id: 'c1', taskId: 't1', toolName: 'Bash', status: 'auto_allowed', createdAt, ...overrides })
const task = () => ({
  id: 't1',
  title: 'Run the migration',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Migrate the users table',
  createdAt,
  messages: [],
  toolCalls: [call()],
})

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
  updateTaskTitle: vi.fn(),
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
  const trajectoryButton = () => [...el.querySelectorAll('button')].find((b) => b.textContent.includes('Trajectory'))
  return { toolCallCount: () => trajectoryButton().querySelector('span.rounded-full')?.textContent.trim() }
}

async function publish(payload) {
  events.value.push({ type: 'toolCall.updated', payload })
  await settle()
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  events.value = []
  cacheTaskUpdate.mockClear()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe('TaskDetailView and toolCall.updated', () => {
  it('adds a new tool call to the open task, and keeps the cache in step', async () => {
    const view = await mount()
    expect(view.toolCallCount()).toBe('1')

    await publish({ workspaceId: 'ws1', toolCall: call({ id: 'c2', status: 'pending' }) })

    expect(view.toolCallCount()).toBe('2')
    expect(cacheTaskUpdate).toHaveBeenCalledTimes(1)
    expect(cacheTaskUpdate.mock.calls[0][2].toolCalls.map((c) => c.id)).toEqual(['c1', 'c2'])
  })

  it('replaces a tool call it already lists rather than adding it twice', async () => {
    const view = await mount()

    await publish({ workspaceId: 'ws1', toolCall: call({ status: 'allowed' }) })

    expect(view.toolCallCount()).toBe('1')
    expect(cacheTaskUpdate.mock.calls[0][2].toolCalls).toEqual([call({ status: 'allowed' })])
  })

  it('ignores a tool call made on another task', async () => {
    const view = await mount()

    await publish({ workspaceId: 'ws1', toolCall: call({ id: 'c9', taskId: 't2' }) })

    expect(view.toolCallCount()).toBe('1')
    expect(cacheTaskUpdate).not.toHaveBeenCalled()
  })
})
