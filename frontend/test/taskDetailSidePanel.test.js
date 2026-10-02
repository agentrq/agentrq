// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The side panel button in the task view's header, mounted for real: the
 * coverage gate does not see `.vue` files, and what matters here is that the
 * button is on the desktop only and drives the one shared panel.
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
const { usePlatformStore } = await import('../src/stores/platformStore')
const { resetSidePanelForTests, useSidePanel } = await import('../src/composables/useSidePanel')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
const mounted = []

async function mount(platform) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(TaskDetailView) })
  mounted.push(app)
  const pinia = createPinia()
  app.use(pinia)
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  usePlatformStore(pinia).setPlatform(platform, 'linux')
  app.mount(el)
  await settle()
  return { el, toggle: () => el.querySelector('[data-side-panel-toggle]') }
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  resetSidePanelForTests()
})

describe('the side panel button on a task', () => {
  it('is not in the browser, which has no side panel', async () => {
    const { toggle } = await mount('web')
    expect(toggle()).toBeNull()
  })

  it('opens the shared panel on the desktop, and is gone while it is open', async () => {
    const { toggle } = await mount('desktop')
    const panel = useSidePanel()

    toggle().click()
    await settle()
    expect(panel.state.open).toBe(true)
    // The panel's own close button is the way back; a second one here is noise.
    expect(toggle()).toBeNull()

    panel.close()
    await settle()
    expect(toggle()).not.toBeNull()
  })
})
