// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * Workspace forks on screen: the sidebar's tree and row menu, the fork and
 * merge dialogs, the Move dialog's destinations, and the menu's keyboard use.
 *
 * The rules are `workspaceForks.test.js`'s and `workspaceContextMenu.test.js`'s;
 * this is that the right ones reach the page and the right calls leave it.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, ref } from 'vue'
import { createPinia } from 'pinia'

const route = { path: '/workspaces/p1', params: { id: 'p1' }, query: {} }
const push = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push }),
}))

let workspaceList = []
const fetchWorkspaces = vi.fn(() => Promise.resolve({ workspaces: workspaceList }))
const forkWorkspace = vi.fn(() => Promise.resolve({ workspace: { id: 'f9', name: 'ops fork' } }))
const mergeFork = vi.fn(() => Promise.resolve({ parentId: 'p1', movedTasks: 2 }))
const fetchTaskCounts = vi.fn(() => Promise.resolve({ ongoing: 0, notstarted: 0, scheduled: 0, completed: 2 }))
vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchWorkspaces: (...a) => fetchWorkspaces(...a),
  forkWorkspace: (...a) => forkWorkspace(...a),
  mergeFork: (...a) => mergeFork(...a),
  fetchTaskCounts: (...a) => fetchTaskCounts(...a),
}))

const { default: SidebarWorkspaces } = await import('../src/components/SidebarWorkspaces.vue')
const { default: WorkspaceForksBar } = await import('../src/components/WorkspaceForksBar.vue')
const { default: MoveTaskModal } = await import('../src/components/MoveTaskModal.vue')
const { default: ContextMenu } = await import('../src/components/ContextMenu.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')
const { useTooltipStore } = await import('../src/stores/tooltipStore')
const { useToasts } = await import('../src/composables/useToasts')

const settle = () => new Promise((r) => setTimeout(r, 30))

const PARENT = { id: 'p1', name: 'ops', workingDirectory: '/srv/ops' }
const WEB = { id: 'w1', name: 'web' }
const FORK = { id: 'f1', name: 'ops try', forkOfId: 'p1', forkOf: { id: 'p1', name: 'ops' }, unfinishedTasks: 0, workingDirectory: '/home/me/.agentrq/forks/f1' }
const SUPERVISOR = { id: 's1', name: 'supervisor' }

let apps = []
function mount(component, props = {}, workspaces = [PARENT, FORK, WEB, SUPERVISOR]) {
  workspaceList = workspaces.map((w) => ({ ...w }))
  const el = document.createElement('div')
  document.body.appendChild(el)
  const pinia = createPinia()
  const app = createApp({ render: () => h(component, props) })
  app.use(pinia)
  useWorkspaceStore(pinia).workspaces = workspaces.map((w) => ({ ...w }))
  app.component('RouterLink', {
    props: ['to'],
    setup: (p, { slots, attrs }) => () => h('a', { ...attrs, href: String(p.to) }, slots.default?.()),
  })
  app.mount(el)
  apps.push(app)
  return { el, pinia, store: useWorkspaceStore(pinia) }
}

const text = (node) => (node?.textContent ?? '').replace(/\s+/g, ' ').trim()
const menuButtons = () => [...document.body.querySelectorAll('[role=menu] button')]

beforeEach(() => {
  apps.forEach((a) => a.unmount())
  apps = []
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
})
afterEach(() => {
  apps.forEach((a) => a.unmount())
  apps = []
})

