// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A card's Button clicked in the thread, mounted: a reply's card posts the
 * click through the a2ui action route, a question asked as a card answers
 * through the elicitation route, and either card turns read-only with the
 * chosen Button marked once the server confirms it.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: { view: 'x' } }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() }),
}))

vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]), isConnected: ref(false) }),
}))

vi.mock('../src/composables/useSpeechToText', () => ({
  useSpeechToText: () => ({
    isRecording: ref(false), isTranscribing: ref(false), isModelLoading: ref(false), modelProgress: ref(0),
    error: ref(null), isSupported: ref(false), toggleRecording: vi.fn(),
  }),
}))

vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTask: vi.fn(),
  cacheTaskUpdate: (_cache, current, payload) => ({ ...current, ...payload }),
  sharedCache: () => null,
}))

const CATALOG = 'https://a2ui.org/specification/v1_0/catalogs/basic/catalog.json'
const surface = (id, components, dataModel) => [
  { version: '1.0', createSurface: { surfaceId: id, catalogId: CATALOG } },
  { version: '1.0', updateComponents: { surfaceId: id, components } },
  { version: '1.0', updateDataModel: { surfaceId: id, path: '/', value: dataModel } },
]

const REPLY_CARD = {
  type: 'a2ui', version: '1.0', surfaceId: 'pick',
  messages: surface('pick', [
    { id: 'root', component: 'Row', children: ['yes', 'no'] },
    { id: 'yes', component: 'Button', child: 'yes-t', variant: 'primary', action: { event: { name: 'approve', context: { pr: 1 } } } },
    { id: 'yes-t', component: 'Text', text: 'Approve' },
    { id: 'no', component: 'Button', child: 'no-t', action: { event: { name: 'reject', userMessage: 'No thanks' } } },
    { id: 'no-t', component: 'Text', text: 'Reject' },
  ], {}),
}

const QUESTION = {
  type: 'elicitation_request', mode: 'a2ui', status: 'pending', requestId: 'r1', version: '1.0', surfaceId: 'ask',
  expiresAt: '2999-01-01T00:00:00Z',
  messages: surface('ask', [
    { id: 'root', component: 'Column', children: ['name', 'send'] },
    { id: 'name', component: 'TextField', label: 'Branch name', value: { '@path': '/branch' } },
    { id: 'send', component: 'Button', child: 'send-t', action: { event: { name: 'create', context: { branch: { '@path': '/branch' } } } } },
    { id: 'send-t', component: 'Text', text: 'Create' },
  ], { branch: '' }),
}

let thread
const task = () => ({
  id: 't1', title: 'Ship it', status: 'ongoing', assignee: 'human', body: '',
  messages: [
    { id: 'm1', sender: 'human', text: 'do the thing', createdAt: '2026-11-25T07:00:00Z' },
    ...thread,
  ],
  toolCalls: [], attachments: [],
})

const sendA2uiAction = vi.fn()
const respondToElicitation = vi.fn()
let workspace

vi.mock('../src/api', async (importOriginal) => ({
  ...await importOriginal(),
  getWorkspace: () => Promise.resolve({ workspace }),
  getTask: () => Promise.resolve({ task: task() }),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  recordTelemetry: () => Promise.resolve(),
  listTaskToolCalls: () => Promise.resolve({ toolCalls: [] }),
  getTaskPlan: () => Promise.resolve(null),
  sendA2uiAction: (...a) => sendA2uiAction(...a),
  respondToElicitation: (...a) => respondToElicitation(...a),
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
  return el
}
const buttons = (el) => [...el.querySelectorAll('[data-test="a2ui-button"]')]
const byText = (el, text) => buttons(el).find((b) => b.textContent.trim() === text)
const lastToast = () => useToasts().toasts.value.at(-1)

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  sendA2uiAction.mockReset()
  respondToElicitation.mockReset()
  workspace = { id: 'ws1', name: 'Ops', agentConnected: true }
  thread = []
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
})

