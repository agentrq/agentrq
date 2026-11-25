// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The New Task form, mounted: its description can be resized by hand, and the
 * form widens only when asked: by the expand button, or by dragging its edges.
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

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'ops' } }),
  fetchEvents: () => Promise.resolve({ events: [] }),
  fetchWorkflows: () => Promise.resolve({ workflows: [] }),
  fetchWorkspaceSession: () => Promise.resolve(null),
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
})
afterEach(() => app?.unmount())

describe('the New Task description size', () => {
  it('has a grip to drag its height by', async () => {
    const form = await mount()
    const text = form.el.querySelector('#taskForm textarea')
    expect(text.classList.contains('resize-y')).toBe(true)
    expect(text.style.height).toBe('160px')
  })

  it('is widened and given the room left by the expand button, and restored by it', async () => {
    const form = await mount()
    const expand = form.el.querySelector('#taskForm button[aria-label="Expand editor"]')
    const column = form.el.querySelector('#taskForm').parentElement
    expect(column.classList.contains('max-w-5xl')).toBe(true)
    expect(expand.getAttribute('aria-pressed')).toBe('false')

    expand.click()
    await settle()
    const restore = form.el.querySelector('#taskForm button[aria-label="Restore editor size"]')
    expect(restore.getAttribute('aria-pressed')).toBe('true')
    expect(column.classList.contains('max-w-none')).toBe(true)
    expect(column.classList.contains('max-w-5xl')).toBe(false)

    restore.click()
    await settle()
    expect(column.classList.contains('max-w-5xl')).toBe(true)
  })

  it('keeps the page width until an edge is dragged, and gets it back on a double-click', async () => {
    const form = await mount()
    const column = form.el.querySelector('#taskForm').parentElement
    const edges = form.el.querySelectorAll('#taskForm [role="separator"]')
    expect(edges).toHaveLength(2)
    expect(column.style.maxWidth).toBe('')

    // jsdom lays nothing out: the page gives the column no width, so a drag
    // from there settles on the least.
    edges[1].dispatchEvent(new PointerEvent('pointerdown', { button: 0, clientX: 500 }))
    globalThis.dispatchEvent(new PointerEvent('pointermove', { clientX: 520 }))
    globalThis.dispatchEvent(new PointerEvent('pointerup'))
    await settle()
    expect(column.style.maxWidth).toBe('384px')

    edges[0].dispatchEvent(new MouseEvent('dblclick'))
    await settle()
    expect(column.style.maxWidth).toBe('')
  })
})
