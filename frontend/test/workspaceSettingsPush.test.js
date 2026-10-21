// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Push notification availability on the Notifications tab of workspace
 * settings, mounted: `initPushAvailability()` probes
 * `${API_BASE_URL}/push/vapid-public-key` so deployments served under a
 * reverse-proxy base path still offer the "Enable Push" control.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'ws1' }, query: { tab: 'notifications' } }
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))

const WORKSPACE = { id: 'ws1', name: 'ops', notificationSettings: { channels: ['email'] } }
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  API_BASE_URL: '/subpath/api/v1',
  getWorkspace: () => Promise.resolve({ workspace: { ...WORKSPACE } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  fetchMachines: () => Promise.resolve({ machines: [] }),
}))

const checkWorkspaceSubscription = vi.fn(() => Promise.resolve(false))
vi.mock('../src/composables/usePushNotifications', () => ({
  usePushNotifications: () => ({
    checkWorkspaceSubscription: (...a) => checkWorkspaceSubscription(...a),
    subscribeWorkspace: vi.fn(),
    unsubscribeWorkspace: vi.fn(),
  }),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')

const settle = () => new Promise((r) => setTimeout(r, 30))
const text = (n) => (n?.textContent ?? '').replace(/\s+/g, ' ').trim()
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
  const pushButton = () =>
    [...el.querySelectorAll('button')].find((b) => ['Enable Push', 'Push Active'].includes(text(b)))
  return { el, pushButton }
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
  Object.defineProperty(navigator, 'serviceWorker', {
    value: { ready: Promise.resolve({}) },
    configurable: true,
  })
  window.PushManager = function PushManager() {}
})

afterEach(() => {
  app?.unmount()
  delete window.PushManager
})

describe('workspace settings push availability probe', () => {
  it('fetches ${API_BASE_URL}/push/vapid-public-key and shows Enable Push when configured', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ publicKey: 'BEl62iUYgUivxIkv69yViEuiBIa-Ib9-SkvMeAtA3LFgDzkrxZJjSgSnfckjBJuBkr3qBUYIHBQFLXYp5Nksh8U' }),
    })

    const { pushButton } = await mount()

    expect(fetchSpy).toHaveBeenCalledWith('/subpath/api/v1/push/vapid-public-key')
    expect(pushButton()).not.toBeUndefined()
    expect(text(pushButton())).toBe('Enable Push')
    expect(checkWorkspaceSubscription).toHaveBeenCalledWith('ws1')
    fetchSpy.mockRestore()
  })

  it('hides Enable Push when the VAPID public key endpoint returns no key', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({
      ok: true,
      json: () => Promise.resolve({ publicKey: '' }),
    })

    const { pushButton } = await mount()

    expect(fetchSpy).toHaveBeenCalledWith('/subpath/api/v1/push/vapid-public-key')
    expect(pushButton()).toBeUndefined()
    expect(checkWorkspaceSubscription).not.toHaveBeenCalled()
    fetchSpy.mockRestore()
  })
})
