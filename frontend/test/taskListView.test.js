// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The lists behind the sidebar's links, and a workspace's own list, offer the
 * same row actions and the same load more.
 *
 * They drifted once: the sidebar's rows had no edit or delete, and its load more
 * blanked the list while the next page loaded.
 */

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createApp, h, reactive, ref } from 'vue'
import { createPinia } from 'pinia'

const push = vi.fn()
const route = reactive({ params: { filter: 'notstarted' }, query: {} })
vi.mock('vue-router', () => ({
  useRoute: () => route,
  useRouter: () => ({ push, currentRoute: ref(route) }),
}))

const busEvents = ref([])
vi.mock('../src/useEventBus', () => ({
  useEventBus: () => ({ connect() {}, disconnect() {}, events: busEvents }),
}))

vi.mock('../src/composables/useCachedTasks', () => ({
  cacheTasks() {},
  sharedCache: () => null,
}))

const readAllCachedTasks = vi.fn(async () => [])
vi.mock('../src/composables/useCachedReads', () => ({
  readAllCachedTasks: (...a) => readAllCachedTasks(...a),
  shouldPaintCache: () => false,
}))

const task = (id, workspaceId = 'w1', status = 'notstarted') => ({
  id, workspaceId, status, title: `task ${id}`, assignee: 'agent', createdAt: '2026-09-01T00:00:00Z',
})

const WORKSPACES = [
  { id: 'w1', name: 'Ops' },
  { id: 'w2', name: 'Old', archivedAt: '2026-01-01T00:00:00Z' },
  { id: 'w3', name: 'Billing' },
]

let pages = []
const byPage = async ({ offset }) => ({ tasks: pages[offset === 0 ? 0 : 1] || [] })
const fetchGlobalTasks = vi.fn(byPage)
let fetchTasksPages = []
const fetchTasks = vi.fn(async () => ({ tasks: fetchTasksPages.shift() || [] }))
const deleteTask = vi.fn(async () => ({}))
const moveTask = vi.fn(async () => ({}))
const updateTaskOrder = vi.fn(async () => ({}))

vi.mock('../src/api', async (importOriginal) => ({
  ...(await importOriginal()),
  fetchGlobalTasks: (...a) => fetchGlobalTasks(...a),
  fetchWorkspaces: async () => ({ workspaces: WORKSPACES }),
  fetchTasks: (...a) => fetchTasks(...a),
  fetchTaskCounts: async () => ({ notstarted: 12 }),
  deleteTask: (...a) => deleteTask(...a),
  moveTask: (...a) => moveTask(...a),
  updateTaskOrder: (...a) => updateTaskOrder(...a),
}))

const { default: TaskListView } = await import('../src/views/TaskListView.vue')
const { default: TaskFeed } = await import('../src/components/TaskFeed.vue')

const settle = () => new Promise((resolve) => setTimeout(resolve, 30))
let apps = []

async function mount(component, props = {}) {
  const el = document.createElement('div')
  document.body.appendChild(el)
  const app = createApp({ render: () => h(component, props) })
  app.use(createPinia())
  app.component('RouterView', { render: () => null })
  app.component('RouterLink', { render: () => null })
  app.directive('click-outside', {})
  app.mount(el)
  apps.push(app)
  await settle()
  return el
}

const rows = (el) => [...el.querySelectorAll('h3.truncate')].map((n) => n.textContent.trim())
const rowOf = (el, title) => [...el.querySelectorAll('h3.truncate')].find((n) => n.textContent.trim() === title).closest('.group')
const button = (el, text) => [...document.querySelectorAll('button')].find((b) => b.textContent.trim() === text)

beforeEach(() => {
  apps.forEach((a) => a.unmount())
  apps = []
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
  route.params = { filter: 'notstarted' }
  busEvents.value = []
  fetchGlobalTasks.mockImplementation(byPage)
  readAllCachedTasks.mockImplementation(async () => [])
  pages = [
    [...Array.from({ length: 9 }, (_, i) => task(`a${i}`)), task('arch', 'w2')],
    [task('b0'), task('b1')],
  ]
})