describe('the sidebar', () => {
  const sidebar = () => mount(SidebarWorkspaces, { workspaces: [PARENT, FORK, WEB, SUPERVISOR] })

  it('lists each fork under its parent, indented, with the fork mark', async () => {
    const { el } = sidebar()
    await settle()
    const rows = [...el.querySelectorAll('[data-test]')].map((r) => `${r.dataset.test}:${text(r.querySelector('span.truncate'))}`)
    expect(rows).toEqual(['sidebar-workspace:ops', 'sidebar-fork:ops-try', 'sidebar-workspace:web', 'sidebar-workspace:supervisor'])
    const fork = el.querySelector('[data-test=sidebar-fork] a')
    expect(fork.className).toMatch(/pl-6/)
    expect(fork.querySelector('svg circle')).toBeTruthy()
  })

  it('folds a parent\'s forks away, and remembers it', async () => {
    const { el } = sidebar()
    await settle()
    const fold = el.querySelector('[aria-label="Hide forks"]')
    fold.click()
    await settle()
    expect(el.querySelector('[data-test=sidebar-fork]')).toBe(null)
    expect(el.querySelector('[aria-label="Show 1 forks"]').getAttribute('aria-expanded')).toBe('false')
    expect(JSON.parse(localStorage.getItem('agentrq:sidebarCollapsedForks'))).toEqual(['p1'])

    const again = mount(SidebarWorkspaces, { workspaces: [PARENT, FORK] }).el
    await settle()
    expect(again.querySelector('[data-test=sidebar-fork]')).toBe(null)
    again.querySelector('[aria-label="Show 1 forks"]').click()
    await settle()
    expect(again.querySelector('[data-test=sidebar-fork]')).toBeTruthy()
  })

  it('offers Fork on a right-click, prompts on "<name>-fork", forks and goes to it', async () => {
    const { el } = sidebar()
    await settle()
    const row = el.querySelector('[data-test=sidebar-workspace] a')
    row.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true, clientX: 30, clientY: 40 }))
    await settle()
    expect(fetchWorkspaces).toHaveBeenCalled()
    expect(menuButtons().map(text)).toEqual(['Fork workspace'])

    menuButtons()[0].click()
    await settle()
    const input = document.body.querySelector('#fork-name')
    expect(input.value).toBe('ops-fork')
    input.value = 'Ops experiment'
    input.dispatchEvent(new Event('input'))
    await settle()
    expect(input.value).toBe('ops-experiment')
    input.value = 'ops-experiment!'
    input.dispatchEvent(new Event('input'))
    await settle()
    expect(input.value).toBe('ops-experiment') // a dropped keystroke leaves the field as it was
    input.value = ' '
    input.dispatchEvent(new Event('input'))
    await settle()
    const submit = document.body.querySelector('[aria-labelledby=fork-modal-title] button[type=submit]')
    expect(submit.disabled).toBe(true) // "-" is no name
    document.body.querySelector('[aria-labelledby=fork-modal-title] form').dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    expect(forkWorkspace).not.toHaveBeenCalled()
    input.value = 'ops-experiment'
    input.dispatchEvent(new Event('input'))
    await settle()
    document.body.querySelector('[aria-labelledby=fork-modal-title] form').dispatchEvent(new Event('submit', { cancelable: true }))
    await settle()
    expect(forkWorkspace).toHaveBeenCalledWith('p1', { name: 'ops-experiment' })
    expect(push).toHaveBeenCalledWith('/workspaces/f9')
  })

  it('offers Merge on a fork, disabled with the reason while a task is unfinished', async () => {
    const blocked = { ...FORK, unfinishedTasks: 2 }
    const { el } = mount(SidebarWorkspaces, { workspaces: [PARENT, blocked] }, [PARENT, blocked])
    await settle()
    el.querySelector('[data-test=sidebar-fork] a').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await settle()
    const [merge] = menuButtons()
    expect([...merge.querySelectorAll('span')].map(text)).toEqual(['Merge into ops', '2 tasks are not finished'])
    expect(merge.disabled).toBe(true)
    merge.click()
    await settle()
    expect(document.body.querySelector('[aria-labelledby=merge-modal-title]')).toBe(null)
  })

  it('confirms a merge with what will happen, then says how many moved and goes to the parent', async () => {
    const { el } = sidebar()
    await settle()
    el.querySelector('[data-test=sidebar-fork] a').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await settle()
    menuButtons()[0].click()
    await settle()
    const dialog = document.body.querySelector('[aria-labelledby=merge-modal-title]')
    expect(text(dialog.querySelector('#merge-modal-title'))).toBe('Merge into ops')
    expect(text(dialog.querySelector('[data-test=merge-message]'))).toBe(
      "The fork's agent is stopped, 2 tasks move back to ops, and the fork is removed. Its folder on the machine is left as it is: /home/me/.agentrq/forks/f1"
    )
    ;[...dialog.querySelectorAll('button')].find((b) => text(b) === 'Merge').click()
    await settle()
    expect(mergeFork).toHaveBeenCalledWith('f1', { deleteFolder: false })
    expect(useToasts().toasts.value.at(-1).message).toBe('Merged into ops: 2 tasks moved back')
    expect(push).toHaveBeenCalledWith('/workspaces/p1')
  })

  it('deletes the folder when the box is ticked, and starts unticked every time', async () => {
    const { el } = sidebar()
    await settle()
    const open = async () => {
      el.querySelector('[data-test=sidebar-fork] a').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
      await settle()
      menuButtons()[0].click()
      await settle()
      return document.body.querySelector('[aria-labelledby=merge-modal-title]')
    }
    let dialog = await open()
    const box = dialog.querySelector('[data-test=merge-delete-folder]')
    expect(box.checked).toBe(false)
    box.click()
    await settle()
    expect(text(dialog.querySelector('[data-test=merge-message]'))).toMatch(/Its folder on the machine is deleted, and its git branch is kept: \/home\/me/)
    ;[...dialog.querySelectorAll('button')].find((b) => text(b) === 'Cancel').click()
    await settle()

    dialog = await open()
    expect(dialog.querySelector('[data-test=merge-delete-folder]').checked).toBe(false)
    dialog.querySelector('[data-test=merge-delete-folder]').click()
    await settle()
    ;[...dialog.querySelectorAll('button')].find((b) => text(b) === 'Merge').click()
    await settle()
    expect(mergeFork).toHaveBeenLastCalledWith('f1', { deleteFolder: true })
  })

  it('offers no checkbox for a fork with no folder yet', async () => {
    const list = [PARENT, { ...FORK, workingDirectory: '' }, WEB, SUPERVISOR]
    const { el } = mount(SidebarWorkspaces, { workspaces: list }, list)
    await settle()
    el.querySelector('[data-test=sidebar-fork] a').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await settle()
    menuButtons()[0].click()
    await settle()
    const dialog = document.body.querySelector('[aria-labelledby=merge-modal-title]')
    expect(dialog.querySelector('[data-test=merge-delete-folder]')).toBe(null)
    ;[...dialog.querySelectorAll('button')].find((b) => text(b) === 'Merge').click()
    await settle()
    expect(mergeFork).toHaveBeenLastCalledWith('f1', { deleteFolder: false })
  })

  it('shows a 409 from the merge as the server wrote it', async () => {
    mergeFork.mockImplementationOnce(() => Promise.reject(new Error('a task in this fork was reopened')))
    const { el } = sidebar()
    await settle()
    el.querySelector('[data-test=sidebar-fork] a').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await settle()
    menuButtons()[0].click()
    await settle()
    ;[...document.body.querySelectorAll('[aria-labelledby=merge-modal-title] button')].find((b) => text(b) === 'Merge').click()
    await settle()
    expect(useToasts().toasts.value.at(-1).message).toBe('a task in this fork was reopened')
    expect(push).not.toHaveBeenCalled()
  })

  it('opens the menu from the row\'s button and from Shift+F10, with the focus in it', async () => {
    const { el } = sidebar()
    await settle()
    el.querySelector('[aria-label="web actions"]').click()
    await settle()
    expect(menuButtons().map(text)).toEqual(['Fork workspace'])
    expect(document.activeElement).toBe(menuButtons()[0])

    document.body.querySelector('[role=menu]').dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }))
    await settle()
    expect(menuButtons()).toHaveLength(0)

    el.querySelectorAll('[data-test=sidebar-workspace] a')[1].dispatchEvent(new KeyboardEvent('keydown', { key: 'F10', shiftKey: true, bubbles: true }))
    await settle()
    expect(menuButtons().map(text)).toEqual(['Fork workspace'])
    menuButtons()[0].click()
    await settle()
    expect(document.body.querySelector('#fork-name').value).toBe('web-fork')
  })

  it('offers nothing on the supervisor workspace', async () => {
    const { el } = sidebar()
    await settle()
    expect(el.querySelector('[aria-label="supervisor actions"]')).toBe(null)
    const rows = el.querySelectorAll('[data-test=sidebar-workspace] a')
    rows[rows.length - 1].dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    rows[rows.length - 1].dispatchEvent(new KeyboardEvent('keydown', { key: 'ContextMenu', bubbles: true }))
    rows[rows.length - 1].dispatchEvent(new KeyboardEvent('keydown', { key: 'a', bubbles: true }))
    await settle()
    expect(menuButtons()).toHaveLength(0)
  })
})

