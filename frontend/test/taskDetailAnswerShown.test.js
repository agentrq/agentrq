// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Answering a question or a permission request, mounted for real: the card
 * shows the answer once the server confirms it, with no live event saying so.
 * The event bus here never delivers one.
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

let card
const task = () => ({
  id: 't1',
  title: 'Ship it',
  status: 'ongoing',
  assignee: 'agent',
  body: '',
  messages: [
    { id: 'm1', sender: 'human', text: 'do the thing', createdAt: '2026-10-03T04:00:00Z' },
    { id: 'm2', sender: 'agent', text: 'Over to you', createdAt: '2026-10-03T04:50:00Z', metadata: card },
  ],
  toolCalls: [],
})

const respondToElicitation = vi.fn()
const sendPermissionVerdict = vi.fn()
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
  sendPermissionVerdict: (...args) => sendPermissionVerdict(...args),
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
  sendPermissionVerdict.mockReset()
  useToasts().toasts.value.splice(0)
})

afterEach(() => {
  // The view's clock is still ticking; stop it before jsdom goes away.
  app?.unmount()
  app = null
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
const linkQuestion = () => ({ type: 'elicitation_request', mode: 'url', url: 'https://example.com/', status: 'pending', requestId: 'r1', expiresAt: '2999-01-01T00:00:00Z' })
const formQuestion = () => ({
  type: 'elicitation_request', mode: 'form', status: 'pending', requestId: 'r1', expiresAt: '2999-01-01T00:00:00Z',
  requestedSchema: { type: 'object', properties: { choice: { type: 'string', title: 'Reading', enum: ['A', 'B'] } } },
})

describe('answering a question', () => {
  it('shows a link question answered once the server takes "I\'m Done"', async () => {
    card = linkQuestion()
    respondToElicitation.mockResolvedValue({})
    const el = await mount()

    button(el, "I'm Done").click()
    await settle()
    expect(respondToElicitation).toHaveBeenCalledWith('ws1', 't1', 'r1', 'accept', undefined)
    expect(button(el, "I'm Done")).toBeUndefined()
    expect(el.textContent).toContain('Answered')
    expect(lastToast()).toBe('Response sent')
  })

  it('shows what a form was answered with', async () => {
    card = formQuestion()
    respondToElicitation.mockResolvedValue({})
    const el = await mount()

    const choice = [...el.querySelectorAll('input[type="radio"]')].find((i) => i.parentElement.textContent.includes('B'))
    choice.dispatchEvent(new Event('change'))
    await settle()
    button(el, 'Submit').click()
    await settle()
    expect(respondToElicitation).toHaveBeenCalledWith('ws1', 't1', 'r1', 'accept', { choice: 'B' })
    expect(button(el, 'Submit')).toBeUndefined()
    expect(el.textContent).toContain('Answered — B')
  })

  it('shows a declined question declined', async () => {
    card = formQuestion()
    respondToElicitation.mockResolvedValue({})
    const el = await mount()

    button(el, 'Decline').click()
    await settle()
    expect(button(el, 'Submit')).toBeUndefined()
    expect(el.textContent).toContain('Declined')
  })

  // Nobody was waiting any more, and the server closed the question instead.
  it('shows a question the server closed as cancelled', async () => {
    card = linkQuestion()
    respondToElicitation.mockRejectedValue(Object.assign(new Error('closed now'), { closed: true }))
    const el = await mount()

    button(el, "I'm Done").click()
    await settle()
    expect(button(el, "I'm Done")).toBeUndefined()
    expect(el.textContent).toContain('Cancelled')
    expect(lastToast()).toBe('Failed to send response: closed now')
  })

  it('keeps the buttons when the answer did not get through', async () => {
    card = linkQuestion()
    respondToElicitation.mockRejectedValue(Object.assign(new Error('This request has expired'), { closed: false }))
    const el = await mount()

    button(el, "I'm Done").click()
    await settle()
    expect(button(el, "I'm Done")).toBeDefined()
    expect(el.textContent).not.toContain('Answered')
  })
})

describe('answering a permission request', () => {
  const permission = () => ({ type: 'permission_request', status: 'pending', requestId: 'p1', toolName: 'Bash' })

  // A resolved permission request is read in the trajectory, so it leaves the thread.
  it('takes the request out of the thread once the server takes the verdict', async () => {
    card = permission()
    sendPermissionVerdict.mockResolvedValue({})
    const el = await mount()

    button(el, 'Allow Once').click()
    await settle()
    expect(sendPermissionVerdict).toHaveBeenCalledWith('ws1', 't1', 'p1', 'allow')
    expect(button(el, 'Allow Once')).toBeUndefined()
    expect(el.textContent).not.toContain('Authorization Required')
  })

  it('keeps the request when the verdict did not get through', async () => {
    card = permission()
    sendPermissionVerdict.mockRejectedValue(new Error('session gone'))
    const el = await mount()

    button(el, 'Deny').click()
    await settle()
    expect(button(el, 'Deny')).toBeDefined()
    expect(lastToast()).toBe('Failed to send verdict: session gone')
  })
})
