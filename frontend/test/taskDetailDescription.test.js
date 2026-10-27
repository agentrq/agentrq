// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The task's description, opened above the conversation. The header holding it
 * cannot shrink, so the description scrolls inside a capped box: uncapped, a
 * long one squeezed the conversation to a sliver, and replies that arrived
 * while the task was open landed out of sight.
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
  body: 'Fix the login page redirect.\n\n' + 'A long brief. '.repeat(400),
  createdAt,
  messages: [],
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
  return {
    el,
    description: () => el.querySelector('[data-test="task-description-body"]'),
  }
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
})

describe('the task description', () => {
  it('opens in a capped box that scrolls, leaving the conversation its room', async () => {
    const page = await mount()
    expect(page.description()).toBeNull()

    page.el.querySelector('.truncate.cursor-pointer').click()
    await nextTick()

    const box = page.description()
    expect(box.textContent).toContain('A long brief.')
    expect(box.classList).toContain('max-h-[30vh]')
    expect(box.classList).toContain('overflow-y-auto')
  })
})

describe('the task view', () => {
  it('clips sideways without being a scroll container', async () => {
    const page = await mount()
    const root = page.el.querySelector('.\\@container')
    // A scroll container here makes WebKitGTK repaint the view on every frame
    // of the timeline's ping: ~45% of a core beside the desktop side panel.
    expect(root.classList).toContain('supports-[overflow:clip]:overflow-x-clip')
    expect(root.classList).toContain('min-h-0')
  })
})