describe('what each sidebar row is doing', () => {
  const BUSY = { ...PARENT, agentConnected: true, taskSummary: { ongoing: 1, needsYou: 1, current: { id: 't1', title: 'Fix the build' } } }
  const ASKING = { ...FORK, agentConnected: true, taskSummary: { ongoing: 1, needsYou: 2, current: { id: 't2', title: 'Try Postgres' } } }

  let store
  const tooltipText = () => store.text
  const tooltipVisible = () => store.visible

  it('pulses the dot of an agent at work, counts what needs you, and says it on hover', async () => {
    const tooltip = vi.fn()
    const hide = vi.fn()
    const { el, pinia } = mount(SidebarWorkspaces, { workspaces: [BUSY, ASKING, WEB], showTooltip: tooltip, hideTooltip: hide })
    store = useTooltipStore(pinia)
    await settle()
    const rows = [...el.querySelectorAll('[data-test=sidebar-workspace], [data-test=sidebar-fork]')]
    expect(rows.map((r) => !!r.querySelector('[data-test=sidebar-working]'))).toEqual([true, true, false])
    expect(rows.map((r) => text(r.querySelector('[data-test=sidebar-needs-you]')))).toEqual(['1', '2', ''])

    rows[0].querySelector('a').dispatchEvent(new MouseEvent('mouseenter'))
    expect(tooltipText()).toBe('ops\nWorking on Fix the build\n1 needs you')
    rows[0].querySelector('a').dispatchEvent(new MouseEvent('mouseleave'))
    expect(tooltipVisible()).toBe(false)
    expect(hide).toHaveBeenCalled()
    // A row with nothing going on leaves its name to the shell.
    rows[2].querySelector('a').dispatchEvent(new MouseEvent('mouseenter'))
    expect(tooltip.mock.calls.at(-1)[1]).toBe('web')
    expect(tooltipVisible()).toBe(false)
  })

  it('sums up its folded forks on the parent\'s row', async () => {
    const { el, pinia } = mount(SidebarWorkspaces, { workspaces: [BUSY, ASKING, WEB] })
    store = useTooltipStore(pinia)
    await settle()
    el.querySelector('[aria-label="Hide forks"]').click()
    await settle()
    const parent = el.querySelector('[data-test=sidebar-workspace]')
    expect(text(parent.querySelector('[data-test=sidebar-needs-you]'))).toBe('3')
    parent.querySelector('a').dispatchEvent(new MouseEvent('mouseenter'))
    expect(tooltipText()).toBe('ops\nWorking on Fix the build\n1 needs you\nops try — 2 need you')
  })
})

