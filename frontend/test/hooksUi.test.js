// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The account's hooks: the calls, the helpers, and the Hooks tab's pages.
 */

import { describe, it, expect, vi, afterEach, beforeEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
// What fetch's Response reads; jsdom's own Blob it would turn into text.
import { Blob } from 'node:buffer'

const push = vi.fn()
const route = { path: '/events/hooks', params: {}, query: {} }
vi.mock('vue-router', () => ({
  useRouter: () => ({ push, replace: vi.fn() }),
  useRoute: () => route,
}))

import {
  createHookRegistration, deleteHook, deleteHookRegistration, fetchAvailableHooks, fetchHookDeliveries,
  fetchHookRegistrations, getHookRegistration, replayHookDelivery, rotateHookToken, updateHookRegistration, uploadHook,
} from '../src/api'
import { useToasts } from '../src/composables/useToasts'
import { notifyWebMCPChange } from '../src/composables/useWebMCPChanges'
import {
  generateSecret, inputText, inputValue, plural, rememberTokenUrl, statusStyle, takeTokenUrl,
} from '../src/utils/hooks'
import EventsTabs from '../src/components/EventsTabs.vue'
import HookRegistrationForm from '../src/components/HookRegistrationForm.vue'
import HooksView from '../src/views/HooksView.vue'
import HookNewView from '../src/views/HookNewView.vue'
import HookRegistrationView from '../src/views/HookRegistrationView.vue'
import EventsView from '../src/views/EventsView.vue'

const API = '/api/v1'

const github = (over = {}) => ({
  id: 'gh1', builtIn: true, name: 'github', version: '1.0.0', description: 'GitHub webhooks', enabled: true,
  inputs: [
    { name: 'webhookSecret', label: 'Webhook secret', type: 'string', secret: true, required: true },
    { name: 'branches', type: 'array', items: 'string', secret: false, required: false },
    { name: 'maxRuns', type: 'number', secret: false, required: false },
    { name: 'drafts', type: 'boolean', secret: false, required: false },
  ],
  events: [{ name: 'ci.failed', description: 'A run failed' }, { name: 'pr.opened' }],
  ...over,
})
const own = (over = {}) => github({ id: 'own1', builtIn: false, name: 'sentry', version: '0.2.1', description: '', ...over })
const events = [{ id: 'ev1', name: 'ci_failed' }, { id: 'ev2', name: 'review_pr' }]
const reg = (over = {}) => ({
  id: 'r1', hookId: 'gh1', hookName: 'github', hookAvailable: true, name: 'agentrq CI', enabled: true,
  inputs: { branches: ['main', 'release/*'], drafts: true }, secretsSet: ['webhookSecret'],
  routes: { 'ci.failed': 'ev1' }, url: 'https://x.test/api/v1/hooks/in/r1', allowAllCommandsEvents: [],
  ...over,
})
const delivery = (over = {}) => ({
  id: 'd1', registrationId: 'r1', receivedAt: '2026-11-12T04:12:00Z', status: 'published', attempts: 1,
  eventNames: ['ci.failed'], error: '', durationMs: 3, replayable: false, ...over,
})

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

async function mount(component, props = {}) {
  const el = document.createElement('div')
  document.body.append(el)
  const app = createApp({ render: () => h(component, props) })
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
const modalButton = (label) => [...document.body.querySelectorAll('[role="dialog"] button')].find((b) => b.textContent.trim() === label)

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

describe('the hook calls', () => {
  it('reads the hooks, registrations and deliveries', async () => {
    const seen = serve({
      [`GET ${API}/hooks`]: [[{ hooks: [github()], allowUserHooks: true }, 200]],
      [`GET ${API}/hooks/registrations`]: [[{ registrations: [reg()] }, 200]],
      [`GET ${API}/hooks/registrations/r1`]: [[reg(), 200]],
      [`GET ${API}/hooks/registrations/r1/deliveries?limit=50`]: [[{ deliveries: [delivery()] }, 200]],
      [`GET ${API}/hooks/registrations/r1/deliveries?limit=10&before=d9`]: [[{ deliveries: [] }, 200]],
    })
    expect((await fetchAvailableHooks()).allowUserHooks).toBe(true)
    expect((await fetchHookRegistrations()).registrations).toHaveLength(1)
    expect((await getHookRegistration('r1')).name).toBe('agentrq CI')
    expect((await fetchHookDeliveries('r1')).deliveries).toHaveLength(1)
    expect((await fetchHookDeliveries('r1', { before: 'd9', limit: 10 })).deliveries).toEqual([])
    expect(seen).toHaveLength(5)
  })

  it('writes registrations as JSON', async () => {
    const seen = serve({
      [`POST ${API}/hooks/registrations`]: [[reg({ urlWithToken: 'u?t' }), 201]],
      [`PATCH ${API}/hooks/registrations/r1`]: [[reg({ enabled: false }), 200]],
      [`POST ${API}/hooks/registrations/r1/rotateToken`]: [[reg({ urlWithToken: 'u?t2' }), 200]],
      [`POST ${API}/hooks/registrations/r1/deliveries/d1/replay`]: [[undefined, 204]],
      [`DELETE ${API}/hooks/registrations/r1`]: [[undefined, 204]],
      [`DELETE ${API}/hooks/own1`]: [[undefined, 204]],
    })
    await createHookRegistration({ hookId: 'gh1', name: 'n' })
    expect(JSON.parse(seen[0].init.body)).toEqual({ hookId: 'gh1', name: 'n', enabled: true, inputs: {}, routes: {} })
    expect(seen[0].init.headers).toEqual({ 'Content-Type': 'application/json' })
    expect((await updateHookRegistration('r1', { enabled: false })).enabled).toBe(false)
    expect(seen[1].init.body).toBe('{"enabled":false}')
    expect((await rotateHookToken('r1')).urlWithToken).toBe('u?t2')
    expect(await replayHookDelivery('r1', 'd1')).toBe(true)
    expect(await deleteHookRegistration('r1')).toBe(true)
    expect(await deleteHook('own1')).toBe(true)
  })

  it('uploads a hook gzipped, adding or upgrading', async () => {
    const seen = serve({ [`POST ${API}/hooks`]: [[own(), 201]], [`PUT ${API}/hooks/own1`]: [[own({ version: '0.3.0' }), 200]] })
    expect((await uploadHook(new Blob(['\0asm']))).name).toBe('sentry')
    expect((await uploadHook(new Blob(['\0asm']), 'own1')).version).toBe('0.3.0')
    expect(seen.map((s) => [s.init.method, s.init.headers['Content-Encoding']])).toEqual([['POST', 'gzip'], ['PUT', 'gzip']])
  })

  it("throws the server's message, or a fallback", async () => {
    serve({
      [`POST ${API}/hooks/registrations`]: [[{ error: { message: 'a name is required' } }, 422]],
      [`GET ${API}/hooks`]: [[null, 500]],
    })
    await expect(createHookRegistration({ hookId: 'gh1', name: '' })).rejects.toThrow('a name is required')
    await expect(fetchAvailableHooks()).rejects.toThrow('Failed to fetch hooks')
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('oops', { status: 502 }))))
    await expect(rotateHookToken('r1')).rejects.toThrow('Failed to rotate the token')
  })
})

