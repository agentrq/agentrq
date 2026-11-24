// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * The Events pages: the list with its New Event form, an event's page, and
 * the trigger form it opens.
 */

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { createPinia } from 'pinia'
import { useWorkspaceStore } from '../src/stores/workspaceStore'

const push = vi.fn()
const route = { path: '/events', params: {}, query: {} }
vi.mock('vue-router', () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  useRoute: () => route,
}))

import { useToasts } from '../src/composables/useToasts'
import { notifyWebMCPChange } from '../src/composables/useWebMCPChanges'
import EventsView from '../src/views/EventsView.vue'
import EventDetailView from '../src/views/EventDetailView.vue'
import EventTriggerForm from '../src/components/EventTriggerForm.vue'

const API = '/api/v1'
const workspaces = [{ id: 'w1', name: 'backend' }, { id: 'w2', name: 'notes' }]
const events = [
  { id: 'ev1', name: 'issue_opened', payloadGuidelines: 'The issue', createdAt: '2026-11-20T10:00:00Z' },
  { id: 'ev2', name: 'deploy_done', payloadGuidelines: '', createdAt: '' },
]
const trigger = (over = {}) => ({
  id: 't1', eventId: 'ev1', workspaceId: 'w1', title: 'Triage it', body: 'Look: {{EVENT_PAYLOAD}}', assignee: 'agent',
  allowAllCommands: false, cronSchedule: '', emitEventId: '', ...over,
})
const task = (over = {}) => ({ id: 'k1', workspaceId: 'w1', title: 'Triage it', status: 'completed', assignee: 'agent', createdAt: '2026-11-23T09:00:00Z', ...over })

// Answers each call with the next reply for its method and path; a path
// whose replies ran out keeps its last one.
function serve(replies) {
  const seen = []
  vi.stubGlobal('fetch', vi.fn((url, init = {}) => {
    seen.push({ url, init })
    const key = `${init.method || 'GET'} ${url}`
    const queue = replies[key]
    const [body, status] = (queue?.length > 1 ? queue.shift() : queue?.[0]) ?? [{ error: { message: 'not found' } }, 404]
    return Promise.resolve(new Response(body === undefined ? null : JSON.stringify(body), { status }))
  }))
  return seen
}

const settle = () => new Promise((r) => setTimeout(r, 0))
async function flush() {
  for (let i = 0; i < 8; i++) await settle()
  await nextTick()
}

const RouterLink = { props: ['to'], setup: (props, { slots, attrs }) => () => h('a', { ...attrs, href: props.to }, slots.default?.()) }
const apps = []

async function mount(component, props = {}, pinia = createPinia()) {
  const el = document.createElement('div')
  document.body.append(el)
  const app = createApp({ render: () => h(component, props) })
  app.use(pinia)
  app.component('RouterLink', RouterLink)
  app.mount(el)
  apps.push(app)
  await flush()
  return el
}

const q = (el, test) => el.querySelector(`[data-test="${test}"]`)
const qa = (el, test) => [...el.querySelectorAll(`[data-test="${test}"]`)]
const toastMessages = () => useToasts().toasts.value.map((t) => t.message)
const calls = (seen) => seen.map((s) => `${s.init.method || 'GET'} ${s.url}`)
const bodyOf = (seen, method) => JSON.parse(seen.filter((s) => s.init.method === method).at(-1).init.body)
const modalButton = (label) => [...document.body.querySelectorAll('[role="dialog"] button')].find((b) => b.textContent.trim() === label)
const keydown = (target, key, mods = {}) => target.dispatchEvent(new KeyboardEvent('keydown', { key, bubbles: true, cancelable: true, ...mods }))
function errorFor(control) {
  const ids = (control.getAttribute('aria-describedby') || '').split(' ')
  return ids.map((id) => document.getElementById(id)).find((n) => n?.dataset.test === 'field-error') ?? null
}
function type(input, value) {
  input.value = value
  input.dispatchEvent(new Event('input'))
  input.dispatchEvent(new Event('change'))
}

