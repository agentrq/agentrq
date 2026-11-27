// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A workspace's board with a card's chat open beside it is still the board:
 * its heading says so, and a phone gets the card's own page.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, reactive } from 'vue'
import { createPinia } from 'pinia'

const route = reactive({ params: { id: 'ws1', taskId: 't1' }, path: '/workspaces/ws1/board/chat/t1', query: {} })
const replace = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), replace }),
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

async function mount() {
  el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(WorkspaceDetailView) })
  app.use(createPinia())
  app.component('RouterView', stub)
  app.directive('click-outside', {})
  app.mount(el)
  await new Promise((r) => setTimeout(r, 30))
}

afterEach(() => {
  app?.unmount()
  el?.remove()
  replace.mockClear()
})

describe('a workspace board with a card open beside it', () => {
  it('is still the board', async () => {
    await mount()
    expect(el.querySelector('h1').textContent).toMatch(/\/\s*Board/)
    expect(el.querySelector('[title="List view"]')).not.toBe(null)
    expect(replace).not.toHaveBeenCalled()
  })

  it('sends a phone to the card\'s own page', async () => {
    const width = window.innerWidth
    window.innerWidth = 390
    try {
      await mount()
    } finally {
      window.innerWidth = width
    }
    expect(replace).toHaveBeenCalledWith('/workspaces/ws1/tasks/t1')
  })

  it('sends a phone on the board with no card open to the list', async () => {
    route.params = { id: 'ws1' }
    route.path = '/workspaces/ws1/board'
    const width = window.innerWidth
    window.innerWidth = 390
    try {
      await mount()
    } finally {
      window.innerWidth = width
    }
    expect(replace).toHaveBeenCalledWith('/workspaces/ws1')
  })
})
