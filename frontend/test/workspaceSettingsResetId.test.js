// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Resetting a workspace's id from its Danger Zone: the card says when it is
 * needed and that the MCP URL changes, the dialog asks first, and a reset
 * lands on the new id's Setup tab, where the new URL is.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'w1' }, query: { tab: 'danger' } }
const push = vi.fn()
const replace = vi.fn()
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push, replace }) }))

const WORKSPACE = { id: 'w1', name: 'ops', workingDirectory: '/srv/ops' }
const resetWorkspaceId = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { ...WORKSPACE } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  fetchMachines: () => Promise.resolve({ machines: [] }),
  resetWorkspaceId: (...a) => resetWorkspaceId(...a),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((r) => setTimeout(r, 30))
const text = (n) => (n?.textContent ?? '').replace(/\s+/g, ' ').trim()
let app

async function mount() {
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
  return el
}

const dialog = () => [...document.body.querySelectorAll('h3')].find((n) => text(n) === 'Reset Workspace ID')?.closest('div.fixed')
const button = (root, label) => [...root.querySelectorAll('button')].find((b) => text(b) === label)
const toasts = () => useToasts().toasts.value.map((t) => `${t.type}: ${t.message}`)

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
  useToasts().toasts.value.splice(0)
})
afterEach(() => app?.unmount())

describe('resetting a workspace id', () => {
  it('says it is only for a compromised MCP token, and that the MCP URL changes', async () => {
    const el = await mount()
    const card = el.querySelector('[data-test=reset-id-card]')
    expect(text(card.querySelector('h4'))).toBe('Reset Workspace ID')
    expect(text(card)).toMatch(/Only needed if this workspace's MCP token has been compromised\./)
    expect(text(card)).toMatch(/The MCP URL will change\./)
  })

  it('asks first, and leaves the workspace alone when cancelled', async () => {
    const el = await mount()
    button(el.querySelector('[data-test=reset-id-card]'), 'Reset ID').click()
    await settle()
    expect(text(dialog())).toMatch(/Give 'ops' a new ID\? Do this only if its MCP token has been compromised\. Its MCP URL will change/)
    button(dialog(), 'Cancel').click()
    await settle()
    expect(resetWorkspaceId).not.toHaveBeenCalled()
  })

  it('lands on the new id\'s Setup tab, where the new MCP URL is', async () => {
    resetWorkspaceId.mockResolvedValue({ id: 'w2' })
    const el = await mount()
    button(el.querySelector('[data-test=reset-id-card]'), 'Reset ID').click()
    await settle()
    button(dialog(), 'Reset ID').click()
    await settle()
    expect(resetWorkspaceId).toHaveBeenCalledWith('w1')
    expect(replace).toHaveBeenCalledWith('/workspaces/w2/settings?tab=setup')
    expect(toasts()).toContain('success: Workspace ID reset. Set up your agents again with the new MCP URL.')
  })

  it('says why when the reset fails, and stays put', async () => {
    resetWorkspaceId.mockRejectedValue(new Error('not found'))
    const el = await mount()
    button(el.querySelector('[data-test=reset-id-card]'), 'Reset ID').click()
    await settle()
    button(dialog(), 'Reset ID').click()
    await settle()
    expect(replace).not.toHaveBeenCalled()
    expect(toasts()).toContain('error: Reset failed: not found')
  })
})
