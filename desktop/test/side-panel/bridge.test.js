// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

import { MAX_MESSAGE_BYTES, createPanelHub, deliverTo, extensionOf } from '../../src/main/side-panel/bridge.js'

function setup() {
  const show = vi.fn()
  const deliver = vi.fn(() => 1)
  const pagesFor = vi.fn(async (owner) => (owner === 'notes' ? [{ id: 'board', label: 'Board', entry: 'panel/index.html' }] : []))
  const hub = createPanelHub({ show, deliver, pagesFor })
  return { hub, show, deliver, panel: hub.capabilityFor('notes') }
}

describe('extensionOf', () => {
  it('names the extension an address belongs to', () => {
    expect(extensionOf('agentrq-ext://notes/panel/index.html')).toBe('notes')
  })

  it('names none for the web, the app, or junk', () => {
    expect(extensionOf('https://notes/panel/index.html')).toBe('')
    expect(extensionOf('app://agentrq/')).toBe('')
    expect(extensionOf('not a url')).toBe('')
    expect(extensionOf(undefined)).toBe('')
  })
})

describe('ctx.panel.open', () => {
  it('opens one of the extension’s own pages', async () => {
    const { panel, show } = setup()
    expect(await panel.open({ page: 'board' })).toEqual({ ok: true })
    expect(show).toHaveBeenCalledWith('agentrq-ext://notes/panel/index.html')
  })

  it('refuses a page it did not declare, with its name on it', async () => {
    const { panel, show } = setup()
    expect(await panel.open({ page: 'secret' })).toEqual({ ok: false, reason: 'notes has no panel page "secret".' })
    expect(show).not.toHaveBeenCalled()
  })

  it('opens a web page', async () => {
    const { panel, show } = setup()
    expect(await panel.open({ url: 'https://example.com/docs' })).toEqual({ ok: true })
    expect(show).toHaveBeenCalledWith('https://example.com/docs')
  })

  it('refuses anything else', async () => {
    const { panel, show } = setup()
    for (const target of [{ url: 'file:///etc/passwd' }, { url: 'javascript:alert(1)' }, { url: 'not a url' }, {}, undefined]) {
      expect(await panel.open(target), JSON.stringify(target)).toEqual({ ok: false, reason: 'notes: the side panel opens a page or an http(s) address.' })
    }
    expect(show).not.toHaveBeenCalled()
  })
})

describe('a page sending to its extension', () => {
  it('reaches its own extension, and gets the answer back', async () => {
    const { hub, panel } = setup()
    const handler = vi.fn(async (message) => ({ echoed: message }))
    panel.onMessage(handler)
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', { type: 'refresh' })).toEqual({ ok: true, reply: { echoed: { type: 'refresh' } } })
    expect(handler).toHaveBeenCalledWith({ type: 'refresh' })
  })

  it('answers with null when the handler returns nothing', async () => {
    const { hub, panel } = setup()
    panel.onMessage(() => {})
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', 1)).toEqual({ ok: true, reply: null })
  })

  it('never reaches an extension from a web page, whatever the message says', async () => {
    const { hub, panel } = setup()
    const handler = vi.fn()
    panel.onMessage(handler)
    expect(await hub.handleSend('https://evil.example/', { to: 'notes' })).toEqual({ ok: false, reason: 'Only an extension’s own pages can send to it.' })
    expect(handler).not.toHaveBeenCalled()
  })

  it('never reaches another extension', async () => {
    const { hub, panel } = setup()
    const notes = vi.fn()
    panel.onMessage(notes)
    const other = vi.fn(() => 'other')
    hub.capabilityFor('other').onMessage(other)
    expect(await hub.handleSend('agentrq-ext://other/panel/x.html', { to: 'notes' })).toEqual({ ok: true, reply: 'other' })
    expect(notes).not.toHaveBeenCalled()
  })

  it('says so when the extension does not listen', async () => {
    const { hub } = setup()
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', {})).toEqual({ ok: false, reason: 'notes does not listen to its page.' })
  })

  it('replaces a handler, and needs a function', () => {
    const { panel } = setup()
    expect(() => panel.onMessage('nope')).toThrow('notes: onMessage needs a function.')
  })

  it('turns a throwing handler into a refusal with the extension’s name', async () => {
    const { hub, panel } = setup()
    panel.onMessage(() => { throw new Error('database unavailable') })
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', {})).toEqual({ ok: false, reason: 'notes failed: database unavailable' })
    panel.onMessage(() => { throw 'threw a bare string' })
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', {})).toEqual({ ok: false, reason: 'notes failed: threw a bare string' })
  })

  it('refuses a message over 1 MB, or one that is not data, either way', async () => {
    const { hub, panel } = setup()
    panel.onMessage((message) => message)
    const big = 'x'.repeat(MAX_MESSAGE_BYTES)
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', big)).toEqual({ ok: false, reason: 'notes: that message is larger than 1 MB.' })
    const circular = {}
    circular.self = circular
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', circular)).toEqual({ ok: false, reason: 'notes: that message is not plain data.' })

    panel.onMessage(() => 'y'.repeat(MAX_MESSAGE_BYTES))
    expect(await hub.handleSend('agentrq-ext://notes/panel/index.html', 'hi')).toEqual({ ok: false, reason: 'notes: that message is larger than 1 MB.' })
  })

  it('stops answering once the extension stops', async () => {
    const { hub, panel } = setup()
    panel.onMessage(() => 'hi')
    hub.retract('notes')
    expect((await hub.handleSend('agentrq-ext://notes/panel/index.html', {})).reason).toBe('notes does not listen to its page.')
  })
})

describe('ctx.panel.post', () => {
  it('delivers plain data to the extension’s pages', () => {
    const { panel, deliver } = setup()
    expect(panel.post({ count: 3 })).toEqual({ ok: true, reached: 1 })
    expect(deliver).toHaveBeenCalledWith('notes', { count: 3 })
  })

  it('refuses what cannot cross', () => {
    const { panel, deliver } = setup()
    expect(panel.post(() => {})).toEqual({ ok: false, reason: 'notes: that message is not plain data.' })
    expect(deliver).not.toHaveBeenCalled()
  })
})

describe('deliverTo', () => {
  const guest = (url, destroyed = false) => ({ getURL: () => url, isDestroyed: () => destroyed, send: vi.fn() })

  it('reaches only open pages of that extension', () => {
    const mine = guest('agentrq-ext://notes/panel/index.html')
    const gone = guest('agentrq-ext://notes/panel/index.html', true)
    const web = guest('https://example.com/')
    const other = guest('agentrq-ext://other/panel/x.html')
    const bare = { getURL: () => 'agentrq-ext://notes/panel/b.html', send: vi.fn() }
    expect(deliverTo([mine, gone, web, other, bare], 'notes', 'agentrq:panel:message', { n: 1 })).toBe(2)
    expect(mine.send).toHaveBeenCalledWith('agentrq:panel:message', { n: 1 })
    expect(bare.send).toHaveBeenCalled()
    for (const skipped of [gone, web, other]) expect(skipped.send).not.toHaveBeenCalled()
  })
})
