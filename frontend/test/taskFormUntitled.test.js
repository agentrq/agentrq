// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The New Task form with no title, mounted: it can be sent, the server is
 * asked for a task with no title, and the browser then queues it for naming.
 * Editing a scheduled task still needs one.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'ws1' }, query: {} }
const push = vi.fn()
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push }) }))

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

const nameUntitledTask = vi.fn()
vi.mock('../src/composables/useUntitledTaskTitles', () => ({
  nameUntitledTask: (...a) => nameUntitledTask(...a),
}))

const createTask = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'ops' } }),
  getTask: () => Promise.resolve({ task: { id: 't1', title: 'Nightly report', body: 'Summarise the day', assignee: 'agent', cronSchedule: '0 9 * * *' } }),
  fetchEvents: () => Promise.resolve({ events: [] }),
  fetchWorkflows: () => Promise.resolve({ workflows: [] }),
  fetchWorkspaceSession: () => Promise.resolve(null),
  createTask: (...a) => createTask(...a),
}))

const { default: TaskFormView } = await import('../src/views/TaskFormView.vue')

const settle = () => new Promise((r) => setTimeout(r, 30))
let app

async function mount(params = { id: 'ws1' }) {
  route.params = params
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(TaskFormView) })
  app.use(createPinia())
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  const fill = (selector, value) => {
    const field = el.querySelector(selector)
    field.value = value
    field.dispatchEvent(new Event('input'))
  }
  return {
    el,
    fill,
    title: () => el.querySelector('#taskForm input'),
    submit: () => el.querySelector('#taskForm button[type=submit]'),
  }
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
  createTask.mockResolvedValue({ task: { id: 't9', title: 'Untitled' } })
})
afterEach(() => app?.unmount())

describe('a new task without a title', () => {
  it('puts the cursor in the description', async () => {
    await mount()
    expect(document.activeElement).toBe(document.querySelector('#taskForm textarea'))
  })

  it('can be sent once it has a description, and is queued for naming', async () => {
    const form = await mount()
    expect(form.title().placeholder).toMatch(/optional/)
    expect(form.submit().disabled).toBe(true)

    form.fill('#taskForm textarea', 'Fix the login page redirect')
    await settle()
    expect(form.submit().disabled).toBe(false)

    form.el.querySelector('#taskForm').dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()

    expect(createTask.mock.calls[0].slice(0, 3)).toEqual(['ws1', '', 'Fix the login page redirect'])
    expect(nameUntitledTask).toHaveBeenCalledWith('ws1', 't9', 'Fix the login page redirect')
    expect(push).toHaveBeenCalledWith('/workspaces/ws1')
  })

  it('is not queued for naming when it was given a title', async () => {
    const form = await mount()
    form.fill('#taskForm input', '  Fix login  ')
    form.fill('#taskForm textarea', 'Fix the login page redirect')
    await settle()

    form.el.querySelector('#taskForm').dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()

    expect(createTask.mock.calls[0][1]).toBe('Fix login')
    expect(nameUntitledTask).not.toHaveBeenCalled()
  })

  it('still needs a title when editing a scheduled task', async () => {
    const form = await mount({ id: 'ws1', taskId: 't1' })
    expect(form.submit().disabled).toBe(false)
    expect(form.title().placeholder).not.toMatch(/optional/)

    form.fill('#taskForm input', '')
    await settle()
    expect(form.submit().disabled).toBe(true)
  })
})
