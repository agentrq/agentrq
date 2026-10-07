// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Emoji reactions in the task thread, mounted for real: the coverage gate does
 * not see `.vue` files, and the promise here lives in the wiring — which
 * message a badge hangs on, what a quick reaction sends, and where the
 * composer's emoji go.
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

let messages = []
let connected = true

const task = () => ({
  id: 't1',
  title: 'Ship it',
  status: 'completed',
  assignee: 'agent',
  body: '',
  messages,
  toolCalls: [],
})

const respondToTask = vi.fn()
const recordTelemetry = vi.fn()

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: connected } }),
  getTask: () => Promise.resolve({ task: task() }),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  forkTask: vi.fn(),
  respondToTask: (...args) => respondToTask(...args),
  recordTelemetry: (...args) => recordTelemetry(...args),
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
  TELEMETRY_UI_MESSAGE_REACT: 'ui_message_react',
  API_BASE_URL: '/api/v1',
}))

const { default: TaskDetailView } = await import('../src/views/TaskDetailView.vue')
const { useToasts } = await import('../src/composables/useToasts')
const { usePlatformStore } = await import('../src/stores/platformStore')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
const mounted = []

async function mount({ desktop = false } = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(TaskDetailView) })
  mounted.push(app)
  const pinia = createPinia()
  app.use(pinia)
  if (desktop) usePlatformStore(pinia).setPlatform('desktop', 'darwin')
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  const badges = () => [...el.querySelectorAll('[role="img"][aria-label*="reacted"]')].map((b) => b.getAttribute('aria-label'))
  const reactButtons = () => [...el.querySelectorAll('button[aria-label="React"]')]
  const composerEmoji = () => el.querySelector('button[aria-label="Emoji"]')
  // Matched by attribute rather than selector: jsdom's selector engine does
  // not match an emoji inside an attribute value.
  const labelled = (label) => [...el.querySelectorAll('[aria-label]')].filter((n) => n.getAttribute('aria-label') === label)
  const pick = (label) => labelled(label).at(-1)
  return { el, labelled, badges, reactButtons, composerEmoji, pick, textarea: () => el.querySelector('textarea') }
}

const human = (id, text, minute) => ({ id, sender: 'human', text, createdAt: `2026-09-22T07:0${minute}:00Z` })
const agent = (id, text, minute) => ({ id, sender: 'agent', text, createdAt: `2026-09-22T07:0${minute}:00Z` })

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  respondToTask.mockReset()
  respondToTask.mockImplementation(() => Promise.resolve({ task: task() }))
  recordTelemetry.mockReset()
  delete window.agentrq
  useToasts().toasts.value.splice(0)
  messages = [human('m1', 'do the thing', 0), agent('m2', 'done', 1)]
  connected = true
})

describe('a reply of one emoji', () => {
  it('is drawn as a badge on the message it answers, from either side', async () => {
    messages.push(human('m3', '👍', 2), agent('m4', '✅', 3))
    const { el, badges } = await mount()

    expect(badges()).toEqual(['Agent reacted ✅', 'You reacted 👍'])
    // Neither reaction is a bubble of its own.
    expect(el.textContent).not.toMatch(/You · .*You ·/s)
    expect([...el.querySelectorAll('span')].filter((s) => /^Agent · /.test(s.textContent))).toHaveLength(1)
  })

  it("sits at the bottom right of the agent's message, and on the edge of the person's bubble", async () => {
    messages.push(human('m3', '👍', 2), agent('m4', '✅', 3))
    const { labelled } = await mount()
    // Bottom right is where people look for a reaction. The agent's message
    // has no bubble to overlap, so its badge tucks up into the last line's
    // leading.
    const row = (label) => labelled(label)[0].parentElement.parentElement
    expect([...row('You reacted 👍').classList]).toEqual(expect.arrayContaining(['justify-end', '-mt-1']))
    // Every pill is filled, with no border, on the page and on a bubble.
    for (const label of ['You reacted 👍', 'Agent reacted ✅']) {
      const pill = labelled(label)[0].parentElement.classList
      expect(pill).toContain('bg-gray-200')
      expect(pill).not.toContain('border')
    }
    expect([...row('Agent reacted ✅').classList]).toEqual(expect.arrayContaining(['justify-end', '-mt-2']))
  })

  it('counts the same emoji sent twice', async () => {
    messages.push(human('m3', '👍', 2), human('m4', '👍', 3))
    const { labelled, badges } = await mount()
    expect(badges()).toEqual(['You reacted 👍'])
    expect(labelled('You reacted 👍')[0].textContent).toBe('👍2')
  })
})

describe('the react button', () => {
  it("is only on the agent's latest message, where a reaction would land", async () => {
    messages.push(human('m3', 'again', 2), agent('m4', 'done again', 3))
    const { reactButtons } = await mount()
    expect(reactButtons()).toHaveLength(1)
  })

  it('sends the emoji picked as a reply and counts it', async () => {
    const { reactButtons, pick } = await mount()
    reactButtons()[0].click()
    await settle()
    pick('React 🎉').click()
    await settle()

    expect(respondToTask).toHaveBeenCalledWith('ws1', 't1', 'text', '🎉', [])
    expect(recordTelemetry).toHaveBeenCalledWith('ui_message_react', 'ws1')
    expect(reactButtons()).toHaveLength(1)
  })

  it('is not offered while the agent is offline, as nothing could be sent', async () => {
    connected = false
    const { reactButtons } = await mount()
    expect(reactButtons()).toHaveLength(0)
  })
})

describe('the composer emoji button', () => {
  it('puts a quick pick in at the cursor', async () => {
    const { composerEmoji, pick, textarea } = await mount()
    composerEmoji().click()
    await settle()
    pick('React 😂').click()
    await settle()

    expect(textarea().value).toBe('😂')
    expect(respondToTask).not.toHaveBeenCalled()
  })

  it('says how to open the system keyboard in a browser, which cannot', async () => {
    const { composerEmoji, pick } = await mount()
    composerEmoji().click()
    await settle()
    pick('All emoji').click()
    await settle()

    expect(useToasts().toasts.value.at(-1).message).toMatch(/emoji/)
  })

  it("opens the desktop shell's emoji panel where it has one", async () => {
    const showPanel = vi.fn(async () => true)
    window.agentrq = { emoji: { showPanel } }
    const { composerEmoji, pick } = await mount({ desktop: true })
    composerEmoji().click()
    await settle()
    pick('All emoji').click()
    await settle()

    expect(showPanel).toHaveBeenCalledOnce()
    expect(useToasts().toasts.value).toHaveLength(0)
  })
})
