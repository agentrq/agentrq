// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The operator's pages: the admin sign-in with the root access token, the
 * calls behind it, the state that re-reads the figures, and both pages mounted.
 */

import { describe, it, expect, vi, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'

const replace = vi.fn()
const push = vi.fn()
vi.mock('vue-router', () => ({
  useRouter: () => ({ replace, push }),
  useRoute: () => ({ path: '/admin/stats', query: {} }),
}))

// The sidebar's links, as plain anchors.
const RouterLink = { props: ['to'], setup: (props, { slots, attrs }) => () => h('a', { href: props.to, onClick: (e) => { e.preventDefault(); attrs.onClick?.(e) } }, slots.default?.()) }

import { adminLogin, adminLogout, fetchAdminStats } from '../src/api'
import { useAdminStats } from '../src/composables/useAdminStats'
import LoginView from '../src/views/LoginView.vue'
import AdminStatsView from '../src/views/AdminStatsView.vue'

const SOME = {
  mcpConnections: 4,
  machines: 2,
  users: [
    { userId: 'u1', mcpConnections: 3, machines: 1 },
    { userId: 'u2', mcpConnections: 1, machines: 1 },
  ],
}

// Answers by path.
function serve(routes) {
  const seen = []
  vi.stubGlobal('fetch', vi.fn((url, init) => {
    seen.push({ url, init })
    const [body, status, headers] = routes[url] ?? [{ error: 'not found' }, 404]
    return Promise.resolve(new Response(JSON.stringify(body), { status, headers }))
  }))
  return seen
}

const STATS = '/admin/v1/stats'
const LOGIN = '/auth/admin/login'
const LOGOUT = '/auth/admin/logout'

const settle = () => new Promise((r) => setTimeout(r, 0))
const apps = []

afterEach(() => {
  apps.splice(0).forEach((app) => app.unmount())
  document.body.innerHTML = ''
  vi.unstubAllGlobals()
  vi.useRealTimers()
  replace.mockClear()
  push.mockClear()
})

// Relative, so they stay same-origin, and apart from the users' /api/v1.
describe('the admin calls', () => {
  it('signs in with the root access token', async () => {
    const seen = serve({ [LOGIN]: [{ token: 'jwt' }, 200] })
    await adminLogin('s3cret')
    expect(seen).toEqual([{ url: LOGIN, init: { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: '{"rootToken":"s3cret"}' } }])
  })

  it('says the token is wrong on a 401, and fails on anything else', async () => {
    serve({ [LOGIN]: [{ error: 'invalid root token' }, 401] })
    await expect(adminLogin('guess')).rejects.toThrow('Invalid root access token')
    serve({ [LOGIN]: [{ error: 'invalid payload' }, 400] })
    await expect(adminLogin('s3cret')).rejects.toThrow('Failed to sign in')
  })

  it('says how long to wait once the attempt is refused', async () => {
    serve({ [LOGIN]: [{ error: 'too many attempts' }, 429, { 'Retry-After': '540' }] })
    await expect(adminLogin('s3cret')).rejects.toThrow('Too many attempts. Try again in 9 minutes.')
    serve({ [LOGIN]: [{ error: 'too many attempts' }, 429, { 'Retry-After': '30' }] })
    await expect(adminLogin('s3cret')).rejects.toThrow('Try again in 1 minute.')
    serve({ [LOGIN]: [{ error: 'too many attempts' }, 429] })
    await expect(adminLogin('s3cret')).rejects.toThrow('Try again in 10 minutes.')
  })

  it('signs out', async () => {
    const seen = serve({ [LOGOUT]: [{ status: 'ok' }, 200] })
    await adminLogout()
    expect(seen).toEqual([{ url: LOGOUT, init: { method: 'POST' } }])
  })

  it('reads the stats, null once signed out, and fails on anything else', async () => {
    const seen = serve({ [STATS]: [SOME, 200] })
    expect(await fetchAdminStats()).toEqual(SOME)
    expect(seen[0].url).toBe(STATS)
    serve({ [STATS]: [{ error: 'admin session required' }, 401] })
    expect(await fetchAdminStats()).toBeNull()
    serve({ [STATS]: [{ error: 'boom' }, 500] })
    await expect(fetchAdminStats()).rejects.toThrow('Failed to fetch admin stats')
  })
})

describe('useAdminStats', () => {
  it('reads at once and on an interval until stopped', async () => {
    vi.useFakeTimers()
    const fetchFn = vi.fn().mockResolvedValue(SOME)
    const s = useAdminStats({ fetchFn, intervalMs: 1000 })
    await s.start()
    expect(s.stats.value).toEqual(SOME)
    expect(s.updatedAt.value).toBeInstanceOf(Date)
    expect(s.loading.value).toBe(false)

    await vi.advanceTimersByTimeAsync(2000)
    expect(fetchFn).toHaveBeenCalledTimes(3)
    s.stop()
    await vi.advanceTimersByTimeAsync(2000)
    expect(fetchFn).toHaveBeenCalledTimes(3)
  })

  it('stops and says so once the admin session has ended', async () => {
    vi.useFakeTimers()
    const onSignedOut = vi.fn()
    const fetchFn = vi.fn().mockResolvedValue(null)
    const s = useAdminStats({ fetchFn, intervalMs: 1000, onSignedOut })
    await s.start()
    expect(onSignedOut).toHaveBeenCalledTimes(1)
    await vi.advanceTimersByTimeAsync(3000)
    expect(fetchFn).toHaveBeenCalledTimes(1)
  })

  it('keeps the last figures on a failed read, and clears the error on the next good one', async () => {
    const fetchFn = vi.fn().mockResolvedValueOnce(SOME).mockRejectedValueOnce(new Error('Failed to fetch admin stats')).mockResolvedValueOnce(SOME)
    const s = useAdminStats({ fetchFn })
    await s.start()
    await s.refresh()
    expect(s.stats.value).toEqual(SOME)
    expect(s.error.value).toBe('Failed to fetch admin stats')
    await s.refresh()
    expect(s.error.value).toBe('')
    s.stop()
  })

  it('uses the API call by default, and a signed-out read needs no handler', async () => {
    const seen = serve({ [STATS]: [{ error: 'admin session required' }, 401] })
    const s = useAdminStats()
    await s.start()
    expect(seen[0].url).toBe(STATS)
    expect(s.stats.value).toBeNull()
  })
})

const q = (el, test) => el.querySelector(`[data-test="${test}"]`)

async function mount(component, props = {}) {
  const el = document.createElement('div')
  document.body.append(el)
  const app = createApp({ render: () => h(component, props) })
  app.component('RouterLink', RouterLink)
  app.mount(el)
  apps.push(app)
  await settle()
  await nextTick()
  return el
}

describe('LoginView in admin mode', () => {
  // The public totals stream: nothing to follow in a test.
  function noStream() {
    vi.stubGlobal('EventSource', class { close() {} })
  }

  async function submit(el, token) {
    const input = el.querySelector('input[type="password"]')
    input.value = token
    input.dispatchEvent(new Event('input'))
    el.querySelector('form').dispatchEvent(new Event('submit'))
    await settle()
    await nextTick()
  }

  it('offers the root token alone, even where root sign-in is off for users', async () => {
    noStream()
    serve({ '/api/v1/auth/config': [{ rootLoginEnabled: false, githubLoginEnabled: true }, 200] })
    const el = await mount(LoginView, { admin: true })
    expect(el.textContent).toContain('Admin sign in')
    expect(el.querySelector('form')).not.toBeNull()
    expect(el.querySelector('a[href*="/auth/google/login"]')).toBeNull()
    expect(el.querySelector('a[href*="/auth/github/login"]')).toBeNull()
  })

  it('signs in and opens the stats', async () => {
    noStream()
    const seen = serve({ '/api/v1/auth/config': [{}, 200], [LOGIN]: [{ token: 'jwt' }, 200] })
    const el = await mount(LoginView, { admin: true })
    await submit(el, '  s3cret ')
    expect(seen.find((r) => r.url === LOGIN).init.body).toBe('{"rootToken":"s3cret"}')
    expect(push).toHaveBeenCalledWith('/admin/stats')
  })

  it('shows a refusal and stays', async () => {
    noStream()
    serve({ '/api/v1/auth/config': [{}, 200], [LOGIN]: [{ error: 'invalid root token' }, 401] })
    const el = await mount(LoginView, { admin: true })
    await submit(el, 'guess')
    expect(el.textContent).toContain('Invalid root access token')
    expect(push).not.toHaveBeenCalled()
  })
})

describe('AdminStatsView', () => {
  it('shows the totals and one row per user, busiest first', async () => {
    serve({ [STATS]: [SOME, 200] })
    const el = await mount(AdminStatsView)
    const totals = [...el.querySelectorAll('[data-test="admin-total"]')].map((c) => [...c.children].map((p) => p.textContent.trim()).join(' '))
    expect(totals).toEqual(['Connections 4', 'Machines 2'])
    const rows = [...el.querySelectorAll('[data-test="admin-row"]')].map((r) => [...r.children].map((c) => c.textContent.trim()))
    expect(rows).toEqual([['u1', '3', '1', ''], ['u2', '1', '1', '']])
    const shares = [...el.querySelectorAll('[data-test="admin-row"] [style]')].map((s) => s.style.width)
    expect(shares).toEqual(['67%', '33%'])
    expect(q(el, 'admin-updated').textContent).toMatch(/^Updated /)
  })

  it('says so when nothing is connected', async () => {
    serve({ [STATS]: [{ mcpConnections: 0, machines: 0, users: [] }, 200] })
    const el = await mount(AdminStatsView)
    expect(q(el, 'admin-empty').textContent.trim()).toBe('No activity')
  })

  it('sends a signed-out operator to the admin sign-in', async () => {
    serve({ [STATS]: [{ error: 'admin session required' }, 401] })
    const el = await mount(AdminStatsView)
    expect(replace).toHaveBeenCalledWith('/admin/login')
    expect(q(el, 'admin-total')).toBeNull()
  })

  it('shows a failed read', async () => {
    serve({ [STATS]: [{ error: 'boom' }, 500] })
    const el = await mount(AdminStatsView)
    expect(q(el, 'admin-error').textContent).toBe('Failed to fetch admin stats')
  })

  it('links its own pages, refreshes on demand, and signs out', async () => {
    const seen = serve({ [STATS]: [SOME, 200], [LOGOUT]: [{ status: 'ok' }, 200] })
    const el = await mount(AdminStatsView)
    expect(el.querySelector('a[href="/admin/stats"]').className).toContain('bg-gray-200')
    q(el, 'admin-refresh').dispatchEvent(new MouseEvent('click'))
    await settle()
    expect(seen.filter((r) => r.url === STATS)).toHaveLength(2)

    q(el, 'admin-logout').dispatchEvent(new MouseEvent('click'))
    await settle()
    expect(seen.at(-1).url).toBe(LOGOUT)
    expect(replace).toHaveBeenCalledWith('/admin/login')
  })

  it('opens the sidebar from the Menu button on a phone, and closes it on the overlay or a link', async () => {
    serve({ [STATS]: [SOME, 200] })
    const el = await mount(AdminStatsView)
    const nav = el.querySelector('nav')
    const open = async () => {
      q(el, 'admin-menu').dispatchEvent(new MouseEvent('click'))
      await nextTick()
      expect(nav.classList).toContain('flex')
    }
    expect(nav.classList).toContain('hidden')
    await open()
    q(el, 'admin-menu-overlay').dispatchEvent(new MouseEvent('click'))
    await nextTick()
    expect(nav.classList).toContain('hidden')
    expect(q(el, 'admin-menu-overlay')).toBeNull()

    await open()
    el.querySelector('a[href="/admin/stats"]').dispatchEvent(new MouseEvent('click', { bubbles: true, cancelable: true }))
    await nextTick()
    expect(nav.classList).toContain('hidden')
  })
})