beforeEach(() => {
  route.params = {}
})

afterEach(() => {
  apps.splice(0).forEach((app) => app.unmount())
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  push.mockClear()
  useToasts().toasts.value = []
})

describe('EventsView', () => {
  const list = (over = {}) => ({ [`GET ${API}/events`]: [[{ events }, 200]], ...over })

  it('lists the events as links, each with a delete button named for it', async () => {
    serve(list())
    const el = await mount(EventsView)
    expect(qa(el, 'event-link').map((a) => a.getAttribute('href'))).toEqual(['/events/ev1', '/events/ev2'])
    const del = qa(el, 'event-row-delete')
    expect(del.map((b) => b.getAttribute('aria-label'))).toEqual(['Delete event issue_opened', 'Delete event deploy_done'])
    expect(q(el, 'event-row').textContent).toContain('Nov 20, 2026')
  })

  it('offers a first event when there is none, and focuses its name', async () => {
    serve({ [`GET ${API}/events`]: [[{}, 200]] })
    const el = await mount(EventsView)
    expect(q(el, 'events-empty')).not.toBeNull()
    q(el, 'events-empty-new').click()
    await flush()
    expect(document.activeElement).toBe(q(el, 'event-name-input'))
    expect(q(el, 'event-new').getAttribute('aria-expanded')).toBe('true')
  })

  it('says why a name is refused, under the field, and keeps the cursor there', async () => {
    const seen = serve(list())
    const el = await mount(EventsView)
    q(el, 'event-new').click()
    await flush()
    const name = q(el, 'event-name-input')
    q(el, 'event-submit').click()
    await flush()
    expect(errorFor(name).textContent.trim()).toBe('Give the event a name.')
    expect(name.getAttribute('aria-invalid')).toBe('true')
    expect(document.activeElement).toBe(name)

    type(name, '1 Deploy')
    await flush()
    expect(name.value).toBe('1_deploy')
    expect(errorFor(name)).toBeNull()
    q(el, 'event-submit').click()
    await flush()
    expect(errorFor(name).textContent.trim()).toBe('Start with a letter; then only letters, digits and underscores.')

    type(name, 'a'.repeat(130))
    q(el, 'event-submit').click()
    await flush()
    expect(errorFor(name).textContent.trim()).toBe('Use at most 129 characters.')
    expect(calls(seen).some((c) => c.startsWith('POST'))).toBe(false)
  })

  it('creates an event from Cmd+Enter, shows a refusal, and closes on Esc', async () => {
    const seen = serve(list({
      [`POST ${API}/events`]: [[{ error: { message: 'name taken' } }, 409], [{ event: events[0] }, 201]],
    }))
    const el = await mount(EventsView)
    q(el, 'event-new').click()
    await flush()
    type(q(el, 'event-name-input'), 'Deploy Done')
    type(q(el, 'event-guidelines-new'), '  The SHA  ')
    keydown(q(el, 'event-guidelines-new'), 'Enter', { metaKey: true })
    await flush()
    expect(toastMessages()).toEqual(['name taken'])
    expect(bodyOf(seen, 'POST')).toEqual({ name: 'deploy_done', payloadGuidelines: 'The SHA' })

    keydown(q(el, 'event-name-input'), 'a')
    keydown(q(el, 'event-name-input'), 'Enter', { ctrlKey: true })
    await flush()
    expect(toastMessages()).toContain('Event created')
    // A Transition's leave never ends in jsdom: the toggle says it closed.
    expect(q(el, 'event-new').getAttribute('aria-expanded')).toBe('false')

    q(el, 'event-new').click()
    await flush()
    keydown(q(el, 'event-name-input'), 'Escape')
    await flush()
    expect(q(el, 'event-new').getAttribute('aria-expanded')).toBe('false')
    q(el, 'event-new').click()
    await flush()
    q(el, 'event-new').click()
    await flush()
    expect(q(el, 'event-new').getAttribute('aria-expanded')).toBe('false')
  })

  it('ignores a second submit while the first is on its way', async () => {
    let answer
    const seen = serve(list())
    const plain = globalThis.fetch
    vi.stubGlobal('fetch', vi.fn((url, init = {}) => {
      if (init.method === 'POST') { seen.push({ url, init }); return new Promise((r) => { answer = r }) }
      return plain(url, init)
    }))
    const el = await mount(EventsView)
    q(el, 'event-new').click()
    await flush()
    type(q(el, 'event-name-input'), 'deploy')
    q(el, 'event-submit').click()
    await flush()
    expect(q(el, 'event-submit').textContent.trim()).toBe('Creating…')
    keydown(q(el, 'event-name-input'), 'Enter', { metaKey: true })
    await flush()
    expect(calls(seen).filter((c) => c.startsWith('POST'))).toHaveLength(1)
    answer(new Response(JSON.stringify({ event: events[0] }), { status: 201 }))
    await flush()
  })

  it('deletes an event once confirmed, and shows a refused delete', async () => {
    serve(list({ [`DELETE ${API}/events/ev2`]: [[{ error: { message: 'busy' } }, 500], [undefined, 204]] }))
    const el = await mount(EventsView)
    qa(el, 'event-row-delete')[1].click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(toastMessages()).toEqual(['Failed to delete event'])
    expect(qa(el, 'event-row')).toHaveLength(2)
    qa(el, 'event-row-delete')[1].click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(qa(el, 'event-row')).toHaveLength(1)
    qa(el, 'event-row-delete')[0].click()
    await flush()
    modalButton('Cancel').click()
    await flush()
    expect(qa(el, 'event-row')).toHaveLength(1)
  })

  it('shows a failed read, and reloads quietly on a WebMCP change', async () => {
    const seen = serve({ [`GET ${API}/events`]: [[null, 500], [{ events }, 200]] })
    const el = await mount(EventsView)
    expect(toastMessages()).toEqual(['Failed to fetch events'])
    notifyWebMCPChange()
    await flush()
    expect(qa(el, 'event-row')).toHaveLength(2)
    expect(calls(seen)).toHaveLength(2)
  })
})

