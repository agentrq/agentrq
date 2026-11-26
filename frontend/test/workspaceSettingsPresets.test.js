// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The Presets tab, mounted: the workspace's presets listed, one added, one
 * edited and one removed at once, with no Save, and the server's sentence when it refuses.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'ws1' }, query: { tab: 'presets' } }
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push: vi.fn() }) }))

const WORKSPACE = { id: 'ws1', name: 'ops', notificationSettings: {} }
const listWorkspacePresets = vi.fn()
const createWorkspacePreset = vi.fn()
const deleteWorkspacePreset = vi.fn()
const updateWorkspacePreset = vi.fn()
const updateWorkspace = vi.fn()
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { ...WORKSPACE } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  updateWorkspace: (...a) => updateWorkspace(...a),
  fetchWorkspaces: () => Promise.resolve({ workspaces: [WORKSPACE] }),
  fetchMachines: () => Promise.resolve({ machines: [] }),
  listWorkspacePresets: (...a) => listWorkspacePresets(...a),
  createWorkspacePreset: (...a) => createWorkspacePreset(...a),
  deleteWorkspacePreset: (...a) => deleteWorkspacePreset(...a),
  updateWorkspacePreset: (...a) => updateWorkspacePreset(...a),
  searchWorkspaceSkills: () => Promise.resolve({ skills: [
    { name: 'release', description: 'Cut a release', workspaceEnabled: true },
    { name: 'review', workspaceEnabled: true },
  ] }),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')
const { notifyWebMCPChange } = await import('../src/composables/useWebMCPChanges')

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
  const q = (t) => el.querySelector(`[data-test=${t}]`)
  const fill = (t, value) => {
    q(t).value = value
    q(t).dispatchEvent(new Event('input'))
  }
  const names = () => [...el.querySelectorAll('[data-test=preset-row]')].map((r) => r.querySelector('span').textContent)
  return { el, q, fill, names }
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
  vi.stubGlobal('Worker', class {})
  listWorkspacePresets.mockResolvedValue({ presets: [{ name: 'pull-n-push', body: 'pull latest main, tag and push' }] })
  createWorkspacePreset.mockImplementation((id, name, body) => Promise.resolve({ preset: { name, body } }))
  deleteWorkspacePreset.mockResolvedValue(true)
  updateWorkspacePreset.mockImplementation((id, name, newName, body) => Promise.resolve({ preset: { name: newName, body } }))
})
afterEach(() => {
  app?.unmount()
  vi.unstubAllGlobals()
})

