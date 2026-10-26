// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, expect, it, vi } from 'vitest'

import { DEADLINE_MS, FAILED, MAX_LINE, METHOD_NOT_FOUND, PING, createPeer } from '../src/ext-host/rpc.js'

function rig(options = {}) {
  const sent = []
  const strays = []
  const peer = createPeer({
    write: (line) => sent.push(line),
    onStray: (line) => strays.push(line),
    ...options,
  })
  const messages = () => sent.map((line) => JSON.parse(line))
  const settle = () => new Promise((resolve) => setTimeout(resolve, 0))
  return { peer, sent, strays, messages, settle }
}

describe('the extension host wire', () => {
  it('matches the shell on its codes and limits', () => {
    expect([METHOD_NOT_FOUND, FAILED]).toEqual([-32601, -32000])
    expect(MAX_LINE).toBe(40 * 1024 * 1024)
    expect(DEADLINE_MS).toBe(120_000)
  })

  it('answers what it serves, one line per message', async () => {
    const { peer, sent, messages, settle } = rig({
      handlers: { state: async (params) => ({ asked: params }), nothing: () => undefined },
    })
    peer.receive('{"jsonrpc":"2.0","id":1,"method":"state","params":{"a":1}}\n')
    peer.receive('{"jsonrpc":"2.0","id":2,"method":"nothing"}\n')
    await settle()
    expect(sent.every((line) => line.endsWith('\n') && line.indexOf('\n') === line.length - 1)).toBe(true)
    expect(messages()).toEqual([
      { jsonrpc: '2.0', id: 1, result: { asked: { a: 1 } } },
      { jsonrpc: '2.0', id: 2, result: null },
    ])
  })

  it('refuses a method it does not serve, inherited ones included', async () => {
    const { peer, messages, settle } = rig()
    peer.receive('{"jsonrpc":"2.0","id":3,"method":"toString"}\n')
    await settle()
    expect(messages()).toEqual([
      { jsonrpc: '2.0', id: 3, error: { code: METHOD_NOT_FOUND, message: 'No such method: toString' } },
    ])
  })

  it('turns a throw into an error answer', async () => {
    const { peer, messages, settle } = rig({
      handlers: {
        broken: async () => {
          throw new Error('it broke')
        },
        bare: () => {
          throw 'just a string'
        },
        silent: () => {
          throw new Error('')
        },
      },
    })
    peer.receive(
      ['broken', 'bare', 'silent'].map((method, i) => JSON.stringify({ jsonrpc: '2.0', id: i, method })).join('\n') + '\n',
    )
    await settle()
    expect(messages().sort((a, b) => a.id - b.id).map((m) => m.error)).toEqual([
      { code: FAILED, message: 'it broke' },
      { code: FAILED, message: 'just a string' },
      { code: FAILED, message: 'Error' },
    ])
  })

  it('hands notifications on, and drops those nobody listens for', () => {
    const context = vi.fn()
    const { peer, sent, strays } = rig({ notifications: { context } })
    peer.receive('{"jsonrpc":"2.0","method":"context","params":{"serverUrl":"x"}}\n')
    peer.receive('{"jsonrpc":"2.0","method":"other"}\n')
    expect(context).toHaveBeenCalledWith({ serverUrl: 'x' })
    expect(sent).toEqual([])
    expect(strays).toEqual([])
  })

  it('reads lines split anywhere, and skips blank ones', async () => {
    const { peer, messages, settle } = rig({ handlers: { echo: () => 'echo' } })
    peer.receive('{"jsonrpc":"2.0",')
    peer.receive('"id":1,"method":"echo"}\n\n   \n{"jsonrpc":"2.0","id":2,')
    peer.receive('"method":"echo"}\n')
    await settle()
    expect(messages().map((m) => m.id)).toEqual([1, 2])
  })

  it('answers a ping itself, at once, whatever the handlers are doing', () => {
    const { peer, messages } = rig({ handlers: { ping: () => 'never asked', slow: () => new Promise(() => {}) } })
    peer.receive('{"jsonrpc":"2.0","id":1,"method":"slow"}\n{"jsonrpc":"2.0","id":2,"method":"ping"}\n')
    expect(PING).toBe('ping')
    expect(messages()).toEqual([{ jsonrpc: '2.0', id: 2, result: true }])
  })

  it('reports what is not a message', () => {
    const { peer, strays } = rig()
    peer.receive('hello\n{"id":1}\n{"jsonrpc":"2.0","id":99,"result":1}\nnull\n')
    expect(strays).toEqual(['hello', '{"id":1}', '{"jsonrpc":"2.0","id":99,"result":1}', 'null'])
  })

  it('drops a line longer than the limit, skips its tail, and handles the next message', async () => {
    const { peer, strays } = rig()
    const answer = peer.request('vault')
    peer.receive('x'.repeat(MAX_LINE + 1))
    expect(strays).toEqual(['(a line longer than the limit was dropped)'])
    peer.receive('middle_chunk_without_newline')
    peer.receive('tail_of_long_line\n{"jsonrpc":"2.0","id":1,"result":"ok"}\n')
    expect(strays).toEqual(['(a line longer than the limit was dropped)'])
    await expect(answer).resolves.toBe('ok')
  })

  it('drops a line whose UTF-8 byte length exceeds MAX_LINE in a single line', () => {
    const { peer, strays } = rig()
    const chars = '€'.repeat(Math.ceil(MAX_LINE / 3) + 1)
    peer.receive(`${chars}\n`)
    expect(strays).toEqual(['(a line longer than the limit was dropped)'])
  })

  it('asks the shell and resolves with its answer', async () => {
    const { peer, messages } = rig()
    const first = peer.request('vault')
    const second = peer.request('seal', { texts: ['a'] })
    expect(messages()).toEqual([
      { jsonrpc: '2.0', id: 1, method: 'vault', params: {} },
      { jsonrpc: '2.0', id: 2, method: 'seal', params: { texts: ['a'] } },
    ])
    peer.receive('{"jsonrpc":"2.0","id":2,"result":{"ok":true}}\n{"jsonrpc":"2.0","id":1}\n')
    await expect(first).resolves.toBeNull()
    await expect(second).resolves.toEqual({ ok: true })
  })

  it('rejects with the shell s error', async () => {
    const { peer } = rig()
    const refused = peer.request('profileFetch')
    const wordless = peer.request('profileFetch')
    peer.receive('{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"Not this server."}}\n')
    peer.receive('{"jsonrpc":"2.0","id":2,"error":{}}\n')
    await expect(refused).rejects.toThrow('Not this server.')
    await expect(wordless).rejects.toThrow('The shell failed without saying why.')
  })

  it('gives up on an answer that does not come', async () => {
    vi.useFakeTimers()
    try {
      const { peer, strays } = rig({ deadlineMs: 50 })
      const late = peer.request('vault')
      const later = peer.request('open', {}, { timeoutMs: 500 })
      vi.advanceTimersByTime(50)
      await expect(late).rejects.toThrow('The app did not answer "vault" in time.')
      // An answer after the deadline is nobody's.
      peer.receive('{"jsonrpc":"2.0","id":1,"result":1}\n')
      expect(strays).toHaveLength(1)
      vi.advanceTimersByTime(450)
      await expect(later).rejects.toThrow('The app did not answer "open" in time.')
    } finally {
      vi.useRealTimers()
    }
  })

  it('fails everything waiting when the wire closes, and everything after', async () => {
    const { peer, sent } = rig()
    const waiting = peer.request('vault')
    peer.close()
    await expect(waiting).rejects.toThrow('The app closed the extension host.')
    await expect(peer.request('vault')).rejects.toThrow('The app closed the extension host.')
    peer.notify('running', {})
    expect(sent).toHaveLength(1)

    const other = rig().peer
    other.close('gone')
    await expect(other.request('x')).rejects.toThrow('gone')
  })

  it('notifies without an id', () => {
    const { peer, messages } = rig()
    peer.notify('ready', { pid: 1 })
    peer.notify('started')
    expect(messages()).toEqual([
      { jsonrpc: '2.0', method: 'ready', params: { pid: 1 } },
      { jsonrpc: '2.0', method: 'started', params: {} },
    ])
  })

  it('uses the real timers by default', async () => {
    const peer = createPeer({ write: () => {} })
    const waiting = peer.request('x', {}, { timeoutMs: 1 })
    await expect(waiting).rejects.toThrow('in time')
    const answered = peer.request('y')
    peer.receive('{"jsonrpc":"2.0","id":2,"result":"y"}\nnot a message\n')
    await expect(answered).resolves.toBe('y')
  })
})
