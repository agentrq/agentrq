// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The account settings page, mounted: its tabs follow ?tab=, the clock is
 * picked and saved at once (and put back when refused), and the Presets tab
 * keeps the account's own presets.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, reactive } from 'vue'
import { createPinia } from 'pinia'

const route = reactive({ path: '/settings', query: {} })
const replace = vi.fn((to) => { route.query = to.query })
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ replace, push: vi.fn() }) }))

const fetchAccountSettings = vi.fn()
const updateAccountSettings = vi.fn()
const listAccountPresets = vi.fn()
const createAccountPreset = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchAccountSettings: (...a) => fetchAccountSettings(...a),
  updateAccountSettings: (...a) => updateAccountSettings(...a),
  listAccountPresets: (...a) => listAccountPresets(...a),
  createAccountPreset: (...a) => createAccountPreset(...a),
  listWorkspacePresets: () => { throw new Error('the account page asked for a workspace\'s presets') },
}))

const { default: AccountSettingsView } = await import('../src/views/AccountSettingsView.vue')
const { timeFormat } = await import('../src/composables/useTimeFormat')
const { notifyWebMCPChange } = await import('../src/composables/useWebMCPChanges')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((r) => setTimeout(r, 30))
let app

async function mount() {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(AccountSettingsView) })
  app.use(createPinia())
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  const q = (t) => el.querySelector(`[data-test=${t}]`)
  return { el, q }
}

const pick = async (q, format) => {
  const input = q(`time-format-${format}`).querySelector('input')
  input.checked = true
  input.dispatchEvent(new Event('change'))
  await settle()
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
  route.query = {}
  // As read at sign-in.
  timeFormat.value = '24h'
  fetchAccountSettings.mockResolvedValue({ settings: { display: { timeFormat: '24h' } } })
  updateAccountSettings.mockImplementation((s) => Promise.resolve({ settings: s }))
  listAccountPresets.mockResolvedValue({ presets: [{ name: 'standup', body: 'post the standup' }] })
  createAccountPreset.mockImplementation((name, body) => Promise.resolve({ preset: { name, body } }))
})
afterEach(() => {
  app?.unmount()
  timeFormat.value = 'browser'
})

describe('the account settings page', () => {
  it('opens on General with the account\'s clock picked and shown, reading nothing again', async () => {
    const { q } = await mount()
    expect(fetchAccountSettings).not.toHaveBeenCalled()
    expect(q('settings-tab-general').getAttribute('aria-selected')).toBe('true')
    expect(q('time-format-24h').querySelector('input').checked).toBe(true)
    expect(q('time-format-example').textContent).toMatch(/^\d\d:\d\d$/)
  })

  it('saves a pick at once, in the display group, and the example follows it', async () => {
    const { q } = await mount()
    await pick(q, '12h')
    expect(updateAccountSettings).toHaveBeenCalledWith({ display: { timeFormat: '12h' } })
    expect(timeFormat.value).toBe('12h')
    expect(q('time-format-example').textContent).toMatch(/^\d\d:\d\d [AP]M$/)
  })

  it('puts the last clock back and says why when the server refuses', async () => {
    updateAccountSettings.mockRejectedValue(new Error('database unavailable'))
    const { q } = await mount()
    await pick(q, 'browser')
    expect(timeFormat.value).toBe('24h')
    expect(useToasts().toasts.value.at(-1).message).toBe('database unavailable')
    updateAccountSettings.mockRejectedValue(null)
    await pick(q, '12h')
    expect(useToasts().toasts.value.at(-1).message).toBe('Failed to save account settings')
  })

  it('reads the clock again after a browser agent changes it', async () => {
    await mount()
    fetchAccountSettings.mockResolvedValue({ settings: { display: { timeFormat: '12h' } } })
    notifyWebMCPChange()
    await settle()
    expect(timeFormat.value).toBe('12h')
  })

  it('moves between tabs through ?tab=, and keeps the account\'s own presets', async () => {
    const { el, q } = await mount()
    q('settings-tab-presets').click()
    await settle()
    expect(replace).toHaveBeenCalledWith({ path: '/settings', query: { tab: 'presets' } })
    expect(q('settings-tab-presets').getAttribute('aria-selected')).toBe('true')
    expect(el.querySelectorAll('[data-test=preset-row]').length).toBe(1)
    expect(q('presets-account-note')).not.toBeNull()

    q('preset-name').value = 'retro'
    q('preset-name').dispatchEvent(new Event('input'))
    q('preset-body').value = 'run the retro'
    q('preset-body').dispatchEvent(new Event('input'))
    await settle()
    q('preset-add').click()
    await settle()
    expect(createAccountPreset).toHaveBeenCalledWith('retro', 'run the retro')
    expect(el.querySelectorAll('[data-test=preset-row]').length).toBe(2)
  })

  it('opens General for a tab it does not have', async () => {
    route.query = { tab: 'danger' }
    const { q } = await mount()
    expect(q('settings-tab-general').getAttribute('aria-selected')).toBe('true')
  })
})
