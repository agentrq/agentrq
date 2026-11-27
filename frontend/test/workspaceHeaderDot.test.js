// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The workspace page's header dot is the sidebar's dot for that workspace: it
 * reads the shell's task counts, and moves only while a task is running.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick, reactive, ref } from 'vue'
import { createPinia } from 'pinia'

const route = reactive({ params: { id: 'ws1' }, path: '/workspaces/ws1', query: {} })
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
  RouterView: { render: () => null },
}))

vi.mock('../src/useEventBus', async () => {
  const { ref } = await import('vue')
  return { useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]), isConnected: ref(true) }) }
})

let connected = true
vi.mock('../src/api', async (importOriginal) => ({
  ...await importOriginal(),
  getWorkspace: () => Promise.resolve({ workspace: { id: 'ws1', name: 'Ops', agentConnected: connected } }),
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
  app = null
})

async function mount(overview) {
  el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(WorkspaceDetailView) })
  app.use(createPinia())
  if (overview) app.provide('taskOverview', overview)
  app.component('RouterView', stub)
  app.directive('click-outside', {})
  app.mount(el)
  await new Promise((r) => setTimeout(r, 30))
}

const dot = () => el.querySelector('[data-test="workspace-status-dot"]')

describe('the workspace header dot', () => {
  it('is still for an idle online agent, and pulses once a task is ongoing', async () => {
    connected = true
    const overview = ref(new Map())
    await mount(overview)
    expect(dot().className).toContain('bg-green-500')
    expect(dot().className).not.toMatch(/animate-/)

    overview.value = new Map([['ws1', { counts: { ongoing: 1 }, tasks: [] }]])
    await nextTick()
    expect(dot().className).toContain('animate-pulse')

    overview.value = new Map([['ws1', { counts: { pending: 1 }, tasks: [] }]])
    await nextTick()
    expect(dot().className).toContain('bg-yellow-400')
    expect(dot().className).not.toMatch(/animate-/)
  })

  it('is grey, not red, for an offline agent, even without the shell', async () => {
    connected = false
    await mount()
    expect(dot().className).toContain('bg-gray-300')
    expect(dot().className).not.toContain('bg-red-500')
  })
})
