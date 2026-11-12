// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The operator's built-in hooks: the calls behind the page, and the page.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
// What fetch's Response reads; jsdom's own Blob it would turn into text.
import { Blob } from 'node:buffer'

const replace = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace, push: vi.fn() }),
  useRoute: () => ({ path: '/admin/hooks', query: {} }),
}))

const RouterLink = { props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to }, slots.default?.()) }

import { deleteAdminHook, fetchAdminHooks, setAdminHookEnabled, uploadAdminHook } from '../src/api'
import { useToasts } from '../src/composables/useToasts'
import AdminHooksView from '../src/views/AdminHooksView.vue'

const HOOKS = '/admin/v1/hooks'

const github = (over = {}) => ({
  id: 'gh1', builtIn: true, name: 'github', version: '1.0.0', description: 'GitHub webhooks',
  sha256: '9f2c00000000000000000000000000000000000000000000000000000000a41e', sizeBytes: 4796072, abiVersion: 1,
  inputs: [
    { name: 'webhookSecret', type: 'string', secret: true, required: true },
    { name: 'branches', type: 'array', items: 'string', secret: false, required: false },
  ],
  events: [{ name: 'ci.failed', description: 'A run failed' }, { name: 'push' }],
  enabled: true, ...over,
})

// Answers each call with the next reply for its method and path.
function serve(replies) {
  const seen = []
  vi.stubGlobal('fetch', vi.fn((url, init = {}) => {
    seen.push({ url, init })
    const key = `${init.method || 'GET'} ${url}`
    const [body, status] = replies[key]?.shift() ?? [{ error: { message: 'not found' } }, 404]
    return Promise.resolve(new Response(body === undefined ? null : JSON.stringify(body), { status }))
  }))
  return seen
}

const settle = () => new Promise((r) => setTimeout(r, 0))
const apps = []

afterEach(() => {
  apps.splice(0).forEach((app) => app.unmount())
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  replace.mockClear()
  const { toasts } = useToasts()
  toasts.value = []
})

async function gunzip(blob) {
  return new Response(new Response(await blob.arrayBuffer()).body.pipeThrough(new DecompressionStream('gzip'))).text()
}

describe('the admin hook calls', () => {
  it('lists the hooks', async () => {
    serve({ [`GET ${HOOKS}`]: [[{ hooks: [github()] }, 200]] })
    expect(await fetchAdminHooks()).toEqual([github()])
  })

  it('uploads a module gzipped, adding or upgrading', async () => {
    const seen = serve({ [`POST ${HOOKS}`]: [[github(), 201]], [`PUT ${HOOKS}/gh1`]: [[github({ version: '2.0.0' }), 200]] })
    expect(await uploadAdminHook(new Blob(['\0asm module']))).toEqual(github())
    expect((await uploadAdminHook(new Blob(['\0asm v2']), 'gh1')).version).toBe('2.0.0')
    expect(seen.map((s) => [s.init.method, s.url, s.init.headers])).toEqual([
      ['POST', HOOKS, { 'Content-Type': 'application/wasm', 'Content-Encoding': 'gzip' }],
      ['PUT', `${HOOKS}/gh1`, { 'Content-Type': 'application/wasm', 'Content-Encoding': 'gzip' }],
    ])
    expect(await gunzip(seen[0].init.body)).toBe('\0asm module')
    expect(await gunzip(seen[1].init.body)).toBe('\0asm v2')
  })

  it('turns a hook on or off, and deletes one', async () => {
    const seen = serve({ [`PATCH ${HOOKS}/gh1`]: [[github({ enabled: false }), 200]], [`DELETE ${HOOKS}/gh1`]: [[undefined, 204]] })
    expect((await setAdminHookEnabled('gh1', false)).enabled).toBe(false)
    expect(seen[0].init.body).toBe('{"enabled":false}')
    expect(await deleteAdminHook('gh1')).toBe(true)
  })

  it('answers a signed-out session with null, or false for a delete', async () => {
    const out = [[{ error: 'admin session required' }, 401]]
    serve({
      [`GET ${HOOKS}`]: [...out], [`POST ${HOOKS}`]: [...out],
      [`PATCH ${HOOKS}/gh1`]: [...out], [`DELETE ${HOOKS}/gh1`]: [...out],
    })
    expect(await fetchAdminHooks()).toBeNull()
    expect(await uploadAdminHook(new Blob(['x']))).toBeNull()
    expect(await setAdminHookEnabled('gh1', true)).toBeNull()
    expect(await deleteAdminHook('gh1')).toBe(false)
  })

  it("throws the server's own message, or a fallback", async () => {
    const refused = [[{ error: { code: 409, message: 'the hook "github" is registered' } }, 409]]
    serve({
      [`GET ${HOOKS}`]: [[null, 500]], [`POST ${HOOKS}`]: [[{ error: { message: 'invalid hook: no manifest' } }, 422]],
      [`PATCH ${HOOKS}/gh1`]: [[{}, 500]], [`DELETE ${HOOKS}/gh1`]: refused,
    })
    await expect(fetchAdminHooks()).rejects.toThrow('Failed to fetch hooks')
    await expect(uploadAdminHook(new Blob(['x']))).rejects.toThrow('invalid hook: no manifest')
    await expect(setAdminHookEnabled('gh1', true)).rejects.toThrow('Failed to update the hook')
    await expect(deleteAdminHook('gh1')).rejects.toThrow('the hook "github" is registered')
  })

  it('falls back when the error is not JSON', async () => {
    vi.stubGlobal('fetch', vi.fn(() => Promise.resolve(new Response('oops', { status: 502 }))))
    await expect(uploadAdminHook(new Blob(['x']), 'gh1')).rejects.toThrow('Failed to upload the hook')
  })
})

