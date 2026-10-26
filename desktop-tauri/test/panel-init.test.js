// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { readFileSync } from 'node:fs'
import { describe, expect, it, vi } from 'vitest'

// What `core::side_panel::child::guest_init_script` sends: the text, with the
// placeholder filled.
const SOURCE = readFileSync(new URL('../src/guest/panel.js', import.meta.url), 'utf8')

/**
 * Run the init script in a page at `href`, as a frame of `top` or the top,
 * and hand an Extension's page `token` as the shell does once it loads.
 */
function run(href, { webview2 = false, frame = false, answer, token = 'tk', chrome, setTimeout = (fn) => fn() } = {}) {
  const url = new URL(href)
  const listeners = []
  const window = {
    location: { protocol: url.protocol, hostname: url.hostname },
    fetch: vi.fn(async () => answer ?? { json: async () => ({ ok: true, reply: 'pong' }) }),
    addEventListener: vi.fn((type, listener, capture) => listeners.push({ type, listener, capture })),
    setTimeout,
    chrome,
    listeners,
  }
  window.top = frame ? {} : window
  const script = SOURCE.replaceAll('__WEBVIEW2__', String(webview2))
  new Function('window', 'location', script)(window, window.location)
  if (token !== null) window.__agentrqPanelToken?.(token)
  return window
}

const HEADERS = { 'content-type': 'application/json', 'x-agentrq-panel-token': 'tk' }

describe('the side panel guest init script', () => {
  it('gives a website nothing', () => {
    expect(run('https://a.example/').agentrq).toBeUndefined()
    expect(run('http://agentrq-ext.notes/', { webview2: false }).agentrq).toBeUndefined()
    expect(run('agentrq-ext://notes/', { webview2: true }).agentrq).toBeUndefined()
    expect(run('https://agentrq-ext.notes/', { webview2: true }).agentrq).toBeDefined()
    expect(run('ftp://agentrq-ext.notes/', { webview2: true }).agentrq).toBeUndefined()
    expect(run('https://notes.example/', { webview2: true }).agentrq).toBeUndefined()
  })

  it('gives a frame nothing, even of an extension', () => {
    expect(run('agentrq-ext://notes/panel/index.html', { frame: true }).agentrq).toBeUndefined()
  })

  it('gives an extension page a send that posts to its own bridge', async () => {
    const window = run('agentrq-ext://notes/panel/index.html')
    expect(Object.keys(window.agentrq)).toEqual(['panel'])
    await expect(window.agentrq.panel.send({ hello: 1 })).resolves.toBe('pong')
    expect(window.fetch).toHaveBeenCalledWith('/.agentrq/panel-send', {
      method: 'POST',
      headers: HEADERS,
      body: '{"hello":1}',
    })
    // The page cannot swap it, or what it calls.
    expect(() => {
      'use strict'
      window.agentrq = null
    }).toThrow()
    expect(Object.isFrozen(window.agentrq.panel)).toBe(true)
    window.fetch = vi.fn()
    await window.agentrq.panel.send({})
    expect(window.fetch).not.toHaveBeenCalled()
  })

  it('rejects with the reason the shell gave, or a plain one', async () => {
    const refused = run('agentrq-ext://notes/', { answer: { json: async () => ({ ok: false, reason: 'Too big.' }) } })
    await expect(refused.agentrq.panel.send(1)).rejects.toThrow('Too big.')
    const garbled = run('agentrq-ext://notes/', { answer: { json: async () => JSON.parse('{') } })
    await expect(garbled.agentrq.panel.send(1)).rejects.toThrow('The extension did not answer.')
  })

  it('sends with the token the shell handed it, and waits for one', async () => {
    const window = run('agentrq-ext://notes/', { token: null })
    const sent = window.agentrq.panel.send(1)
    await Promise.resolve()
    expect(window.fetch).not.toHaveBeenCalled()
    window.__agentrqPanelToken('first')
    await expect(sent).resolves.toBe('pong')
    expect(window.fetch.mock.calls[0][1].headers['x-agentrq-panel-token']).toBe('first')
    // A new one replaces it; the page cannot swap the function itself.
    window.__agentrqPanelToken('second')
    await window.agentrq.panel.send(2)
    expect(window.fetch.mock.calls[1][1].headers['x-agentrq-panel-token']).toBe('second')
    expect(() => {
      'use strict'
      window.__agentrqPanelToken = () => {}
    }).toThrow()
    // A website has no receiver at all.
    expect(run('https://a.example/').__agentrqPanelToken).toBeUndefined()
  })

  it('tells WebView2 which keys the page left unhandled, from the top frame only', () => {
    const postMessage = vi.fn()
    const window = run('https://a.example/', { webview2: true, chrome: { webview: { postMessage } } })
    const [{ type, listener, capture }] = window.listeners
    expect([type, capture]).toEqual(['keydown', true])
    const key = (init) => ({ isTrusted: true, isComposing: false, defaultPrevented: false, ...init })
    listener(key({ key: 'R', ctrlKey: true }))
    listener(key({ key: 'F11' }))
    expect(postMessage.mock.calls).toEqual([['agentrq-panel-key:r'], ['agentrq-panel-key:f11']])
    // Handled by the page, typed, composed, or made by a script.
    listener(key({ key: 'r', ctrlKey: true, defaultPrevented: true }))
    listener(key({ key: 'r' }))
    listener(key({ key: 'r', altKey: true, isComposing: true }))
    listener(key({ key: 'r', metaKey: true, isTrusted: false }))
    expect(postMessage).toHaveBeenCalledTimes(2)
    // Not a frame, not elsewhere, not without the channel.
    for (const options of [
      { webview2: true, frame: true, chrome: { webview: { postMessage } } },
      { webview2: false, chrome: { webview: { postMessage } } },
      { webview2: true },
    ]) {
      expect(run('https://a.example/', options).listeners).toEqual([])
    }
  })

  it('hands the page what its Extension posts, only with the token it holds', () => {
    const later = []
    const window = run('agentrq-ext://notes/', { setTimeout: (fn) => later.push(fn) })
    const heard = []
    window.agentrq.panel.onMessage(() => {
      throw new Error('broken')
    })
    window.agentrq.panel.onMessage((message) => heard.push(message))
    window.__agentrqPanelMessage('tk', { n: 1 })
    expect(heard).toEqual([{ n: 1 }])
    // The listener that threw is reported once the others have heard it.
    expect(later).toHaveLength(1)
    expect(() => later[0]()).toThrow('broken')
    window.__agentrqPanelMessage('guess', 2)
    expect(heard).toEqual([{ n: 1 }])
    expect(() => {
      'use strict'
      window.__agentrqPanelMessage = () => {}
    }).toThrow()
    // Before the page has a token, nothing it is handed is from the shell.
    const early = run('agentrq-ext://notes/', { token: null })
    const none = vi.fn()
    early.agentrq.panel.onMessage(none)
    early.__agentrqPanelMessage(null, 3)
    early.__agentrqPanelMessage('null', 3)
    expect(none).not.toHaveBeenCalled()
  })

  it('lets a page listen, and stop', () => {
    const { agentrq } = run('agentrq-ext://notes/')
    const stop = agentrq.panel.onMessage(() => {})
    expect(stop()).toBe(true)
    expect(stop()).toBe(false)
  })
})
