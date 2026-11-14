// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A teammate's task, opened in team mode (?team=): read through the team's
 * routes, with nothing on the page that writes to it, and a way to clone it.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
const replace = vi.fn()
const route = { path: '/tasks/active/tw1/t1', params: { filter: 'active', workspaceId: 'tw1', taskId: 't1' }, query: { team: 'tm1' } }
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push, replace, back: vi.fn() }),
}))

import { unref } from 'vue'
const connect = vi.fn()
const events = ref([])
let streamOf = null
vi.mock('../src/useEventBus', () => ({
  useEventBus: (workspaceId) => {
    streamOf = workspaceId
    return { connect, disconnect() {}, events }
  },
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
  cacheTask: (...a) => cacheTask(...a),
  cacheTaskUpdate: (_cache, current, payload) => ({ ...current, ...payload }),
  sharedCache: () => null,
}))

const bob = { userId: 'u2', name: 'Bob' }
const teamTask = () => ({
  task: {
    id: 't1',
    workspaceId: 'tw1',
    title: 'Fix the login page',
    status: 'blocked',
    assignee: 'human',
    createdBy: 'agent',
    body: 'Redirect loops',
    createdAt: new Date().toISOString(),
    messages: [{ id: 'm1', sender: 'agent', text: 'Allow this?', metadata: { type: 'permission_request', status: 'pending', requestId: 'r1' } }],
    toolCalls: [],
  },
  workspace: { id: 'tw1', name: 'api', sharedName: 'Backend' },
  owner: bob,
})

const getTeamTask = vi.fn()
const getTask = vi.fn()
const getTaskPlan = vi.fn()
const updateTaskTitle = vi.fn()
const updateTaskStatus = vi.fn()
const cloneTeamTask = vi.fn()

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getTeamTask: (...a) => getTeamTask(...a),
  getTask: (...a) => getTask(...a),
  getTaskPlan: (...a) => getTaskPlan(...a),
  getWorkspace: vi.fn(),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [{ id: 'm1', name: 'API' }] }),
  fetchTeamWorkspaces: () => Promise.resolve({ workspaces: [] }),
  listTeams: () => Promise.resolve({ teams: [] }),
  listMyInvitations: () => Promise.resolve({ invitations: [] }),
  updateTaskTitle: (...a) => updateTaskTitle(...a),
  updateTaskStatus: (...a) => updateTaskStatus(...a),
  cloneTeamTask: (...a) => cloneTeamTask(...a),
  recordTelemetry: () => {},
}))

const { default: TaskDetailView } = await import('../src/views/TaskDetailView.vue')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
let app

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

const buttons = (el, label) => [...el.querySelectorAll('button')].filter((b) => b.textContent.trim() === label)

