// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * Every Settings tab shows its icon. WebKit (the Tauri app on macOS) draws an
 * inline SVG with no width or height of its own at zero width inside a flex
 * box, so an icon given as markup must be sized by its wrapper.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: 'w1' }, query: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() }),
}))

const WORKSPACE = { id: 'w1', name: 'ops' }
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { ...WORKSPACE } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  fetchMachines: () => Promise.resolve({ machines: [] }),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')

let app
afterEach(() => app?.unmount())

describe('settings tab icons', () => {
  it('sizes an icon given as SVG markup to its box, as a path icon is', async () => {
    const el = document.createElement('div')
    document.body.appendChild(el)
    app = createApp({ render: () => h(WorkspaceSettingsView) })
    app.use(createPinia())
    app.component('RouterLink', { setup: (_, { slots }) => () => h('a', slots.default?.()) })
    app.directive('click-outside', {})
    app.mount(el)
    await new Promise((r) => setTimeout(r, 30))

    const tab = (label) => [...el.querySelectorAll('nav button')].find((b) => b.textContent.trim() === label)
    for (const label of ['Setup', 'Slack']) {
      const wrapper = tab(label).querySelector('svg').parentElement
      expect(wrapper.className, label).toContain('[&>svg]:w-full')
      expect(wrapper.className, label).toContain('[&>svg]:h-full')
    }
    expect(tab('General').querySelector('svg').getAttribute('class')).toBe('w-full h-full')
  })
})