describe('the hook helpers', () => {
  it('hands the URL with its token to the next page once', () => {
    rememberTokenUrl('r1', 'u?t')
    rememberTokenUrl('r2', '')
    expect(takeTokenUrl('r1')).toBe('u?t')
    expect(takeTokenUrl('r1')).toBe('')
    expect(takeTokenUrl('r2')).toBe('')
  })

  it('turns inputs into fields and back', () => {
    const [, list, num, bool] = github().inputs
    expect(inputText(list, ['a', 'b'])).toBe('a, b')
    expect(inputText(list, undefined)).toBe('')
    expect(inputText(bool, undefined)).toBe(false)
    expect(inputText(bool, 1)).toBe(true)
    expect(inputText(num, 3)).toBe('3')
    expect(inputValue(list, ' a, ,b ')).toEqual(['a', 'b'])
    expect(inputValue({ ...list, items: 'number' }, '1,2')).toEqual([1, 2])
    expect(inputValue(num, '7')).toBe(7)
    expect(inputValue(num, '  ')).toBeUndefined()
    expect(inputValue(num, null)).toBeUndefined()
    expect(inputValue(bool, '')).toBe(false)
    expect(inputValue({ type: 'string' }, ' s ')).toBe('s')
  })

  it('makes a secret, styles a status, counts', () => {
    expect(generateSecret()).toMatch(/^[0-9a-f]{32}$/)
    expect(generateSecret()).not.toBe(generateSecret())
    expect(statusStyle('failed')).toContain('red')
    expect(statusStyle('queued')).toContain('gray')
    expect(plural(1, 'route')).toBe('1 route')
    expect(plural(2, 'route')).toBe('2 routes')
  })
})

