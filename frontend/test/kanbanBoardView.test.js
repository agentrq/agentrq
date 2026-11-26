// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The board's columns, mounted: the order they draw in and what a drop saves.
 *
 * The ordering rules are tested in `kanbanOrder.test.js`. This is the join
 * that decides which rule a column uses and which call a drop makes.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, reactive, ref } from 'vue'
import { createPinia } from 'pinia'

// The board under a workspace has `id` in the route; on /kanban it has none.
// Reactive, as the router's is: the board follows its query.
const route = reactive({ params: { id: 'ws1' }, query: {} })
const push = vi.fn()
const replace = vi.fn()
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push, replace }),
}))

const events = ref([])
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events }),
}))

vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTasks: () => {},
  sharedCache: () => null,
}))
const readAllCachedTasks = vi.fn(() => Promise.resolve([]))
vi.mock('../src/composables/useCachedReads', () => ({
  readCachedTasks: () => Promise.resolve([]),
  readAllCachedTasks: (...args) => readAllCachedTasks(...args),
  shouldPaintCache: () => false,
}))

let byStatus = {}
const updateTaskStatus = vi.fn()
const updateTaskAssignee = vi.fn(() => Promise.resolve({}))
const sendPermissionVerdict = vi.fn(() => Promise.resolve({}))
const updateTaskOrder = vi.fn(() => Promise.resolve({}))
const fetchTasks = vi.fn((_ws, { status }) => Promise.resolve({ tasks: byStatus[status] || [] }))
const fetchGlobalTasks = vi.fn(({ status }) => Promise.resolve({ tasks: byStatus[status] || [] }))
const moveTask = vi.fn(() => Promise.resolve({}))
const forkWorkspace = vi.fn(() => Promise.resolve({ workspace: { id: 'f1', name: 'task-n1' } }))
const launchAgent = vi.fn(() => Promise.resolve({ session: { id: 's1' } }))
let workspaceList = []
const fetchWorkspaces = vi.fn(() => Promise.resolve({ workspaces: workspaceList }))
const fetchTeamTasks = vi.fn((_team, { status }) => Promise.resolve({ tasks: byStatus[status] || [] }))
const fetchTeamWorkspaces = vi.fn(() => Promise.resolve({ workspaces: [{ id: 'theirs', name: 'api', sharedName: 'Backend' }] }))

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchTasks: (...args) => fetchTasks(...args),
  fetchGlobalTasks: (...args) => fetchGlobalTasks(...args),
  fetchWorkspaces: (...args) => fetchWorkspaces(...args),
  fetchTeamTasks: (...args) => fetchTeamTasks(...args),
  fetchTeamWorkspaces: (...args) => fetchTeamWorkspaces(...args),
  moveTask: (...args) => moveTask(...args),
  fetchMachines: () => Promise.resolve({ machines: [{ id: 'm1', name: 'pi', enabled: true, online: true }] }),
  forkWorkspace: (...args) => forkWorkspace(...args),
  launchAgent: (...args) => launchAgent(...args),
  recordTelemetry: () => {},
  updateTaskStatus: (...args) => updateTaskStatus(...args),
  updateTaskAssignee: (...args) => updateTaskAssignee(...args),
  sendPermissionVerdict: (...args) => sendPermissionVerdict(...args),
  updateTaskOrder: (...args) => updateTaskOrder(...args),
}))

const { default: KanbanBoardView } = await import('../src/views/KanbanBoardView.vue')
const { default: KanbanView } = await import('../src/views/KanbanView.vue')
const { useWorkspaceStore } = await import('../src/stores/workspaceStore')
const { useKanbanScheduled, KANBAN_SCHEDULED_KEY } = await import('../src/composables/useKanbanScheduled')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))

let app
async function mount({ component = KanbanBoardView, workspaces } = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(component) })
  const pinia = createPinia()
  app.use(pinia)
  // Copied: the store writes into the array it is given, and a later case must not see it.
  if (workspaces) useWorkspaceStore(pinia).workspaces = workspaces.map((w) => ({ ...w }))
  app.directive('click-outside', {})
  app.mount(el)
  await settle()

  const column = (title) =>
    [...el.querySelectorAll('h3')].find((n) => n.textContent.trim() === title).closest('.rounded-xl')
  const inColumn = (title, text) =>
    [...column(title).querySelectorAll('[draggable]')].some((c) => c.textContent.includes(text))
  const card = (text) => [...el.querySelectorAll('[draggable]')].find((c) => c.textContent.includes(text))

  const fire = (target, type) => {
    const e = new Event(type, { bubbles: true, cancelable: true })
    e.dataTransfer = { setData() {}, effectAllowed: '' }
    e.clientY = 0
    target.dispatchEvent(e)
  }
  /** Drag `text`'s card and drop it just above `beforeText`'s, or at the end of `colTitle`. */
  const drag = async (text, { before, into }) => {
    fire(card(text), 'dragstart')
    await settle()
    if (before) fire(card(before), 'dragover')
    else fire(column(into), 'dragover')
    await settle()
    fire(before ? card(before) : column(into), 'drop')
    await settle()
  }
  return { el, inColumn, drag, card, fire }
}

