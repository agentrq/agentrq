import { describe, it, expect, vi } from 'vitest'
import { EventEmitter } from 'node:events'

import {
  OAUTH_PROVIDERS,
  AUTH_COOKIE,
  matchOAuthLogin,
  isOAuthReturn,
  oauthStartUrl,
  runOAuthFlow,
} from '../src/main/auth.js'

const SERVER = 'https://agentrq.example.com'

/** Stand-in for the OAuth BrowserWindow: only what runOAuthFlow touches. */
function fakeWindow() {
  const win = new EventEmitter()
  win.webContents = new EventEmitter()
  win.destroyed = false
  win.loadURL = vi.fn()
  win.isDestroyed = () => win.destroyed
  win.close = vi.fn(() => {
    win.destroyed = true
    win.emit('closed')
  })
  return win
}

describe('matchOAuthLogin', () => {
  it('matches the providers the login view links to', () => {
    expect(matchOAuthLogin('/api/v1/auth/google/login')).toBe('google')
    expect(matchOAuthLogin('/api/v1/auth/github/login')).toBe('github')
  })

  it('tolerates a trailing slash', () => {
    expect(matchOAuthLogin('/api/v1/auth/github/login/')).toBe('github')
  })

  it('ignores every other auth path', () => {
    // The callback and the root-token endpoint must go through the normal
    // proxy — taking those over would break sign-in rather than fix it.
    expect(matchOAuthLogin('/api/v1/auth/github/callback')).toBeNull()
    expect(matchOAuthLogin('/api/v1/auth/root/login')).toBeNull()
    expect(matchOAuthLogin('/api/v1/auth/user')).toBeNull()
  })

  it('ignores an unknown provider and near-misses', () => {
    expect(matchOAuthLogin('/api/v1/auth/gitlab/login')).toBeNull()
    expect(matchOAuthLogin('/api/v1/auth/github/login/extra')).toBeNull()
    expect(matchOAuthLogin('/prefix/api/v1/auth/github/login')).toBeNull()
    expect(matchOAuthLogin('/')).toBeNull()
  })

  it('exports the providers it recognises', () => {
    expect(OAUTH_PROVIDERS).toEqual(['google', 'github'])
  })
})

describe('isOAuthReturn', () => {
  it('is true once the flow lands back on the app itself', () => {
    expect(isOAuthReturn(`${SERVER}/`, SERVER)).toBe(true)
    expect(isOAuthReturn(`${SERVER}/workspaces/abc`, SERVER)).toBe(true)
  })

  it('is false while still inside the auth endpoints', () => {
    // The callback sets the cookie and then redirects on; treating it as the
    // end would check for the cookie a moment too early.
    expect(isOAuthReturn(`${SERVER}/api/v1/auth/github/callback?code=x`, SERVER)).toBe(false)
    expect(isOAuthReturn(`${SERVER}/api/v1/auth/github/login`, SERVER)).toBe(false)
  })

  it('is false while on the identity provider', () => {
    expect(isOAuthReturn('https://github.com/login/oauth/authorize?x=1', SERVER)).toBe(false)
    expect(isOAuthReturn('https://accounts.google.com/o/oauth2/auth', SERVER)).toBe(false)
  })

  it('does not confuse a lookalike host', () => {
    expect(isOAuthReturn('https://agentrq.example.com.evil.test/', SERVER)).toBe(false)
    expect(isOAuthReturn('http://agentrq.example.com/', SERVER)).toBe(false)
  })

  it('is false for anything unparseable', () => {
    expect(isOAuthReturn('not a url', SERVER)).toBe(false)
    expect(isOAuthReturn(`${SERVER}/`, 'not a url')).toBe(false)
  })
})

describe('oauthStartUrl', () => {
  it('rebuilds the link against the real server origin', () => {
    expect(oauthStartUrl(SERVER, '/api/v1/auth/github/login')).toBe(`${SERVER}/api/v1/auth/github/login`)
  })

  it('keeps the query string', () => {
    expect(oauthStartUrl(SERVER, '/api/v1/auth/google/login', '?next=%2Fevents')).toBe(
      `${SERVER}/api/v1/auth/google/login?next=%2Fevents`
    )
  })

  it('respects a server mounted under a base path', () => {
    expect(oauthStartUrl('https://example.com/agentrq', '/api/v1/auth/github/login')).toBe(
      'https://example.com/api/v1/auth/github/login'
    )
  })
})

