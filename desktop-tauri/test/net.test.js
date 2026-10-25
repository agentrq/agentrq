// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'
import { installEventSource, installFetch, sameOrigin } from '../src/bridge/net.js'

const location = { href: 'app://localhost/workspaces', protocol: 'app:', host: 'localhost', origin: 'app://localhost' }

describe('sameOrigin', () => {
  it('compares scheme and host, since a custom scheme has an opaque origin', () => {
    expect(sameOrigin(new URL('app://localhost/api/v1/x'), location)).toBe(true)
    expect(sameOrigin(new URL('app://other/api'), location)).toBe(false)
    expect(sameOrigin(new URL('https://localhost/api'), location)).toBe(false)
  })
})

describe('installFetch', () => {
  function rig() {
    const native = vi.fn(async () => new Response('ok'))
    const win = { location, fetch: native }
    installFetch(win)
    return { win, native }
  }

  const bytes = async (init) => new Uint8Array(init.body)

  it('leaves a request with no body, or a plain one, to the native fetch untouched', async () => {
    const { win, native } = rig()
    const init = { method: 'POST', body: '{"a":1}' }
    await win.fetch('/api/v1/x', init)
    await win.fetch('/api/v1/y')
    expect(native.mock.calls).toEqual([
      ['/api/v1/x', init],
      ['/api/v1/y', undefined],
    ])
    expect(native.mock.contexts[0]).toBe(win)
  })

  it('leaves a cross-origin request alone, whatever its body', async () => {
    const { win, native } = rig()
    const init = { method: 'PUT', body: new Blob(['x']) }
    await win.fetch('https://huggingface.co/upload', init)
    expect(native).toHaveBeenCalledWith('https://huggingface.co/upload', init)
  })

  it('sends a same-origin Blob as its bytes, keeping the method and headers', async () => {
    const { win, native } = rig()
    const controller = new AbortController()
    await win.fetch('/api/v1/files', {
      method: 'PUT',
      headers: { 'x-a': '1' },
      body: new Blob(['hello'], { type: 'text/plain' }),
      signal: controller.signal,
    })
    const [url, init] = native.mock.calls[0]
    expect(url).toBe('app://localhost/api/v1/files')
    expect(init.method).toBe('PUT')
    expect(init.headers.get('x-a')).toBe('1')
    expect(init.headers.get('content-type')).toBe('text/plain')
    expect(init.signal).toBe(controller.signal)
    expect(new TextDecoder().decode(await bytes(init))).toBe('hello')
  })

  it('sends FormData as multipart bytes with its boundary', async () => {
    const { win, native } = rig()
    const form = new FormData()
    form.append('file', new File(['abc'], 'a.txt'))
    await win.fetch('/api/v1/upload', { method: 'POST', body: form })
    const [, init] = native.mock.calls[0]
    const boundary = init.headers.get('content-type').match(/boundary=(.+)$/)[1]
    const text = new TextDecoder().decode(await bytes(init))
    expect(text).toContain(boundary)
    expect(text).toContain('filename="a.txt"')
    expect(text).toContain('abc')
  })

  it('sends a stream body, and a Request object carrying one, as bytes', async () => {
    const { win, native } = rig()
    const stream = () =>
      new ReadableStream({
        start(c) {
          c.enqueue(new TextEncoder().encode('chunk'))
          c.close()
        },
      })
    await win.fetch('/api/v1/s', { method: 'POST', body: stream(), duplex: 'half' })
    await win.fetch(new Request('app://localhost/api/v1/r', { method: 'POST', body: 'req' }))
    const [first, second] = native.mock.calls
    expect(new TextDecoder().decode(await bytes(first[1]))).toBe('chunk')
    expect(second[0]).toBe('app://localhost/api/v1/r')
    expect(new TextDecoder().decode(await bytes(second[1]))).toBe('req')
    expect(second[1].signal).toBeInstanceOf(AbortSignal)
  })
})