describe('the sidebar task list', () => {
  it('offers edit and delete on a row, as a workspace list does', async () => {
    const el = await mount(TaskListView)
    const row = rowOf(el, 'task a0')
    expect(row.querySelector('[title="Edit Task"]')).not.toBeNull()
    expect(row.querySelector('[title="Delete Task"]')).not.toBeNull()

    row.querySelector('[title="Edit Task"]').click()
    expect(push).toHaveBeenCalledWith('/workspaces/w1/tasks/a0/edit')
  })

  it('leaves an archived workspace’s task read-only', async () => {
    const el = await mount(TaskListView)
    const row = rowOf(el, 'task arch')
    expect(row.querySelector('[title="Edit Task"]')).toBeNull()
    expect(row.querySelector('[title="Delete Task"]')).toBeNull()
    expect(row.querySelector('[title="Move Up"]')).toBeNull()

    row.dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))
    await settle()
    expect(button(el, 'Move Task')).toBeUndefined()
  })

  it('deletes after the confirmation, and the row goes', async () => {
    const el = await mount(TaskListView)
    rowOf(el, 'task a1').querySelector('[title="Delete Task"]').click()
    await settle()
    button(el, 'Delete').click()
    await settle()

    expect(deleteTask).toHaveBeenCalledWith('w1', 'a1')
    expect(rows(el)).not.toContain('task a1')
  })

  it('closes the open task’s pane when that task is deleted', async () => {
    route.params = { filter: 'notstarted', workspaceId: 'w1', taskId: 'a1' }
    const el = await mount(TaskListView)
    rowOf(el, 'task a1').querySelector('[title="Delete Task"]').click()
    await settle()
    button(el, 'Delete').click()
    await settle()
    expect(push).toHaveBeenCalledWith('/tasks/notstarted')
  })

  it('says so when a delete fails, and keeps the row', async () => {
    deleteTask.mockRejectedValueOnce(new Error('network unreachable'))
    const { useToasts } = await import('../src/composables/useToasts')
    const el = await mount(TaskListView)
    rowOf(el, 'task a1').querySelector('[title="Delete Task"]').click()
    await settle()
    button(el, 'Delete').click()
    await settle()
    expect(useToasts().toasts.value.at(-1).message).toContain('network unreachable')
    expect(rows(el)).toContain('task a1')
  })

  it('moves a task to another workspace from the right-click menu', async () => {
    const el = await mount(TaskListView)
    rowOf(el, 'task a2').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))
    await settle()
    button(el, 'Move Task').click()
    await settle()
    button(el, 'Billing').click()
    await settle()
    button(el, 'Move').click()
    await settle()

    expect(moveTask).toHaveBeenCalledWith('w1', 'a2', 'w3')
    expect(rows(el)).not.toContain('task a2')
  })

  it('says so when a move fails', async () => {
    moveTask.mockRejectedValueOnce(new Error('refused'))
    const { useToasts } = await import('../src/composables/useToasts')
    const el = await mount(TaskListView)
    rowOf(el, 'task a2').dispatchEvent(new MouseEvent('contextmenu', { bubbles: true }))
    await settle()
    button(el, 'Move Task').click()
    await settle()
    button(el, 'Billing').click()
    await settle()
    button(el, 'Move').click()
    await settle()
    expect(useToasts().toasts.value.at(-1).message).toContain('refused')
  })

  it('loads more without blanking the rows already shown', async () => {
    let release
    const el = await mount(TaskListView)
    fetchGlobalTasks.mockImplementationOnce(() => new Promise((r) => { release = r }))
    button(el, 'Load More').click()
    await settle()

    expect(rows(el)).toHaveLength(10)
    expect(button(el, 'Loading...')).not.toBeUndefined()

    release({ tasks: pages[1] })
    await settle()
    expect(rows(el)).toHaveLength(12)
    expect(button(el, 'Load More')).toBeUndefined()
  })

  // One piece of activity arrives as a burst of events, each a refresh of its own.
  it('shows each task once when refreshes overlap', async () => {
    const el = await mount(TaskListView)
    const answers = []
    fetchGlobalTasks.mockImplementation(() => new Promise((r) => answers.push(r)))

    busEvents.value = [...busEvents.value, { type: 'reply.received' }]
    await settle()
    busEvents.value = [...busEvents.value, { type: 'task.updated' }]
    await settle()
    expect(answers).toHaveLength(2)

    answers[0]({ tasks: pages[0] })
    await settle()
    answers[1]({ tasks: pages[0] })
    await settle()
    expect(rows(el)).toHaveLength(10)
  })

  it('drops an older refresh that answers after a newer one', async () => {
    const el = await mount(TaskListView)
    const answers = []
    fetchGlobalTasks.mockImplementation(() => new Promise((r) => answers.push(r)))

    busEvents.value = [...busEvents.value, { type: 'task.updated' }]
    await settle()
    busEvents.value = [...busEvents.value, { type: 'task.deleted' }]
    await settle()

    answers[1]({ tasks: [task('fresh')] })
    await settle()
    answers[0]({ tasks: pages[0] })
    await settle()
    expect(rows(el)).toEqual(['task fresh'])
  })

  it('asks the server once when a newer refresh starts during the cache read', async () => {
    await mount(TaskListView)
    let releaseCache
    readAllCachedTasks.mockImplementationOnce(() => new Promise((r) => { releaseCache = r }))
    fetchGlobalTasks.mockClear()

    busEvents.value = [...busEvents.value, { type: 'task.updated' }]
    await settle()
    busEvents.value = [...busEvents.value, { type: 'reply.received' }]
    await settle()
    releaseCache([])
    await settle()

    expect(fetchGlobalTasks).toHaveBeenCalledTimes(1)
  })

  it('drops a load more that answers after a refresh began', async () => {
    const el = await mount(TaskListView)
    const answers = []
    fetchGlobalTasks.mockImplementation(() => new Promise((r) => answers.push(r)))

    button(el, 'Load More').click()
    await settle()
    busEvents.value = [...busEvents.value, { type: 'task.created' }]
    await settle()

    answers[1]({ tasks: pages[0] })
    await settle()
    answers[0]({ tasks: pages[1] })
    await settle()
    expect(rows(el)).toHaveLength(10)
  })
})