describe('EventTriggerForm', () => {
  async function form(props = {}) {
    const onSubmit = vi.fn()
    const onCancel = vi.fn()
    const el = await mount(EventTriggerForm, { workspaces, events, onSubmit, onCancel, ...props })
    return { el, onSubmit, onCancel }
  }

  it('starts at the workspace, and says what is missing under each field', async () => {
    const { el, onSubmit } = await form()
    expect(document.activeElement).toBe(q(el, 'trigger-workspace'))
    q(el, 'trigger-submit').click()
    await flush()
    expect(errorFor(q(el, 'trigger-workspace')).textContent.trim()).toBe('Pick the workspace that gets the task.')
    expect(errorFor(q(el, 'trigger-title')).textContent.trim()).toBe('Give the task a title.')
    expect(document.activeElement).toBe(q(el, 'trigger-workspace'))
    const select = q(el, 'trigger-workspace')
    select.value = 'w2'
    select.dispatchEvent(new Event('change'))
    await flush()
    expect(errorFor(select)).toBeNull()
    q(el, 'trigger-submit').click()
    await flush()
    expect(document.activeElement).toBe(q(el, 'trigger-title'))
    type(q(el, 'trigger-title'), 'x')
    await flush()
    expect(errorFor(q(el, 'trigger-title'))).toBeNull()
    expect(onSubmit).not.toHaveBeenCalled()
  })

  it('inserts the variables at the cursor, and offers the payload until it is used', async () => {
    const { el } = await form()
    const body = q(el, 'trigger-body')
    expect(q(el, 'insert-payload').textContent.trim()).toBe('Event payload')
    expect(q(el, 'trigger-payload-hint')).not.toBeNull()
    type(body, 'Fix this now')
    body.setSelectionRange(8, 8)
    q(el, 'insert-faq').click()
    await flush()
    expect(body.value).toBe('Fix this {{EVENT_FAQ}} now')
    expect(document.activeElement).toBe(body)
    expect(body.selectionStart).toBe('Fix this {{EVENT_FAQ}}'.length)
    q(el, 'trigger-payload-insert').click()
    await flush()
    expect(body.value).toBe('Fix this {{EVENT_FAQ}} {{EVENT_PAYLOAD}} now')
    expect(q(el, 'trigger-payload-hint')).toBeNull()
  })

  it('warns of YOLO, and never sends it for a human', async () => {
    const { el, onSubmit } = await form()
    const select = q(el, 'trigger-workspace')
    select.value = 'w1'
    select.dispatchEvent(new Event('change'))
    type(q(el, 'trigger-title'), '  Triage it  ')
    expect(q(el, 'trigger-yolo-warning')).toBeNull()
    const on = q(el, 'trigger-yolo-on').querySelector('input')
    on.checked = true
    on.dispatchEvent(new Event('change'))
    await flush()
    expect(q(el, 'trigger-yolo-warning')).not.toBeNull()
    expect(el.textContent).toContain("The workspace's agent picks the task up")
    const emit = q(el, 'trigger-emit')
    emit.value = 'ev2'
    emit.dispatchEvent(new Event('change'))
    keydown(q(el, 'trigger-title'), 'Enter', { metaKey: true })
    await flush()
    expect(onSubmit).toHaveBeenLastCalledWith({
      workspaceId: 'w1', title: 'Triage it', body: '', assignee: 'agent', allowAllCommands: true, cronSchedule: '', emitEventId: 'ev2',
    })
    const human = q(el, 'trigger-assignee-human').querySelector('input')
    human.checked = true
    human.dispatchEvent(new Event('change'))
    await flush()
    expect(q(el, 'trigger-yolo-on')).toBeNull()
    expect(q(el, 'trigger-yolo-warning')).toBeNull()
    expect(el.textContent).toContain('The task waits for you.')
    q(el, 'trigger-submit').click()
    await flush()
    expect(onSubmit.mock.lastCall[0]).toMatchObject({ assignee: 'human', allowAllCommands: false })
  })

  it('edits a trigger: starts at its title, keeps its schedule, cancels on Esc', async () => {
    const { el, onSubmit, onCancel } = await form({ trigger: trigger({ cronSchedule: '0 9 * * *', emitEventId: 'ev2' }), events: [] })
    expect(document.activeElement).toBe(q(el, 'trigger-title'))
    expect(q(el, 'trigger-emit')).toBeNull()
    expect(q(el, 'trigger-payload-hint')).toBeNull()
    expect(q(el, 'trigger-submit').textContent.trim()).toBe('Save trigger')
    keydown(q(el, 'trigger-title'), 'x')
    q(el, 'trigger-submit').click()
    await flush()
    expect(onSubmit.mock.lastCall[0]).toMatchObject({ cronSchedule: '0 9 * * *', emitEventId: 'ev2', body: 'Look: {{EVENT_PAYLOAD}}' })
    keydown(q(el, 'trigger-title'), 'Escape')
    expect(onCancel).toHaveBeenCalledTimes(1)
    q(el, 'trigger-cancel').click()
    expect(onCancel).toHaveBeenCalledTimes(2)
  })

  it('reads a stored trigger with no body or assignee', async () => {
    const { el } = await form({ trigger: { id: 't9', workspaceId: 'w1', title: 'T' } })
    expect(q(el, 'trigger-body').value).toBe('')
    expect(q(el, 'trigger-assignee-agent').querySelector('input').checked).toBe(true)
  })

  it('sends nothing while busy', async () => {
    const { el, onSubmit } = await form({ trigger: trigger(), busy: true })
    expect(q(el, 'trigger-submit').textContent.trim()).toBe('Saving…')
    keydown(q(el, 'trigger-title'), 'Enter', { ctrlKey: true })
    await flush()
    expect(onSubmit).not.toHaveBeenCalled()
    const { el: el2 } = await form({ busy: true })
    expect(q(el2, 'trigger-submit').textContent.trim()).toBe('Creating…')
  })
})