describe("a click on a reply's card", () => {
  beforeEach(() => {
    thread = [{ id: 'm2', sender: 'agent', text: 'Approve or reject?', createdAt: '2026-11-25T07:01:00Z', metadata: REPLY_CARD }]
  })

  it('posts the click as the reply, then marks the card answered', async () => {
    let resolve
    sendA2uiAction.mockImplementation(() => new Promise((r) => { resolve = r }))
    const el = await mount()
    expect(el.textContent).not.toContain('Approve or reject?')
    byText(el, 'Approve').click()
    await settle()
    expect(sendA2uiAction).toHaveBeenCalledWith('ws1', 't1', {
      messageId: 'm2', surfaceId: 'pick', name: 'approve', sourceComponentId: 'yes', context: { pr: 1 }, userMessage: undefined,
    })
    // While it is on its way, no second click goes out.
    expect(buttons(el).every((b) => b.disabled)).toBe(true)
    byText(el, 'Reject').click()
    expect(sendA2uiAction).toHaveBeenCalledTimes(1)
    resolve({ task: { id: 't1', messages: [...task().messages, { id: 'm3', sender: 'human', text: 'Approve', createdAt: '2026-11-25T07:02:00Z' }] } })
    await settle()
    expect(byText(el, 'Approve').dataset.chosen).toBe('true')
    expect(byText(el, 'Reject').disabled).toBe(true)
    expect(el.querySelector('[data-test="a2ui-surface"]').dataset.answered).toBe('true')
  })

  it("sends the button's own userMessage, and marks it answered when the answer carries no task", async () => {
    sendA2uiAction.mockResolvedValue({})
    const el = await mount()
    byText(el, 'Reject').click()
    await settle()
    expect(sendA2uiAction.mock.calls[0][2]).toMatchObject({ name: 'reject', userMessage: 'No thanks' })
    expect(byText(el, 'Reject').dataset.chosen).toBe('true')
  })

  it('closes a card answered elsewhere, saying so', async () => {
    sendA2uiAction.mockRejectedValue(Object.assign(new Error('this card is already answered'), { status: 409 }))
    const el = await mount()
    byText(el, 'Approve').click()
    await settle()
    expect(lastToast().message).toContain('this card is already answered')
    expect(buttons(el).every((b) => b.disabled)).toBe(true)
    expect(buttons(el).some((b) => b.dataset.chosen)).toBe(false)
  })

  it('keeps the card open after any other failure', async () => {
    sendA2uiAction.mockRejectedValue(Object.assign(new Error('rate limit exceeded'), { status: 429 }))
    const el = await mount()
    byText(el, 'Approve').click()
    await settle()
    expect(lastToast().message).toContain('rate limit exceeded')
    expect(byText(el, 'Approve').disabled).toBe(false)
  })

  it('takes no click in an archived workspace', async () => {
    workspace = { ...workspace, archivedAt: '2026-11-24T00:00:00Z' }
    const el = await mount()
    expect(buttons(el).every((b) => b.disabled)).toBe(true)
  })
})

describe('a question asked as a card', () => {
  beforeEach(() => {
    thread = [{ id: 'm2', sender: 'agent', text: 'Which branch?', createdAt: '2026-11-25T07:01:00Z', metadata: QUESTION }]
  })

  it('is drawn in place of its text, with none of the link controls', async () => {
    const el = await mount()
    expect(el.querySelector('[data-test="a2ui-surface"]')).not.toBeNull()
    expect(el.textContent).not.toContain('Which branch?')
    expect(el.textContent).not.toContain("I'm Done")
    expect(el.textContent).not.toContain('Input Requested')
  })

  it('answers where the agent waits, with what was entered, then shows it answered', async () => {
    respondToElicitation.mockResolvedValue({})
    const el = await mount()
    const input = el.querySelector('[data-test="a2ui-textfield"] input')
    input.value = 'feat/x'
    input.dispatchEvent(new Event('input'))
    await settle()
    byText(el, 'Create').click()
    await settle()
    expect(respondToElicitation).toHaveBeenCalledWith('ws1', 't1', 'r1', 'accept', {
      name: 'create', context: { branch: 'feat/x' }, dataModel: { branch: 'feat/x' },
    })
    expect(lastToast().message).toBe('Response sent')
    expect(byText(el, 'Create').dataset.chosen).toBe('true')
    expect(el.querySelector('[data-test="a2ui-textfield"] input').value).toBe('feat/x')
    expect(el.querySelector('[data-test="a2ui-textfield"] input').disabled).toBe(true)
    expect(el.textContent).toContain('Answered')
  })

  it('closes a question nobody waits for any more', async () => {
    respondToElicitation.mockRejectedValue(Object.assign(new Error('nobody is waiting'), { closed: true }))
    const el = await mount()
    byText(el, 'Create').click()
    await settle()
    expect(lastToast().message).toContain('nobody is waiting')
    expect(el.textContent).toContain('Cancelled')
    expect(byText(el, 'Create').disabled).toBe(true)
  })

  it('keeps the question open after any other failure', async () => {
    respondToElicitation.mockRejectedValue(new Error('offline'))
    const el = await mount()
    byText(el, 'Create').click()
    await settle()
    expect(byText(el, 'Create').disabled).toBe(false)
    expect(el.textContent).not.toContain('Cancelled')
  })

  it('past its deadline, is read-only and offers to dismiss it', async () => {
    thread = [{ id: 'm2', sender: 'agent', text: 'Which branch?', createdAt: '2026-11-25T07:01:00Z', metadata: { ...QUESTION, expiresAt: '2000-01-01T00:00:00Z' } }]
    const el = await mount()
    expect(byText(el, 'Create').disabled).toBe(true)
    expect(el.textContent).toContain('The agent stopped waiting for this answer')
    expect([...el.querySelectorAll('button')].some((b) => b.textContent.trim() === 'Dismiss')).toBe(true)
  })

  it('answered earlier, shows the answer on the card', async () => {
    thread = [{ id: 'm2', sender: 'agent', text: 'Which branch?', createdAt: '2026-11-25T07:01:00Z',
      metadata: { ...QUESTION, status: 'accept', content: { name: 'create', context: {}, dataModel: { branch: 'main' } } } }]
    const el = await mount()
    expect(el.querySelector('[data-test="a2ui-textfield"] input').value).toBe('main')
    expect(byText(el, 'Create').dataset.chosen).toBe('true')
    expect(el.textContent).toContain('Answered')
    expect(el.textContent).not.toContain('context')
  })
})