describe('the forks bar', () => {
  const DONE = { ...FORK, agentConnected: true, taskSummary: { ongoing: 0, needsYou: 0, done: 2, total: 2 } }
  const BUSY = { id: 'f2', name: 'ops pg', forkOfId: 'p1', agentConnected: true, unfinishedTasks: 3, taskSummary: { ongoing: 2, needsYou: 0, done: 1, total: 4, current: { id: 't2', title: 'Try Postgres' } } }
  const bar = (workspaces = [PARENT, DONE, BUSY, WEB]) => mount(WorkspaceForksBar, { workspaceId: 'p1' }, workspaces)

  it('shows each fork of the workspace busiest first, with Merge once it is done and its state on hover', async () => {
    const ASKING = { id: 'f3', name: 'ops ask', forkOfId: 'p1', unfinishedTasks: 1, taskSummary: { needsYou: 1, total: 1 } }
    const { el, pinia } = bar([PARENT, DONE, BUSY, ASKING, WEB])
    await settle()
    const chips = [...el.querySelectorAll('[data-test=fork-chip]')]
    expect(chips.map((c) => c.querySelector('a').getAttribute('href'))).toEqual(['/workspaces/f3', '/workspaces/f2', '/workspaces/f1'])
    expect(chips.map((c) => text(c.querySelector('[data-test=fork-needs-you]')))).toEqual(['1', '', ''])
    expect(chips.map((c) => !!c.querySelector('[data-test=fork-working]'))).toEqual([false, true, false])
    expect(chips.map((c) => [...c.querySelectorAll('button')].map(text))).toEqual([[], [], ['Merge']])
    expect(el.querySelector('[data-test=forks-more]')).toBe(null)

    const tooltip = useTooltipStore(pinia)
    chips[1].dispatchEvent(new MouseEvent('mouseenter'))
    expect(tooltip.text).toBe('ops pg\nWorking on Try Postgres (+1 more)\n1/4 done')
    chips[1].dispatchEvent(new MouseEvent('mouseleave'))
    expect(tooltip.visible).toBe(false)
  })

  it('shows five forks, and the rest on "+N more"', async () => {
    const many = Array.from({ length: 7 }, (_, i) => ({ id: `m${i}`, name: `try ${i}`, forkOfId: 'p1', unfinishedTasks: 1 }))
    const { el } = bar([PARENT, ...many])
    await settle()
    const chips = () => el.querySelectorAll('[data-test=fork-chip]').length
    const more = () => el.querySelector('[data-test=forks-more]')
    expect(chips()).toBe(5)
    expect(text(more())).toBe('+2 more')
    more().click()
    await settle()
    expect(chips()).toBe(7)
    expect(text(more())).toBe('Show less')
    more().click()
    await settle()
    expect(chips()).toBe(5)
  })

  it('merges a fork from its chip, through the merge dialog', async () => {
    const { el } = bar()
    await settle()
    el.querySelector('[data-test=fork-chip]:last-child button').click()
    await settle()
    const dialog = document.body.querySelector('[aria-labelledby=merge-modal-title]')
    expect(text(dialog.querySelector('#merge-modal-title'))).toBe('Merge into ops')
    ;[...dialog.querySelectorAll('button')].find((b) => text(b) === 'Merge').click()
    await settle()
    expect(mergeFork).toHaveBeenCalledWith('f1', { deleteFolder: false })
    expect(push).toHaveBeenCalledWith('/workspaces/p1')
  })

  it('shows nothing for a workspace with no forks', async () => {
    const { el } = bar([PARENT, WEB])
    await settle()
    expect(el.querySelector('[data-test=forks-bar]')).toBe(null)
  })
})

