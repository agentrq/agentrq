// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A task arrives with only its latest tool calls (#921), mounted for real: the
 * badge counts them all, and the trajectory pages in the rest on request.
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
const notifyError = vi.fn()
vi.mock('../src/composables/useToasts', () => ({
  useToasts: () => ({ notifyError, notifySuccess: vi.fn(), notifyInfo: vi.fn() }),
}))

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: task() }),
  listTaskToolCalls: (...args) => listTaskToolCalls(...args),
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
  recordTelemetry: vi.fn(),
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
  trajectoryButton().click()
  await settle()
  return {
    badge: () => trajectoryButton().querySelector('span.rounded-full')?.textContent.trim(),
    loadEarlier: () => el.querySelector('[data-testid="trajectory-load-earlier"]'),
    listed: () => el.textContent,
  }
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  events.value = []
  count = 250
  loaded = calls(51, 250).map((c, i) => ({ ...c, createdAt: new Date(Date.UTC(2026, 9, 15, 10, 50, i)).toISOString() }))
  listTaskToolCalls.mockReset()
  cacheTask.mockClear()
  notifyError.mockClear()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe('TaskDetailView and earlier tool calls', () => {
  it('counts every call, and traces only the loaded window', async () => {
    const view = await mount()

    expect(view.badge()).toBe('250')
    expect(view.loadEarlier().textContent).toContain('Load earlier')
    expect(view.loadEarlier().textContent).toContain('50')
    expect(view.listed()).toContain('Inside the window')
    expect(view.listed()).not.toContain('Before the window')
  })

  it('pages in the calls before the oldest loaded one, and stops at the first', async () => {
    listTaskToolCalls.mockResolvedValue({ toolCalls: calls(1, 50) })
    const view = await mount()

    view.loadEarlier().click()
    await settle()

    expect(listTaskToolCalls).toHaveBeenCalledWith('ws1', 't1', { before: 'c51', limit: 200 })
    expect(view.loadEarlier()).toBeNull()
    expect(view.badge()).toBe('250')
    expect(view.listed()).toContain('Before the window')
    const cached = cacheTask.mock.calls.at(-1)[1]
    expect(cached.toolCalls).toHaveLength(250)
    expect(cached.toolCallCount).toBe(250)
  })

  it('keeps offering more after a full page', async () => {
    count = 1000
    const page = calls(1, 200).map((c, i) => ({ ...c, id: `old${i}`, createdAt: new Date(Date.UTC(2026, 9, 15, 9, 0, i)).toISOString() }))
    listTaskToolCalls.mockResolvedValue({ toolCalls: page })
    const view = await mount()

    view.loadEarlier().click()
    await settle()

    expect(view.loadEarlier().textContent).toContain('600')
    expect(view.badge()).toBe('1000')
    view.loadEarlier().click()
    await settle()
    expect(listTaskToolCalls).toHaveBeenLastCalledWith('ws1', 't1', { before: 'old0', limit: 200 })
  })

  it('asks for one page at a time', async () => {
    let answer
    listTaskToolCalls.mockReturnValue(new Promise((resolve) => { answer = resolve }))
    const view = await mount()

    view.loadEarlier().click()
    await settle()
    expect(view.loadEarlier().textContent).toContain('Loading')
    view.loadEarlier().click()
    answer({ toolCalls: [] })
    await settle()

    expect(listTaskToolCalls).toHaveBeenCalledTimes(1)
  })

  it('says so when a page fails, and lets it be tried again', async () => {
    listTaskToolCalls.mockRejectedValueOnce(new Error('offline'))
    const view = await mount()

    view.loadEarlier().click()
    await settle()

    expect(notifyError).toHaveBeenCalledWith('Failed to load earlier tool calls: offline')
    expect(view.loadEarlier().disabled).toBe(false)
  })
})
