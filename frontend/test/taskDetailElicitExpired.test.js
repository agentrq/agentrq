// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A question nobody waits for any more, mounted for real: its buttons could
 * only fail, so past its deadline the card offers Dismiss instead.
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

let elicitation
const task = () => ({
  id: 't1',
  title: 'Ship it',
  status: 'ongoing',
  assignee: 'agent',
  body: '',
  messages: [
    { id: 'm1', sender: 'human', text: 'do the thing', createdAt: '2026-10-03T04:00:00Z' },
    {
      id: 'm2', sender: 'agent', text: 'Which reading?', createdAt: '2026-10-03T04:50:00Z',
      metadata: {
        type: 'elicitation_request', mode: 'form', status: 'pending', requestId: 'r1',
        requestedSchema: { type: 'object', properties: { choice: { type: 'string', enum: ['A', 'B'] } } },
        ...elicitation,
      },
    },
  ],
  toolCalls: [],
})

const respondToElicitation = vi.fn()
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
  respondToElicitation: (...args) => respondToElicitation(...args),
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
let app

beforeEach(() => {
  app?.unmount()
  app = null
  document.body.innerHTML = ''
  localStorage.clear()
  respondToElicitation.mockReset()
  useToasts().toasts.value.splice(0)
  vi.useFakeTimers({ toFake: ['Date', 'setInterval', 'clearInterval'] })
  vi.setSystemTime(new Date('2026-10-03T05:00:00Z'))
})

afterEach(() => {
  // The view's clock is still ticking; stop it before jsdom goes away.
  app?.unmount()
  app = null
  vi.useRealTimers()
})

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(TaskDetailView) })
  app.use(createPinia())
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  return el
}

const button = (el, label) => [...el.querySelectorAll('button')].find((b) => b.textContent.trim() === label)
const lastToast = () => useToasts().toasts.value.at(-1)?.message

describe('a question past its deadline', () => {
  it('offers Dismiss instead of the form, and dismissing closes it', async () => {
    elicitation = { expiresAt: '2026-10-03T04:59:00Z' }
    respondToElicitation.mockResolvedValue({})
    const el = await mount()

    expect(el.textContent).toContain('The agent stopped waiting for this answer')
    expect(button(el, 'Submit')).toBeUndefined()
    expect(button(el, 'Decline')).toBeUndefined()

    button(el, 'Dismiss').click()
    await settle()
    expect(respondToElicitation).toHaveBeenCalledWith('ws1', 't1', 'r1', 'cancel', undefined)
  })

  // The usual case: nobody was waiting, so the server closed it instead.
  it('counts a question the server closed as dismissed', async () => {
    elicitation = { expiresAt: '2026-10-03T04:59:00Z' }
    respondToElicitation.mockRejectedValue(Object.assign(new Error('closed now'), { closed: true }))
    const el = await mount()

    button(el, 'Dismiss').click()
    await settle()
    expect(lastToast()).toBe('Dismissed')
  })

  it('reports why when it could not be dismissed', async () => {
    elicitation = { expiresAt: '2026-10-03T04:59:00Z' }
    respondToElicitation.mockRejectedValue(Object.assign(new Error('This request has expired'), { closed: false }))
    const el = await mount()

    button(el, 'Dismiss').click()
    await settle()
    expect(lastToast()).toBe('Failed to send response: This request has expired')
  })
})

describe('a question still within its deadline', () => {
  it('keeps its form until the deadline passes, without a reload', async () => {
    elicitation = { expiresAt: '2026-10-03T05:00:10Z' }
    const el = await mount()
    expect(button(el, 'Submit')).toBeDefined()
    expect(button(el, 'Dismiss')).toBeUndefined()

    vi.advanceTimersByTime(15000)
    await settle()
    expect(button(el, 'Submit')).toBeUndefined()
    expect(button(el, 'Dismiss')).toBeDefined()
  })

  it('shows the reason an answer failed', async () => {
    elicitation = { expiresAt: '2026-10-03T05:30:00Z' }
    respondToElicitation.mockRejectedValue(Object.assign(new Error('This request has expired'), { closed: false }))
    const el = await mount()

    button(el, 'Decline').click()
    await settle()
    expect(lastToast()).toBe('Failed to send response: This request has expired')
  })
})
