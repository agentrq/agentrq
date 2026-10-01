// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * A fork's settings page, mounted: it says where its settings come from,
 * does not let them be edited, shows the folder the machine made, and offers
 * Merge where a workspace offers Archive and Purge. The payload rule is
 * `workspaceForkSettings.test.js`'s; this is that the page sends it.
 * Last, which tab a workspace's settings live on.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { createPinia } from 'pinia'

const route = { params: { id: 'f1' }, query: {} }
const push = vi.fn()
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push }) }))

const PARENT = { id: 'p1', name: 'ops', workingDirectory: '/srv/ops', forkCount: 1 }
const FORK = {
  id: 'f1',
  name: 'ops-try',
  description: 'try things',
  forkOfId: 'p1',
  workingDirectory: '/home/me/.agentrq/forks/f1',
  selfLearningLoopNote: 'parent note',
  inputSendDelaySeconds: 5,
  allowAllCommands: false,
  clearContextDefault: false,
  notificationSettings: { taskCreated: true },
}
let loaded = FORK
let listed = []
const updateWorkspace = vi.fn((_id, ws) => Promise.resolve({ workspace: { ...loaded, ...ws } }))
const mergeFork = vi.fn(() => Promise.resolve({ parentId: 'p1', movedTasks: 1 }))
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  getWorkspace: () => Promise.resolve({ workspace: { ...loaded } }),
  getWorkspaceToken: () => Promise.resolve({ token: 't' }),
  updateWorkspace: (...a) => updateWorkspace(...a),
  fetchWorkspaces: () => Promise.resolve({ workspaces: listed }),
  fetchMachines: () => Promise.resolve({ machines: [] }),
  mergeFork: (...a) => mergeFork(...a),
  fetchTaskCounts: () => Promise.resolve({ completed: 1 }),
}))

const { default: WorkspaceSettingsView } = await import('../src/views/WorkspaceSettingsView.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')

const settle = () => new Promise((r) => setTimeout(r, 30))
const text = (n) => (n?.textContent ?? '').replace(/\s+/g, ' ').trim()
let app

async function mount({ workspace = FORK, list = [PARENT, { ...FORK, forkOf: { id: 'p1', name: 'ops' } }], tab } = {}) {
  loaded = workspace
  listed = list
  route.params = { id: workspace.id }
  route.query = tab ? { tab } : {}
  const el = document.createElement('div')
  document.body.appendChild(el)
  const pinia = createPinia()
  app = createApp({ render: () => h(WorkspaceSettingsView) })
  app.use(pinia)
  useWorkspaceStore(pinia).workspaces = list.map((w) => ({ ...w }))
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots }) => () => h('a', { href: String(p.to) }, slots.default?.()),
  })
  app.directive('click-outside', {})
  app.mount(el)
  await settle()
  const tabButton = (label) => [...el.querySelectorAll('nav button')].find((b) => text(b).toLowerCase() === label)
  return { el, tabButton }
}

beforeEach(() => {
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
})
afterEach(() => app?.unmount())