beforeEach(() => {
  vi.clearAllMocks()
  events.value = []
  useToasts().toasts.value = []
  getTeamTask.mockImplementation(() => Promise.resolve(teamTask()))
})

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('a teammate\'s task in team mode', () => {
  it('is read through the team, never cached, and live on your account stream', async () => {
    const el = await mount()
    expect(getTeamTask).toHaveBeenCalledWith('tm1', 't1')
    expect(getTask).not.toHaveBeenCalled()
    expect(cacheTask).not.toHaveBeenCalled()
    expect(connect).toHaveBeenCalled()
    expect(unref(streamOf)).toBe('')
    expect(el.querySelector('[data-test="task-title"]').textContent.trim()).toBe('Fix the login page')
  })

  it('says whose it is, and offers no way to change it', async () => {
    const el = await mount()
    expect(el.querySelector('[data-test="team-task-owner"]').textContent).toContain('Bob')
    expect(el.querySelector('[data-test="team-task-owner"]').textContent).toContain('api')
    expect(el.querySelector('[data-test="team-task-status"]').textContent).toMatch(/blocked/i)
    expect(el.querySelector('textarea')).toBe(null)
    for (const label of ['Allow', 'Deny', 'Allow All']) expect(buttons(el, label)).toEqual([])

    el.querySelector('[data-test="task-title"]').click()
    await settle()
    expect(el.querySelector('[data-test="task-title-input"]')).toBe(null)
    expect(updateTaskTitle).not.toHaveBeenCalled()
    expect(updateTaskStatus).not.toHaveBeenCalled()
  })

  it('clones it into one of your workspaces, for a human unless you choose an agent', async () => {
    cloneTeamTask.mockResolvedValue({ task: { id: 'c1', workspaceId: 'm1' } })
    const el = await mount()
    const clone = el.querySelector('[data-test="clone-task"]')
    // Its words are hidden where the header is narrow; its name never is.
    expect(clone.getAttribute('aria-label')).toBe('Clone to my workspace')
    expect(clone.querySelector('[data-test="clone-icon"]')).not.toBe(null)
    expect(clone.querySelector('span.hidden').textContent).toBe('Clone to my workspace')
    clone.dispatchEvent(new MouseEvent('mouseenter'))
    clone.dispatchEvent(new MouseEvent('mouseleave'))

    clone.click()
    await settle()
    const modal = document.body.querySelector('[data-test="clone-modal"]')
    expect(modal.textContent).toContain('Fix the login page')
    expect(modal.querySelector('select')).toBe(null)
    expect(modal.querySelector('[data-test="clone-status"]')).toBe(null)
    expect(modal.querySelector('[data-test="clone-workspace"]').textContent).toContain('API')
    expect(modal.querySelector('[data-test="clone-assignee-human"]').getAttribute('aria-checked')).toBe('true')

    modal.querySelector('[data-test="clone-assignee-agent"]').click()
    await settle()
    expect(modal.querySelector('[data-test="clone-assignee-agent"]').getAttribute('aria-checked')).toBe('true')
    modal.querySelector('[data-test="clone-confirm"]').click()
    await settle()

    expect(cloneTeamTask).toHaveBeenCalledWith('tm1', 't1', 'm1', 'agent')
    expect(useToasts().toasts.value.at(-1).message).toBe('Task cloned')
    expect(document.body.querySelector('[data-test="clone-modal"]')).toBe(null)
  })

  it('says when it is no longer shared', async () => {
    getTeamTask.mockImplementation(() => Promise.reject(Object.assign(new Error('gone'), { status: 404 })))
    const el = await mount()
    expect(el.querySelector('[data-test="no-longer-shared"]').textContent).toContain('No longer shared')
  })

  it('goes back to Me when you are no longer in the team, and says other failures', async () => {
    getTeamTask.mockImplementation(() => Promise.reject(Object.assign(new Error('forbidden'), { status: 403 })))
    await mount()
    expect(replace).toHaveBeenCalledWith('/')
    app.unmount()
    getTeamTask.mockImplementation(() => Promise.reject(Object.assign(new Error('down'), { status: 500 })))
    await mount()
    expect(useToasts().toasts.value.map((t) => t.message)).toContain('Failed to load task context: down')
  })

  it('follows the team\'s events for it, and nothing else', async () => {
    const el = await mount()
    const title = () => el.querySelector('[data-test="task-title"]').textContent.trim()
    const send = async (ev) => { events.value = [...events.value, ev]; await settle() }
    await send({ type: 'task.updated', payload: { id: 't1', title: 'Your own event' } })
    await send({ type: 'team.task.updated', teamId: 'tm2', payload: { id: 't1', title: 'Another team' } })
    await send({ type: 'team.task.updated', teamId: 'tm1', payload: { id: 't9', title: 'Another task' } })
    expect(title()).toBe('Fix the login page')
    await send({ type: 'team.task.updated', teamId: 'tm1', payload: { id: 't1', title: 'Renamed by Bob', status: 'ongoing' } })
    expect(title()).toBe('Renamed by Bob')
    expect(el.querySelector('[data-test="team-task-status"]').textContent).toMatch(/ongoing/i)
  })

  it('reads it again when the team says it was deleted', async () => {
    const el = await mount()
    getTeamTask.mockImplementation(() => Promise.reject(Object.assign(new Error('gone'), { status: 404 })))
    events.value = [{ type: 'team.task.deleted', teamId: 'tm1', payload: { id: 't9' } }]
    await settle()
    expect(getTeamTask).toHaveBeenCalledTimes(1)
    events.value = [...events.value, { type: 'team.task.deleted', teamId: 'tm1', payload: { id: 't1' } }]
    await settle()
    expect(getTeamTask).toHaveBeenCalledTimes(2)
    expect(el.querySelector('[data-test="no-longer-shared"]')).not.toBe(null)
  })

  it('shows its plan from the team\'s answer, never the owner\'s route', async () => {
    getTeamTask.mockImplementation(() => Promise.resolve({ ...teamTask(), plan: { id: 'p1', content: '- [x] Find it\n- [ ] Fix it', version: 2 } }))
    const el = await mount()
    expect(getTaskPlan).not.toHaveBeenCalled()
    expect(el.querySelector('[data-testid="task-plan-progress"]').textContent).toContain('1 / 2')
  })

  it('reads its files from the links the team gave, and never offers them as public', async () => {
    const link = (id) => `/api/v1/teams/tm1/tasks/t1/attachments/${id}`
    getTeamTask.mockImplementation(() => {
      const t = teamTask()
      t.task.attachments = [
        { id: 'a1', filename: 'shot.png', mimeType: 'image/png', url: link('a1') },
        { id: 'a2', filename: 'logo.svg', mimeType: 'image/svg+xml', url: link('a2') },
      ]
      return Promise.resolve(t)
    })
    const el = await mount()
    const imgs = [...el.querySelectorAll('img')].map((i) => i.getAttribute('src'))
    expect(imgs).toContain(link('a1'))
    expect(imgs).not.toContain(link('a2'))
    ;[...el.querySelectorAll('span')].find((s) => s.textContent.trim() === 'logo.svg').click()
    await settle()
    expect(document.body.querySelector('[aria-label="Download"]').getAttribute('href')).toBe(link('a2'))
    expect(document.body.querySelector('[aria-label="Download"]').getAttribute('target')).toBe(null)
    expect(document.body.querySelector('[aria-label="Open in browser"]')).toBe(null)
    expect(document.body.querySelector('[aria-label="Copy link"]')).toBe(null)
  })
})
