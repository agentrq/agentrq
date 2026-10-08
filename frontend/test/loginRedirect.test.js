// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The login page an MCP client's authorize step sends a signed-out person to:
 * every sign-in it offers returns them to the consent page, and nothing but
 * this origin is ever a return address.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h } from 'vue'
import { loginRedirect, providerLoginUrl } from '../src/utils/loginRedirect'

const origin = 'https://app.agentrq.com'
const authorize = `${origin}/mcp/oauth2/authorize?client_id=a&state=b`

describe('loginRedirect', () => {
  it('keeps an address on this origin', () => {
    expect(loginRedirect(authorize, origin)).toBe(authorize)
    expect(loginRedirect('/workspaces/a', origin)).toBe(`${origin}/workspaces/a`)
  })

  it.each([
    ['another site', 'https://evil.example/steal'],
    ['a protocol-relative address', '//evil.example/steal'],
    ['a backslash trick', '/\\evil.example/steal'],
    ['another scheme', 'javascript:alert(1)'],
    ['the same host over http', 'http://app.agentrq.com/x'],
  ])('refuses %s', (_, raw) => {
    expect(loginRedirect(raw, origin)).toBeNull()
  })

  it.each([
    ['nothing', undefined],
    ['an empty value', ''],
    ['a repeated parameter', ['/a', '/b']],
    ['an address that does not parse', 'http://[bad'],
  ])('is null for %s', (_, raw) => {
    expect(loginRedirect(raw, origin)).toBeNull()
  })
})

describe('providerLoginUrl', () => {
  it('carries the return address, encoded', () => {
    expect(providerLoginUrl('/api/v1/auth/github/login', authorize))
      .toBe(`/api/v1/auth/github/login?redirect_url=${encodeURIComponent(authorize)}`)
  })

  it('is the bare route without one', () => {
    expect(providerLoginUrl('/api/v1/auth/github/login', null)).toBe('/api/v1/auth/github/login')
  })
})

const route = { query: {} }
const push = vi.fn()
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push }) }))

const { default: LoginView } = await import('../src/views/LoginView.vue')

const settle = () => new Promise((r) => setTimeout(r, 0))
let app

async function mount({ query = {}, config = {}, rootLogin = { ok: true } } = {}) {
  route.query = query
  vi.stubGlobal('fetch', vi.fn((url) => Promise.resolve(
    url.endsWith('/auth/config')
      ? { json: () => Promise.resolve(config) }
      : { ok: rootLogin.ok, json: () => Promise.resolve({}) },
  )))
  const el = document.createElement('div')
  document.body.appendChild(el)
  app = createApp({ render: () => h(LoginView) })
  app.mount(el)
  await settle()
  return el
}

const links = (el) => [...el.querySelectorAll('a[href*="/auth/"]')].map((a) => a.getAttribute('href'))

async function signInWithToken(el) {
  const input = el.querySelector('input[type=password]')
  input.value = 'root-token'
  input.dispatchEvent(new Event('input'))
  el.querySelector('form').dispatchEvent(new Event('submit'))
  await settle()
}

beforeEach(() => {
  document.body.innerHTML = ''
  vi.clearAllMocks()
})
afterEach(() => {
  app?.unmount()
  vi.unstubAllGlobals()
})

describe('the login page', () => {
  const back = `${window.location.origin}/mcp/oauth2/authorize?client_id=a`

  it('sends Google and GitHub back to the authorize step', async () => {
    const el = await mount({ query: { redirect_url: back }, config: { githubLoginEnabled: true } })
    const encoded = encodeURIComponent(back)
    expect(links(el)).toEqual([
      `/api/v1/auth/google/login?redirect_url=${encoded}`,
      `/api/v1/auth/github/login?redirect_url=${encoded}`,
    ])
  })

  it('drops a return address on another site', async () => {
    const el = await mount({ query: { redirect_url: 'https://evil.example/' }, config: { githubLoginEnabled: true } })
    expect(links(el)).toEqual(['/api/v1/auth/google/login', '/api/v1/auth/github/login'])
  })

  it('returns a root-token sign-in to the authorize step', async () => {
    const assign = vi.fn()
    vi.stubGlobal('location', { origin: window.location.origin, assign })
    const el = await mount({ query: { redirect_url: back }, config: { rootLoginEnabled: true } })
    await signInWithToken(el)
    expect(assign).toHaveBeenCalledWith(back)
    expect(push).not.toHaveBeenCalled()
  })

  it('opens the app after a root-token sign-in with nowhere to return to', async () => {
    const el = await mount({ config: { rootLoginEnabled: true } })
    await signInWithToken(el)
    expect(push).toHaveBeenCalledWith('/')
  })
})