describe('a fork\'s settings page', () => {
  it('says where its settings, memory and skills come from, with a link there', async () => {
    const { el } = await mount()
    const banner = el.querySelector('[data-test=fork-banner]')
    expect(text(banner)).toMatch(/^Settings, memory and skills are inherited from ops\./)
    expect(banner.querySelector('a').getAttribute('href')).toBe('/workspaces/p1/settings')
  })

  it('shows the folder the machine made, read-only, and locks the inherited fields', async () => {
    const { el } = await mount()
    const dir = el.querySelector('#workingDirectory')
    expect(dir.readOnly).toBe(true)
    expect(dir.value).toBe('/home/me/.agentrq/forks/f1')
    const locked = [...el.querySelectorAll('fieldset')].map((f) => f.disabled)
    expect(locked).toEqual([true])
    expect(el.querySelector('input[type=text]').disabled).toBe(false)
  })

  it('says where the folder will come from before the first launch', async () => {
    const { el } = await mount({ workspace: { ...FORK, workingDirectory: '' } })
    expect(el.querySelector('#workingDirectory').placeholder).toBe('Made on the first launch, from /srv/ops')
  })

  it('saves its own fields and sends the inherited ones as they came', async () => {
    const { el } = await mount()
    const save = [...el.querySelectorAll('button')].find((b) => text(b) === 'Update Workspace')
    save.closest('form').dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    const [id, sent] = updateWorkspace.mock.calls[0]
    expect(id).toBe('f1')
    expect(sent.notificationSettings).toEqual({ taskCreated: true })
    expect(sent).not.toHaveProperty('autoAllowedTools')
    expect(sent).toMatchObject({ name: 'ops-try', selfLearningLoopNote: 'parent note', inputSendDelaySeconds: 5 })
  })

  it('locks the send delay but keeps the voice language and Save on the input tab', async () => {
    const { el } = await mount({ tab: 'input' })
    expect(el.querySelector('[data-test=send-delay]').disabled).toBe(true)
    expect(el.querySelector('[data-test=voice-language] select').disabled).toBe(false)
    expect([...el.querySelectorAll('button')].some((b) => text(b) === 'Update Workspace')).toBe(true)
  })

  it('locks the automations and notifications tabs, with no Save', async () => {
    const { el } = await mount({ tab: 'automations' })
    expect(el.querySelector('[data-test=automations]').disabled).toBe(true)
    expect([...el.querySelectorAll('button')].some((b) => text(b) === 'Update Workspace')).toBe(false)
    app.unmount()
    const n = await mount({ tab: 'notifications' })
    expect(n.el.querySelector('[data-test=event-triggers]').disabled).toBe(true)
  })

  it('offers Merge instead of Archive and Purge, and merges', async () => {
    const { el } = await mount({ tab: 'danger' })
    expect(text(el)).not.toMatch(/Archive Workspace|Purge Permanent/)
    const card = el.querySelector('[data-test=merge-card]')
    expect(text(card.querySelector('h4'))).toBe('Merge into ops')
    card.querySelector('button').click()
    await settle()
    const dialog = document.body.querySelector('[aria-labelledby=merge-modal-title]')
    expect(text(dialog.querySelector('[data-test=merge-message]'))).toMatch(/1 task moves back to ops/)
    ;[...dialog.querySelectorAll('button')].find((b) => text(b) === 'Merge').click()
    await settle()
    expect(mergeFork).toHaveBeenCalledWith('f1', { deleteFolder: false })
    expect(push).toHaveBeenCalledWith('/workspaces/p1')
  })

  it('keeps Merge disabled, and says why, while a task is unfinished', async () => {
    const { el } = await mount({ tab: 'danger', list: [PARENT, { ...FORK, forkOf: { id: 'p1', name: 'ops' }, unfinishedTasks: 2 }] })
    expect(text(el.querySelector('[data-test=merge-blocked]'))).toBe('Not yet: 2 tasks are not finished.')
    expect(el.querySelector('[data-test=merge-card] button').disabled).toBe(true)
  })
})

describe('a parent\'s settings page', () => {
  it('has no banner, and says its forks must be merged before it is archived or purged', async () => {
    const fork = { id: 'f1', name: 'ops try', forkOfId: 'p1' }
    const { el } = await mount({ workspace: PARENT, list: [PARENT, fork], tab: 'danger' })
    expect(el.querySelector('[data-test=fork-banner]')).toBe(null)
    expect(text(el.querySelector('[data-test=has-forks]'))).toBe('This workspace has 1 fork. Merge it back before archiving or purging it.')
    expect(text(el)).toMatch(/Archive Workspace/)
    app.unmount()
    const two = await mount({ workspace: PARENT, list: [PARENT, fork, { ...fork, id: 'f2' }], tab: 'danger' })
    expect(text(two.el.querySelector('[data-test=has-forks]'))).toBe('This workspace has 2 forks. Merge them back before archiving or purging it.')
  })
})

describe('a workspace\'s settings tabs', () => {
  it('puts the name beside the folder, and keeps input and local storage off General, each on a tab of its own', async () => {
    const { el, tabButton } = await mount({ workspace: PARENT, list: [PARENT] })
    const row = el.querySelector('#workingDirectory').closest('.md\\:grid-cols-4')
    expect(row.querySelector('input[placeholder="e.g. project-redstone"]')).not.toBe(null)
    for (const section of ['voice-language', 'send-delay', 'local-storage']) expect(el.querySelector(`[data-test=${section}]`)).toBe(null)
    tabButton('input').click()
    await settle()
    expect(text(el.querySelector('[data-test=voice-language]'))).toMatch(/^Voice Input Language/)
    expect(text(el.querySelector('[data-test=send-delay]'))).toMatch(/^Message Send Delay/)
    expect([...el.querySelectorAll('button')].some((b) => text(b) === 'Update Workspace')).toBe(true)
    tabButton('local storage').click()
    await settle()
    expect(text(el.querySelector('[data-test=local-storage]'))).toMatch(/^Keep a copy on this device/)
    expect(el.querySelector('[data-test=send-delay]')).toBe(null)
    expect([...el.querySelectorAll('button')].some((b) => text(b) === 'Update Workspace')).toBe(false)
  })
})
