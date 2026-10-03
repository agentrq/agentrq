// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The "Name untitled tasks" switch on the input tab, mounted: it reads and
 * writes this device's own choice, and never goes to the server.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'ws1' }, query: { tab: 'input' } }
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))

const WORKSPACE = { id: 'ws1', name: 'ops', notificationSettings: {} }
const updateWorkspace = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { ...WORKSPACE } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  updateWorkspace: (...a) => updateWorkspace(...a),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  fetchMachines: () => Promise.resolve({ machines: [] }),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')
const { autoTitleKey } = await import('../src/composables/useUntitledTaskTitles')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((r) => setTimeout(r, 30))
let app

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(WorkspaceSettingsView) })
  app.use(createPinia())
  app.component('RouterLink', { props: ['to'], setup: (p, { slots }) => () => h('a', {}, slots.default?.()) })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  return { switchButton: () => el.querySelector('[data-test=auto-title] [role=switch]') }
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
  useToasts().toasts.value.splice(0)
})
afterEach(() => app?.unmount())

describe('the "Name untitled tasks" switch', () => {
  it('starts on, on a computer, and turning it off is kept on this device only', async () => {
    const { switchButton } = await mount()
    expect(switchButton().getAttribute('aria-checked')).toBe('true')

    switchButton().click()
    await settle()

    expect(switchButton().getAttribute('aria-checked')).toBe('false')
    expect(localStorage.getItem(autoTitleKey('ws1'))).toBe('off')
    expect(useToasts().toasts.value.at(-1).message).toBe('Untitled tasks will keep their title on this device')
    expect(updateWorkspace).not.toHaveBeenCalled()
  })

  it('shows a choice made earlier, and turns back on', async () => {
    localStorage.setItem(autoTitleKey('ws1'), 'off')
    const { switchButton } = await mount()
    expect(switchButton().getAttribute('aria-checked')).toBe('false')

    switchButton().click()
    await settle()

    expect(localStorage.getItem(autoTitleKey('ws1'))).toBe('on')
    expect(useToasts().toasts.value.at(-1).message).toBe('Untitled tasks will be named on this device')
  })
})
