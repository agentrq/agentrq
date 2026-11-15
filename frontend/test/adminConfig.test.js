// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The operator's config page: the calls behind it, and the page, one
 * vertical tab per config block.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'

const replace = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace, push: vi.fn() }),
  useRoute: () => ({ path: '/admin/config', query: {} }),
}))

const RouterLink = { props: ['to'], setup: (props, { slots }) => () => h('a', { href: props.to }, slots.default?.()) }

import { fetchAdminConfigurations, updateAdminConfiguration } from '../src/api'
import { useToasts } from '../src/composables/useToasts'
import AdminConfigView from '../src/views/AdminConfigView.vue'

const CONFIGS = '/admin/v1/configurations'
// What the server sends for a secret that is set.
const MASK = '••••••••'

const blocks = () => [
  { key: 'smtp', value: { enabled: true, host: 'mail', port: 587, password: MASK, from: null } },
  { key: 'auth', value: { apiJwtSecret: MASK, mcpJwtSecret: MASK, allowedRedirectDomains: ['a.com', 'b.com'], google: { clientId: 'g', clientSecret: '' } } },
  { key: 'webPush', value: { subscriber: 'mailto:x' } },
  { key: 'teams', value: {} },
  { key: 'future', value: { on: true, name: 'x' } },
]

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

describe('the admin config calls', () => {
  it('lists the blocks', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]] })
    expect(await fetchAdminConfigurations()).toEqual(blocks())
  })

  it('saves a block as its value', async () => {
    const seen = serve({ [`PUT ${CONFIGS}/smtp`]: [[{ key: 'smtp', value: { port: 2525 } }, 200]] })
    expect(await updateAdminConfiguration('smtp', { port: 2525 })).toEqual({ key: 'smtp', value: { port: 2525 } })
    expect(seen[0].init).toEqual({ method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: '{"value":{"port":2525}}' })
  })

  it('is null once the admin session has ended', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ error: 'admin session required' }, 401]], [`PUT ${CONFIGS}/smtp`]: [[{ error: 'admin session required' }, 401]] })
    expect(await fetchAdminConfigurations()).toBeNull()
    expect(await updateAdminConfiguration('smtp', {})).toBeNull()
  })

  it("says why, in the server's words", async () => {
    serve({
      [`GET ${CONFIGS}`]: [[null, 500]],
      [`PUT ${CONFIGS}/memory`]: [[{ error: { message: 'memory.maxFileSize: -1 bytes is not between 0 and 131072', code: 422 } }, 422]],
    })
    await expect(fetchAdminConfigurations()).rejects.toThrow('Failed to fetch the config')
    await expect(updateAdminConfiguration('memory', { maxFileSize: -1 })).rejects.toThrow('memory.maxFileSize: -1 bytes is not between 0 and 131072')
  })
})

async function mount() {
  const el = document.createElement('div')
  document.body.append(el)
  const app = createApp({ render: () => h(AdminConfigView) })
  app.component('RouterLink', RouterLink)
  app.mount(el)
  apps.push(app)
  await settle()
  await nextTick()
  return el
}

const q = (el, t) => el.querySelector(`[data-test="${t}"]`)
const qa = (el, t) => [...el.querySelectorAll(`[data-test="${t}"]`)]

async function openTab(el, key) {
  q(el, `config-tab-${key}`)?.click()
  await nextTick()
}

async function type(input, value) {
  input.value = value
  input.dispatchEvent(new Event('input'))
  await nextTick()
}

