// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * A workspace's task list leads to its skills: the sidebar has no Skills page.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
let path = '/workspaces/ws1'
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'ws1' }, path, query: {} }),
  useRouter: () => ({ push }),
  RouterView: { render: () => null },
}))

vi.mock('../src/useEventBus', async () => {
  const { ref } = await import('vue')
  return { useEventBus: () => ({ connect() {}, disconnect() {}, events: ref([]), isConnected: ref(true) }) }
})

let workspace = { id: 'ws1', name: 'Ops' }
vi.mock('../src/api', async (importOriginal) => ({
  ...await importOriginal(),
  getWorkspace: () => Promise.resolve({ workspace }),
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
  app.component('RouterLink', { render() { return h('a', this.$slots.default?.()) } })
  app.component('RouterView', stub)
  app.directive('click-outside', {})
  app.mount(el)
  await new Promise((r) => setTimeout(r, 30))
}

describe('the workspace Skills button', () => {
  beforeEach(() => {
    push.mockReset()
    path = '/workspaces/ws1'
    workspace = { id: 'ws1', name: 'Ops' }
  })
  afterEach(() => {
    app?.unmount()
    el?.remove()
  })

  it('opens the workspace settings Skills tab', async () => {
    await mount()
    const btn = el.querySelector('[data-test="workspace-skills-link"]')
    expect(btn, 'the Skills button').not.toBeNull()
    expect(btn.getAttribute('title')).toBe('Skills')
    btn.click()
    expect(push).toHaveBeenCalledWith('/workspaces/ws1/settings?tab=skills')
  })

  it('is not offered on an archived workspace, like its settings', async () => {
    workspace = { id: 'ws1', name: 'Ops', archivedAt: '2026-11-01T00:00:00Z' }
    await mount()
    expect(el.querySelector('button[title="Analytics"]'), 'the header is drawn').not.toBeNull()
    expect(el.querySelector('[data-test="workspace-skills-link"]')).toBeNull()
  })

  it('is not offered on the settings, whose own Skills tab is the same place', async () => {
    path = '/workspaces/ws1/settings'
    await mount()
    expect(el.querySelector('button[title="Analytics"]'), 'the header is drawn').not.toBeNull()
    expect(el.querySelector('[data-test="workspace-skills-link"]')).toBeNull()
  })
})