describe('installEventSource', () => {
  class FakeChannel {
    onmessage = null
  }

  function rig({ open = async () => 7 } = {}) {
    const Native = vi.fn(function Native(url, init) {
      this.native = { url, init }
    })
    const invoke = vi.fn(async (cmd, args) => (cmd === 'event_source_open' ? open(args) : undefined))
    const win = Object.assign(new EventTarget(), { location, EventSource: Native })
    installEventSource(win, invoke, FakeChannel)
    return { win, invoke, Native }
  }

  const channelOf = (invoke) => invoke.mock.calls[0][1].onEvent
  const flush = () => new Promise((resolve) => setTimeout(resolve, 0))

  it('hands a cross-origin source to the native EventSource', () => {
    const { win, Native, invoke } = rig()
    const source = new win.EventSource('https://example.com/pub/stats', { withCredentials: true })
    expect(source.native).toEqual({ url: 'https://example.com/pub/stats', init: { withCredentials: true } })
    expect(Native).toHaveBeenCalledOnce()
    expect(invoke).not.toHaveBeenCalled()
  })

  it('opens a same-origin stream in Rust by path and query', async () => {
    const { win, invoke } = rig()
    const source = new win.EventSource('/api/v1/events?w=1')
    expect(source.url).toBe('app://localhost/api/v1/events?w=1')
    expect(source.withCredentials).toBe(false)
    expect(source.readyState).toBe(win.EventSource.CONNECTING)
    expect(invoke).toHaveBeenCalledWith('event_source_open', {
      url: '/api/v1/events?w=1',
      onEvent: expect.any(FakeChannel),
    })
    expect(new win.EventSource('/x', { withCredentials: true }).withCredentials).toBe(true)
  })

  it('has the constants on the class and on every source', () => {
    const { win } = rig()
    expect([win.EventSource.CONNECTING, win.EventSource.OPEN, win.EventSource.CLOSED]).toEqual([0, 1, 2])
    expect(new win.EventSource('/e').CLOSED).toBe(2)
  })

  it('opens, delivers messages to onmessage and listeners, and named events to listeners only', () => {
    const { win, invoke } = rig()
    const source = new win.EventSource('/api/v1/events')
    const seen = []
    source.onopen = () => seen.push(['onopen', source.readyState])
    source.onmessage = (e) => seen.push(['onmessage', e.data, e.lastEventId, e.origin])
    source.addEventListener('message', (e) => seen.push(['message', e.data]))
    source.addEventListener('task.created', (e) => seen.push(['task.created', e.data]))

    const channel = channelOf(invoke)
    channel.onmessage({ type: 'open' })
    channel.onmessage({ type: 'message', event: 'message', data: 'hi', lastEventId: '4' })
    channel.onmessage({ type: 'message', event: 'task.created', data: '{}', lastEventId: '5' })

    expect(seen).toEqual([
      ['onopen', 1],
      ['message', 'hi'],
      ['onmessage', 'hi', '4', 'app://localhost'],
      ['task.created', '{}'],
    ])
  })

  it('reports a reconnect as an error while connecting, and a failure as closed', () => {
    const { win, invoke } = rig()
    const source = new win.EventSource('/api/v1/events')
    const states = []
    source.onerror = () => states.push(source.readyState)
    const channel = channelOf(invoke)
    channel.onmessage({ type: 'error', readyState: 0 })
    channel.onmessage({ type: 'error', readyState: 2 })
    channel.onmessage({ type: 'open' })
    expect(states).toEqual([0, 2])
    expect(source.readyState).toBe(2)
  })

  it('works with no handlers set', () => {
    const { win, invoke } = rig()
    const source = new win.EventSource('/api/v1/events')
    const channel = channelOf(invoke)
    channel.onmessage({ type: 'open' })
    channel.onmessage({ type: 'message', event: 'message', data: 'x', lastEventId: '' })
    channel.onmessage({ type: 'error', readyState: 0 })
    expect(source.readyState).toBe(0)
  })

  it('closes the Rust stream once, and stops delivering', async () => {
    const { win, invoke } = rig()
    const source = new win.EventSource('/api/v1/events')
    await flush()
    const onmessage = vi.fn()
    source.onmessage = onmessage
    source.close()
    source.close()
    channelOf(invoke).onmessage({ type: 'message', event: 'message', data: 'late', lastEventId: '' })
    expect(source.readyState).toBe(2)
    expect(onmessage).not.toHaveBeenCalled()
    expect(invoke.mock.calls.filter(([cmd]) => cmd === 'event_source_close')).toEqual([['event_source_close', { id: 7 }]])
  })

  it('closes a stream that opened after the page had already closed it', async () => {
    const { win, invoke } = rig()
    new win.EventSource('/api/v1/events').close()
    await flush()
    expect(invoke).toHaveBeenLastCalledWith('event_source_close', { id: 7 })
  })

  it('closes every stream still open when the page goes away', async () => {
    let next = 0
    const { win, invoke } = rig({ open: async () => ++next })
    const kept = new win.EventSource('/api/v1/events')
    const closed = new win.EventSource('/api/v1/events?w=1')
    const failed = new win.EventSource('/api/v1/events?w=2')
    await flush()
    closed.close()
    invoke.mock.calls[2][1].onEvent.onmessage({ type: 'error', readyState: 2 })
    invoke.mockClear()

    win.dispatchEvent(new Event('pagehide'))
    expect(kept.readyState).toBe(2)
    expect(failed.readyState).toBe(2)
    expect(invoke.mock.calls).toEqual([['event_source_close', { id: 1 }]])
  })

  it('fails the source for good when Rust refuses to open it', async () => {
    const { win } = rig({ open: async () => Promise.reject(new Error('Forbidden')) })
    const source = new win.EventSource('/api/v1/events')
    const onerror = vi.fn()
    source.onerror = onerror
    await flush()
    expect(source.readyState).toBe(2)
    expect(onerror).toHaveBeenCalledOnce()
  })
})
