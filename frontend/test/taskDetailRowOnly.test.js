// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A save answers with the task row only — no messages, no tool calls — so the
 * page must merge the answer, not take it whole, or the chat goes blank.
 * Mounted for real, with the real merge.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h, ref, nextTick } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() }),
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

const createdAt = new Date().toISOString()
const row = (overrides = {}) => ({
  id: 't1',
  workspaceId: 'ws1',
  title: 'Run the migration',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Migrate the users table',
  allowAllCommands: false,
  createdAt,
  messages: [],
  toolCalls: [],
  ...overrides,
})
const full = () => row({
  messages: [{ id: 'm1', sender: 'agent', text: 'Users table migrated', createdAt }],
  toolCalls: [{ id: 'c1', taskId: 't1', toolName: 'Bash', status: 'auto_allowed', createdAt }],
})

const updateTaskStatus = vi.fn()
const updateTaskAssignee = vi.fn()
const updateTaskAllowAllCommands = vi.fn()
const updateTaskTitle = vi.fn()

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: full() }),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: vi.fn(),
  respondToTask: vi.fn(),
  getAttachmentUrl: () => '',
  getWorkspaceToken: vi.fn(),
  archiveWorkspace: vi.fn(),
  unarchiveWorkspace: vi.fn(),
  updateWorkspace: vi.fn(),
  updateTaskStatus: (...a) => updateTaskStatus(...a),
  updateTaskAssignee: (...a) => updateTaskAssignee(...a),
  updateTaskTitle: (...a) => updateTaskTitle(...a),
  sendPermissionVerdict: vi.fn(),
  respondToElicitation: vi.fn(),
  stopTask: vi.fn(),
  updateTaskAllowAllCommands: (...a) => updateTaskAllowAllCommands(...a),
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
  return el
}

const buttonNamed = (el, name) => [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === name)

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  for (const fn of [updateTaskStatus, updateTaskAssignee, updateTaskAllowAllCommands, updateTaskTitle]) fn.mockReset()
})

describe('a save that answers with the task row only', () => {
  it('keeps the chat after a status change', async () => {
    updateTaskStatus.mockResolvedValue({ task: row({ status: 'completed' }) })
    const el = await mount()
    expect(el.textContent).toContain('Users table migrated')

    buttonNamed(el, 'ongoing').click()
    await nextTick()
    el.querySelector('button[title="completed"]').click()
    await settle()

    expect(updateTaskStatus).toHaveBeenCalledWith('ws1', 't1', 'completed')
    expect(el.textContent).toContain('Users table migrated')
  })

  it('keeps the chat after a reassignment', async () => {
    updateTaskAssignee.mockResolvedValue({ task: row({ assignee: 'human' }) })
    const el = await mount()

    buttonNamed(el, 'Human').click()
    await settle()

    expect(updateTaskAssignee).toHaveBeenCalledWith('ws1', 't1', 'human')
    expect(el.textContent).toContain('Users table migrated')
  })

  it('keeps the chat after YOLO is switched on', async () => {
    updateTaskAllowAllCommands.mockResolvedValue({ task: row({ allowAllCommands: true }) })
    const el = await mount()

    buttonNamed(el, 'YOLO').click()
    await settle()

    expect(updateTaskAllowAllCommands).toHaveBeenCalledWith('ws1', 't1', true)
    expect(buttonNamed(el, 'YOLO').className).toContain('bg-gray-600')
    expect(el.textContent).toContain('Users table migrated')
  })

  it('keeps the chat after a rename', async () => {
    updateTaskTitle.mockResolvedValue({ task: row({ title: 'Migrate users' }) })
    const el = await mount()

    el.querySelector('[data-test="task-title"]').click()
    await nextTick()
    const input = el.querySelector('[data-test="task-title-input"]')
    input.value = 'Migrate users'
    input.dispatchEvent(new Event('input'))
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true }))
    await settle()

    expect(el.querySelector('[data-test="task-title"]').textContent.trim()).toBe('Migrate users')
    expect(el.textContent).toContain('Users table migrated')
  })
})