describe('the Events tabs', () => {
  it('marks the tab shown', async () => {
    const el = await mount(EventsTabs, { active: 'hooks' })
    expect(q(el, 'tab-hooks').getAttribute('aria-current')).toBe('page')
    expect(q(el, 'tab-events').getAttribute('href')).toBe('/events')
    expect(q(el, 'tab-events').hasAttribute('aria-current')).toBe(false)
  })

  it('shows on the Events page', async () => {
    serve({ [`GET ${API}/events`]: [[{ events: [] }, 200]] })
    const el = await mount(EventsView)
    expect(q(el, 'tab-events').getAttribute('aria-current')).toBe('page')
  })
})

describe('HooksView', () => {
  const list = (regs, available = { hooks: [github()], allowUserHooks: false }) => ({
    [`GET ${API}/hooks/registrations`]: [[{ registrations: regs }, 200]],
    [`GET ${API}/hooks`]: [[available, 200]],
  })

  it('lists the registrations, marking the risky, the off and the broken', async () => {
    serve(list([
      reg(),
      reg({ id: 'r2', name: 'releases', enabled: false, hookAvailable: false, routes: {}, allowAllCommandsEvents: ['pr.opened'] }),
    ]))
    const el = await mount(HooksView)
    const rows = qa(el, 'reg-row')
    expect(rows.map((r) => r.getAttribute('href'))).toEqual(['/events/hooks/r1', '/events/hooks/r2'])
    expect(rows[0].textContent).toContain('1 route')
    expect(q(rows[0], 'reg-risky')).toBeNull()
    expect(q(rows[1], 'reg-risky')).not.toBeNull()
    expect(rows[1].textContent).toContain('0 routes')
    expect(rows[1].textContent).toContain('hook unavailable')
    expect(rows[1].textContent).toContain('Off')
    expect(q(el, 'own-hooks')).toBeNull()
    q(el, 'reg-new').click()
    expect(push).toHaveBeenCalledWith('/events/hooks/new')
  })

  it('says so when there is none, and reloads on a WebMCP change', async () => {
    const seen = serve(list([]))
    const el = await mount(HooksView)
    expect(q(el, 'reg-empty')).not.toBeNull()
    notifyWebMCPChange()
    await flush()
    expect(seen).toHaveLength(4)
  })

  it('reads answers with no lists as empty', async () => {
    serve({ [`GET ${API}/hooks`]: [[{}, 200]], [`GET ${API}/hooks/registrations`]: [[{}, 200]] })
    const el = await mount(HooksView)
    expect(q(el, 'reg-empty')).not.toBeNull()
  })

  it('reads a registration with no routes or warnings', async () => {
    serve(list([reg({ routes: undefined, allowAllCommandsEvents: undefined })], { hooks: [own()], allowUserHooks: true }))
    const el = await mount(HooksView)
    expect(q(el, 'reg-row').textContent).toContain('0 routes')
    q(el, 'own-hook-delete').click()
    await flush()
    modalButton('Cancel').click()
    await new Promise((r) => setTimeout(r, 100))
    expect(document.body.textContent).not.toContain('Delete the hook sentry?')
  })

  it('shows a failed read', async () => {
    serve({ [`GET ${API}/hooks`]: [[null, 500]], [`GET ${API}/hooks/registrations`]: [[{ registrations: [] }, 200]] })
    await mount(HooksView)
    expect(toastMessages()).toEqual(['Failed to fetch hooks'])
  })

  it("lists, uploads, upgrades and deletes the account's own hooks", async () => {
    const seen = serve({
      ...list([], { hooks: [github(), own()], allowUserHooks: true }),
      [`POST ${API}/hooks`]: [[own({ id: 'own2', name: 'linear' }), 201]],
      [`PUT ${API}/hooks/own1`]: [[{ error: { message: 'no required secret input' } }, 422], [own({ version: '0.3.0' }), 200]],
      [`DELETE ${API}/hooks/own1`]: [[{ error: { message: 'registered' } }, 409], [undefined, 204]],
    })
    const el = await mount(HooksView)
    expect(qa(el, 'own-hook').map((r) => r.textContent)).toEqual([expect.stringContaining('sentry')])
    const file = q(el, 'hook-file')
    const click = vi.spyOn(file, 'click').mockImplementation(() => {})
    const choose = async (files) => {
      Object.defineProperty(file, 'files', { configurable: true, value: files })
      file.dispatchEvent(new Event('change'))
      await flush()
    }

    q(el, 'hook-upload').click()
    expect(click).toHaveBeenCalled()
    await choose([])
    expect(calls(seen)).not.toContain(`POST ${API}/hooks`)
    await choose([new Blob(['\0asm'])])
    expect(toastMessages()).toEqual(['Added linear 0.2.1.'])

    q(el, 'own-hook-upgrade').click()
    await choose([new Blob(['\0asm'])])
    expect(toastMessages()).toContain('no required secret input')
    await choose([new Blob(['\0asm'])])
    expect(toastMessages()).toContain('Upgraded sentry to 0.3.0.')

    q(el, 'own-hook-delete').click()
    await flush()
    expect(document.body.textContent).toContain('Delete the hook sentry?')
    modalButton('Delete').click()
    await flush()
    expect(toastMessages()).toContain('registered')
    expect(qa(el, 'own-hook')).toHaveLength(1)
    q(el, 'own-hook-delete').click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(qa(el, 'own-hook')).toHaveLength(0)
    expect(toastMessages()).toContain('Deleted sentry.')
  })
})

