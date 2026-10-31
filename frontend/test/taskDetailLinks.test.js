// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The task's links, mounted for real: read from the task's description and
 * messages, kept current as messages arrive, pinned over the conversation
 * and under the plan for a narrow view, and a followed link counted.
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

let messages
let panoDocs
const task = () => ({
  id: 't1',
  title: 'Ship the fix',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Fix the login, see https://github.com/agentrq/agentrq/pull/12',
  createdAt: at(0),
  messages,
  toolCalls: [],
  toolCallCount: 0,
})

const recordTelemetry = vi.fn()
vi.mock('../src/composables/useToasts', () => ({
  useToasts: () => ({ notifyError: vi.fn(), notifySuccess: vi.fn(), notifyInfo: vi.fn() }),
}))

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: task(), panoDocs }),
  listTaskToolCalls: () => Promise.resolve({ toolCalls: [] }),
  getTaskPlan: () => Promise.resolve(null),
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
  serverOrigin: () => Promise.resolve(window.location.origin),
  recordTelemetry: (...args) => recordTelemetry(...args),
  TELEMETRY_UI_TASK_LINK_OPEN: 'ui_task_link_open',
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
  messages = [{ id: 'm1', sender: 'agent', text: 'Notes are in [Roadmap](/pano/d1)', createdAt: at(1) }]
  panoDocs = undefined
  recordTelemetry.mockReset()
  cacheTask.mockClear()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe('TaskDetailView and the task links', () => {
  const summary = (el) => el.querySelector('[data-testid="task-links-summary"]')?.textContent

  it('lists the links of the description and the messages, pinned and under the plan, and counts a followed one', async () => {
    const el = await mount()
    const [bar, pinned] = el.querySelectorAll('[data-testid="task-links"]')
    expect(bar.hasAttribute('data-pinned')).toBe(false)
    expect(bar.className).toContain('@min-[48rem]:hidden')
    expect(pinned.getAttribute('data-pinned')).toBe('true')
    expect(pinned.className).toContain('@min-[48rem]:flex')
    // It floats over the conversation, which keeps its full width.
    expect(pinned.className).toContain('absolute')
    expect(pinned.parentElement.className).toContain('relative')
    expect(summary(el)).toBe('· gh#12 · Roadmap')

    const pr = pinned.querySelector('[data-testid="task-links-pr"] a')
    expect(pr.textContent).toContain('gh#12')
    pr.addEventListener('click', (e) => e.preventDefault())
    pr.click()
    expect(recordTelemetry).toHaveBeenCalledWith('ui_task_link_open', 'ws1')
  })

  it('picks up a link from a message that arrives while the task is open', async () => {
    const el = await mount()
    messages = [...messages, { id: 'm2', sender: 'agent', text: 'Opened https://github.com/agentrq/agentrq/pull/13', createdAt: at(2) }]
    events.value = [...events.value, { type: 'task.updated', payload: task() }]
    await settle()
    expect(summary(el)).toBe('· gh#12 · gh#13 · Roadmap')
  })

  it('lists the Pano docs linked to the task, and those the agent linked by the next status change', async () => {
    panoDocs = [{ id: 'd1', title: 'Roadmap doc' }]
    const el = await mount()
    expect(summary(el)).toBe('· gh#12 · Roadmap doc')

    panoDocs = [{ id: 'd1', title: 'Roadmap doc' }, { id: 'd7', title: 'Release notes' }]
    events.value = [...events.value, { type: 'task.updated', payload: { ...task(), status: 'completed' } }]
    await settle()
    expect(summary(el)).toBe('· gh#12 · Roadmap doc · Release notes')

    // The bar under the plan, for a narrow view, opens to them too.
    const bar = el.querySelector('[data-testid="task-links"]')
    bar.querySelector('[data-testid="task-links-toggle"]').click()
    await settle()
    const doc = bar.querySelector('[data-testid="task-links-pano"] a')
    expect(doc.textContent).toContain('Roadmap doc')
    doc.click()
    expect(recordTelemetry).toHaveBeenCalledWith('ui_task_link_open', 'ws1')
  })
})