describe('the context menu from the keyboard', () => {
  it('moves the focus with the arrows, wrapping, and skips a disabled item', async () => {
    const items = [{ key: 'a', label: 'A' }, { key: 'b', label: 'B', disabled: true, detail: 'why' }, { key: 'c', label: 'C' }]
    const onSelect = vi.fn()
    const open = ref(false)
    mount({ render: () => h(ContextMenu, { show: open.value, items, autofocus: true, onSelect }) })
    // Opened after mount, as a real menu is.
    open.value = true
    await settle()
    const [a, b, c] = menuButtons()
    expect(document.activeElement).toBe(a)
    expect(b.disabled).toBe(true)
    expect([...b.querySelectorAll('span')].map(text)).toEqual(['B', 'why'])
    const menu = document.body.querySelector('[role=menu]')
    const key = (k) => menu.dispatchEvent(new KeyboardEvent('keydown', { key: k, bubbles: true, cancelable: true }))
    key('ArrowDown')
    expect(document.activeElement).toBe(c)
    key('ArrowDown')
    expect(document.activeElement).toBe(a)
    key('ArrowUp')
    expect(document.activeElement).toBe(c)
    key('Tab')
    expect(document.activeElement).toBe(c)
    c.click()
    expect(onSelect).toHaveBeenCalledWith('c')
  })
})

