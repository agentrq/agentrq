// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A task an agent creates shows up on the workspace's task list, and says so
 * nowhere else: the app-wide stream toast in App.vue already announces it, so a
 * toast from the list as well was the same news twice.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, reactive } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRouter: () => ({ push: vi.fn(), currentRoute: { value: { query: {} } } }),
  useRoute: () => ({ params: {}, query: {} }),
}))

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
  fetchTaskCounts: () => Promise.resolve({}),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [] }),
  recordTelemetry: () => {},
}))

const { default: TaskFeed } = await import('../src/components/TaskFeed.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((r) => setTimeout(r, 30))
let app

afterEach(() => {
  app?.unmount()
  useToasts().toasts.value = []
})

describe('An agent creating a task, on the task list', () => {
  it('adds the row without a toast of its own', async () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    const props = reactive({ workspaceId: 'p1', liveEvents: [] })
    const pinia = createPinia()
    app = createApp({ render: () => h(TaskFeed, props) })
    app.use(pinia)
    useWorkspaceStore(pinia).workspaces = [{ id: 'p1', name: 'ops' }]
    app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
    app.directive('click-outside', {})
    app.mount(el)
    await settle()

    props.liveEvents.push({
      type: 'task.created',
      payload: { id: 't9', workspaceId: 'p1', title: 'Rotate the API keys', status: 'notstarted', assignee: 'agent', createdBy: 'agent', updatedAt: '2026-10-04T20:00:00Z' },
    })
    await settle()

    expect([...el.querySelectorAll('h3')].map((n) => n.textContent.trim())).toContain('Rotate the API keys')
    expect(useToasts().toasts.value).toEqual([])
  })
})