describe('HookRegistrationForm', () => {
  function triggers(map) {
    return Object.fromEntries(Object.entries(map).map(([id, allow]) => [
      `GET ${API}/events/${id}/triggers`, [[{ eventTriggers: [{ id: 't', allowAllCommands: allow }] }, 200]],
    ]))
  }

  async function form(props) {
    const onSubmit = vi.fn()
    const onCancel = vi.fn()
    const el = await mount(HookRegistrationForm, { hooks: [github(), own()], events, onSubmit, onCancel, ...props })
    return { el, onSubmit, onCancel }
  }

  it('asks for a name and a hook before anything else', async () => {
    serve({})
    const { el, onSubmit, onCancel } = await form()
    q(el, 'reg-submit').click()
    await flush()
    expect(q(el, 'reg-error').textContent).toBe('A name is required.')
    type(q(el, 'reg-name'), 'CI')
    q(el, 'reg-submit').click()
    await flush()
    expect(q(el, 'reg-error').textContent).toBe('Pick a hook.')
    expect(onSubmit).not.toHaveBeenCalled()
    q(el, 'reg-cancel').click()
    expect(onCancel).toHaveBeenCalled()
  })

  it('says when no hook is available', async () => {
    serve({})
    const { el } = await form({ hooks: [], events: [] })
    expect(q(el, 'reg-no-hooks')).not.toBeNull()
  })

  it("draws a hook's inputs and routes, warns of a risky route, and sends what was filled", async () => {
    serve(triggers({ ev1: false, ev2: true }))
    const { el, onSubmit } = await form()
    type(q(el, 'reg-name'), '  agentrq CI ')
    const pick = qa(el, 'reg-hook')
    expect(pick.map((p) => p.textContent)).toEqual([expect.stringContaining('built-in'), expect.stringContaining('yours')])
    const choose = async (i) => {
      const radio = pick[i].querySelector('input')
      radio.checked = true
      radio.dispatchEvent(new Event('change'))
      await flush()
    }
    await choose(1)
    type(q(el, 'reg-input-branches'), 'dropped')
    await choose(0)
    expect(q(el, 'reg-input-branches').value).toBe('')
    expect(qa(el, 'reg-input')).toHaveLength(4)
    expect(qa(el, 'reg-route')).toHaveLength(2)

    q(el, 'reg-generate-webhookSecret').click()
    await flush()
    const secret = q(el, 'reg-input-webhookSecret').value
    expect(secret).toMatch(/^[0-9a-f]{32}$/)
    expect(q(el, 'reg-secret-shown').textContent).toBe(secret)
    type(q(el, 'reg-input-branches'), 'main, release/*')
    const box = qa(el, 'reg-input')[3].querySelector('input[type=checkbox]')
    box.checked = true
    box.dispatchEvent(new Event('change'))
    box.checked = false
    box.dispatchEvent(new Event('change'))
    const routeTo = (event, id) => {
      const select = q(el, `reg-route-${event}`)
      select.value = id
      select.dispatchEvent(new Event('change'))
    }
    routeTo('ci.failed', 'ev1')
    await flush()
    expect(q(el, 'reg-warning')).toBeNull()
    routeTo('pr.opened', 'ev2')
    await flush()
    expect(q(el, 'reg-warning').textContent).toContain('review_pr has a trigger')
    routeTo('ci.failed', 'ev2')
    await flush()
    routeTo('ci.failed', 'ev1')
    await flush()

    q(el, 'reg-submit').click()
    await flush()
    expect(onSubmit).toHaveBeenCalledWith({
      hookId: 'gh1', name: 'agentrq CI',
      inputs: { webhookSecret: secret, branches: ['main', 'release/*'], drafts: false },
      routes: { 'ci.failed': 'ev1', 'pr.opened': 'ev2' },
    })
  })

  it('names every risky event, and ignores a trigger it could not read', async () => {
    serve({
      ...triggers({ ev1: true, ev2: true }),
    })
    const both = [{ id: 'ev1', name: 'ci_failed' }, { id: 'ev2', name: 'review_pr' }, { id: 'ev3', name: 'gone' }]
    const { el } = await form({ registration: reg({ routes: { 'ci.failed': 'ev1', 'pr.opened': 'ev2' } }), events: both })
    expect(q(el, 'reg-warning').textContent).toContain('ci_failed, review_pr have a trigger')
    const select = q(el, 'reg-route-pr.opened')
    select.value = 'ev3'
    select.dispatchEvent(new Event('change'))
    await flush()
    expect(q(el, 'reg-warning').textContent).toContain('ci_failed has a trigger')
  })

  it('edits a registration: its hook fixed, a blank secret kept', async () => {
    serve(triggers({ ev1: false }))
    const { el, onSubmit } = await form({ registration: reg(), events: [] })
    expect(qa(el, 'reg-hook')).toHaveLength(0)
    expect(q(el, 'reg-name').value).toBe('agentrq CI')
    expect(q(el, 'reg-input-webhookSecret').placeholder).toContain('leave blank to keep')
    expect(q(el, 'reg-input-branches').value).toBe('main, release/*')
    expect(el.textContent).toContain('You have no events yet')
    expect(q(el, 'reg-submit').textContent.trim()).toBe('Save')
    q(el, 'reg-submit').click()
    await flush()
    expect(onSubmit).toHaveBeenCalledWith({
      name: 'agentrq CI', inputs: { branches: ['main', 'release/*'], drafts: true }, routes: { 'ci.failed': 'ev1' },
    })
  })

  it('shows a hook with neither inputs nor events, and a secret not yet set', async () => {
    serve({})
    const bare = github({ inputs: [], events: [] })
    const { el } = await form({ hooks: [bare], registration: reg({ routes: undefined, inputs: undefined, secretsSet: undefined }) })
    expect(qa(el, 'reg-input')).toHaveLength(0)
    expect(qa(el, 'reg-route')).toHaveLength(0)
    const { el: el2 } = await form({ hooks: [github()], registration: reg({ secretsSet: [] }) })
    expect(q(el2, 'reg-input-webhookSecret').placeholder).toBe('')
  })

  it('reads triggers answered with no list as none', async () => {
    serve({ [`GET ${API}/events/ev1/triggers`]: [[{}, 200]] })
    const { el } = await form({ registration: reg() })
    expect(q(el, 'reg-warning')).toBeNull()
  })

  it('ignores a failed trigger read', async () => {
    serve({ [`GET ${API}/events/ev1/triggers`]: [[null, 500]] })
    const { el } = await form({ registration: reg() })
    expect(q(el, 'reg-warning')).toBeNull()
  })
})

