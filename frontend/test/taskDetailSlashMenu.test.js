// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The reply box's `/` menu, mounted: the agent's own commands and the
 * workspace's skills in one list, the agent's winning a name both have.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h, ref } from 'vue'
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
  body: 'Fix the login page redirect',
  createdAt,
  messages: [],
  toolCalls: [],
})


vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true,
    agentCommands: { commands: [{ name: 'compact', description: 'Shorten the context' }, { name: 'review', description: 'agent review' }] } } }),
  searchWorkspaceSkills: () => Promise.resolve({ skills: [
    { name: 'release', description: 'Cut a release', workspaceEnabled: true },
    { name: 'review', description: 'skill review', workspaceEnabled: true },
    { name: 'hidden', workspaceEnabled: false },
  ] }),
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
const { useToasts } = await import('../src/composables/useToasts')

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
    composer: () => el.querySelector('form textarea'),
  }
}


const rows = (el) => [...el.querySelectorAll('[data-test=slash-menu] li')].map((li) => li.textContent.replace(/\s+/g, ' ').trim())

describe('the reply box\'s / menu', () => {
  it('lists the agent\'s commands, then the workspace\'s skills', async () => {
    const view = await mount()
    const input = view.composer()
    input.value = '/'
    input.dispatchEvent(new Event('input'))
    await settle()
    expect(rows(view.el)).toEqual([
      '/compactShorten the context',
      '/reviewagent review',
      '/releaseCut a releaseskill',
    ])
  })

  it('puts a chosen skill in with Tab, ready for what to do with it', async () => {
    const view = await mount()
    const input = view.composer()
    input.value = '/rel'
    input.dispatchEvent(new Event('input'))
    await settle()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Tab', bubbles: true, cancelable: true }))
    await settle()
    expect(input.value).toBe('/release ')
    expect(view.el.querySelector('[data-test=slash-menu]')).toBeNull()
  })
})
