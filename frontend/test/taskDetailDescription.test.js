// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The task's description, above the conversation. It opens while a task has
 * nothing else to read, and gets out of the way once it does: whichever way the
 * task was reached, and never carried over from the task before.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h, reactive, ref } from 'vue'
import { createPinia } from 'pinia'

const route = reactive({ path: '/workspaces/ws1/tasks/empty', params: { id: 'ws1', taskId: 'empty' }, query: {} })
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), back: vi.fn() }),
}))

const events = ref([])
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events }),
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

const createdAt = new Date().toISOString()
const message = (id, text) => ({ id, sender: 'agent', text, createdAt })
const task = (id, body, messages = []) => ({
  id, title: id, status: 'ongoing', assignee: 'agent', body, createdAt, messages, toolCalls: [],
})
const tasks = {
  empty: task('empty', 'The empty task\'s brief.'),
  talked: task('talked', 'The talked task\'s brief.', [message('m1', 'An earlier message.')]),
  talked2: task('talked2', 'The second talked task\'s brief.', [message('m2', 'Another earlier message.')]),
  bare: task('bare', ''),
  sent: { ...task('sent', 'The sent task\'s brief.'), agentNote: 'The note it was sent.' },
  queued: { ...task('queued', 'The queued task\'s brief.'), status: 'notstarted' },
  human: { ...task('human', 'The human task\'s brief.'), status: 'notstarted', assignee: 'human' },
  stored: { ...task('stored', 'The old brief.\n\nThe note it was sent.'), agentNote: 'The note it was sent.' },
  storedQueued: { ...task('storedQueued', 'The old brief.\n\nKeep me posted.'), status: 'notstarted' },
  old: task('old', 'Handed over before notes were recorded.'),
}
let workspaceNote = ''

vi.mock('../src/api', () => ({
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: true, selfLearningLoopNote: workspaceNote } }),
  getTask: (_ws, id) => Promise.resolve({ task: tasks[id] }),
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

function goTo(taskId) {
  route.path = `/workspaces/ws1/tasks/${taskId}`
  route.params = { id: 'ws1', taskId }
}

async function open(taskId) {
  goTo(taskId)
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
    title: () => el.querySelector('[data-test="task-title"]').textContent,
  }
}

async function switchTo(taskId) {
  goTo(taskId)
  await settle()
}

beforeEach(() => {
  while (mounted.length) mounted.pop().unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  events.value = []
  workspaceNote = ''
})

describe('the task description', () => {
  it('is open on a direct load of a task with no messages', async () => {
    const page = await open('empty')
    expect(page.description().textContent).toContain("The empty task's brief.")
  })

  it('is closed on a direct load of a task with messages, and opens on a click', async () => {
    const page = await open('talked')
    expect(page.description()).toBeNull()

    page.el.querySelector('.truncate.cursor-pointer').click()
    await settle()
    expect(page.description().textContent).toContain("The talked task's brief.")
  })

  it('is decided again for each task it switches to, never carried over', async () => {
    const page = await open('talked')
    page.el.querySelector('.truncate.cursor-pointer').click()
    await settle()
    expect(page.description()).not.toBeNull()

    // Opened on the last task, closed on the next one, which has messages.
    await switchTo('talked2')
    expect(page.title()).toBe('talked2')
    expect(page.description()).toBeNull()

    await switchTo('empty')
    expect(page.description().textContent).toContain("The empty task's brief.")

    // Opened on an empty task, closed on one with messages.
    await switchTo('talked')
    expect(page.description()).toBeNull()

    // A task with no description has nothing to open.
    await switchTo('bare')
    expect(page.title()).toBe('bare')
    expect(page.el.querySelector('.truncate.cursor-pointer')).toBeNull()
  })

  it('closes when the first reply arrives, and stays as the reader left it after that', async () => {
    const page = await open('empty')
    expect(page.description()).not.toBeNull()

    events.value.push({ type: 'reply.received', payload: { ...tasks.empty, messages: [message('m1', 'A live reply.')] } })
    await settle()
    expect(page.el.textContent).toContain('A live reply.')
    expect(page.description()).toBeNull()

    // Opened again by hand, a later reply leaves it open.
    page.el.querySelector('.truncate.cursor-pointer').click()
    await settle()
    events.value.push({ type: 'reply.received', payload: { ...tasks.empty, messages: [message('m1', 'A live reply.'), message('m2', 'Another.')] } })
    await settle()
    expect(page.el.textContent).toContain('Another.')
    expect(page.description()).not.toBeNull()
  })
})

describe("the workspace's self-learning note", () => {
  const note = () => document.querySelector('[data-test="task-agent-note"]')

  it('shows the note an agent task was sent, rendered and raw, whatever the workspace has now', async () => {
    workspaceNote = 'Edited since.'
    const page = await open('sent')
    expect(note().textContent).toContain('Self-learning loop')
    expect(note().textContent).not.toContain('not sent yet')
    expect(note().textContent).toContain('The note it was sent.')
    expect(note().textContent).not.toContain('Edited since.')

    page.el.querySelector('[title="Copy raw text"]').previousElementSibling.click()
    await settle()
    expect(note().textContent).toContain('The note it was sent.')
  })

  it("shows the workspace's note as not sent yet before the first hand-off", async () => {
    workspaceNote = 'Keep me posted.'
    await open('queued')
    expect(note().textContent).toContain('not sent yet')
    expect(note().textContent).toContain('Keep me posted.')
  })

  it('shows the note it was sent once the agent takes it', async () => {
    workspaceNote = 'Keep me posted.'
    await open('queued')
    expect(note().textContent).toContain('not sent yet')

    const taken = { ...tasks.queued, status: 'ongoing' }
    tasks.queued = { ...taken, agentNote: 'Keep me posted.' }
    // The stream carries no note; the read the status change makes does.
    events.value.push({ type: 'status.updated', payload: taken })
    await settle()
    expect(note().textContent).not.toContain('not sent yet')
    expect(note().textContent).toContain('Keep me posted.')
    tasks.queued = { ...taken, status: 'notstarted' }
  })

  it('is left out of a human task, a task handed over before notes were recorded, and a body that already has it', async () => {
    workspaceNote = 'Keep me posted.'
    const page = await open('human')
    expect(note()).toBeNull()

    for (const id of ['old', 'stored', 'storedQueued']) {
      await switchTo(id)
      expect(page.title()).toBe(id)
      expect(page.description()).not.toBeNull()
      expect(note()).toBeNull()
    }

    workspaceNote = ''
    while (mounted.length) mounted.pop().unmount()
    await open('queued')
    expect(note()).toBeNull()
  })
})

describe('the task view', () => {
  it('clips sideways without being a scroll container', async () => {
    const page = await open('talked')
    const root = page.el.querySelector('.\\@container')
    // A scroll container here makes WebKitGTK repaint the view on every frame
    // of the timeline's ping: ~45% of a core beside the desktop side panel.
    expect(root.classList).toContain('supports-[overflow:clip]:overflow-x-clip')
    expect(root.classList).toContain('min-h-0')
  })
})
