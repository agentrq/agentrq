// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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

const recordTelemetry = vi.fn()
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
  listAccountPresets: () => Promise.resolve({ presets: [] }),
  listWorkspacePresets: () => Promise.resolve({ presets: [
    { name: 'pull-n-push', body: 'pull latest main, tag and push' },
    { name: 'deploy', body: 'deploy to staging' },
  ] }),
  recordTelemetry: (...a) => recordTelemetry(...a),
  TELEMETRY_UI_PRESET_INSERT: 'ui_preset_insert',
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

  it('offers the skills for a /name in the middle of the message, but not the agent\'s commands', async () => {
    const view = await mount()
    const input = view.composer()
    input.value = 'please /re and then push'
    input.setSelectionRange(10, 10)
    input.dispatchEvent(new Event('input'))
    await settle()
    expect(rows(view.el)).toEqual([
      '/releaseCut a releaseskill',
      '/reviewskill reviewskill',
    ])
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await settle()
    expect(input.value).toBe('please /release and then push')
    expect(input.selectionStart).toBe(16)
    expect(view.el.querySelector('[data-test=slash-menu]')).toBeNull()
  })

  it('puts a skill in where a row is clicked', async () => {
    const view = await mount()
    const input = view.composer()
    input.value = 'first /'
    input.dispatchEvent(new Event('input'))
    await settle()
    view.el.querySelector('[data-test=slash-menu] li').dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await settle()
    expect(input.value).toBe('first /release ')
  })
})

describe('the reply box\'s # menu', () => {
  it('lists the workspace\'s presets with their message, and puts the chosen one\'s message in', async () => {
    recordTelemetry.mockClear()
    const view = await mount()
    const input = view.composer()
    input.value = 'then #'
    input.dispatchEvent(new Event('input'))
    await settle()
    expect(rows(view.el)).toEqual([
      '#pull-n-pushpull latest main, tag and pushWorkspace preset',
      '#deploydeploy to stagingWorkspace preset',
    ])
    input.value = 'then #pu'
    input.dispatchEvent(new Event('input'))
    await settle()
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await settle()
    expect(input.value).toBe('then pull latest main, tag and push')
    expect(view.el.querySelector('[data-test=slash-menu]')).toBeNull()
    expect(recordTelemetry).toHaveBeenCalledWith('ui_preset_insert', 'ws1')
  })

  it('highlights the row under the pointer, and puts a preset in with a click', async () => {
    const view = await mount()
    const input = view.composer()
    input.value = '#'
    input.dispatchEvent(new Event('input'))
    await settle()
    const second = view.el.querySelectorAll('[data-test=slash-menu] li')[1]
    second.dispatchEvent(new MouseEvent('mouseenter'))
    await settle()
    expect(second.getAttribute('aria-selected')).toBe('true')
    second.dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await settle()
    expect(input.value).toBe('deploy to staging')
  })

  it('puts a preset in with a click', async () => {
    const view = await mount()
    const input = view.composer()
    input.value = '#dep'
    input.dispatchEvent(new Event('input'))
    await settle()
    view.el.querySelector('[data-test=slash-menu] li').dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await settle()
    expect(input.value).toBe('deploy to staging')
  })
})

describe('pasting into the reply box', () => {
  it('attaches a screenshot from the clipboard', async () => {
    const view = await mount()
    const e = new Event('paste', { bubbles: true, cancelable: true })
    Object.defineProperty(e, 'clipboardData', { value: { types: ['Files'], files: [new File(['png'], 'image.png', { type: 'image/png' })] } })
    view.composer().dispatchEvent(e)
    await settle()
    expect(e.defaultPrevented).toBe(true)
    expect(view.el.textContent).toMatch(/pasted-\d{8}-\d{6}\.png/)
  })
})