const task = (id, status, extra = {}) => ({
  id,
  title: `Task ${id}`,
  status,
  assignee: 'agent',
  workspaceId: 'ws1',
  createdAt: '2026-09-20T10:00:00Z',
  messages: [],
  ...extra,
})

const order = (text, el) =>
  [...el.querySelectorAll('[draggable]')].map((c) => c.textContent).findIndex((t) => t.includes(text))

describe('KanbanBoardView', () => {
  beforeEach(() => {
    if (app) app.unmount()
    document.body.innerHTML = ''
    localStorage.clear()
    useKanbanScheduled().setShowScheduled(false)
    events.value = []
    route.params = { id: 'ws1' }
    push.mockClear()
    updateTaskStatus.mockReset()
    updateTaskOrder.mockClear()
    fetchTasks.mockClear()
    fetchGlobalTasks.mockClear()
    fetchWorkspaces.mockClear()
    moveTask.mockClear()
    readAllCachedTasks.mockClear()
    workspaceList = []
    byStatus = {
      notstarted: [
        task('n1', 'notstarted', { sortOrder: 100 }),
        task('n2', 'notstarted', { sortOrder: 200 }),
        task('n3', 'notstarted', { sortOrder: 300 }),
      ],
      ongoing: [task('o1', 'ongoing', { sortOrder: 50, updatedAt: '2026-09-27T09:00:00Z' })],
      'completed,rejected': [
        // Created (and so positioned) oldest first, finished in the other order.
        task('d1', 'completed', { sortOrder: 1, updatedAt: '2026-09-25T10:00:00Z' }),
        task('d2', 'rejected', { sortOrder: 2, updatedAt: '2026-09-26T10:00:00Z' }),
        task('d3', 'completed', { sortOrder: 3, updatedAt: '2026-09-27T10:00:00Z' }),
      ],
    }
  })

  describe('the Scheduled column', () => {
    const titles = (el) => [...el.querySelectorAll('h3.uppercase')].map((n) => n.textContent.trim())
    const cronFetched = () => fetchTasks.mock.calls.some(([, q]) => q.status === 'cron')
    beforeEach(() => {
      byStatus.cron = [task('c1', 'cron', { cronSchedule: '0 9 * * *' })]
    })

    it('is hidden, and not fetched, on every workspace\'s board until turned on', async () => {
      route.params = {}
      const { el } = await mount({ workspaces: [{ id: 'ws1', name: 'ops' }] })
      expect(titles(el)).toEqual(['Not Started', 'Ongoing', 'Blocked', 'Done'])
      expect(fetchGlobalTasks.mock.calls.some(([q]) => q.status === 'cron')).toBe(false)
      expect(el.textContent).not.toContain('Task c1')
    })

    it('comes first once its toggle is on, fetched and remembered, and goes when it is off', async () => {
      route.params = {}
      const { el, card } = await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
      const toggle = el.querySelector('[data-test=scheduled-toggle]')
      expect(toggle.getAttribute('aria-pressed')).toBe('false')

      toggle.click()
      await settle()
      expect(titles(el)).toEqual(['Scheduled', 'Not Started', 'Ongoing', 'Blocked', 'Done'])
      expect(fetchGlobalTasks).toHaveBeenCalledWith(expect.objectContaining({ status: 'cron' }))
      expect(el.textContent).toContain('Task c1')
      // When it next runs, in place of when it was made.
      expect(el.querySelector('[data-test=card-next-run]').textContent.trim()).toMatch(/^In /)
      expect(card('Task c1').textContent).not.toContain('ago')
      expect(toggle.getAttribute('aria-pressed')).toBe('true')
      expect(localStorage.getItem(KANBAN_SCHEDULED_KEY)).toBe('1')

      toggle.click()
      await settle()
      expect(titles(el)).not.toContain('Scheduled')
      expect(localStorage.getItem(KANBAN_SCHEDULED_KEY)).toBeNull()
    })

    it('loads with the board when it was left on', async () => {
      useKanbanScheduled().setShowScheduled(true)
      route.params = {}
      const { el } = await mount({ workspaces: [{ id: 'ws1', name: 'ops' }] })
      expect(titles(el)[0]).toBe('Scheduled')
      expect(fetchGlobalTasks).toHaveBeenCalledWith(expect.objectContaining({ status: 'cron' }))
    })

    it('is always first on one workspace\'s board, toggle or not', async () => {
      const { el } = await mount()
      expect(titles(el)).toEqual(['Scheduled', 'Not Started', 'Ongoing', 'Blocked', 'Done'])
      expect(cronFetched()).toBe(true)
      expect(el.textContent).toContain('Task c1')
    })

    it('takes no drag in or out, since a scheduled task\'s status never changes', async () => {
      useKanbanScheduled().setShowScheduled(true)
      route.params = {}
      const { card, drag, inColumn, fire } = await mount({ workspaces: [{ id: 'ws1', name: 'ops' }] })
      expect(card('Task c1').getAttribute('draggable')).toBe('false')

      fire(card('Task c1'), 'dragstart')
      await settle()
      await drag('Task n1', { into: 'Scheduled' })
      await drag('Task n2', { before: 'Task c1' })

      expect(updateTaskStatus).not.toHaveBeenCalled()
      expect(updateTaskOrder).not.toHaveBeenCalled()
      expect(inColumn('Not Started', 'Task n1')).toBe(true)
      expect(inColumn('Scheduled', 'Task c1')).toBe(true)
    })
  })

  it('lists Done with the most recently finished task first', async () => {
    const { el } = await mount()
    expect(order('Task d3', el)).toBeLessThan(order('Task d2', el))
    expect(order('Task d2', el)).toBeLessThan(order('Task d1', el))
  })

  it('saves the midpoint when a Not Started card is dragged between two others', async () => {
    const { el, drag } = await mount()
    await drag('Task n3', { before: 'Task n2' })

    expect(updateTaskOrder).toHaveBeenCalledWith('ws1', 'n3', 150)
    expect(updateTaskStatus).not.toHaveBeenCalled()
    expect(order('Task n3', el)).toBeLessThan(order('Task n2', el))
  })

  it('puts a card dropped into Done at the top, with the server’s timestamp', async () => {
    updateTaskStatus.mockResolvedValue({
      task: task('o1', 'completed', { sortOrder: 50, updatedAt: '2026-09-27T11:00:00Z' }),
    })
    const { el, drag } = await mount()
    await drag('Task o1', { into: 'Done' })

    expect(updateTaskStatus).toHaveBeenCalledWith('ws1', 'o1', 'completed')
    expect(updateTaskOrder).not.toHaveBeenCalled()
    expect(order('Task o1', el)).toBeLessThan(order('Task d3', el))
  })

  it('puts a card back where it was when the move into Done fails', async () => {
    updateTaskStatus.mockRejectedValue(new Error('network unreachable'))
    const { inColumn, drag } = await mount()
    await drag('Task o1', { into: 'Done' })

    const { useToasts } = await import('../src/composables/useToasts')
    expect(useToasts().toasts.value.at(-1).message).toContain('network unreachable')
    expect(inColumn('Ongoing', 'Task o1')).toBe(true)
    expect(inColumn('Done', 'Task o1')).toBe(false)
  })

  it('saves nothing for a drag within Done, which is not ordered by hand', async () => {
    const { el, drag } = await mount()
    await drag('Task d1', { before: 'Task d3' })

    expect(updateTaskOrder).not.toHaveBeenCalled()
    expect(updateTaskStatus).not.toHaveBeenCalled()
    expect(order('Task d3', el)).toBeLessThan(order('Task d1', el))
  })

  it('names the agent on a card that one has moved, and on none other', async () => {
    byStatus.ongoing = [task('o1', 'ongoing', { agent: 'claude-code' })]
    const { card } = await mount()
    const agent = (text) => card(text).querySelector('[data-test="card-agent"]')
    expect(agent('Task o1').textContent.trim()).toBe('claude-code')
    expect(agent('Task o1').title).toBe('Agent: claude-code')
    expect(agent('Task n1')).toBeNull()
  })

  it('draws the agent’s own icon beside its name, and none for an agent it does not know', async () => {
    byStatus.ongoing = [
      task('o1', 'ongoing', { agent: 'claude-code' }),
      task('o2', 'ongoing', { agent: 'pi' }),
      task('o3', 'ongoing', { agent: 'my-own-agent' }),
    ]
    const { card } = await mount()
    const icon = (text) => card(text).querySelector('[data-test="card-agent"] [data-test="agent-icon"]')
    expect(icon('Task o1').tagName).toBe('IMG')
    expect(icon('Task o1').getAttribute('src')).toContain('claude-agent')
    // An SVG goes in as markup, so a one-colour icon takes the text's colour.
    expect(icon('Task o2').tagName).toBe('SPAN')
    expect(icon('Task o2').querySelector('svg')).not.toBeNull()
    expect(icon('Task o3')).toBeNull()
    expect(card('Task o3').querySelector('[data-test="card-agent"]').textContent.trim()).toBe('my-own-agent')
  })

  it('names the agent the moment a live update brings one, and keeps it through one that does not', async () => {
    const { card } = await mount()
    const agent = (text) => card(text).querySelector('[data-test="card-agent"]')
    events.value = [{ type: 'task.updated', payload: task('n1', 'ongoing', { agent: 'codex', updatedAt: '2026-09-28T10:00:00Z' }) }]
    await settle()
    expect(agent('Task n1').textContent.trim()).toBe('codex')
    events.value = [...events.value, { type: 'task.updated', payload: task('n1', 'blocked', { updatedAt: '2026-09-28T11:00:00Z' }) }]
    await settle()
    expect(agent('Task n1').textContent.trim()).toBe('codex')
  })

  describe('the timing footer', () => {
    const mins = (m) => new Date(Date.now() - m * 60000).toISOString()
    const footer = (card, text) => card(text).querySelector('[data-test="card-timing"]')
    const parts = (card, text) => {
      const f = footer(card, text)
      return [f.querySelector('[data-test="card-worked"]')?.textContent.trim() ?? '', f.querySelector('[data-test="card-state"]').textContent.trim()]
    }

    it('shows the time worked and the current state on ongoing, blocked and closed cards, and none elsewhere', async () => {
      byStatus.ongoing = [task('o1', 'ongoing', { timing: { workedSeconds: 600, workingSince: mins(5), statusSince: mins(5) } })]
      byStatus.blocked = [task('b1', 'blocked', { timing: { workedSeconds: 1380, statusSince: mins(47) } })]
      byStatus['completed,rejected'] = [
        task('d1', 'completed', { timing: { workedSeconds: 3660, statusSince: mins(42) } }),
        task('d2', 'rejected', { timing: { workedSeconds: 0, statusSince: mins(180) } }),
      ]
      byStatus.notstarted = [task('n1', 'notstarted', { timing: { workedSeconds: 0, statusSince: mins(3) } })]
      const { card } = await mount()
      expect(parts(card, 'Task o1')).toEqual(['15m', '5m'])
      expect(parts(card, 'Task b1')).toEqual(['23m', '47m'])
      expect(parts(card, 'Task d1')).toEqual(['1h 1m', '✓ 42m ago'])
      expect(parts(card, 'Task d2')).toEqual(['', '✗ 3h ago'])
      expect(footer(card, 'Task n1')).toBeNull()
    })

    it('times a move a live update brings, which carries no timing', async () => {
      byStatus.ongoing = [task('o1', 'ongoing', { updatedAt: mins(60), timing: { workedSeconds: 60, workingSince: mins(10), statusSince: mins(10) } })]
      const { card } = await mount()
      events.value = [{ type: 'status.updated', payload: task('o1', 'completed', { updatedAt: new Date().toISOString() }) }]
      await settle()
      expect(parts(card, 'Task o1')).toEqual(['11m', '✓ now'])
    })

    it('puts the timing back with the card when a move fails', async () => {
      updateTaskStatus.mockRejectedValue(new Error('network unreachable'))
      byStatus.ongoing = [task('o1', 'ongoing', { timing: { workedSeconds: 0, workingSince: mins(5), statusSince: mins(5) } })]
      const { card, drag } = await mount()
      await drag('Task o1', { into: 'Done' })
      expect(parts(card, 'Task o1')).toEqual(['5m', '5m'])
    })
  })

  it('names no workspace on a card when the board is one workspace’s', async () => {
    const { el } = await mount()
    expect(el.querySelector('[data-test="card-workspace"]')).toBeNull()
    expect(fetchGlobalTasks).not.toHaveBeenCalled()
  })
})

