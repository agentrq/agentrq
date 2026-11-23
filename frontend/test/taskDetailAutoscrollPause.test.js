// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Pausing autoscroll from the chat minimap, mounted for real: while paused a
 * new message leaves the chat where it is, resuming catches up, and a pause
 * is counted.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'
import { resetAutoscrollPauses } from '../src/composables/useChatMinimap'

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
  body: 'Fix the login',
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
  TELEMETRY_UI_AUTOSCROLL_PAUSE: 'ui_autoscroll_pause',
  API_BASE_URL: '/api/v1',
}))

const { default: TaskDetailView } = await import('../src/views/TaskDetailView.vue')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
const mounted = []

// jsdom lays nothing out: every element is a 300px pane over 2000px of
// content, so the chat overflows and the minimap shows, and scrollTop keeps
// what is set.
const saved = {}
const props = {
  clientHeight: { get: () => 300 },
  scrollHeight: { get: () => 2000 },
  scrollTop: { get() { return this._scrollTop || 0 }, set(v) { this._scrollTop = v } },
}

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
  resetAutoscrollPauses()
  events.value = []
  messages = [1, 2, 3].flatMap((i) => [
    { id: `h${i}`, sender: 'human', text: `question ${i}`, createdAt: at(2 * i) },
    { id: `a${i}`, sender: 'agent', text: `answer ${i}`, createdAt: at(2 * i + 1) },
  ])
  recordTelemetry.mockReset()
  for (const [name, desc] of Object.entries(props)) {
    saved[name] = Object.getOwnPropertyDescriptor(Element.prototype, name)
    Object.defineProperty(Element.prototype, name, { configurable: true, ...desc })
  }
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
  for (const [name, desc] of Object.entries(saved)) Object.defineProperty(Element.prototype, name, desc)
})

describe('TaskDetailView and pausing autoscroll', () => {
  const toggle = (el) => el.querySelector('[data-testid="chat-minimap-autoscroll"]')
  const pane = (el) => el.querySelector('[data-minimap-id]').closest('.overflow-y-auto')
  const arrive = async (n) => {
    messages = [...messages, { id: `a${n}`, sender: 'agent', text: `more ${n}`, createdAt: at(20 + n) }]
    events.value = [...events.value, { type: 'task.updated', payload: task() }]
    await settle()
  }

  it('keeps the chat in place while paused, catches up on resume, and counts the pause', async () => {
    const el = await mount()
    expect(toggle(el).getAttribute('aria-pressed')).toBe('false')
    expect(toggle(el).title).toBe('Pause autoscroll')

    // Following along, a new message pulls the chat down.
    pane(el).scrollTop = 100
    await arrive(4)
    expect(pane(el).scrollTop).toBe(2000)

    pane(el).scrollTop = 100
    toggle(el).click()
    await settle()
    expect(toggle(el).getAttribute('aria-pressed')).toBe('true')
    expect(toggle(el).title).toBe('Resume autoscroll')
    expect(recordTelemetry).toHaveBeenCalledWith('ui_autoscroll_pause', 'ws1')

    await arrive(5)
    expect(pane(el).scrollTop).toBe(100)

    toggle(el).click()
    await settle()
    expect(toggle(el).getAttribute('aria-pressed')).toBe('false')
    expect(pane(el).scrollTop).toBe(2000)
    expect(recordTelemetry).toHaveBeenCalledTimes(1)
  })
})
