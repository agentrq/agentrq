// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The toolbar under text selected in an agent's message, mounted for real:
 * Quote puts it in the reply as a markdown quote, Copy copies it, and each
 * is counted. Nothing is offered for a person's own message.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ path: '/workspaces/ws1/tasks/t1', params: { id: 'ws1', taskId: 't1' }, query: {} }),
  useRouter: () => ({ push: vi.fn(), back: vi.fn() }),
}))

const events = ref([])
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events, isConnected: ref(false) }),
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

const cacheTask = vi.fn()
vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTask: (...args) => cacheTask(...args),
  cacheTaskUpdate: (_cache, current, payload) => ({ ...current, ...payload }),
  sharedCache: () => null,
  readCachedTask: () => Promise.resolve(null),
}))

const at = (minute) => new Date(Date.UTC(2026, 9, 15, 10, minute)).toISOString()

let messages
let panoDocs
const task = () => ({
  id: 't1',
  title: 'Ship the fix',
  status: 'ongoing',
  assignee: 'agent',
  body: 'Fix the login',
  createdAt: at(0),
  messages,
  toolCalls: [],
  toolCallCount: 0,
})

const recordTelemetry = vi.fn()
vi.mock('../src/composables/useToasts', () => ({
  useToasts: () => ({ notifyError: vi.fn(), notifySuccess: vi.fn(), notifyInfo: vi.fn() }),
}))

let agentConnected
const respondToTask = vi.fn(() => Promise.resolve({}))
vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected } }),
  getTask: () => Promise.resolve({ task: task(), panoDocs }),
  listTaskToolCalls: () => Promise.resolve({ toolCalls: [] }),
  getTaskPlan: () => Promise.resolve(null),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: vi.fn(),
  respondToTask: (...args) => respondToTask(...args),
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
  serverOrigin: () => Promise.resolve(window.location.origin),
  recordTelemetry: (...args) => recordTelemetry(...args),
  TELEMETRY_UI_TASK_LINK_OPEN: 'ui_task_link_open',
  TELEMETRY_UI_SELECTION_QUOTE: 'ui_selection_quote',
  TELEMETRY_UI_SELECTION_COPY: 'ui_selection_copy',
  API_BASE_URL: '/api/v1',
}))

const { default: TaskDetailView } = await import('../src/views/TaskDetailView.vue')
const { usePlatformStore } = await import('../src/stores/platformStore')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
const mounted = []
let rangeRect
const writeText = vi.fn()

async function mount(os = '') {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(TaskDetailView) })
  mounted.push(app)
  const pinia = createPinia()
  app.use(pinia)
  if (os) usePlatformStore(pinia).setPlatform('desktop', os)
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  return el
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  events.value = []
  agentConnected = true
  messages = [
    { id: 'h1', sender: 'human', text: 'my question', createdAt: at(1) },
    { id: 'a1', sender: 'agent', text: 'first line\n\nsecond line', createdAt: at(2) },
  ]
  recordTelemetry.mockReset()
  writeText.mockReset()
  writeText.mockResolvedValue()
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  rangeRect = Object.getOwnPropertyDescriptor(Range.prototype, 'getBoundingClientRect')
  Range.prototype.getBoundingClientRect = () => ({ bottom: 40, left: 10, width: 20 })
})

afterEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.getSelection().removeAllRanges()
  if (rangeRect) Object.defineProperty(Range.prototype, 'getBoundingClientRect', rangeRect)
  else delete Range.prototype.getBoundingClientRect
  delete navigator.clipboard
})

/** The element holding a message's text, found by what it says. */
const byText = (el, text) => [...el.querySelectorAll('div, p')].reverse().find((n) => n.textContent.trim() === text)

/** Selects all of the node's text, as a drag across it would. */
async function select(node) {
  const range = document.createRange()
  range.selectNodeContents(node)
  const sel = document.getSelection()
  sel.removeAllRanges()
  sel.addRange(range)
  document.dispatchEvent(new Event('selectionchange'))
  await settle()
}
const selectAgent = (el) => select(el.querySelector('[data-quote-source]'))

const toolbar = (el) => el.querySelector('[data-test="selection-toolbar"]')
const composer = (el) => el.querySelector('form textarea')

describe('TaskDetailView: quoting and copying a selection', () => {
  it('quotes the selected text into the reply, after the draft, and counts it', async () => {
    const el = await mount()
    expect(toolbar(el)).toBeNull()

    composer(el).value = 'draft'
    composer(el).dispatchEvent(new Event('input'))
    await selectAgent(el)
    expect(toolbar(el)).not.toBeNull()

    el.querySelector('[data-test="selection-quote"]').click()
    await settle()
    expect(composer(el).value).toBe('draft\n> first line\n> second line\n\n')
    expect(document.activeElement).toBe(composer(el))
    expect(composer(el).selectionStart).toBe(composer(el).value.length)
    expect(toolbar(el)).toBeNull()
    expect(recordTelemetry).toHaveBeenCalledWith('ui_selection_quote', 'ws1')
  })

  it("uses Windows line endings on Windows", async () => {
    const el = await mount('win32')
    await selectAgent(el)
    el.querySelector('[data-test="selection-quote"]').click()
    await settle()
    // A text area shows CRLF as LF; what is sent is the draft itself.
    el.querySelector('form').dispatchEvent(new Event('submit'))
    await settle()
    expect(respondToTask).toHaveBeenCalledWith('ws1', 't1', 'text', '> first line\r\n> second line\r\n\r\n', expect.anything())
  })

  it('copies the selected text and counts it', async () => {
    const el = await mount()
    await selectAgent(el)
    el.querySelector('[data-test="selection-copy"]').click()
    await settle()
    expect(writeText).toHaveBeenCalledWith(expect.stringContaining('first line'))
    expect(recordTelemetry).toHaveBeenCalledWith('ui_selection_copy', 'ws1')
    expect(toolbar(el).querySelector('[data-test="selection-copied"]')).not.toBeNull()
  })

  it("offers nothing for the person's own message", async () => {
    const el = await mount()
    await select(byText(el, 'my question'))
    expect(toolbar(el)).toBeNull()
  })

  it('offers only Copy where there is no reply to quote into', async () => {
    agentConnected = false
    const el = await mount()
    await selectAgent(el)
    expect(toolbar(el)).not.toBeNull()
    expect(el.querySelector('[data-test="selection-quote"]')).toBeNull()
    expect(el.querySelector('[data-test="selection-copy"]')).not.toBeNull()
  })
})
