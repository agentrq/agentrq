// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The attachment preview's Download, Open in browser and Copy link buttons,
 * mounted for real:
 * the coverage gate does not see `.vue` files. Download follows an attachment's
 * public link when there is one, and the signed-in route otherwise; Open in
 * browser and Copy link offer only a public link.
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
  attachments: [
    { id: 'a1', filename: 'shot.png', mimeType: 'image/png', url: 'https://agentrq.example/storage/artifacts/w-1/t1/a1' },
    { id: 'a2', filename: 'old.txt', mimeType: 'text/plain' },
  ],
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

describe('the Download button', () => {
  it('follows the public link, in a new tab', async () => {
    const link = await open(await mount(), 'shot.png')
    expect(link.getAttribute('href')).toBe('https://agentrq.example/storage/artifacts/w-1/t1/a1')
    expect(link.getAttribute('target')).toBe('_blank')
    expect(link.getAttribute('rel')).toBe('noopener noreferrer')
  })

  it('uses the signed-in route for an attachment with no link', async () => {
    const link = await open(await mount(), 'old.txt')
    expect(link.getAttribute('href')).toBe('/api/v1/workspaces/ws1/tasks/t1/attachments/a2')
    expect(link.hasAttribute('target')).toBe(false)
    expect(link.getAttribute('download')).toBe('old.txt')
  })
})

describe('clicking Download', () => {
  it('saves from the signed-in route under the filename and mimeType', async () => {
    const createObjectURL = vi.fn(() => 'blob:app/1')
    Object.assign(URL, { createObjectURL, revokeObjectURL: vi.fn() })
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockImplementation(() =>
      Promise.resolve({ ok: true, blob: () => Promise.resolve(new Blob(['png'])) }))
    const saved = []
    const click = vi.spyOn(HTMLAnchorElement.prototype, 'click')
    const link = await open(await mount(), 'shot.png')
    click.mockImplementation(function () { saved.push({ href: this.getAttribute('href'), download: this.download }) })

    link.dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await settle()

    expect(fetchSpy).toHaveBeenCalledWith('/api/v1/workspaces/ws1/tasks/t1/attachments/a1')
    expect(createObjectURL.mock.calls[0][0].type).toBe('image/png')
    expect(saved).toEqual([{ href: 'blob:app/1', download: 'shot.png' }])
    fetchSpy.mockRestore()
    click.mockRestore()
  })
})

describe('the Open in browser link', () => {
  const link = (el) => el.querySelector('a[aria-label="Open in browser"]')

  it('opens the public link in a new tab', async () => {
    const el = await mount()
    await open(el, 'shot.png')
    expect(link(el).getAttribute('href')).toBe('https://agentrq.example/storage/artifacts/w-1/t1/a1')
    expect(link(el).getAttribute('target')).toBe('_blank')
    expect(link(el).getAttribute('rel')).toBe('noopener noreferrer')
    expect(link(el).hasAttribute('download')).toBe(false)
  })

  it('is not offered for an attachment with no public link', async () => {
    const el = await mount()
    await open(el, 'old.txt')
    expect(link(el)).toBeNull()
  })
})

describe('the Copy link button', () => {
  const button = (el) => [...el.querySelectorAll('button')].find((b) => /Copy link|Copied/.test(b.textContent))

  it('puts the public link on the clipboard and says so', async () => {
    const writeText = vi.fn(() => Promise.resolve())
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const el = await mount()
    await open(el, 'shot.png')
    button(el).click()
    await settle()
    expect(writeText).toHaveBeenCalledWith('https://agentrq.example/storage/artifacts/w-1/t1/a1')
    expect(button(el).textContent.trim()).toBe('Copied')
    expect(recordTelemetry).toHaveBeenCalledWith('ui.copy_link', 'ws1')
  })

  it('stays as it was when the copy fails', async () => {
    const writeText = vi.fn(() => Promise.reject(new Error('not focused')))
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
    const el = await mount()
    await open(el, 'shot.png')
    button(el).click()
    await settle()
    expect(button(el).textContent.trim()).toBe('Copy link')
  })

  it('is not offered for an attachment with no public link', async () => {
    const el = await mount()
    await open(el, 'old.txt')
    expect(button(el)).toBeUndefined()
  })
})

describe('Escape', () => {
  const preview = () => document.querySelector('.fixed.inset-0.z-\\[110\\]')
  const escape = (target = document.body) => {
    const event = new KeyboardEvent('keydown', { key: 'Escape', bubbles: true, cancelable: true })
    target.dispatchEvent(event)
    return event
  }

  it('closes the attachment preview, wherever focus is', async () => {
    const el = await mount()
    await open(el, 'shot.png')
    expect(preview()).not.toBeNull()
    expect(escape().defaultPrevented).toBe(true)
    await settle()
    expect(preview()).toBeNull()
  })

  it('is left to the rest of the page when no preview is open', async () => {
    await mount()
    expect(escape().defaultPrevented).toBe(false)
  })
})
