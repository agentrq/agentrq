import { describe, it, expect } from 'vitest'

import { DEFAULT_SERVER_URL, normalizeServerUrl, resolveServerUrl } from '../src/main/server-config.js'

describe('normalizeServerUrl', () => {
  it('keeps an explicit http or https URL', () => {
    expect(normalizeServerUrl('https://agentrq.example.com')).toEqual({
      ok: true,
      url: 'https://agentrq.example.com',
    })
    expect(normalizeServerUrl('http://localhost:3000')).toEqual({ ok: true, url: 'http://localhost:3000' })
  })

  it('assumes http for a bare host, which is the self-hosted case', () => {
    expect(normalizeServerUrl('localhost:3000')).toEqual({ ok: true, url: 'http://localhost:3000' })
    expect(normalizeServerUrl('192.168.1.20:8080')).toEqual({ ok: true, url: 'http://192.168.1.20:8080' })
  })

  it('trims surrounding whitespace', () => {
    expect(normalizeServerUrl('  https://agentrq.example.com  ').url).toBe('https://agentrq.example.com')
  })

  it('strips the trailing slash so /api/v1 resolves predictably', () => {
    expect(normalizeServerUrl('https://agentrq.example.com/').url).toBe('https://agentrq.example.com')
  })

  it('preserves a base path, which reverse-proxied deployments rely on', () => {
    expect(normalizeServerUrl('https://example.com/agentrq').url).toBe('https://example.com/agentrq')
  })

  it('drops a query or fragment rather than losing it silently later', () => {
    expect(normalizeServerUrl('https://example.com/?a=1#x').url).toBe('https://example.com')
  })

  it('rejects an empty value', () => {
    expect(normalizeServerUrl('')).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(normalizeServerUrl('   ')).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(normalizeServerUrl(null)).toEqual({ ok: false, reason: 'Server URL is required' })
    expect(normalizeServerUrl(undefined)).toEqual({ ok: false, reason: 'Server URL is required' })
  })

  it('rejects a scheme that is not http or https', () => {
    // A stored value must never be able to smuggle in file:// or javascript:.
    expect(normalizeServerUrl('file:///etc/passwd')).toEqual({
      ok: false,
      reason: 'Server URL must use http or https',
    })
    expect(normalizeServerUrl('javascript://alert(1)')).toEqual({
      ok: false,
      reason: 'Server URL must use http or https',
    })
  })

  it('rejects a URL with no host', () => {
    // http and https are "special" schemes: the parser itself refuses an empty
    // host, so this never reaches the scheme check.
    expect(normalizeServerUrl('http://')).toEqual({ ok: false, reason: 'Not a valid URL' })
  })

  it('rejects unparseable input', () => {
    expect(normalizeServerUrl('http://[')).toEqual({ ok: false, reason: 'Not a valid URL' })
  })
})

describe('resolveServerUrl', () => {
  it('falls back to localhost when nothing is configured', () => {
    expect(resolveServerUrl()).toBe(DEFAULT_SERVER_URL)
    expect(resolveServerUrl({})).toBe(DEFAULT_SERVER_URL)
    expect(resolveServerUrl({ env: {}, stored: '' })).toBe(DEFAULT_SERVER_URL)
  })

  it('prefers the environment override, so a scratch backend needs no stored change', () => {
    expect(resolveServerUrl({ env: { AGENTRQ_SERVER_URL: 'http://localhost:3999' }, stored: 'https://prod.example.com' }))
      .toBe('http://localhost:3999')
  })

  it('uses the stored value when there is no override', () => {
    expect(resolveServerUrl({ env: {}, stored: 'https://prod.example.com' })).toBe('https://prod.example.com')
  })

  it('normalises whatever it takes', () => {
    expect(resolveServerUrl({ env: {}, stored: 'example.com/' })).toBe('http://example.com')
  })

  it('skips an invalid candidate rather than failing the launch', () => {
    // A corrupted stored value should not leave the app with no server at all.
    expect(resolveServerUrl({ env: { AGENTRQ_SERVER_URL: 'file:///x' }, stored: 'https://ok.example.com' }))
      .toBe('https://ok.example.com')
    expect(resolveServerUrl({ env: {}, stored: 'file:///x' })).toBe(DEFAULT_SERVER_URL)
  })
})
