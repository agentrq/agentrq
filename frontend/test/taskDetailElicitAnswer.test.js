// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * An answered elicitation, mounted for real: the coverage gate does not see
 * `.vue` files, and the promise — each question with its answer under it, and no
 * card around them — lives in the wiring.
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
    {
      id: 'm2', sender: 'agent', text: 'Which transport?', createdAt: '2026-09-22T07:01:00Z',
      metadata: {
        type: 'elicitation_request', mode: 'form', status: 'accept', requestId: 'r1',
        requestedSchema: { type: 'object', properties: { transport: { type: 'string', title: 'Transport' } } },
        content: { transport: 'streamable HTTP' },
      },
    },
  ],
  toolCalls: [],
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
let app

beforeEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
  localStorage.clear()
})

describe('an answered elicitation', () => {
  it('lists each question with its answer under it', async () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(TaskDetailView) })
    app.use(createPinia())
    app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
    app.directive('click-outside', {})
    app.mount(el)
    await settle()

    const question = [...el.querySelectorAll('li p')].find((p) => p.textContent.trim() === 'Transport')
    expect(question.nextElementSibling.textContent.trim()).toBe('streamable HTTP')
    expect(el.textContent).not.toContain('Input Requested')
  })
})
