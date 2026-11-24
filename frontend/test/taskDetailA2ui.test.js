// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A thread with an agent's A2UI card, mounted: the card is drawn in place of
 * the message's markdown, an ordinary reply is not, and the message's MD
 * toggle shows the text every card carries.
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

const agentBubble = (el, text) => [...el.querySelectorAll('div.group')].find((d) => d.textContent.includes(text))

describe('an agent message with a card', () => {
  it('draws the card instead of the markdown', async () => {
    const el = await mount()
    const surface = el.querySelector('[data-test="a2ui-surface"]')
    expect(surface).not.toBeNull()
    expect(surface.querySelector('[data-test="a2ui-card"]').textContent.trim()).toBe('PR #1 opened')
    expect(el.textContent).not.toContain('Plain fallback text')
    expect(el.querySelectorAll('[data-test="a2ui-surface"]').length).toBe(1)
    expect(el.textContent).toContain('An ordinary reply')
  })

  it('shows the text under MD, and the card again after', async () => {
    const el = await mount()
    const bubble = agentBubble(el, 'PR #1 opened')
    const md = [...bubble.querySelectorAll('button')].find((b) => b.textContent.trim() === 'MD')
    md.click()
    await settle()
    expect(el.querySelector('[data-test="a2ui-surface"]')).toBeNull()
    expect(el.textContent).toContain('Plain **fallback** text')
    md.click()
    await settle()
    expect(el.querySelector('[data-test="a2ui-surface"]')).not.toBeNull()
  })
})
