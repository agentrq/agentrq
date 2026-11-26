// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A thread with an agent's progress card, mounted: it folds to its text and
 * back, and pins under the timeline, where it folds on its own and unpins;
 * both choices outlive a reload.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: { view: 'x' } }),
  useRouter: () => ({ push, back: vi.fn() }),
}))

vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]), isConnected: ref(false) }),
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

const CARD = {
  type: 'a2ui',
  version: '1.0',
  surfaceId: 'pr-1',
  messages: [
    { version: '1.0', createSurface: { surfaceId: 'pr-1', catalogId: 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json' } },
    { version: '1.0', updateComponents: { surfaceId: 'pr-1', components: [
      { id: 'root', component: 'Card', child: 'title' },
      { id: 'title', component: 'Text', text: { '@path': '/title' } },
    ] } },
    { version: '1.0', updateDataModel: { surfaceId: 'pr-1', path: '/', value: { title: 'PR #1 opened' } } },
  ],
}

const task = () => ({
  id: 't1',
  title: 'Ship it',
  status: 'completed',
  assignee: 'agent',
  body: '',
  messages: [
    { id: 'm1', sender: 'human', text: 'do the thing', createdAt: '2026-09-22T07:00:00Z' },
    { id: 'm2', sender: 'agent', text: 'Plain **fallback** text', createdAt: '2026-09-22T07:01:00Z', metadata: CARD },
    { id: 'm3', sender: 'agent', text: 'An ordinary reply', createdAt: '2026-09-22T07:02:00Z' },
    { id: 'm4', sender: 'agent', text: 'Which one?', createdAt: '2026-09-22T07:03:00Z', metadata: { ...CARD, type: 'elicitation_request', mode: 'a2ui', status: 'answered', surfaceId: 'q-1' } },
  ],
  toolCalls: [],
  attachments: [],
})

const forkTask = vi.fn()
const recordTelemetry = vi.fn(() => Promise.resolve())

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true } }),
  getTask: () => Promise.resolve({ task: task() }),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: (...args) => forkTask(...args),
  recordTelemetry: (...args) => recordTelemetry(...args),
  respondToTask: vi.fn(),
  getAttachmentUrl: (ws, task, id) => `/api/v1/workspaces/${ws}/tasks/${task}/attachments/${id}`,
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
  TELEMETRY_UI_COPY_LINK: 'ui.copy_link',
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

async function open(el, filename) {
  const chip = [...el.querySelectorAll('span')].find((s) => s.textContent.trim() === filename)
  chip.closest('div.cursor-pointer').click()
  await settle()
  return [...el.querySelectorAll('a')].find((a) => a.textContent.trim() === 'Download')
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})


const q = (el, sel) => el.querySelectorAll(`[data-test="${sel}"]`)
const click = async (node) => { node.click(); await settle() }

const threadCard = (el) => el.querySelector('[data-test="a2ui-surface"][data-surface-id="pr-1"]:not([data-test="pinned-card"] *)')

describe('a progress card', () => {
  it('offers pin and collapse on a reply card only', async () => {
    const el = await mount()
    expect(q(el, 'card-pin').length).toBe(1)
    expect(q(el, 'card-collapse').length).toBe(1)
    expect(q(el, 'pinned-card').length).toBe(0)
    expect(q(el, 'card-pin')[0].getAttribute('aria-pressed')).toBe('false')
  })

  it('folds to its top and opens again, and remembers it', async () => {
    const el = await mount()
    await click(q(el, 'card-collapse')[0])
    expect(threadCard(el).dataset.collapsed).toBe('true')
    expect(q(el, 'card-collapse')[0].getAttribute('aria-expanded')).toBe('false')

    const again = await mount()
    expect(threadCard(again).dataset.collapsed).toBe('true')
    await click(q(again, 'card-collapse')[0])
    expect(threadCard(again).dataset.collapsed).toBeUndefined()
  })

  it('shows its text under MD with no collapse button', async () => {
    const el = await mount()
    const md = [...q(el, 'card-pin')[0].parentElement.querySelectorAll('button')].find((b) => b.textContent.trim() === 'MD')
    await click(md)
    expect(q(el, 'card-collapse').length).toBe(0)
    expect(q(el, 'card-pin').length).toBe(1)
  })

  it('pins under the timeline, folds there on its own, and unpins', async () => {
    const el = await mount()
    await click(q(el, 'card-pin')[0])
    const pinned = q(el, 'pinned-card')
    expect(pinned.length).toBe(1)
    expect(pinned[0].dataset.messageId).toBe('m2')
    expect(q(el, 'card-pin')[0].getAttribute('aria-pressed')).toBe('true')
    expect(pinned[0].textContent).toContain('PR #1 opened')
    // The thread's copy is still drawn, and the question's.
    expect(q(el, 'a2ui-surface').length).toBe(3)

    await click(q(el, 'pinned-card-toggle')[0])
    expect(q(el, 'pinned-card')[0].dataset.collapsed).toBe('true')
    expect(threadCard(el).dataset.collapsed).toBeUndefined()

    const again = await mount()
    expect(q(again, 'pinned-card').length).toBe(1)
    expect(q(again, 'pinned-card')[0].dataset.collapsed).toBe('true')
    await click(q(again, 'pinned-card-toggle')[0])
    expect(q(again, 'pinned-card')[0].dataset.collapsed).toBeUndefined()
    await click(q(again, 'pinned-card-unpin')[0])
    expect(q(again, 'pinned-card').length).toBe(0)
  })

  it('unpins from the message too', async () => {
    const el = await mount()
    await click(q(el, 'card-pin')[0])
    await click(q(el, 'card-pin')[0])
    expect(q(el, 'pinned-card').length).toBe(0)
  })
})
