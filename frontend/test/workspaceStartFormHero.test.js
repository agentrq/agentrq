// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * An offline workspace folds its "Connect your first Agent" introduction away
 * while the start form is open, so the form has the room.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'ws1' }, path: '/workspaces/ws1', query: {} }),
  useRouter: () => ({ push: vi.fn() }),
  RouterView: { render: () => null },
}))

vi.mock('../src/useEventBus', async () => {
  const { ref } = await import('vue')
  return { useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]), isConnected: ref(true) }) }
})

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: false } }),
  fetchTasks: () => Promise.resolve({ tasks: [] }),
}))

const stub = { render: () => null }
vi.mock('../src/components/TaskFeed.vue', () => ({ default: stub }))

// The panel's only part here is to say when its form opens.
let emitOpen
vi.mock('../src/components/StartAgentPanel.vue', () => ({
  default: {
    emits: ['open', 'availability'],
    setup(_, { emit }) {
      emitOpen = (v) => emit('open', v)
      return () => null
    },
  },
}))

const { default: WorkspaceDetailView } = await import('../src/views/WorkspaceDetailView.vue')

let app
let el

afterEach(() => {
  app?.unmount()
  el?.remove()
})

describe('the offline workspace introduction', () => {
  it('folds away while the start form is open, and comes back when it closes', async () => {
    el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(WorkspaceDetailView) })
    app.use(createPinia())
    app.component('RouterLink', { render() { return h('a', this.$slots.default?.()) } })
    app.component('RouterView', stub)
    app.directive('click-outside', {})
    app.mount(el)
    await new Promise((r) => setTimeout(r, 30))

    expect(el.textContent).toContain('Connect your first Agent')
    emitOpen(true)
    await nextTick()
    expect(el.textContent).not.toContain('Connect your first Agent')
    emitOpen(false)
    await nextTick()
    expect(el.textContent).toContain('Connect your first Agent')
  })
})