describe('runOAuthFlow', () => {
  it('opens the flow in its own window at the start URL', async () => {
    const win = fakeWindow()
    const startUrl = `${SERVER}/api/v1/auth/github/login`

    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl,
      createWindow: () => win,
      hasAuthCookie: async () => true,
    })

    expect(win.loadURL).toHaveBeenCalledWith(startUrl)
    win.webContents.emit('did-navigate', {}, `${SERVER}/`)
    await expect(flow).resolves.toEqual({ ok: true })
  })

  it('succeeds when the cookie is in the jar on return', async () => {
    const win = fakeWindow()
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie: async () => true,
    })

    win.webContents.emit('did-navigate', {}, `${SERVER}/`)

    await expect(flow).resolves.toEqual({ ok: true })
    expect(win.close).toHaveBeenCalled()
  })

  it('recognises the return when it arrives as a redirect', async () => {
    // The provider's callback normally reaches the app as a redirect chain
    // rather than a fresh navigation.
    const win = fakeWindow()
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/google/login`,
      createWindow: () => win,
      hasAuthCookie: async () => true,
    })

    win.webContents.emit('did-redirect-navigation', {}, `${SERVER}/`)

    await expect(flow).resolves.toEqual({ ok: true })
  })

  it('fails when the flow returns without a cookie', async () => {
    const win = fakeWindow()
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie: async () => false,
    })

    win.webContents.emit('did-navigate', {}, `${SERVER}/`)

    await expect(flow).resolves.toEqual({ ok: false, reason: 'Sign-in did not complete' })
  })

  it('does not check for a cookie while still at the provider', async () => {
    const hasAuthCookie = vi.fn(async () => true)
    const win = fakeWindow()
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie,
    })

    win.webContents.emit('did-navigate', {}, 'https://github.com/login/oauth/authorize')
    win.webContents.emit('did-navigate', {}, `${SERVER}/api/v1/auth/github/callback?code=x`)
    expect(hasAuthCookie).not.toHaveBeenCalled()

    win.webContents.emit('did-navigate', {}, `${SERVER}/`)
    await expect(flow).resolves.toEqual({ ok: true })
    expect(hasAuthCookie).toHaveBeenCalledOnce()
  })

  it('resolves when the user closes the window instead of signing in', async () => {
    const win = fakeWindow()
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie: async () => false,
    })

    win.emit('closed')

    await expect(flow).resolves.toEqual({ ok: false, reason: 'Sign-in window was closed' })
  })

  it('settles once, even if more navigations arrive', async () => {
    // The window emits 'closed' as part of finishing, and a redirect can race
    // it; resolving twice would be a silent bug.
    const win = fakeWindow()
    const hasAuthCookie = vi.fn(async () => true)
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie,
    })

    win.webContents.emit('did-navigate', {}, `${SERVER}/`)
    await flow
    win.webContents.emit('did-navigate', {}, `${SERVER}/events`)
    win.emit('closed')

    expect(hasAuthCookie).toHaveBeenCalledOnce()
    await expect(flow).resolves.toEqual({ ok: true })
  })

  it('does not close a window that is already gone', async () => {
    const win = fakeWindow()
    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie: async () => true,
    })

    win.destroyed = true
    win.webContents.emit('did-navigate', {}, `${SERVER}/`)

    await flow
    expect(win.close).not.toHaveBeenCalled()
  })

  it('resolves once when two returns race the cookie lookup', async () => {
    // Both navigations pass the settled check before either await finishes, so
    // two calls reach finish(); without its own guard the promise would be
    // resolved twice and the window closed twice.
    const win = fakeWindow()
    let release
    const gate = new Promise((r) => { release = r })
    const hasAuthCookie = vi.fn(async () => { await gate; return true })

    const flow = runOAuthFlow({
      serverUrl: SERVER,
      startUrl: `${SERVER}/api/v1/auth/github/login`,
      createWindow: () => win,
      hasAuthCookie,
    })

    win.webContents.emit('did-navigate', {}, `${SERVER}/`)
    win.webContents.emit('did-redirect-navigation', {}, `${SERVER}/events`)
    release()

    await expect(flow).resolves.toEqual({ ok: true })
    expect(hasAuthCookie).toHaveBeenCalledTimes(2)
    expect(win.close).toHaveBeenCalledOnce()
  })

  it('names the cookie the backend actually sets', () => {
    expect(AUTH_COOKIE).toBe('at')
  })
})
