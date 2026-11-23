// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

/**
 * How a terminal socket is addressed and authenticated.
 *
 * This is the one API call in the app that cannot rely on the `at` cookie, and
 * the reason the desktop terminal never connected. Every other call is a
 * same-origin relative URL, which is what lets the desktop app forward it
 * through its `app://` handler with the cookie attached in the main process. A
 * WebSocket cannot be forwarded that way, so the socket URL is absolute, its
 * upgrade is cross-site, and the browser withholds a SameSite=Lax cookie from
 * it. The ticket is what replaces the cookie there.
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { terminalTicket, terminalSocketUrl } from '../src/api.js'

const ticketFor = (ticket) => ({
  ok: true,
  status: 200,
  json: async () => ({ ticket, expiresIn: 60 }),
})

describe('a terminal ticket', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(ticketFor('tkt-1')))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    delete window.agentrq
  })

  // Relative, and that is the whole point of it existing: this request is
  // forwarded by the desktop shell like every other one, so the cookie is
  // applied where it works.
  it('is asked for over the ordinary same-origin API', async () => {
    expect(await terminalTicket('0is9t9UOwO9')).toBe('tkt-1')

    const [url, init] = fetch.mock.calls[0]
    expect(url).toBe('/api/v1/sessions/0is9t9UOwO9/terminal/ticket')
    expect(init.method).toBe('POST')
    expect(String(url)).not.toMatch(/^https?:/)
  })

  it('refuses a session it was not given permission for', async () => {
    fetch.mockResolvedValue({ ok: false, status: 404, json: async () => ({}) })
    await expect(terminalTicket('0is9t9UOwO9')).rejects.toThrow(/permission/)
  })

  // A 200 with nothing in it is not a ticket. Returning undefined here would
  // put `ticket=undefined` in the socket URL and turn a clear refusal into a
  // terminal that reconnects forever.
  it('refuses an answer with no ticket in it', async () => {
    fetch.mockResolvedValue({ ok: true, status: 200, json: async () => ({ expiresIn: 60 }) })
    await expect(terminalTicket('0is9t9UOwO9')).rejects.toThrow(/permission/)
  })
})

describe('the terminal socket URL', () => {
  beforeEach(() => {
    vi.stubGlobal('fetch', vi.fn().mockResolvedValue(ticketFor('tkt-1')))
  })

  afterEach(() => {
    vi.unstubAllGlobals()
    delete window.agentrq
  })

  it('carries the ticket, on the WebSocket scheme', async () => {
    const url = new URL(await terminalSocketUrl('0is9t9UOwO9'))
    expect(url.protocol).toBe('ws:')
    expect(url.pathname).toBe('/api/v1/sessions/0is9t9UOwO9/terminal')
    expect(url.searchParams.get('ticket')).toBe('tkt-1')
  })

  // The desktop case. The renderer's own origin is `app://`, which is not an
  // address a WebSocket can be opened against, so the shell is asked where the
  // server actually is — and wss, because that server is https.
  it('asks the desktop shell where the server is', async () => {
    window.agentrq = {
      connection: { get: vi.fn().mockResolvedValue({ serverUrl: 'https://agentrq.example/' }) },
    }

    const url = new URL(await terminalSocketUrl('0is9t9UOwO9'))
    expect(url.protocol).toBe('wss:')
    expect(url.host).toBe('agentrq.example')
    expect(url.searchParams.get('ticket')).toBe('tkt-1')
    expect(window.agentrq.connection.get).toHaveBeenCalled()
  })

  // Asked for again every time rather than cached. The ticket is good for
  // about a minute, so a URL kept across a reconnect an hour later is one the
  // server refuses — and nothing would say why.
  it('mints a new ticket for every call', async () => {
    fetch.mockResolvedValueOnce(ticketFor('tkt-1')).mockResolvedValueOnce(ticketFor('tkt-2'))

    const first = new URL(await terminalSocketUrl('0is9t9UOwO9'))
    const second = new URL(await terminalSocketUrl('0is9t9UOwO9'))

    expect(first.searchParams.get('ticket')).toBe('tkt-1')
    expect(second.searchParams.get('ticket')).toBe('tkt-2')
  })
})