const q = (el, test) => el.querySelector(`[data-test="${test}"]`)
const qa = (el, test) => [...el.querySelectorAll(`[data-test="${test}"]`)]

async function mount() {
  const el = document.createElement('div')
  document.body.append(el)
  const app = createApp({ render: () => h(AdminHooksView) })
  app.component('RouterLink', RouterLink)
  app.mount(el)
  apps.push(app)
  await settle()
  await nextTick()
  return el
}

async function flush() {
  for (let i = 0; i < 6; i++) await settle()
  await nextTick()
}

function choose(el, contents) {
  const input = q(el, 'hook-file')
  Object.defineProperty(input, 'files', { configurable: true, value: [new Blob([contents])] })
  input.dispatchEvent(new Event('change'))
}

const toastMessages = () => useToasts().toasts.value.map((t) => t.message)

describe('AdminHooksView', () => {
  it('shows each hook with its events, inputs, size and digest', async () => {
    serve({ [`GET ${HOOKS}`]: [[{ hooks: [github(), github({ id: 'g2', name: 'generic', sizeBytes: 900, enabled: false, description: '', events: [{ name: 'request.received' }], inputs: [{ name: 'token', type: 'string', secret: true, required: true }] })] }, 200]] })
    const el = await mount()
    const rows = qa(el, 'hook-row')
    expect(rows).toHaveLength(2)
    expect(q(rows[0], 'hook-facts').textContent.replace(/\s+/g, ' ').trim()).toBe('2 events · 2 inputs · 4.6 MB · sha256 9f2c…a41e')
    expect(rows[0].textContent).toContain('webhookSecret (secret, required), branches')
    expect(rows[0].textContent).toContain('ci.failed')
    expect(q(rows[1], 'hook-facts').textContent.replace(/\s+/g, ' ').trim()).toBe('1 event · 1 input · 1 KB · sha256 9f2c…a41e')
    expect(rows[1].textContent).toContain('Off')
    expect(q(rows[1], 'hook-enabled').getAttribute('aria-checked')).toBe('false')
  })

  it('says so when there is none', async () => {
    serve({ [`GET ${HOOKS}`]: [[{ hooks: [] }, 200]] })
    const el = await mount()
    expect(q(el, 'hook-empty').textContent).toContain('No built-in hooks')
  })

  it('shows a failed read', async () => {
    serve({ [`GET ${HOOKS}`]: [[null, 500]] })
    const el = await mount()
    expect(q(el, 'hook-error').textContent.trim()).toBe('Failed to fetch hooks')
    expect(q(el, 'hook-empty')).not.toBeNull()
  })

  it('sends a signed-out operator to the admin sign-in', async () => {
    serve({ [`GET ${HOOKS}`]: [[{}, 401]] })
    await mount()
    expect(replace).toHaveBeenCalledWith('/admin/login')
  })

  it('adds a hook from the picked file', async () => {
    const seen = serve({
      [`GET ${HOOKS}`]: [[{ hooks: [] }, 200], [{ hooks: [github()] }, 200]],
      [`POST ${HOOKS}`]: [[github(), 201]],
    })
    const el = await mount()
    const click = vi.spyOn(q(el, 'hook-file'), 'click').mockImplementation(() => {})
    q(el, 'hook-add').click()
    expect(click).toHaveBeenCalled()
    choose(el, '\0asm')
    await flush()
    expect(seen.map((s) => `${s.init.method || 'GET'} ${s.url}`)).toEqual([`GET ${HOOKS}`, `POST ${HOOKS}`, `GET ${HOOKS}`])
    expect(qa(el, 'hook-row')).toHaveLength(1)
    expect(toastMessages()).toEqual(['Added github 1.0.0.'])
  })

  it('upgrades a hook, and shows a refused upgrade', async () => {
    serve({
      [`GET ${HOOKS}`]: [[{ hooks: [github()] }, 200], [{ hooks: [github({ version: '2.0.0' })] }, 200]],
      [`PUT ${HOOKS}/gh1`]: [[github({ version: '2.0.0' }), 200], [{ error: { message: 'version 3.0.0 no longer writes the event "push"' } }, 409]],
    })
    const el = await mount()
    vi.spyOn(q(el, 'hook-file'), 'click').mockImplementation(() => {})
    q(el, 'hook-upgrade').click()
    choose(el, 'v2')
    await flush()
    expect(toastMessages()).toEqual(['Upgraded github to 2.0.0.'])
    expect(el.textContent).toContain('2.0.0')

    q(el, 'hook-upgrade').click()
    choose(el, 'v3')
    await flush()
    expect(q(el, 'hook-error').textContent).toContain('no longer writes the event "push"')
  })

  it('ignores a pick of no file', async () => {
    const seen = serve({ [`GET ${HOOKS}`]: [[{ hooks: [] }, 200]] })
    const el = await mount()
    const input = q(el, 'hook-file')
    Object.defineProperty(input, 'files', { value: [] })
    input.dispatchEvent(new Event('change'))
    await flush()
    expect(seen).toHaveLength(1)
  })

  it('turns a hook off', async () => {
    serve({ [`GET ${HOOKS}`]: [[{ hooks: [github(), github({ id: 'g2', name: 'generic' })] }, 200]], [`PATCH ${HOOKS}/gh1`]: [[github({ enabled: false }), 200]] })
    const el = await mount()
    q(el, 'hook-enabled').click()
    await flush()
    expect(qa(el, 'hook-enabled').map((b) => b.getAttribute('aria-checked'))).toEqual(['false', 'true'])
  })

  it('shows a failed switch, leaving the hook as it was', async () => {
    serve({ [`GET ${HOOKS}`]: [[{ hooks: [github()] }, 200]], [`PATCH ${HOOKS}/gh1`]: [[{}, 500]] })
    const el = await mount()
    q(el, 'hook-enabled').click()
    await flush()
    expect(q(el, 'hook-error').textContent.trim()).toBe('Failed to update the hook')
    expect(q(el, 'hook-enabled').getAttribute('aria-checked')).toBe('true')
  })

  it('deletes a hook once confirmed, and shows a refused delete', async () => {
    serve({
      [`GET ${HOOKS}`]: [[{ hooks: [github(), github({ id: 'g2', name: 'generic' })] }, 200]],
      [`DELETE ${HOOKS}/gh1`]: [[{ error: { message: 'the hook "github" is registered; delete its registrations first' } }, 409]],
      [`DELETE ${HOOKS}/g2`]: [[undefined, 204]],
    })
    const el = await mount()
    const confirm = () => [...document.body.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Delete' && !b.dataset.test)

    qa(el, 'hook-delete')[0].click()
    await flush()
    expect(document.body.textContent).toContain('Delete the hook github?')
    confirm().click()
    await flush()
    expect(q(el, 'hook-error').textContent).toContain('is registered')
    expect(qa(el, 'hook-row')).toHaveLength(2)

    qa(el, 'hook-delete')[1].click()
    await flush()
    confirm().click()
    await flush()
    expect(qa(el, 'hook-row').map((r) => r.textContent)).toEqual([expect.stringContaining('github')])
    expect(toastMessages()).toEqual(['Deleted generic.'])
  })

  it('closes the delete dialog on cancel', async () => {
    const seen = serve({ [`GET ${HOOKS}`]: [[{ hooks: [github()] }, 200]] })
    const el = await mount()
    q(el, 'hook-delete').click()
    await flush()
    ;[...document.body.querySelectorAll('button')].find((b) => b.textContent.trim() === 'Cancel').click()
    // The dialog leaves on the next animation frames.
    await new Promise((r) => setTimeout(r, 100))
    expect(document.body.textContent).not.toContain('Delete the hook github?')
    expect(seen).toHaveLength(1)
  })
})
