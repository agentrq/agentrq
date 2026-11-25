// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A workspace page translated by the browser still changes route. Translation
 * rewrites the heading's text, and with it the empty text nodes Vue marks a
 * fragment's ends with; a fragment there failed to be removed on the way from
 * the board back to the list, and every patch of the page failed after it.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick, reactive } from 'vue'
import { createPinia } from 'pinia'

const route = reactive({ params: { id: 'ws1' }, path: '/workspaces/ws1/board', query: {} })
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  RouterView: { render: () => null },
}))

vi.mock('../src/useEventBus', async () => {
  const { ref } = await import('vue')
  return { useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]), isConnected: ref(true) }) }
})

vi.mock('../src/api', async (importOriginal) => ({
  ...await importOriginal(),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops' } }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
}))

const stub = { render: () => null }
vi.mock('../src/components/TaskFeed.vue', () => ({ default: stub }))
vi.mock('../src/components/StartAgentPanel.vue', () => ({ default: stub }))

const { default: WorkspaceDetailView } = await import('../src/views/WorkspaceDetailView.vue')

let app
let el

afterEach(() => {
  app?.unmount()
  el?.remove()
})

describe('a translated workspace page', () => {
  it('goes from the board and analytics back to the list', async () => {
    const errors = vi.spyOn(console, 'error').mockImplementation(() => {})
    el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(WorkspaceDetailView) })
    app.use(createPinia())
    app.component('RouterView', stub)
    app.directive('click-outside', {})
    app.mount(el)
    await new Promise((r) => setTimeout(r, 30))

    for (const section of ['board', 'analytics']) {
      route.path = `/workspaces/ws1/${section}`
      await nextTick()
      const heading = el.querySelector('h1')
      expect(heading.textContent, `the ${section} heading`).toMatch(/\/\s*(Board|Analytics)/)
      for (const c of [...heading.childNodes]) if (c.nodeType === Node.TEXT_NODE && !c.nodeValue.trim()) c.remove()

      route.path = '/workspaces/ws1'
      await nextTick()
      expect(el.querySelector('h1').textContent.trim(), `the list heading, after the ${section}`).toBe('ops')
    }
    expect(errors).not.toHaveBeenCalled()
    errors.mockRestore()
  })
})