describe('a workspace task list', () => {
  it('uses the same load more, one per group', async () => {
    fetchTasksPages = [Array.from({ length: 10 }, (_, i) => task(`n${i}`)), [task('n10')]]
    const el = await mount(TaskFeed, { workspaceId: 'w1', filter: 'notstarted' })
    expect(rows(el)).toHaveLength(10)

    button(el, 'Load More').click()
    await settle()
    expect(fetchTasks).toHaveBeenLastCalledWith('w1', { status: 'notstarted', limit: 10, offset: 10 })
    expect(rows(el)).toHaveLength(11)
    expect(button(el, 'Load More')).toBeUndefined()
  })

  it('edits under its workspace', async () => {
    fetchTasksPages = [[task('n0')]]
    const el = await mount(TaskFeed, { workspaceId: 'w1', filter: 'notstarted' })
    rowOf(el, 'task n0').querySelector('[title="Edit Task"]').click()
    expect(push).toHaveBeenCalledWith('/workspaces/w1/tasks/n0/edit')
  })
})

// Not Started shows the lowest sort order first. The bug: a step set the task
// just the far side of its neighbour's order from where it meant to go, so it
// stayed below the task it was moved up past.
describe('Move Up and Move Down', () => {
  const ordered = () => [
    { ...task('a'), sortOrder: 10 },
    { ...task('b'), sortOrder: 20 },
    { ...task('c'), sortOrder: 30 },
  ]
  const lists = {
    'the sidebar task list': async () => {
      pages = [ordered()]
      return mount(TaskListView)
    },
    'a workspace task list': async () => {
      fetchTasksPages = [ordered(), ordered()]
      return mount(TaskFeed, { workspaceId: 'w1', filter: 'notstarted' })
    },
  }

  for (const [name, open] of Object.entries(lists)) {
    describe(name, () => {
      it('puts a task moved up between the two tasks above it', async () => {
        const el = await open()
        rowOf(el, 'task c').querySelector('[title="Move Up"]').click()
        await settle()
        expect(updateTaskOrder).toHaveBeenCalledWith('w1', 'c', 15)
      })

      it('puts a task moved down between the two tasks below it', async () => {
        const el = await open()
        rowOf(el, 'task a').querySelector('[title="Move Down"]').click()
        await settle()
        expect(updateTaskOrder).toHaveBeenCalledWith('w1', 'a', 25)
      })

      it('puts a task moved up to the top before the first one', async () => {
        const el = await open()
        rowOf(el, 'task b').querySelector('[title="Move Up"]').click()
        await settle()
        expect(updateTaskOrder).toHaveBeenCalledWith('w1', 'b', 9)
      })

      it('leaves the first task where it is when moved up', async () => {
        const el = await open()
        rowOf(el, 'task a').querySelector('[title="Move Up"]').click()
        await settle()
        expect(updateTaskOrder).not.toHaveBeenCalled()
      })

      it('says so when the move fails', async () => {
        updateTaskOrder.mockRejectedValueOnce(new Error('database unavailable'))
        const { useToasts } = await import('../src/composables/useToasts')
        const el = await open()
        rowOf(el, 'task c').querySelector('[title="Move Up"]').click()
        await settle()
        expect(useToasts().toasts.value.at(-1).message).toBe('Reorder Error: database unavailable')
      })
    })
  }
})
