// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The New Task form's `/` menu, mounted: typing `/` in the description lists
 * the workspace's skills, and choosing one puts `/<name> ` in it.
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
  searchWorkspaceSkills: () => Promise.resolve({ skills: [
    { name: 'release', description: 'Cut a release', workspaceEnabled: true },
    { name: 'review', workspaceEnabled: true },
    { name: 'hidden', workspaceEnabled: false },
  ] }),
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

const key = (k) => new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true })
const rows = (el) => [...el.querySelectorAll('[data-test=slash-menu] li')].map((li) => li.querySelector('span').textContent)

describe('the description\'s / menu', () => {
  it('lists the skills the workspace\'s agents can load', async () => {
    const form = await mount()
    expect(form.el.querySelector('[data-test=slash-menu]')).toBeNull()
    form.fill('#taskForm textarea', '/')
    await settle()
    expect(rows(form.el)).toEqual(['/release', '/review'])
    expect(form.el.querySelector('[data-test=slash-menu]').textContent).toContain('skill')
  })

  it('narrows as you type and puts the chosen skill in with Enter', async () => {
    const form = await mount()
    form.fill('#taskForm textarea', '/rev')
    await settle()
    expect(rows(form.el)).toEqual(['/review'])
    const field = form.el.querySelector('#taskForm textarea')
    field.dispatchEvent(key('Enter'))
    await settle()
    expect(field.value).toBe('/review ')
    expect(form.el.querySelector('[data-test=slash-menu]')).toBeNull()
  })

  it('moves the highlight with the arrows, and a click chooses', async () => {
    const form = await mount()
    form.fill('#taskForm textarea', '/')
    await settle()
    const field = form.el.querySelector('#taskForm textarea')
    field.dispatchEvent(key('ArrowDown'))
    await settle()
    expect(form.el.querySelectorAll('[aria-selected=true]')[0].textContent).toContain('/review')
    form.el.querySelectorAll('[data-test=slash-menu] li')[0].dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await settle()
    expect(field.value).toBe('/release ')
  })

  it('still creates the task with Cmd-Enter when the menu is shut', async () => {
    const form = await mount()
    form.fill('#taskForm textarea', 'Ship it')
    await settle()
    form.el.querySelector('#taskForm textarea').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', metaKey: true, bubbles: true, cancelable: true }))
    await settle()
    expect(createTask).toHaveBeenCalled()
  })
})