describe('HookNewView', () => {
  it('creates a registration and opens it with its token', async () => {
    const seen = serve({
      [`GET ${API}/hooks`]: [[{ hooks: [github(), own({ enabled: false })], allowUserHooks: true }, 200]],
      [`GET ${API}/events`]: [[{ events }, 200]],
      [`POST ${API}/hooks/registrations`]: [[{ error: { message: 'the hook is off' } }, 422], [reg({ id: 'r9', urlWithToken: 'https://x/r9?t=k' }), 201]],
    })
    const el = await mount(HookNewView)
    expect(qa(el, 'reg-hook')).toHaveLength(1)
    type(q(el, 'reg-name'), 'CI')
    const radio = q(el, 'reg-hook').querySelector('input')
    radio.checked = true
    radio.dispatchEvent(new Event('change'))
    await flush()
    type(q(el, 'reg-input-webhookSecret'), 's3cret')
    q(el, 'reg-submit').click()
    await flush()
    expect(toastMessages()).toEqual(['the hook is off'])
    expect(push).not.toHaveBeenCalled()
    q(el, 'reg-submit').click()
    await flush()
    expect(calls(seen).filter((c) => c.startsWith('POST'))).toHaveLength(2)
    expect(push).toHaveBeenCalledWith('/events/hooks/r9')
    expect(takeTokenUrl('r9')).toBe('https://x/r9?t=k')
    q(el, 'reg-cancel').click()
    expect(push).toHaveBeenLastCalledWith('/events/hooks')
  })

  it('reads answers with no lists as empty', async () => {
    serve({ [`GET ${API}/hooks`]: [[{}, 200]], [`GET ${API}/events`]: [[{}, 200]] })
    const el = await mount(HookNewView)
    expect(q(el, 'reg-no-hooks')).not.toBeNull()
  })

  it('shows a failed read', async () => {
    serve({ [`GET ${API}/hooks`]: [[{ hooks: [] }, 200]], [`GET ${API}/events`]: [[null, 500]] })
    const el = await mount(HookNewView)
    expect(toastMessages()).toEqual(['Failed to fetch events'])
    expect(q(el, 'reg-no-hooks')).not.toBeNull()
  })
})

