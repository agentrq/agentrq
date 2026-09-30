// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The setup tab of a workspace's settings, with and without a machine to run
 * on: with none, setting one up is the first connection tab, and the one it
 * opens on; with one, the tab is not there and Claude is where it starts.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'w1' }, query: { tab: 'setup' } }
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))

const WORKSPACE = { id: 'w1', name: 'ops', workingDirectory: '/srv/ops', agentConnected: false }
const ONLINE = { id: 'm1', name: 'workshop-pi', enabled: true, online: true }
let machines = []
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { ...WORKSPACE } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  fetchMachines: () => Promise.resolve({ machines }),
  fetchAcpAgents: () => Promise.resolve({ agents: [] }),
  fetchAcpModels: () => Promise.resolve({ agent: '', models: [] }),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')

const settle = () => new Promise((r) => setTimeout(r, 30))
const text = (n) => (n?.textContent ?? '').replace(/\s+/g, ' ').trim()
let app

async function mount(list) {
  machines = list
  const el = document.createElement('div')
  document.body.appendChild(el)
  const pinia = createPinia()
  app = createApp({ render: () => h(WorkspaceSettingsView) })
  app.use(pinia)
  useWorkspaceStore(pinia).workspaces = [{ ...WORKSPACE }]
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots }) => () => h('a', { href: String(p.to) }, slots.default?.()),
  })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  const tab = (label) =>
    [...el.querySelectorAll('button')].find((b) => text(b).toLowerCase() === label)
  return { el, tab }
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
})
afterEach(() => app?.unmount())

describe('the setup tab, by whether there is a machine', () => {
  it('opens on a Machine tab, before Claude, when none is online', async () => {
    const { el, tab } = await mount([{ ...ONLINE, online: false }])
    const machineTab = el.querySelector('[data-test="machine-tab"]')
    expect(machineTab).not.toBeNull()
    expect(machineTab.nextElementSibling).toBe(tab('claude'))
    const panel = el.querySelector('[data-test="machine-setup"]')
    expect(text(panel)).toContain('No machine is online')
    expect(panel.querySelector('a').getAttribute('href')).toBe('/machines')
    // The manual steps belong to the other tabs.
    expect(text(el)).not.toContain('1. Configuration')
  })

  it('shows the manual steps again when another tab is picked', async () => {
    const { el, tab } = await mount([])
    tab('claude').click()
    await nextTick()
    expect(el.querySelector('[data-test="machine-setup"]')).toBeNull()
    expect(text(el)).toContain('1. Configuration')
    expect(el.querySelector('[data-test="machine-tab"]')).not.toBeNull()
  })

  it('has no Machine tab, and starts on Claude, with a machine online', async () => {
    const { el } = await mount([ONLINE])
    expect(el.querySelector('[data-test="machine-tab"]')).toBeNull()
    expect(el.querySelector('[data-test="machine-setup"]')).toBeNull()
    expect(text(el)).toContain('1. Configuration')
  })
})