describe('AdminConfigView', () => {
  it('shows a vertical tab per block, the first open, as a form', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]] })
    const el = await mount()
    const tabs = [...el.querySelectorAll('[role="tab"]')]
    // In the sidebar's categories, a block none names under Other.
    expect(tabs.map((t) => t.textContent.trim())).toEqual(['Auth', 'SMTP', 'Web push', 'Teams', 'Future'])
    expect(qa(el, 'config-group-name').map((g) => g.textContent.trim())).toEqual(['Security', 'Messaging', 'Features', 'Other'])
    expect(el.querySelector('[role="tablist"]').getAttribute('aria-orientation')).toBe('vertical')
    expect(tabs[0].getAttribute('aria-selected')).toBe('true')
    expect([...q(el, 'config-tab-select').options].map((o) => o.value)).toEqual(['auth', 'smtp', 'webPush', 'teams', 'future'])
    expect([...q(el, 'config-tab-select').querySelectorAll('optgroup')].map((g) => g.label)).toEqual(['Security', 'Messaging', 'Features', 'Other'])
    tabs[1].click()
    await nextTick()

    expect(q(el, 'config-smtp.enabled').getAttribute('aria-checked')).toBe('true')
    expect(q(el, 'config-smtp.host').value).toBe('mail')
    expect(q(el, 'config-smtp.port').type).toBe('number')
    expect(q(el, 'config-smtp.password').type).toBe('password')
    expect(q(el, 'config-smtp.from').value).toBe('')
    expect(q(el, 'config-reveal-smtp.host')).toBeNull()

    // A secret is hidden until its eye is pressed, and hidden again after.
    const eye = q(el, 'config-reveal-smtp.password')
    expect(eye.getAttribute('aria-label')).toBe('Show password')
    eye.click()
    await nextTick()
    expect(q(el, 'config-smtp.password').type).toBe('text')
    expect(eye.getAttribute('aria-label')).toBe('Hide password')
    expect(eye.getAttribute('aria-pressed')).toBe('true')
    eye.click()
    await nextTick()
    expect(q(el, 'config-smtp.password').type).toBe('password')
    expect(el.textContent).toContain('Host')
    expect(q(el, 'config-save').disabled).toBe(true)

    tabs[0].click()
    await nextTick()
    expect(q(el, 'config-auth.apiJwtSecret').type).toBe('password')
    expect(q(el, 'config-auth.allowedRedirectDomains').value).toBe('a.com, b.com')
    expect(qa(el, 'config-group')).toHaveLength(1)
    expect(q(el, 'config-auth.google.clientId').value).toBe('g')
    expect(el.textContent).toContain('Allowed redirect domains')

    const select = q(el, 'config-tab-select')
    select.value = 'webPush'
    select.dispatchEvent(new Event('change'))
    await nextTick()
    expect(q(el, 'config-webPush.subscriber').value).toBe('mailto:x')
  })

  it('describes each block, group and setting in a line linked to its field', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]] })
    const el = await mount()
    expect(q(el, 'config-block-help').textContent).toBe('How people sign in to the web app.')
    // A group's line sits under its name.
    expect(q(el, 'config-group').querySelector('[data-test="config-help"]').textContent).toBe('Sign in with a Google account.')

    // A field's line describes it, and its name is its label.
    const input = q(el, 'config-auth.apiJwtSecret')
    const line = document.getElementById(input.getAttribute('aria-describedby'))
    expect(line.textContent).toBe("The key a person's session tokens, terminal and browser tickets and sign-in state are signed with. Required. Changing it signs everyone out.")
    expect(el.querySelector(`label[for="${input.id}"]`).textContent).toContain('Api jwt secret')

    await openTab(el, 'smtp')
    const sw = q(el, 'config-smtp.enabled')
    expect(document.getElementById(sw.getAttribute('aria-describedby')).textContent).toBe('Sends email through the server below. Off, no email is sent.')
    expect(el.querySelector(`label[for="${sw.id}"]`).textContent).toBe('Enabled')

    // A block or setting with no help shows none.
    await openTab(el, 'future')
    expect(q(el, 'config-block-help')).toBeNull()
    expect(q(el, 'config-help')).toBeNull()
    expect(q(el, 'config-future.on').hasAttribute('aria-describedby')).toBe(false)
    expect(q(el, 'config-future.name').hasAttribute('aria-describedby')).toBe(false)
  })

  it('saves an edited block as its own types, then shows it as stored', async () => {
    const saved = { enabled: false, host: 'smtp.example.com', port: 2525, password: MASK, from: '' }
    const seen = serve({
      [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]],
      [`PUT ${CONFIGS}/smtp`]: [[{ key: 'smtp', value: saved }, 200]],
    })
    const el = await mount()
    await openTab(el, 'smtp')
    q(el, 'config-smtp.enabled').click()
    await type(q(el, 'config-smtp.host'), 'smtp.example.com')
    await type(q(el, 'config-smtp.port'), '2525')
    // Unset stays unset until something is typed; emptied, it is empty.
    await type(q(el, 'config-smtp.from'), '')
    await type(q(el, 'config-smtp.from'), 'x')
    await type(q(el, 'config-smtp.from'), '')
    expect(qa(el, 'config-dirty')).toHaveLength(1)
    expect(q(el, 'config-tab-select').options[1].textContent).toBe('SMTP •')

    q(el, 'config-save').click()
    await settle()
    await nextTick()
    expect(JSON.parse(seen[1].init.body)).toEqual({ value: saved })
    expect(qa(el, 'config-dirty')).toHaveLength(0)
    expect(useToasts().toasts.value.map((t) => t.message)).toEqual(['Saved SMTP. It takes effect when the server restarts.'])
  })

  it('reads lists and numbers back, and resets an edit', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]] })
    const el = await mount()
    await openTab(el, 'smtp')
    await type(q(el, 'config-smtp.port'), '')
    expect(q(el, 'config-smtp.port').value).toBe('0')
    q(el, 'config-reset').click()
    await nextTick()
    expect(q(el, 'config-smtp.port').value).toBe('587')
    expect(q(el, 'config-save').disabled).toBe(true)

    el.querySelector('[data-test="config-tab-auth"]').click()
    await nextTick()
    await type(q(el, 'config-auth.allowedRedirectDomains'), 'x.com, , y.com')
    expect(q(el, 'config-auth.allowedRedirectDomains').value).toBe('x.com, y.com')
  })

  it('never shows a stored secret, and keeps it unless a new one is typed', async () => {
    const seen = serve({
      [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]],
      [`PUT ${CONFIGS}/smtp`]: [[{ key: 'smtp', value: blocks()[0].value }, 200]],
    })
    const el = await mount()
    await openTab(el, 'smtp')
    const input = q(el, 'config-smtp.password')
    expect(input.value).toBe('')
    expect(input.placeholder).toBe('Set. Type a new value to replace it')
    q(el, 'config-reveal-smtp.password').click()
    await nextTick()
    expect(input.value).toBe('')

    // Typed and emptied again, it is still the stored secret: nothing to save.
    await type(input, 'x')
    expect(qa(el, 'config-dirty')).toHaveLength(1)
    await type(input, '')
    expect(qa(el, 'config-dirty')).toHaveLength(0)

    await type(input, 'n3w$%#!')
    expect(input.value).toBe('n3w$%#!')
    q(el, 'config-save').click()
    await settle()
    expect(JSON.parse(seen[1].init.body).value.password).toBe('n3w$%#!')
    await nextTick()
    expect(input.value).toBe('')

    // An unset secret is an empty field, with nothing to keep.
    await openTab(el, 'auth')
    expect(q(el, 'config-auth.google.clientSecret').placeholder).toBe('')
    expect(q(el, 'config-auth.apiJwtSecret').value).toBe('')
  })

  it('generates a random JWT secret, shown, and saves it', async () => {
    const seen = serve({
      [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]],
      [`PUT ${CONFIGS}/auth`]: [[{ key: 'auth', value: blocks()[1].value }, 200]],
    })
    const el = await mount()
    // Only a key the server signs with; a provider's secret is not made up.
    expect(q(el, 'config-generate-auth.google.clientSecret')).toBeNull()
    await openTab(el, 'smtp')
    expect(q(el, 'config-generate-smtp.password')).toBeNull()
    expect(q(el, 'config-generate-smtp.host')).toBeNull()
    await openTab(el, 'auth')

    const button = q(el, 'config-generate-auth.mcpJwtSecret')
    expect(button.getAttribute('aria-label')).toBe('Generate mcp jwt secret')
    button.click()
    await nextTick()
    const input = q(el, 'config-auth.mcpJwtSecret')
    expect(input.type).toBe('text')
    expect(input.value).toMatch(/^[A-Za-z0-9_-]{64}$/)
    const first = input.value
    button.click()
    await nextTick()
    expect(input.value).not.toBe(first)
    expect(qa(el, 'config-dirty')).toHaveLength(1)
    const generated = input.value

    q(el, 'config-save').click()
    await settle()
    const sent = JSON.parse(seen[1].init.body).value
    expect(sent.mcpJwtSecret).toBe(generated)
    expect(sent.apiJwtSecret).toBe(MASK)
  })

  it("shows a refused save's reason and keeps the edit", async () => {
    serve({
      [`GET ${CONFIGS}`]: [[{ configurations: blocks() }, 200]],
      [`PUT ${CONFIGS}/smtp`]: [[{ error: { message: 'smtp: port must be a number', code: 422 } }, 422]],
    })
    const el = await mount()
    await openTab(el, 'smtp')
    await type(q(el, 'config-smtp.host'), 'other')
    q(el, 'config-save').click()
    await settle()
    await nextTick()
    expect(q(el, 'config-error').textContent.trim()).toBe('smtp: port must be a number')
    expect(q(el, 'config-smtp.host').value).toBe('other')
  })

  it('goes to the sign-in once the admin session has ended', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ error: 'admin session required' }, 401]] })
    await mount()
    expect(replace).toHaveBeenCalledWith('/admin/login')
  })

  it('says why the config could not be read', async () => {
    serve({ [`GET ${CONFIGS}`]: [[null, 500]] })
    const el = await mount()
    await openTab(el, 'smtp')
    expect(q(el, 'config-error').textContent.trim()).toBe('Failed to fetch the config')
    expect(q(el, 'config-panel')).toBeNull()
  })

  it('shows nothing to edit with no blocks', async () => {
    serve({ [`GET ${CONFIGS}`]: [[{ configurations: [] }, 200]] })
    const el = await mount()
    await openTab(el, 'smtp')
    expect(el.querySelector('[role="tablist"]')).toBeNull()
  })
})
