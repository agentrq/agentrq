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

const connect = vi.fn()
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect, disconnect() {}, events: ref([]) }),
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
  workspace: { id: 'tw1', name: 'api', group: 'Backend' },
  owner: bob,
})

const getTeamTask = vi.fn()
const getTask = vi.fn()
const updateTaskTitle = vi.fn()
const updateTaskStatus = vi.fn()

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getTeamTask: (...a) => getTeamTask(...a),
  getTask: (...a) => getTask(...a),
  getWorkspace: vi.fn(),
  fetchUser: () => Promise.resolve({ id: 'u1', name: 'Dev' }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [{ id: 'm1', name: 'API' }] }),
  fetchTeamWorkspaces: () => Promise.resolve({ workspaces: [] }),
  listTeams: () => Promise.resolve({ teams: [] }),
  listMyInvitations: () => Promise.resolve({ invitations: [] }),
  updateTaskTitle: (...a) => updateTaskTitle(...a),
  updateTaskStatus: (...a) => updateTaskStatus(...a),
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
  useToasts().toasts.value = []
  getTeamTask.mockImplementation(() => Promise.resolve(teamTask()))
})

afterEach(() => {
  app?.unmount()
  document.body.innerHTML = ''
})

describe('a teammate\'s task in team mode', () => {
  it('is read through the team, never cached, and has no live stream', async () => {
    const el = await mount()
    expect(getTeamTask).toHaveBeenCalledWith('tm1', 't1')
    expect(getTask).not.toHaveBeenCalled()
    expect(cacheTask).not.toHaveBeenCalled()
    expect(connect).not.toHaveBeenCalled()
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

  it('clones it into one of your workspaces', async () => {
    const el = await mount()
    el.querySelector('[data-test="clone-task"]').click()
    await settle()
    const modal = document.body.querySelector('[data-test="clone-modal"]')
    expect(modal.textContent).toContain('Fix the login page')
    expect(modal.querySelector('[data-test="clone-workspace"]').value).toBe('m1')
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
})