describe('the Presets tab', () => {
  it('lists the workspace\'s presets, with no Save bar', async () => {
    const page = await mount()
    expect(listWorkspacePresets).toHaveBeenCalledWith('ws1')
    expect(page.names()).toEqual(['#pull-n-push'])
    expect(page.q('preset-row').textContent).toContain('pull latest main, tag and push')
    expect([...page.el.querySelectorAll('button')].some((b) => b.textContent.includes('Update Workspace'))).toBe(false)
  })

  it('adds a preset at once, its name made a usable one, and clears the form', async () => {
    const page = await mount()
    expect(page.q('preset-add').disabled).toBe(true)
    page.fill('preset-name', 'Deploy Staging')
    page.fill('preset-body', 'deploy to staging')
    await settle()
    page.q('preset-add').click()
    await settle()
    expect(createWorkspacePreset).toHaveBeenCalledWith('ws1', 'deploy-staging', 'deploy to staging')
    expect(page.names()).toEqual(['#deploy-staging', '#pull-n-push'])
    expect(page.q('preset-name').value).toBe('')
    expect(page.q('preset-body').value).toBe('')
    expect(updateWorkspace).not.toHaveBeenCalled()
  })

  it('adds with Enter in the name, and shows the server\'s sentence when it refuses', async () => {
    createWorkspacePreset.mockRejectedValue(new Error('a preset named "pull-n-push" exists'))
    const page = await mount()
    page.fill('preset-name', 'pull-n-push')
    page.fill('preset-body', 'again')
    await settle()
    page.q('preset-name').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await settle()
    expect(page.q('presets-error').textContent).toBe('a preset named "pull-n-push" exists')
    expect(page.q('preset-name').value).toBe('pull-n-push')
  })

  it('does nothing for Enter on an incomplete form, and tidies the name on blur', async () => {
    const page = await mount()
    page.fill('preset-name', ' Ship It! ')
    page.q('preset-name').dispatchEvent(new Event('blur'))
    await settle()
    expect(page.q('preset-name').value).toBe('ship-it')
    page.q('preset-name').dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await settle()
    expect(createWorkspacePreset).not.toHaveBeenCalled()
  })

  it('edits a preset at once: its message and its name, shown in full in the form', async () => {
    const long = 'line one\nline two\nline three\nline four'
    listWorkspacePresets.mockResolvedValue({ presets: [{ name: 'pull-n-push', body: long }] })
    const page = await mount()
    expect(page.q('preset-form-title').textContent).toBe('Add a Preset')
    expect(page.q('preset-cancel')).toBeNull()

    createWorkspacePreset.mockRejectedValueOnce(new Error('a preset named "pull-n-push" exists'))
    page.fill('preset-name', 'pull-n-push')
    page.fill('preset-body', 'again')
    await settle()
    page.q('preset-add').click()
    await settle()
    expect(page.q('presets-error')).not.toBeNull()

    page.q('preset-edit').click()
    await settle()
    expect(page.q('presets-error')).toBeNull()
    expect(page.q('preset-form-title').textContent).toBe('Edit #pull-n-push')
    expect(page.q('preset-name').value).toBe('pull-n-push')
    expect(page.q('preset-body').value).toBe(long)
    expect(document.activeElement).toBe(page.q('preset-body'))
    expect(page.q('preset-add').textContent.trim()).toBe('Save Preset')

    page.fill('preset-name', 'Ship It')
    page.fill('preset-body', 'ship it')
    await settle()
    page.q('preset-add').click()
    await settle()
    expect(updateWorkspacePreset).toHaveBeenCalledWith('ws1', 'pull-n-push', 'ship-it', 'ship it')
    expect(createWorkspacePreset).toHaveBeenCalledTimes(1)
    expect(page.names()).toEqual(['#ship-it'])
    expect(page.q('preset-form-title').textContent).toBe('Add a Preset')
    expect(page.q('preset-name').value).toBe('')
    expect(page.q('preset-body').value).toBe('')
  })

  it('keeps the edit open when the server refuses it, and Cancel goes back to adding', async () => {
    updateWorkspacePreset.mockRejectedValue(new Error('a preset named "deploy" exists'))
    const page = await mount()
    page.q('preset-edit').click()
    await settle()
    page.fill('preset-name', 'deploy')
    await settle()
    page.q('preset-add').click()
    await settle()
    expect(page.q('presets-error').textContent).toBe('a preset named "deploy" exists')
    expect(page.q('preset-form-title').textContent).toBe('Edit #pull-n-push')
    expect(page.names()).toEqual(['#pull-n-push'])

    page.q('preset-cancel').click()
    await settle()
    expect(page.q('presets-error')).toBeNull()
    expect(page.q('preset-form-title').textContent).toBe('Add a Preset')
    expect(page.q('preset-name').value).toBe('')
    expect(page.q('preset-body').value).toBe('')
    expect(updateWorkspacePreset).toHaveBeenCalledTimes(1)
  })

  it('removes a preset at once', async () => {
    const page = await mount()
    page.q('preset-remove').click()
    await settle()
    expect(deleteWorkspacePreset).toHaveBeenCalledWith('ws1', 'pull-n-push')
    expect(page.names()).toEqual([])
    expect(page.q('presets-empty')).not.toBeNull()
  })

  it('says it is loading until the list arrives', async () => {
    let resolve
    listWorkspacePresets.mockReturnValue(new Promise((r) => { resolve = r }))
    const page = await mount()
    expect(page.el.textContent).toContain('Loading presets…')
    resolve({ presets: [] })
    await settle()
    expect(page.q('presets-empty')).not.toBeNull()
  })

  it('shows Saving… while the server answers', async () => {
    let resolve
    createWorkspacePreset.mockReturnValue(new Promise((r) => { resolve = r }))
    const page = await mount()
    page.fill('preset-name', 'deploy')
    page.fill('preset-body', 'x')
    await settle()
    page.q('preset-add').click()
    await settle()
    expect(page.q('preset-add').textContent.trim()).toBe('Saving…')
    resolve({ preset: { name: 'deploy', body: 'x' } })
    await settle()
    expect(page.q('preset-add').textContent.trim()).toBe('Add Preset')
  })

  it('loads again when a browser agent changes the presets', async () => {
    const page = await mount()
    listWorkspacePresets.mockResolvedValue({ presets: [{ name: 'deploy', body: 'x' }] })
    notifyWebMCPChange()
    await settle()
    expect(page.names()).toEqual(['#deploy'])
  })

  it('lists skills for a / typed in the message, also in its middle', async () => {
    const page = await mount()
    const field = page.q('preset-body')
    const menu = () => page.el.querySelector('[data-test=slash-menu]')
    const rows = () => [...page.el.querySelectorAll('[data-test=slash-menu] li')].map((li) => li.querySelector('span').textContent)
    field.value = 'pull main, /rev then push'
    field.setSelectionRange(15, 15)
    field.dispatchEvent(new Event('input'))
    await settle()
    expect(rows()).toEqual(['/review'])

    field.dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', bubbles: true, cancelable: true }))
    await settle()
    expect(field.value).toBe('pull main, /review then push')
    expect(menu()).toBeNull()

    page.fill('preset-body', '/')
    await settle()
    expect(rows()).toEqual(['/release', '/review'])
    page.el.querySelector('[data-test=slash-menu] li').dispatchEvent(new MouseEvent('mousedown', { bubbles: true, cancelable: true }))
    await settle()
    expect(field.value).toBe('/release ')
  })
})