describe('EventDetailView', () => {
  const E = `${API}/events/ev1`
  const page = (over = {}) => ({
    [`GET ${API}/workspaces`]: [[{ workspaces }, 200]],
    [`GET ${E}`]: [[{ event: events[0] }, 200]],
    [`GET ${E}/triggers`]: [[{ eventTriggers: [trigger(), trigger({ id: 't2', workspaceId: 'gone', assignee: 'human', title: 'Note it', body: '', allowAllCommands: true, emitEventId: 'evX' })] }, 200]],
    [`GET ${E}/tasks`]: [[{ tasks: [task(), task({ id: 'k2', status: 'weird', workspaceId: 'w2' })] }, 200]],
    [`GET ${API}/events`]: [[{ events }, 200]],
    ...over,
  })

  beforeEach(() => {
    route.params = { id: 'ev1' }
  })

  it('shows the guidelines, the triggers and the tasks', async () => {
    serve(page({ [`GET ${E}/triggers`]: [[{ eventTriggers: [trigger({ allowAllCommands: true, emitEventId: 'ev2', cronSchedule: '0 9 * * *' }), trigger({ id: 't2', workspaceId: 'gone', assignee: 'human', allowAllCommands: true, emitEventId: 'evX' })] }, 200]] }))
    const el = await mount(EventDetailView)
    expect(q(el, 'event-name').textContent).toBe('issue_opened')
    expect(q(el, 'event-guidelines-text').textContent).toBe('The issue')
    const rows = qa(el, 'trigger-row')
    expect(rows[0].textContent).toContain('backend')
    expect(q(rows[0], 'trigger-yolo-badge')).not.toBeNull()
    expect(rows[0].querySelector('code').textContent).toBe('0 9 * * *')
    expect(q(rows[0], 'trigger-emits').textContent.replace(/\s+/g, ' ').trim()).toBe('then fires deploy_done')
    expect(rows[1].textContent).toContain('(deleted workspace)')
    expect(q(rows[1], 'trigger-assignee').textContent.trim()).toBe('Human')
    expect(q(rows[1], 'trigger-yolo-badge')).toBeNull()
    expect(q(rows[1], 'trigger-emits').textContent).toContain('(deleted event)')
    expect(q(rows[0], 'trigger-edit').getAttribute('aria-label')).toBe('Edit trigger Triage it')
    const where = rows[0].querySelector('span.truncate')
    where.dispatchEvent(new MouseEvent('mouseenter'))
    where.dispatchEvent(new MouseEvent('mouseleave'))
    const tasks = qa(el, 'event-task')
    expect(tasks[0].textContent.replace(/\s+/g, ' ')).toContain('Done · backend · Nov 23, 2026')
    expect(tasks[1].textContent).toContain('weird · notes')
  })

  it('edits the guidelines in place: Esc cancels, Cmd+Enter saves, a refusal keeps the form', async () => {
    const seen = serve(page({ [`PATCH ${E}`]: [[null, 500], [{ event: { ...events[0], payloadGuidelines: 'New' } }, 200]] }))
    const el = await mount(EventDetailView)
    q(el, 'event-guidelines-edit').click()
    await flush()
    const input = q(el, 'event-guidelines-input')
    expect(document.activeElement).toBe(input)
    expect(input.value).toBe('The issue')
    keydown(input, 'Escape')
    await flush()
    expect(q(el, 'event-guidelines-input')).toBeNull()
    q(el, 'event-guidelines-edit').click()
    await flush()
    ;[...el.querySelectorAll('form button')].find((b) => b.textContent.trim() === 'Cancel').click()
    await flush()
    expect(q(el, 'event-guidelines-input')).toBeNull()

    q(el, 'event-guidelines-edit').click()
    await flush()
    type(q(el, 'event-guidelines-input'), ' New ')
    keydown(q(el, 'event-guidelines-input'), 'n')
    q(el, 'event-guidelines-save').click()
    await flush()
    expect(toastMessages()).toEqual(['Failed to update event'])
    expect(q(el, 'event-guidelines-input')).not.toBeNull()
    keydown(q(el, 'event-guidelines-input'), 'Enter', { metaKey: true })
    await flush()
    expect(bodyOf(seen, 'PATCH')).toEqual({ payloadGuidelines: 'New' })
    expect(q(el, 'event-guidelines-text').textContent).toBe('New')
    expect(toastMessages()).toContain('Payload guidelines saved')
  })

  it('offers guidelines when there are none, and saves once at a time', async () => {
    let answer
    serve(page({ [`GET ${E}`]: [[{ event: { id: 'ev1', name: 'issue_opened' } }, 200]] }))
    const plain = globalThis.fetch
    const patches = vi.fn(() => new Promise((r) => { answer = r }))
    vi.stubGlobal('fetch', vi.fn((url, init = {}) => (init.method === 'PATCH' ? patches() : plain(url, init))))
    const el = await mount(EventDetailView)
    expect(q(el, 'event-guidelines-empty')).not.toBeNull()
    q(el, 'event-guidelines-empty').querySelector('button').click()
    await flush()
    expect(q(el, 'event-guidelines-input').value).toBe('')
    q(el, 'event-guidelines-save').click()
    keydown(q(el, 'event-guidelines-input'), 'Enter', { metaKey: true })
    await flush()
    expect(patches).toHaveBeenCalledTimes(1)
    answer(new Response(JSON.stringify({ event: events[0] }), { status: 200 }))
    await flush()
  })

  it('adds a trigger from the empty list, and shows a refused one', async () => {
    const seen = serve(page({
      [`GET ${E}/triggers`]: [[{}, 200]],
      [`GET ${E}/tasks`]: [[{}, 200]],
      [`GET ${API}/events`]: [[{}, 200]],
      [`POST ${E}/triggers`]: [[null, 500], [{ eventTrigger: trigger({ id: 't9', title: 'New one' }) }, 201]],
    }))
    const el = await mount(EventDetailView)
    q(el, 'triggers-empty-add').click()
    await flush()
    expect(q(el, 'trigger-add')).toBeNull()
    const select = q(el, 'trigger-workspace')
    select.value = 'w1'
    select.dispatchEvent(new Event('change'))
    type(q(el, 'trigger-title'), 'New one')
    q(el, 'trigger-submit').click()
    await flush()
    expect(toastMessages()).toEqual(['Failed to create event trigger'])
    q(el, 'trigger-submit').click()
    await flush()
    expect(bodyOf(seen, 'POST')).toMatchObject({ workspaceId: 'w1', title: 'New one' })
    expect(q(el, 'trigger-form')).toBeNull()
    expect(qa(el, 'trigger-row').map((r) => r.textContent)).toEqual([expect.stringContaining('New one')])
    q(el, 'trigger-add').click()
    await flush()
    q(el, 'trigger-cancel').click()
    await flush()
    expect(q(el, 'trigger-form')).toBeNull()
  })

  it('edits a trigger in place of its row', async () => {
    const seen = serve(page({ [`PATCH ${E}/triggers/t2`]: [[{ eventTrigger: trigger({ id: 't2', title: 'Renamed' }) }, 200]] }))
    const el = await mount(EventDetailView)
    q(el, 'trigger-add').click()
    await flush()
    qa(el, 'trigger-edit')[1].click()
    await flush()
    // One form at a time: the new one closed, the row turned into the form.
    expect(qa(el, 'trigger-form')).toHaveLength(1)
    expect(q(qa(el, 'trigger-row')[1], 'trigger-form')).not.toBeNull()
    type(q(el, 'trigger-title'), 'Renamed')
    q(el, 'trigger-submit').click()
    await flush()
    expect(calls(seen)).toContain(`PATCH ${E}/triggers/t2`)
    expect(qa(el, 'trigger-row')[1].textContent).toContain('Renamed')
    expect(toastMessages()).toContain('Trigger saved')
    qa(el, 'trigger-edit')[0].click()
    await flush()
    q(el, 'trigger-add').click()
    await flush()
    expect(qa(el, 'trigger-form')).toHaveLength(1)
    expect(q(qa(el, 'trigger-row')[0], 'trigger-form')).toBeNull()
  })

  it('deletes a trigger and the event once confirmed', async () => {
    serve(page({
      [`DELETE ${E}/triggers/t1`]: [[null, 500], [undefined, 204]],
      [`DELETE ${E}`]: [[null, 500], [undefined, 204]],
    }))
    const el = await mount(EventDetailView)
    q(el, 'trigger-delete').click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(toastMessages()).toEqual(['Failed to delete event trigger'])
    q(el, 'trigger-delete').click()
    await flush()
    modalButton('Cancel').click()
    await flush()
    q(el, 'trigger-delete').click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(qa(el, 'trigger-row')).toHaveLength(1)
    expect(toastMessages()).toContain('Trigger deleted')

    q(el, 'event-delete').click()
    await flush()
    modalButton('Cancel').click()
    await flush()
    q(el, 'event-delete').click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(toastMessages()).toContain('Failed to delete event')
    expect(push).not.toHaveBeenCalled()
    q(el, 'event-delete').click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(push).toHaveBeenCalledWith('/events')
  })

  it('pages the tasks ten at a time, using the workspaces already loaded', async () => {
    const seen = serve(page({ [`GET ${E}/tasks`]: [[{ tasks: Array.from({ length: 12 }, (_, i) => task({ id: `k${i}`, createdAt: '' })) }, 200]] }))
    const pinia = createPinia()
    useWorkspaceStore(pinia).workspaces = workspaces
    const el = await mount(EventDetailView, {}, pinia)
    expect(calls(seen)).not.toContain(`GET ${API}/workspaces`)
    expect(qa(el, 'event-task')).toHaveLength(10)
    const more = [...el.querySelectorAll('button')].find((b) => /Load More/i.test(b.textContent))
    more.click()
    await flush()
    expect(qa(el, 'event-task')).toHaveLength(12)
  })

  it('shows failed reads, and reloads quietly on a WebMCP change', async () => {
    const seen = serve(page({
      [`GET ${E}`]: [[null, 500], [{ event: events[0] }, 200]],
      [`GET ${E}/triggers`]: [[null, 500], [{ eventTriggers: [trigger()] }, 200]],
      [`GET ${E}/tasks`]: [[null, 500], [{ tasks: [] }, 200]],
      [`GET ${API}/events`]: [[null, 500], [{ events }, 200]],
    }))
    const el = await mount(EventDetailView)
    expect(toastMessages()).toEqual(expect.arrayContaining(['Failed to fetch event', 'Failed to fetch event triggers', 'Failed to fetch event tasks']))
    expect(q(el, 'event-delete')).toBeNull()
    notifyWebMCPChange()
    await flush()
    expect(q(el, 'event-name').textContent).toBe('issue_opened')
    expect(qa(el, 'trigger-row')).toHaveLength(1)
    expect(calls(seen).filter((c) => c === `GET ${API}/workspaces`)).toHaveLength(1)
  })
})