describe('KanbanBoardView across every workspace', () => {
  const workspaces = [
    { id: 'ws1', name: 'alpha', agentConnected: true },
    { id: 'ws2', name: 'beta', agentConnected: false },
    { id: 'ws3', name: 'gamma', agentConnected: false, archivedAt: '2026-09-01T00:00:00Z' },
  ]

  beforeEach(() => {
    if (app) app.unmount()
    document.body.innerHTML = ''
    localStorage.clear()
    events.value = []
    route.params = {}
    push.mockClear()
    updateTaskStatus.mockReset()
    updateTaskOrder.mockClear()
    fetchTasks.mockClear()
    fetchGlobalTasks.mockClear()
    fetchWorkspaces.mockClear()
    moveTask.mockClear()
    readAllCachedTasks.mockClear()
    workspaceList = workspaces
    byStatus = {
      notstarted: [
        task('a1', 'notstarted', { workspaceId: 'ws1', sortOrder: 100 }),
        task('b1', 'notstarted', { workspaceId: 'ws2', sortOrder: 200 }),
        task('b2', 'notstarted', { workspaceId: 'ws2', sortOrder: 300 }),
        task('g1', 'notstarted', { workspaceId: 'ws3', sortOrder: 400 }),
      ],
    }
  })

  const chip = (card) => card.querySelector('[data-test="card-workspace"]')

  it('lists every workspace’s tasks, from the account-wide list and cache', async () => {
    const { card } = await mount({ workspaces })
    expect(fetchTasks).not.toHaveBeenCalled()
    expect(fetchGlobalTasks).toHaveBeenCalledWith({ status: 'notstarted', limit: 10, offset: 0 })
    expect(readAllCachedTasks).toHaveBeenCalled()
    expect(card('Task a1')).toBeTruthy()
    expect(card('Task b1')).toBeTruthy()
  })

  it('names each card’s workspace, with whether its agent is live', async () => {
    const { card } = await mount({ workspaces })
    const live = chip(card('Task a1'))
    expect(live.textContent.trim()).toBe('alpha')
    expect(live.querySelector('[title]').getAttribute('title')).toBe('Agent Online')
    expect(live.querySelector('[title]').className).toContain('bg-green-500')

    const idle = chip(card('Task b1'))
    expect(idle.textContent.trim()).toBe('beta')
    expect(idle.querySelector('[title]').getAttribute('title')).toBe('Agent Offline')
  })

  it('turns a card’s dot green when its workspace’s agent connects', async () => {
    const { card } = await mount({ workspaces })
    useWorkspaceStore().updateAgentStatus('ws2', true)
    await settle()
    expect(chip(card('Task b1')).querySelector('[title]').getAttribute('title')).toBe('Agent Online')
  })

  it('fetches the workspace list when nothing has yet', async () => {
    const { card } = await mount()
    expect(fetchWorkspaces).toHaveBeenCalled()
    expect(chip(card('Task a1')).textContent.trim()).toBe('alpha')
  })

  it('names a workspace it does not know as ...', async () => {
    byStatus.notstarted.push(task('x1', 'notstarted', { workspaceId: 'gone', sortOrder: 500 }))
    const { card } = await mount({ workspaces })
    expect(chip(card('Task x1')).textContent.trim()).toBe('...')
  })

  it('saves a drag in the workspace the card belongs to', async () => {
    const { drag } = await mount({ workspaces })
    await drag('Task b2', { before: 'Task a1' })
    expect(updateTaskOrder).toHaveBeenCalledWith('ws2', 'b2', 99)
  })

  it('saves a move into another column in the card’s workspace', async () => {
    updateTaskStatus.mockResolvedValue({})
    const { drag } = await mount({ workspaces })
    await drag('Task b1', { into: 'Ongoing' })
    expect(updateTaskStatus).toHaveBeenCalledWith('ws2', 'b1', 'ongoing')
    expect(updateTaskOrder).toHaveBeenCalledWith('ws2', 'b1', expect.any(Number))
  })

  it('does not let a card from an archived workspace be dragged', async () => {
    const { card } = await mount({ workspaces })
    expect(card('Task g1').getAttribute('draggable')).toBe('false')
    expect(card('Task a1').getAttribute('draggable')).toBe('true')
  })

  it('ignores a drag started on a card from an archived workspace', async () => {
    const { card, fire, drag } = await mount({ workspaces })
    fire(card('Task g1'), 'dragstart')
    await settle()
    expect(card('Task g1').className).not.toContain('opacity-40')
    await drag('Task g1', { before: 'Task a1' })
    expect(updateTaskOrder).not.toHaveBeenCalled()
  })

  it('offers Assign to agent and Deny only where that card’s agent is live', async () => {
    const asks = { assignee: 'human', createdBy: 'agent' }
    byStatus.notstarted = [
      task('a9', 'notstarted', { workspaceId: 'ws1', sortOrder: 1, ...asks }),
      task('b9', 'notstarted', { workspaceId: 'ws2', sortOrder: 2, ...asks }),
    ]
    const { card } = await mount({ workspaces })
    const hasAssign = (c) => [...c.querySelectorAll('button')].some((b) => b.textContent.trim() === 'Assign to agent')
    expect(hasAssign(card('Task a9'))).toBe(true)
    expect(hasAssign(card('Task b9'))).toBe(false)
  })

  it('assigns an agent’s not-started task to the agent and leaves it not started, for its queue to send', async () => {
    updateTaskAssignee.mockClear()
    byStatus.notstarted = [task('a9', 'notstarted', { workspaceId: 'ws1', sortOrder: 1, assignee: 'human', createdBy: 'agent' })]
    const { card } = await mount({ workspaces })
    card('Task a9').querySelector('[data-test="card-allow"]').click()
    await settle()
    expect(updateTaskAssignee).toHaveBeenCalledWith('ws1', 'a9', 'agent')
    expect(updateTaskStatus).not.toHaveBeenCalled()
  })

  it('rejects an agent’s not-started task on Deny', async () => {
    updateTaskStatus.mockResolvedValue({})
    byStatus.notstarted = [task('a9', 'notstarted', { workspaceId: 'ws1', sortOrder: 1, assignee: 'human', createdBy: 'agent' })]
    const { card } = await mount({ workspaces })
    ;[...card('Task a9').querySelectorAll('button')].find((b) => b.textContent.trim() === 'Deny').click()
    await settle()
    expect(updateTaskStatus).toHaveBeenCalledWith('ws1', 'a9', 'rejected')
  })

  it('keeps Allow for a pending permission request, and answers it', async () => {
    sendPermissionVerdict.mockClear()
    byStatus.ongoing = [task('p9', 'ongoing', {
      workspaceId: 'ws1', sortOrder: 1,
      messages: [{ id: 'm1', metadata: { type: 'permission_request', status: 'pending', requestId: 'r1' } }],
    })]
    const { card } = await mount({ workspaces })
    const allow = card('Task p9').querySelector('[data-test="card-allow"]')
    expect(allow.textContent.trim()).toBe('Allow')
    allow.click()
    await settle()
    expect(sendPermissionVerdict).toHaveBeenCalledWith('ws1', 'p9', 'r1', 'allow')
  })

  it('opens a card in its own workspace', async () => {
    const { card } = await mount({ workspaces })
    card('Task b1').click()
    expect(push).toHaveBeenCalledWith({ path: '/workspaces/ws2/tasks/b1', query: {} })
  })

  it('keeps a moved card on the board, under its new workspace', async () => {
    const { el, card } = await mount({ workspaces })
    card('Task b1').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await settle()
    ;[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Move Task').click()
    await settle()
    ;[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'alpha').click()
    await settle()
    ;[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Move').click()
    await settle()

    expect(moveTask).toHaveBeenCalledWith('ws2', 'b1', 'ws1')
    expect(chip(card('Task b1')).textContent.trim()).toBe('alpha')
    expect(el.contains(card('Task b1'))).toBe(true)
  })

  it('takes live updates from every workspace', async () => {
    const { card } = await mount({ workspaces })
    events.value = [{ type: 'task.created', payload: task('n9', 'notstarted', { workspaceId: 'ws2', sortOrder: 900 }) }]
    await settle()
    expect(chip(card('Task n9')).textContent.trim()).toBe('beta')
  })

  it('is what the Kanban page draws, under its heading', async () => {
    replace.mockClear()
    const { el, card } = await mount({ component: KanbanView, workspaces })
    expect(el.querySelector('h1').textContent.trim()).toBe('Kanban')
    expect(chip(card('Task a1')).textContent.trim()).toBe('alpha')
    expect(replace).not.toHaveBeenCalled()
  })

  it('asks the shell for a new task from its heading', async () => {
    const opened = vi.fn()
    window.addEventListener('open-new-task', opened)
    try {
      const { el } = await mount({ component: KanbanView, workspaces })
      el.querySelector('[data-test="new-task"]').click()
      expect(opened).toHaveBeenCalledTimes(1)
    } finally {
      window.removeEventListener('open-new-task', opened)
    }
  })

  it('sends a phone-width screen to the task list, as the workspace board does', async () => {
    replace.mockClear()
    const width = window.innerWidth
    window.innerWidth = 390
    try {
      await mount({ component: KanbanView, workspaces })
    } finally {
      window.innerWidth = width
    }
    expect(replace).toHaveBeenCalledWith({ path: '/tasks/active', query: {} })
  })
})

describe('KanbanBoardView under one workspace', () => {
  beforeEach(() => {
    if (app) app.unmount()
    document.body.innerHTML = ''
    events.value = []
    route.params = { id: 'ws1' }
    byStatus = { notstarted: [task('a1', 'notstarted', { sortOrder: 100 })] }
  })

  it('saves a card that carries no workspace in the one it is shown under', async () => {
    byStatus.notstarted.push({ ...task('a2', 'notstarted', { sortOrder: 200 }), workspaceId: undefined })
    const { drag } = await mount()
    await drag('Task a2', { before: 'Task a1' })
    expect(updateTaskOrder).toHaveBeenCalledWith('ws1', 'a2', 99)
  })

  it('ignores a live update from another workspace, or one with no task', async () => {
    const { card } = await mount()
    events.value = [
      { type: 'task.updated' },
      { type: 'task.created', payload: task('z1', 'notstarted', { workspaceId: 'ws2' }) },
    ]
    await settle()
    expect(card('Task z1')).toBeUndefined()
  })

  it('drops a moved card from the board', async () => {
    const workspaces = [{ id: 'ws1', name: 'alpha' }, { id: 'ws2', name: 'beta' }]
    const { card } = await mount({ workspaces })
    card('Task a1').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true, cancelable: true }))
    await settle()
    ;[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Move Task').click()
    await settle()
    ;[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'beta').click()
    await settle()
    ;[...document.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Move').click()
    await settle()
    expect(moveTask).toHaveBeenCalledWith('ws1', 'a1', 'ws2')
    expect(card('Task a1')).toBeUndefined()
  })
})

describe('Spin up on a card', () => {
  const WS1 = { id: 'ws1', name: 'ops', workingDirectory: '/srv/ops' }
  beforeEach(() => {
    if (app) app.unmount()
    document.body.innerHTML = ''
    localStorage.clear()
    route.params = { id: 'ws1' }
    push.mockClear()
    moveTask.mockClear()
    forkWorkspace.mockClear()
    launchAgent.mockClear()
    // What the list reads back once the fork exists.
    workspaceList = [WS1, { id: 'f1', name: 'task-n1', forkOfId: 'ws1' }]
    byStatus = { notstarted: [task('n1', 'notstarted')], 'completed,rejected': [task('d1', 'completed')] }
  })

  it('is offered on unfinished work only, and not in a fork', async () => {
    const { card } = await mount({ workspaces: [WS1] })
    expect(card('Task n1').querySelector('[title="Spin up in a fork"]')).toBeTruthy()
    expect(card('Task d1').querySelector('[title="Spin up in a fork"]')).toBe(null)
    app.unmount()
    const inFork = await mount({ workspaces: [{ ...WS1, forkOfId: 'p1' }] })
    expect(inFork.card('Task n1').querySelector('[title="Spin up in a fork"]')).toBe(null)
  })

  it('forks, moves the card in, launches, and goes to the terminal', async () => {
    const { card } = await mount({ workspaces: [WS1] })
    card('Task n1').querySelector('[title="Spin up in a fork"]').click()
    await settle()
    const pop = document.body.querySelector('[data-test=spin-up]')
    expect(pop.textContent).toMatch(/On pi, your only machine that is online/)
    ;[...pop.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Spin up').click()
    await settle()
    expect(forkWorkspace).toHaveBeenCalledWith('ws1', { name: 'task-n1' })
    expect(moveTask).toHaveBeenCalledWith('ws1', 'n1', 'f1')
    expect(launchAgent).toHaveBeenCalledWith('f1', expect.objectContaining({ machineId: 'm1', kind: 'claude-code' }))
    expect(push).toHaveBeenCalledWith('/sessions/s1')
    expect(card('Task n1')).toBeUndefined()
    expect(document.body.querySelector('[data-test=spin-up]')).toBe(null)
  })

  it('keeps the card and says so when the move fails, with a way to the fork', async () => {
    moveTask.mockImplementationOnce(() => Promise.reject(new Error('task not found')))
    const { card } = await mount({ workspaces: [WS1] })
    card('Task n1').querySelector('[title="Spin up in a fork"]').click()
    await settle()
    const pop = document.body.querySelector('[data-test=spin-up]')
    ;[...pop.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Spin up').click()
    await settle()
    expect(pop.querySelector('[data-test=spin-up-error]').textContent).toBe(
      'Forked task-n1, but could not move the task into the fork: task not found'
    )
    expect(pop.textContent).toMatch(/Open task-n1/)
    expect(launchAgent).not.toHaveBeenCalled()
    expect(card('Task n1')).toBeTruthy()
    expect(push).not.toHaveBeenCalled()
  })

  it('keeps a spun-up card on the all-workspaces board, under the fork', async () => {
    route.params = {}
    const { card } = await mount({ workspaces: [WS1, { id: 'f1', name: 'task-n1', forkOfId: 'ws1' }] })
    card('Task n1').querySelector('[title="Spin up in a fork"]').click()
    await settle()
    ;[...document.body.querySelectorAll('[data-test=spin-up] button')].find((b) => b.textContent.trim() === 'Spin up').click()
    await settle()
    expect(card('Task n1').querySelector('[data-test=card-workspace]').textContent).toMatch(/task-n1/)
  })
})

describe('the team\'s board (/teams/<id>/kanban)', () => {
  const theirs = (id, status, extra = {}) => task(id, status, { workspaceId: 'theirs', owner: { userId: 'u2', name: 'Bob' }, ...extra })

  beforeEach(() => {
    if (app) app.unmount()
    document.body.innerHTML = ''
    route.params = {}
    route.path = '/teams/tm1/kanban'
    route.query = { workspace: 'Backend' }
    push.mockClear()
    replace.mockClear()
    fetchTeamTasks.mockClear()
    fetchGlobalTasks.mockClear()
    updateTaskStatus.mockReset()
    byStatus = {
      notstarted: [theirs('n1', 'notstarted', { assignee: 'human', createdBy: 'agent' })],
      ongoing: [theirs('o1', 'ongoing')],
    }
  })

  afterEach(() => {
    route.path = '/kanban'
    route.query = {}
  })

  it('reads the team\'s tasks, read-only, each card naming whose it is', async () => {
    const { el, card, fire } = await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
    expect(fetchTeamTasks).toHaveBeenCalledWith('tm1', expect.objectContaining({ status: 'ongoing', workspace: 'Backend', member: '' }))
    expect(fetchGlobalTasks).not.toHaveBeenCalled()
    expect(el.textContent).toContain("Every shared workspace's tasks on one board.")
    expect([...el.querySelectorAll('a, button')].some((b) => /New task/i.test(b.textContent))).toBe(false)

    const n1 = card('Task n1')
    expect(n1.getAttribute('draggable')).toBe('false')
    expect(n1.querySelector('[data-test=card-workspace]').textContent).toMatch(/Bob|B/)
    expect(n1.querySelector('[data-test=card-workspace]').textContent).toContain('Backend')
    expect(n1.querySelector('[data-test=card-owner]')).not.toBe(null)
    // As small as the agent and assignee icons beside it.
    expect(n1.querySelector('[data-test=card-owner]').firstElementChild.className).toMatch(/\bw-3\.5 h-3\.5\b/)
    expect(n1.querySelector('[title="Spin up in a fork"]')).toBe(null)
    expect([...n1.querySelectorAll('button')].some((b) => /Allow|Assign|Deny/.test(b.textContent))).toBe(false)

    fire(n1, 'dragstart')
    fire(n1, 'contextmenu')
    await settle()
    expect(document.body.querySelector('[role=menu]')).toBe(null)

    n1.click()
    expect(push).toHaveBeenCalledWith({ path: '/teams/tm1/workspaces/theirs/tasks/n1', query: { workspace: 'Backend' } })
    expect(updateTaskStatus).not.toHaveBeenCalled()
  })

  it('ignores your own live updates, which are not the team\'s', async () => {
    const { el } = await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
    events.value = [{ type: 'task.created', payload: task('mine', 'ongoing') }]
    await settle()
    expect(el.textContent).not.toContain('Task mine')
  })

  it('follows the team\'s live updates, inside its filters', async () => {
    const { el } = await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
    const send = async (ev) => { events.value = [...events.value, ev]; await settle() }
    await send({ type: 'team.task.created', teamId: 'tm2', payload: theirs('other', 'ongoing', { sharedName: 'Backend' }) })
    expect(el.textContent).not.toContain('Task other')
    await send({ type: 'team.task.created', teamId: 'tm1', payload: theirs('new', 'ongoing', { sharedName: 'backend ' }) })
    expect(el.textContent).toContain('Task new')
    // Moved to another team workspace: off a board filtered to this one.
    await send({ type: 'team.task.updated', teamId: 'tm1', payload: theirs('new', 'ongoing', { sharedName: 'Web' }) })
    expect(el.textContent).not.toContain('Task new')
    await send({ type: 'team.task.deleted', teamId: 'tm1', payload: { id: 'o1' } })
    expect(el.textContent).not.toContain('Task o1')
  })

  it('offers its filters as pickers, and reads the board again when they change', async () => {
    const { el } = await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
    expect(el.querySelector('[data-test=team-task-filters]')).not.toBe(null)
    expect(el.querySelector('[data-test=team-filter-workspace]').textContent).toContain('Backend')
    fetchTeamTasks.mockClear()

    route.query = { workspace: 'Web', member: 'u2' }
    await settle()

    expect(fetchTeamTasks).toHaveBeenCalledWith('tm1', expect.objectContaining({ workspace: 'Web', member: 'u2' }))
  })

  it('keeps a member filter to that member\'s tasks', async () => {
    route.query = { member: 'u2' }
    const { el } = await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
    const send = async (ev) => { events.value = [...events.value, ev]; await settle() }
    await send({ type: 'team.status.updated', teamId: 'tm1', payload: theirs('cy', 'ongoing', { owner: { userId: 'u3', name: 'Cy' } }) })
    expect(el.textContent).not.toContain('Task cy')
    await send({ type: 'team.status.updated', teamId: 'tm1', payload: theirs('bob', 'ongoing') })
    expect(el.textContent).toContain('Task bob')
  })

  it('sends a phone to the team\'s task list', async () => {
    const width = window.innerWidth
    window.innerWidth = 390
    try {
      await mount({ component: KanbanView, workspaces: [{ id: 'ws1', name: 'ops' }] })
      expect(replace).toHaveBeenCalledWith({ path: '/teams/tm1/tasks/active', query: {} })
    } finally {
      window.innerWidth = width
    }
  })
})
