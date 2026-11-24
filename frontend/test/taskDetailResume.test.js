// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Back from the background, the task shows what was said meanwhile: the stream
 * reopening re-reads the task, since nothing replays the events it missed.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() }),
}))

const events = ref([])
const isConnected = ref(false)
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events, isConnected }),
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

const cacheTask = vi.fn()
vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTask: (...args) => cacheTask(...args),
  cacheTaskUpdate: (_cache, current, payload) => ({ ...current, ...payload }),
  sharedCache: () => null,
  readCachedTask: () => Promise.resolve(null),
}))

const at = (minute) => new Date(Date.UTC(2026, 9, 15, 10, minute)).toISOString()
const call = (n) => ({ id: `c${n}`, taskId: 't1', toolName: 'Bash', inputPreview: `{"command":"step ${n}"}`, status: 'auto_allowed', createdAt: at(n) })
const calls = (from, to) => Array.from({ length: to - from + 1 }, (_, i) => call(from + i))

let loaded
let count
const task = () => ({
  id: 't1',
  title: 'Run the migration',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Migrate the users table',
  createdAt: at(0),
  // A permission request for a call not loaded yet, then one for a loaded call.
  messages: [
    { id: 'm1', sender: 'agent', text: 'Before the window', createdAt: at(1) },
    { id: 'm2', sender: 'agent', text: 'Inside the window', createdAt: at(50) },
  ],
  toolCalls: loaded,
  toolCallCount: count,
})

const listTaskToolCalls = vi.fn()
const getTask = vi.fn()
const getTaskPlan = vi.fn()
const respondToTask = vi.fn()
const recordTelemetry = vi.fn()
const notifyError = vi.fn()
vi.mock('../src/composables/useToasts', () => ({
  useToasts: () => ({ notifyError, notifySuccess: vi.fn(), notifyInfo: vi.fn() }),
}))

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: (...args) => getTask(...args),
  listTaskToolCalls: (...args) => listTaskToolCalls(...args),
  getTaskPlan: (...args) => getTaskPlan(...args),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: vi.fn(),
  respondToTask: (...args) => respondToTask(...args),
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
  recordTelemetry: (...args) => recordTelemetry(...args),
  TELEMETRY_UI_COPY_MARKDOWN: 'ui.copy_markdown',
  TELEMETRY_UI_SHORTCUT_USE: 'ui.shortcut_use',
  TELEMETRY_UI_TRAJECTORY_VIEW: 'ui.trajectory_view',
  TELEMETRY_UI_PLAN_COMMENT: 'ui_plan_comment',
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
  return el
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  events.value = []
  isConnected.value = false
  count = 0
  loaded = []
  listTaskToolCalls.mockReset()
  getTaskPlan.mockReset().mockResolvedValue(null)
  getTask.mockReset().mockImplementation(() => Promise.resolve({ task: task() }))
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe('TaskDetailView when its stream reopens', () => {
  it('does not read the task again on the stream\'s first open', async () => {
    await mount()
    expect(getTask).toHaveBeenCalledOnce()

    isConnected.value = true
    await settle()

    expect(getTask).toHaveBeenCalledOnce()
  })

  it('re-reads the task, and shows what was said meanwhile, when it reopens', async () => {
    const el = await mount()
    isConnected.value = true
    await settle()

    getTask.mockImplementation(() => {
      const t = task()
      return Promise.resolve({ task: { ...t, messages: [...t.messages, { id: 'm3', sender: 'agent', text: 'Said while you were away', createdAt: at(55) }] } })
    })
    isConnected.value = false
    await settle()
    isConnected.value = true
    await settle()

    expect(getTask).toHaveBeenCalledTimes(2)
    expect(el.textContent).toContain('Said while you were away')
  })
})