describe('the Move dialog', () => {
  const open = async (current, workspaces = [PARENT, FORK, WEB, SUPERVISOR]) => {
    const onConfirm = vi.fn()
    const show = ref(false)
    const { el } = mount({ render: () => h(MoveTaskModal, { show: show.value, taskTitle: 'Fix login', currentWorkspaceId: current, onConfirm }) }, {}, workspaces)
    show.value = true
    await settle()
    return { el, onConfirm, dialog: () => document.body.querySelector('[role=dialog]') }
  }

  it('lists forks under their parent, and the current workspace only as their heading', async () => {
    const { dialog } = await open('p1')
    const labels = [...dialog().querySelectorAll('.divide-y > *')].map(text)
    expect(labels).toEqual(['ops (this workspace)', 'ops try', 'supervisor', 'web', 'New fork of this workspace…'])
    expect(dialog().querySelector('[data-test=move-fork]').className).toMatch(/pl-7/)
  })

  it('forks, then moves into the new fork, named after the task', async () => {
    const { dialog, onConfirm } = await open('p1')
    dialog().querySelector('[data-test=move-new-fork]').click()
    await settle()
    const name = dialog().querySelector('#move-fork-name')
    expect(name.value).toBe('fix-login')
    const go = [...dialog().querySelectorAll('button')].find((b) => text(b) === 'Fork and move')
    go.click()
    await settle()
    expect(forkWorkspace).toHaveBeenCalledWith('p1', { name: 'fix-login' })
    expect(fetchWorkspaces).toHaveBeenCalled()
    expect(onConfirm).toHaveBeenCalledWith('f9')
  })

  it('says why a fork could not be made, and moves nothing', async () => {
    forkWorkspace.mockImplementationOnce(() => Promise.reject(new Error('rate limit exceeded')))
    const { dialog, onConfirm } = await open('p1')
    dialog().querySelector('[data-test=move-new-fork]').click()
    await settle()
    const name = dialog().querySelector('#move-fork-name')
    name.value = '  '
    name.dispatchEvent(new Event('input'))
    await settle()
    expect(name.value).toBe('-') // kebab-cased as typed, and still no name
    const go = [...dialog().querySelectorAll('button')].find((b) => text(b) === 'Fork and move')
    expect(go.disabled).toBe(true)
    name.value = 'x'
    name.dispatchEvent(new Event('input'))
    await settle()
    go.click()
    await settle()
    expect(text(dialog())).toMatch(/rate limit exceeded/)
    expect(onConfirm).not.toHaveBeenCalled()
  })

  it('moves to a fork like to any workspace, and offers no new fork from a fork', async () => {
    const { dialog, onConfirm } = await open('f1')
    expect(dialog().querySelector('[data-test=move-new-fork]')).toBe(null)
    const labels = [...dialog().querySelectorAll('.divide-y > *')].map(text)
    expect(labels).toEqual(['ops', 'supervisor', 'web'])
    ;[...dialog().querySelectorAll('.divide-y > button')][0].click()
    await settle()
    ;[...dialog().querySelectorAll('button')].find((b) => text(b) === 'Move').click()
    expect(onConfirm).toHaveBeenCalledWith('p1')
  })

  it('has nothing to offer when there is nowhere else', async () => {
    const { dialog } = await open('s1', [SUPERVISOR])
    expect(text(dialog())).toMatch(/No other workspaces available/)
  })
})