describe('HookRegistrationView', () => {
  const R = `${API}/hooks/registrations/r1`
  const page = (over = {}) => ({
    [`GET ${R}`]: [[reg(), 200]],
    [`GET ${API}/hooks`]: [[{ hooks: [github()], allowUserHooks: false }, 200]],
    [`GET ${API}/events`]: [[{ events }, 200]],
    [`GET ${API}/events/ev1/triggers`]: [[{ eventTriggers: [] }, 200]],
    [`GET ${R}/deliveries?limit=50`]: [[{ deliveries: [] }, 200]],
    ...over,
  })

  beforeEach(() => {
    route.params = { id: 'r1' }
  })

  it('shows the URL, inputs and routes, and that the token was shown once', async () => {
    serve(page({ [`GET ${R}`]: [[reg({ allowAllCommandsEvents: ['ci.failed'], routes: { 'ci.failed': 'ev1', 'pr.opened': 'evX' }, inputs: { branches: ['main'], maxRuns: 2 } }), 200]] }))
    const el = await mount(HookRegistrationView)
    expect(q(el, 'reg-title').textContent).toBe('agentrq CI')
    expect(el.textContent).toContain('1.0.0 · built-in')
    expect(q(el, 'reg-url').textContent).toBe('https://x.test/api/v1/hooks/in/r1')
    expect(q(el, 'reg-token-once')).toBeNull()
    const inputs = q(el, 'reg-inputs').textContent.replace(/\s+/g, ' ')
    expect(inputs).toContain('Webhook secret•••••••• set')
    expect(inputs).toContain('branchesmain')
    expect(inputs).toContain('maxRuns2')
    expect(inputs).toContain('drafts—')
    const routes = q(el, 'reg-routes').textContent.replace(/\s+/g, ' ')
    expect(routes).toContain('ci.failed → ci_failed ⚠')
    expect(routes).toContain('pr.opened → evX')
    expect(q(el, 'reg-no-deliveries').textContent).toContain('No requests yet')
  })

  it('shows the URL with its token right after the registration is made', async () => {
    serve(page())
    rememberTokenUrl('r1', 'https://x.test/api/v1/hooks/in/r1?agentrq_hook_token=k3x9q2ma')
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const el = await mount(HookRegistrationView)
    expect(q(el, 'reg-url').textContent).toContain('agentrq_hook_token=k3x9q2ma')
    expect(q(el, 'reg-token-once')).not.toBeNull()
    q(el, 'reg-copy').click()
    await flush()
    expect(writeText).toHaveBeenCalledWith('https://x.test/api/v1/hooks/in/r1?agentrq_hook_token=k3x9q2ma')
    expect(toastMessages()).toEqual(['Copied.'])
  })

  it('says when copying fails', async () => {
    serve(page())
    vi.stubGlobal('navigator', { clipboard: { writeText: () => Promise.reject(new Error('denied')) } })
    const el = await mount(HookRegistrationView)
    q(el, 'reg-copy').click()
    await flush()
    expect(toastMessages()).toEqual(['Could not copy; select the URL instead.'])
  })

  it('says when the registration cannot be loaded', async () => {
    serve(page({ [`GET ${R}`]: [[{ error: { message: 'not found' } }, 404]] }))
    const el = await mount(HookRegistrationView)
    expect(q(el, 'reg-missing')).not.toBeNull()
    expect(toastMessages()).toEqual(['not found'])
  })

  it('marks a registration whose hook is gone, offering no edit', async () => {
    serve(page({ [`GET ${R}`]: [[reg({ hookAvailable: false, routes: {} }), 200]], [`GET ${API}/hooks`]: [[{ hooks: [own()] }, 200]] }))
    const el = await mount(HookRegistrationView)
    expect(el.textContent).toContain('its hook is gone or turned off')
    expect(q(el, 'reg-edit')).toBeNull()
    expect(q(el, 'reg-routes').textContent).toContain('Nothing routed')
  })

  it('turns the registration off, and shows a refused change', async () => {
    const seen = serve(page({ [`PATCH ${R}`]: [[reg({ enabled: false }), 200], [{ error: { message: 'nope' } }, 422]] }))
    const el = await mount(HookRegistrationView)
    q(el, 'reg-enabled').click()
    await flush()
    expect(JSON.parse(seen.find((s) => s.init.method === 'PATCH').init.body)).toEqual({ enabled: false })
    expect(q(el, 'reg-enabled').getAttribute('aria-checked')).toBe('false')
    q(el, 'reg-enabled').click()
    await flush()
    expect(toastMessages()).toEqual(['nope'])
    expect(q(el, 'reg-enabled').getAttribute('aria-checked')).toBe('false')
  })

  it('edits the inputs and routes', async () => {
    serve(page({ [`PATCH ${R}`]: [[{ error: { message: 'bad input' } }, 422], [reg({ name: 'renamed' }), 200]] }))
    const el = await mount(HookRegistrationView)
    q(el, 'reg-edit').click()
    await flush()
    type(q(el, 'reg-name'), 'renamed')
    q(el, 'reg-submit').click()
    await flush()
    expect(toastMessages()).toEqual(['bad input'])
    expect(q(el, 'reg-submit')).not.toBeNull()
    q(el, 'reg-submit').click()
    await flush()
    expect(q(el, 'reg-title').textContent).toBe('renamed')
    expect(q(el, 'reg-submit')).toBeNull()
    q(el, 'reg-edit').click()
    await flush()
    q(el, 'reg-cancel').click()
    await flush()
    expect(q(el, 'reg-submit')).toBeNull()
  })

  it('rotates the token once confirmed, showing the new URL', async () => {
    serve(page({ [`POST ${R}/rotateToken`]: [[reg({ urlWithToken: 'https://x/r1?agentrq_hook_token=new' }), 200]] }))
    const el = await mount(HookRegistrationView)
    q(el, 'reg-rotate').click()
    await flush()
    expect(document.body.textContent).toContain('The current URL stops working at once')
    modalButton('Cancel').click()
    await flush()
    q(el, 'reg-rotate').click()
    await flush()
    modalButton('Rotate').click()
    await flush()
    expect(q(el, 'reg-url').textContent).toBe('https://x/r1?agentrq_hook_token=new')
    expect(toastMessages()).toEqual(['New token made. The old URL no longer works.'])
  })

  it('shows a failed rotation', async () => {
    serve(page({ [`POST ${R}/rotateToken`]: [[null, 500]] }))
    const el = await mount(HookRegistrationView)
    q(el, 'reg-rotate').click()
    await flush()
    modalButton('Rotate').click()
    await flush()
    expect(toastMessages()).toEqual(['Failed to rotate the token'])
  })

  it('lists, filters, replays and pages the deliveries', async () => {
    const first = Array.from({ length: 50 }, (_, i) => delivery({ id: `d${i}` }))
    first[1] = delivery({ id: 'd1', status: 'failed', eventNames: [], error: 'Timed out', attempts: 5, durationMs: 0, replayable: true })
    first[2] = delivery({ id: 'd2', status: 'rejected', eventNames: [], error: 'signature mismatch' })
    const seen = serve(page({
      [`GET ${R}/deliveries?limit=50`]: [[{ deliveries: first }, 200], [{ deliveries: first.slice(0, 3) }, 200]],
      [`GET ${R}/deliveries?limit=50&before=d49`]: [[{ deliveries: [delivery({ id: 'd50', status: 'duplicate', error: 'retry' })] }, 200]],
      [`POST ${R}/deliveries/d1/replay`]: [[{ error: { message: 'gone' } }, 404], [undefined, 204]],
    }))
    const el = await mount(HookRegistrationView)
    expect(qa(el, 'reg-delivery')).toHaveLength(50)
    expect(qa(el, 'reg-delivery')[1].textContent.replace(/\s+/g, ' ')).toContain('Timed out · 5 attempts')
    expect(q(el, 'reg-filter-failed').textContent).toBe('Failed 1')

    q(el, 'reg-more').click()
    await flush()
    expect(qa(el, 'reg-delivery')).toHaveLength(51)
    expect(q(el, 'reg-more')).toBeNull()
    expect(qa(el, 'reg-delivery')[50].textContent).toContain('ci.failed · retry')

    q(el, 'reg-filter-rejected').click()
    await flush()
    expect(qa(el, 'reg-delivery').map((d) => d.textContent)).toEqual([expect.stringContaining('signature mismatch')])
    q(el, 'reg-filter-failed').click()
    await flush()
    q(el, 'reg-replay').click()
    await flush()
    expect(toastMessages()).toEqual(['gone'])
    q(el, 'reg-replay').click()
    await flush()
    expect(toastMessages()).toContain('Queued again.')
    expect(calls(seen).filter((c) => c === `GET ${R}/deliveries?limit=50`)).toHaveLength(2)
    q(el, 'reg-filter-all').click()
    await flush()
    expect(qa(el, 'reg-delivery')).toHaveLength(3)
    q(el, 'reg-filter-failed').click()
    await flush()
    expect(q(el, 'reg-delivery')).not.toBeNull()
  })

  it('says when a filter shows nothing, and when a page fails', async () => {
    serve(page({
      [`GET ${R}/deliveries?limit=50`]: [[{ deliveries: Array.from({ length: 50 }, (_, i) => delivery({ id: `d${i}` })) }, 200]],
      [`GET ${R}/deliveries?limit=50&before=d49`]: [[null, 500]],
    }))
    const el = await mount(HookRegistrationView)
    q(el, 'reg-more').click()
    await flush()
    expect(toastMessages()).toEqual(['Failed to fetch deliveries'])
    q(el, 'reg-filter-failed').click()
    await flush()
    expect(q(el, 'reg-no-deliveries').textContent).toContain('None of these.')
  })

  it("shows an own hook's registration, its secret not set, nothing routed", async () => {
    serve(page({
      [`GET ${R}`]: [[reg({ hookId: 'own1', hookName: 'sentry', routes: undefined, secretsSet: undefined }), 200]],
      [`GET ${API}/hooks`]: [[{ hooks: [own({ inputs: [{ name: 'clientSecret', type: 'string', secret: true, required: true }] })] }, 200]],
      [`GET ${R}/deliveries?limit=50`]: [[{ deliveries: Array.from({ length: 50 }, (_, i) => delivery({ id: `d${i}` })) }, 200]],
      [`GET ${R}/deliveries?limit=50&before=d49`]: [[{}, 200]],
    }))
    const el = await mount(HookRegistrationView)
    expect(el.textContent).toContain('0.2.1 · yours')
    expect(q(el, 'reg-inputs').textContent).toContain('clientSecretnot set')
    expect(q(el, 'reg-routes').textContent).toContain('Nothing routed')
    q(el, 'reg-more').click()
    await flush()
    expect(qa(el, 'reg-delivery')).toHaveLength(50)
    expect(q(el, 'reg-more')).toBeNull()
  })

  it('reads an answer with no lists as empty', async () => {
    serve(page({
      [`GET ${API}/hooks`]: [[{}, 200]], [`GET ${API}/events`]: [[{}, 200]],
      [`GET ${R}/deliveries?limit=50`]: [[{}, 200]],
    }))
    const el = await mount(HookRegistrationView)
    expect(q(el, 'reg-edit')).toBeNull()
    expect(q(el, 'reg-no-deliveries')).not.toBeNull()
  })

  it('deletes the registration once confirmed, and shows a refused delete', async () => {
    serve(page({ [`DELETE ${R}`]: [[{ error: { message: 'busy' } }, 500], [undefined, 204]] }))
    const el = await mount(HookRegistrationView)
    q(el, 'reg-delete').click()
    await flush()
    expect(document.body.textContent).toContain('Delete agentrq CI?')
    modalButton('Delete').click()
    await flush()
    expect(toastMessages()).toEqual(['busy'])
    expect(push).not.toHaveBeenCalled()
    q(el, 'reg-delete').click()
    await flush()
    modalButton('Delete').click()
    await flush()
    expect(push).toHaveBeenCalledWith('/events/hooks')
    expect(toastMessages()).toContain('Deleted agentrq CI.')
  })

  it('closes the delete dialog on cancel', async () => {
    const seen = serve(page())
    const el = await mount(HookRegistrationView)
    q(el, 'reg-delete').click()
    await flush()
    modalButton('Cancel').click()
    await new Promise((r) => setTimeout(r, 100))
    expect(document.body.textContent).not.toContain('Delete agentrq CI?')
    expect(calls(seen).some((c) => c.startsWith('DELETE'))).toBe(false)
  })

  it('reloads on a WebMCP change', async () => {
    const seen = serve(page())
    await mount(HookRegistrationView)
    notifyWebMCPChange()
    await flush()
    expect(calls(seen).filter((c) => c === `GET ${R}`)).toHaveLength(2)
  })
})
