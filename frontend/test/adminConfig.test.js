// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

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

import { fetchAdminConfigurations, updateAdminConfiguration, uploadAdminLoginIcon } from '../src/api'
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

  it('generates a 32-character Prometheus token, under Monitoring, and saves it', async () => {
    const prometheus = { key: 'prometheus', value: { enabled: false, token: '' } }
    const seen = serve({
      [`GET ${CONFIGS}`]: [[{ configurations: [...blocks(), prometheus] }, 200]],
      [`PUT ${CONFIGS}/prometheus`]: [[prometheus, 200]],
    })
    const el = await mount()
    expect(qa(el, 'config-group-name').map((g) => g.textContent.trim())).toContain('Monitoring')
    await openTab(el, 'prometheus')
    expect(q(el, 'config-tab-prometheus').textContent.trim()).toBe('Prometheus')

    q(el, 'config-generate-prometheus.token').click()
    await nextTick()
    const input = q(el, 'config-prometheus.token')
    expect(input.type).toBe('text')
    expect(input.value).toMatch(/^[A-Za-z0-9_-]{32}$/)
    const generated = input.value

    q(el, 'config-save').click()
    await settle()
    expect(JSON.parse(seen[1].init.body).value).toEqual({ enabled: false, token: generated })
  })

  it('lists both Sentry blocks under Monitoring, each with its switch before the DSN', async () => {
    const sentry = [
      { key: 'sentry', value: { dsn: '', enabled: false } },
      { key: 'sentryFrontend', value: { dsn: '', enabled: false } },
    ]
    serve({ [`GET ${CONFIGS}`]: [[{ configurations: [...blocks(), ...sentry] }, 200]] })
    const el = await mount()
    const tabs = [...el.querySelectorAll('[role="tab"]')].map((t) => t.textContent.trim())
    expect(tabs.slice(-3)).toEqual(['Sentry', 'Sentry (frontend)', 'Future'])
    expect(qa(el, 'config-group-name').map((g) => g.textContent.trim()).slice(-2)).toEqual(['Monitoring', 'Other'])
    for (const key of ['sentry', 'sentryFrontend']) {
      await openTab(el, key)
      const ids = [...el.querySelectorAll(`[data-test^="config-${key}."]`)].map((e) => e.dataset.test)
      expect(ids).toEqual([`config-${key}.enabled`, `config-${key}.dsn`])
    }
  })

  it('lists Telemetry under Monitoring, its two switches side by side, and saves one turned off', async () => {
    const seen = serve({
      [`GET ${CONFIGS}`]: [[{ configurations: [...blocks(), { key: 'telemetry', value: { database: true, prometheus: true } }] }, 200]],
      [`PUT ${CONFIGS}/telemetry`]: [[{ key: 'telemetry', value: { database: false, prometheus: true } }, 200]],
    })
    const el = await mount()
    expect(qa(el, 'config-group-name').map((g) => g.textContent.trim()).slice(-2)).toEqual(['Monitoring', 'Other'])
    expect(q(el, 'config-side-by-side')).toBeNull()
    await openTab(el, 'telemetry')
    const cards = q(el, 'config-side-by-side')
    expect([...cards.querySelectorAll('[role="switch"]')].map((s) => s.dataset.test)).toEqual(['config-telemetry.database', 'config-telemetry.prometheus'])
    expect(qa(el, 'config-help').map((p) => p.textContent).join(' ')).toContain('never an account or workspace')
    q(el, 'config-telemetry.database').click()
    await nextTick()
    q(el, 'config-save').click()
    await settle()
    expect(JSON.parse(seen.at(-1).init.body)).toEqual({ value: { database: false, prometheus: true } })
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

const ICONS = '/admin/v1/login-icons'

describe('the login icon upload', () => {
  it('sends the file as it is and returns its link', async () => {
    const seen = serve({ [`POST ${ICONS}`]: [[{ url: 'https://cdn.example/login-icons/abc' }, 201]] })
    const file = new File(['png'], 'icon.png', { type: 'image/png' })
    expect(await uploadAdminLoginIcon(file)).toBe('https://cdn.example/login-icons/abc')
    expect(seen[0].init.headers).toEqual({ 'Content-Type': 'image/png' })
    expect(seen[0].init.body).toBe(file)
  })

  it('is null once the admin session has ended, and says why it was refused', async () => {
    serve({ [`POST ${ICONS}`]: [[{ error: 'admin session required' }, 401], [{ error: { message: 'the icon must be a PNG', code: 422 } }, 422]] })
    const typeless = new File(['x'], 'icon')
    expect(await uploadAdminLoginIcon(typeless)).toBeNull()
    await expect(uploadAdminLoginIcon(typeless)).rejects.toThrow('the icon must be a PNG')
  })
})

describe('AdminConfigView sign-in providers', () => {
  const provider = { id: 'gitlab', name: 'GitLab', clientId: 'cid', clientSecret: MASK, authUrl: 'https://gl/a', tokenUrl: 'https://gl/t', userInfoUrl: 'https://gl/u', scopes: ['openid'], iconUrl: '' }
  const withProviders = (providers) => [
    { key: 'app', value: { baseUrl: 'https://app.example/' } },
    { key: 'auth', value: { allowedRedirectDomains: [], apiJwtSecret: MASK, github: { clientId: '' }, mcpJwtSecret: MASK, oauth2Providers: providers } },
  ]

  async function openAuth(replies) {
    const seen = serve(replies)
    const el = await mount()
    await openTab(el, 'auth')
    return { el, seen }
  }

  it('shows the MCP JWT secret right after the API one', async () => {
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: withProviders([]) }, 200]] })
    const ids = [...el.querySelectorAll('[data-test^="config-auth."]')].map((e) => e.dataset.test)
    expect(ids.slice(0, 3)).toEqual(['config-auth.allowedRedirectDomains', 'config-auth.apiJwtSecret', 'config-auth.mcpJwtSecret'])
  })

  it('lists the providers, each with its callback URL, and adds and removes them', async () => {
    const { el, seen } = await openAuth({
      [`GET ${CONFIGS}`]: [[{ configurations: withProviders([provider]) }, 200]],
      [`PUT ${CONFIGS}/auth`]: [[{ key: 'auth', value: {} }, 200]],
    })
    expect(qa(el, 'config-list-item')).toHaveLength(1)
    expect(q(el, 'config-auth.oauth2Providers.0.clientSecret').type).toBe('password')
    expect(q(el, 'config-auth.oauth2Providers.0.clientSecret').placeholder).toContain('Set.')
    // A link is never a secret, even when its name says token.
    expect(q(el, 'config-auth.oauth2Providers.0.tokenUrl').type).toBe('text')
    expect(q(el, 'config-reveal-auth.oauth2Providers.0.tokenUrl')).toBeNull()
    expect(q(el, 'config-list').querySelector('[data-test="config-help"]').textContent).toContain('OpenID Connect')
    const idInput = q(el, 'config-auth.oauth2Providers.0.id')
    expect(document.getElementById(idInput.getAttribute('aria-describedby')).textContent).toContain('lowercase letters')
    expect(q(q(el, 'config-list-item'), 'config-callback-url').querySelector('code').textContent).toBe('https://app.example/api/v1/auth/oauth2/gitlab/callback')

    q(el, 'config-list-add').click()
    await nextTick()
    const items = qa(el, 'config-list-item')
    expect(items).toHaveLength(2)
    expect(items[1].textContent).toContain('New provider')
    expect(q(items[1], 'config-callback-url').querySelector('code').textContent).toBe('https://app.example/api/v1/auth/oauth2/<id>/callback')
    expect(q(items[1], 'config-callback-copy').disabled).toBe(true)
    expect(q(el, 'config-auth.oauth2Providers.1.scopes').value).toBe('openid, email, profile')
    expect(q(el, 'config-auth.oauth2Providers.1.clientSecret').placeholder).toBe('')
    await type(q(el, 'config-auth.oauth2Providers.1.id'), 'okta')
    await type(q(el, 'config-auth.oauth2Providers.1.name'), 'Okta')
    expect(qa(el, 'config-list-item')[1].textContent).toContain('Okta')
    expect(q(qa(el, 'config-list-item')[1], 'config-callback-url').querySelector('code').textContent).toBe('https://app.example/api/v1/auth/oauth2/okta/callback')

    qa(el, 'config-list-remove')[0].click()
    await nextTick()
    expect(qa(el, 'config-list-item')).toHaveLength(1)
    q(el, 'config-save').click()
    await settle()
    const sent = JSON.parse(seen.find((s) => s.init.method === 'PUT').init.body).value.oauth2Providers
    expect(sent).toEqual([{ id: 'okta', name: 'Okta', clientId: '', clientSecret: '', authUrl: '', tokenUrl: '', userInfoUrl: '', scopes: ['openid', 'email', 'profile'], iconUrl: '' }])
  })

  it('copies a callback URL, or says it could not', async () => {
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: withProviders([provider]) }, 200]] })
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const { toasts } = useToasts()
    q(q(el, 'config-list-item'), 'config-callback-copy').click()
    await settle()
    expect(writeText).toHaveBeenCalledWith('https://app.example/api/v1/auth/oauth2/gitlab/callback')
    expect(toasts.value.at(-1).message).toBe('Copied the callback URL.')

    vi.stubGlobal('navigator', { clipboard: { writeText: () => Promise.reject(new Error('denied')) } })
    q(q(el, 'config-list-item'), 'config-callback-copy').click()
    await settle()
    expect(toasts.value.at(-1).message).toBe('Could not copy. Select the URL and copy it.')
  })

  it('starts a callback URL with the page origin before the app block is known', async () => {
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: withProviders([provider]).slice(1) }, 200]] })
    expect(q(q(el, 'config-list-item'), 'config-callback-url').querySelector('code').textContent).toBe(`${window.location.origin}/api/v1/auth/oauth2/gitlab/callback`)
  })

  it('shows the Google and GitHub callback URLs in their groups', async () => {
    const configs = withProviders([])
    configs[1].value.google = { clientId: '', clientSecret: '' }
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: configs }, 200]] })
    const shown = qa(el, 'config-group').map((g) => [g.querySelector('legend').textContent, q(g, 'config-callback-url').querySelector('code').textContent])
    expect(shown).toEqual([
      [expect.stringContaining('auth.github'), 'https://app.example/api/v1/auth/github/callback'],
      [expect.stringContaining('auth.google'), 'https://app.example/api/v1/auth/google/callback'],
    ])
    expect(qa(el, 'config-callback-copy').every((b) => !b.disabled)).toBe(true)
  })

  it("shows the GitHub App's callback URL, under Features", async () => {
    const configs = [withProviders([])[0], { key: 'githubApp', value: { enabled: false, appId: '', slug: '', clientId: '', clientSecret: MASK, privateKey: MASK } }]
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: configs }, 200]] })
    await openTab(el, 'githubApp')
    const urls = qa(el, 'config-callback-url')
    expect(urls.map((u) => [u.querySelector('p').textContent, u.querySelector('code').textContent])).toEqual([
      ['Callback URL', 'https://app.example/github/callback'],
    ])
    expect(el.textContent).toContain('GitHub App')
  })

  it("shows the Slack app's redirect and request URLs, and copies one", async () => {
    const configs = [withProviders([])[0], { key: 'slack', value: { appId: '', clientId: '', clientSecret: MASK, enabled: false, signingSecret: MASK } }]
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: configs }, 200]] })
    await openTab(el, 'slack')
    const urls = qa(el, 'config-callback-url')
    expect(urls.map((u) => [u.querySelector('p').textContent, u.querySelector('code').textContent])).toEqual([
      ['Redirect URL', 'https://app.example/slack/oauth/callback'],
      ['Events request URL', 'https://app.example/slack/events'],
      ['Interactivity request URL', 'https://app.example/slack/interactions'],
      ['Slash command request URL', 'https://app.example/slack/commands'],
    ])
    const writeText = vi.fn(() => Promise.resolve())
    vi.stubGlobal('navigator', { clipboard: { writeText } })
    const { toasts } = useToasts()
    q(urls[1], 'config-callback-copy').click()
    await settle()
    expect(writeText).toHaveBeenCalledWith('https://app.example/slack/events')
    expect(toasts.value.at(-1).message).toBe('Copied the events request URL.')
  })

  it('shows no URLs on a block that registers with no provider', async () => {
    const configs = [withProviders([])[0], { key: 'smtp', value: { host: '' } }]
    const { el } = await openAuth({ [`GET ${CONFIGS}`]: [[{ configurations: configs }, 200]] })
    await openTab(el, 'smtp')
    expect(q(el, 'config-urls')).toBeNull()
  })

  it('uploads an icon into the provider, or says why not', async () => {
    const { el } = await openAuth({
      [`GET ${CONFIGS}`]: [[{ configurations: withProviders([provider]) }, 200]],
      [`POST ${ICONS}`]: [
        [{ url: 'https://cdn.example/login-icons/abc' }, 201],
        [{ error: { message: 'the icon must be a PNG', code: 422 } }, 422],
        [{ error: 'admin session required' }, 401],
      ],
    })
    expect(q(el, 'config-icon-preview')).toBeNull()
    const input = q(el, 'config-upload-auth.oauth2Providers.0.iconUrl').querySelector('input[type=file]')
    const pick = async (files) => {
      Object.defineProperty(input, 'files', { value: files, configurable: true })
      input.dispatchEvent(new Event('change'))
      await settle()
      await nextTick()
    }

    await pick([])
    expect(q(el, 'config-auth.oauth2Providers.0.iconUrl').value).toBe('')

    await pick([new File(['png'], 'i.png', { type: 'image/png' })])
    expect(q(el, 'config-auth.oauth2Providers.0.iconUrl').value).toBe('https://cdn.example/login-icons/abc')
    expect(q(el, 'config-icon-preview').getAttribute('src')).toBe('https://cdn.example/login-icons/abc')
    expect(q(el, 'config-upload-error')).toBeNull()

    await pick([new File(['x'], 'i.svg', { type: 'image/svg+xml' })])
    expect(q(el, 'config-upload-error').textContent).toBe('the icon must be a PNG')

    await pick([new File(['x'], 'i.png', { type: 'image/png' })])
    expect(q(el, 'config-upload-error').textContent).toBe('The admin session has ended. Sign in again to upload.')
    expect(q(el, 'config-auth.oauth2Providers.0.iconUrl').value).toBe('https://cdn.example/login-icons/abc')
  })
})
