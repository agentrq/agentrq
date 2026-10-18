// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The task's plan, mounted for real: read with the task, kept current by the
 * stream, and a comment on a step sent as a message to the agent.
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
const getTaskPlan = vi.fn()
const respondToTask = vi.fn()
const recordTelemetry = vi.fn()
const notifyError = vi.fn()
vi.mock('../src/composables/useToasts', () => ({
  useToasts: () => ({ notifyError, notifySuccess: vi.fn(), notifyInfo: vi.fn() }),
}))

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: task() }),
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
  count = 250
  loaded = calls(51, 250).map((c, i) => ({ ...c, createdAt: new Date(Date.UTC(2026, 9, 15, 10, 50, i)).toISOString() }))
  listTaskToolCalls.mockReset()
  getTaskPlan.mockReset()
  respondToTask.mockReset()
  recordTelemetry.mockReset()
  cacheTask.mockClear()
  notifyError.mockClear()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe('TaskDetailView and the task plan', () => {
  const plan = (content, updatedAt = '2026-10-18T00:00:00Z') => ({ id: 't1', content, version: 1, createdAt: updatedAt, updatedAt })

  it('shows no plan for a task without one', async () => {
    getTaskPlan.mockResolvedValue(null)
    const el = await mount()
    expect(getTaskPlan).toHaveBeenCalledWith('ws1', 't1')
    expect(el.querySelector('[data-testid="task-plan"]')).toBeNull()
  })

  it('pins the plan, and follows its updates and withdrawal', async () => {
    getTaskPlan.mockResolvedValue(plan('- [ ] Read\n- [ ] Fix'))
    const el = await mount()
    const summary = () => el.querySelector('[data-testid="task-plan-summary"]')?.textContent
    expect(summary()).toContain('Read')

    events.value = [...events.value, { type: 'taskPlan.updated', payload: { workspaceId: 'ws1', taskId: 't1', plan: plan('- [x] Read\n- [ ] Fix', '2026-10-18T00:01:00Z') } }]
    await settle()
    expect(summary()).toContain('Fix')
    expect(el.querySelector('[data-testid="task-plan-progress"]').textContent.trim()).toBe('1 / 2')

    events.value = [...events.value, { type: 'taskPlan.updated', payload: { workspaceId: 'ws1', taskId: 't1', plan: null } }]
    await settle()
    expect(el.querySelector('[data-testid="task-plan"]')).toBeNull()
  })

  it('sends a comment on a step to the agent, quoting the step', async () => {
    getTaskPlan.mockResolvedValue(plan('- [ ] Read\n- [ ] Fix'))
    respondToTask.mockResolvedValue({ task: task() })
    const el = await mount()
    el.querySelector('[data-testid="task-plan-toggle"]').click()
    await settle()
    el.querySelectorAll('[data-testid="task-plan-comment"]')[1].click()
    await settle()
    const textarea = el.querySelector('[data-testid="task-plan-comment-form"] textarea')
    textarea.value = 'use the new helper'
    textarea.dispatchEvent(new Event('input'))
    el.querySelector('[data-testid="task-plan-comment-form"]').dispatchEvent(new Event('submit'))
    await settle()
    expect(respondToTask).toHaveBeenCalledWith('ws1', 't1', 'text', '> Plan step: Fix\n\nuse the new helper', [])
    expect(recordTelemetry).toHaveBeenCalledWith('ui_plan_comment', 'ws1')
  })
})
