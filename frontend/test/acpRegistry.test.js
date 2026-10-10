// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it, vi } from 'vitest'
import {
  ACP_REGISTRY_REFRESH_MS,
  loadAcpRegistry,
  resetAcpRegistry,
  startAcpRegistry,
  useAcpRegistry,
} from '../src/composables/useAcpRegistry.js'

const CODEX = { id: 'codex-acp', name: 'Codex', description: 'OpenAI', runtimes: ['npx'] }
const GEMINI = { id: 'gemini', name: 'Gemini CLI', description: 'Google', runtimes: ['npx'] }

afterEach(() => {
  resetAcpRegistry()
})

describe('loadAcpRegistry', () => {
  it('holds the agents the server answers', async () => {
    const got = await loadAcpRegistry(() => Promise.resolve({ agents: [CODEX, GEMINI] }))
    expect(got).toEqual([CODEX, GEMINI])
    expect(useAcpRegistry().agents.value).toEqual([CODEX, GEMINI])
  })

  it('keeps the last good list when a reload answers nothing or fails', async () => {
    await loadAcpRegistry(() => Promise.resolve({ agents: [CODEX] }))
    await loadAcpRegistry(() => Promise.resolve({ agents: [] }))
    await loadAcpRegistry(() => Promise.resolve(null))
    await loadAcpRegistry(() => Promise.reject(new Error('network down')))
    expect(useAcpRegistry().agents.value).toEqual([CODEX])
  })

  it('shares one request between concurrent loads', async () => {
    let resolve
    const fetchAgents = vi.fn(() => new Promise((r) => { resolve = r }))
    const a = loadAcpRegistry(fetchAgents)
    const b = loadAcpRegistry(fetchAgents)
    resolve({ agents: [GEMINI] })
    expect(await a).toEqual([GEMINI])
    expect(await b).toEqual([GEMINI])
    expect(fetchAgents).toHaveBeenCalledTimes(1)

    const c = loadAcpRegistry(fetchAgents)
    expect(fetchAgents).toHaveBeenCalledTimes(2)
    resolve({ agents: [CODEX] })
    expect(await c).toEqual([CODEX])
  })

  it('asks the server by default', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(JSON.stringify({ agents: [CODEX] }), { status: 200 })
    )
    try {
      expect(await loadAcpRegistry()).toEqual([CODEX])
      expect(String(fetchSpy.mock.calls[0][0])).toMatch(/\/acp-registry\/agents$/)
    } finally {
      fetchSpy.mockRestore()
    }
  })
})

describe('startAcpRegistry', () => {
  it('loads now, again every hour, and stops when told', async () => {
    const fetchAgents = vi.fn(() => Promise.resolve({ agents: [CODEX] }))
    let tick
    const setInterval = vi.fn((fn) => { tick = fn; return 7 })
    const clearInterval = vi.fn()

    const stop = startAcpRegistry({ fetchAgents, setInterval, clearInterval })
    expect(fetchAgents).toHaveBeenCalledTimes(1)
    expect(setInterval).toHaveBeenCalledWith(expect.any(Function), ACP_REGISTRY_REFRESH_MS)
    expect(ACP_REGISTRY_REFRESH_MS).toBe(60 * 60 * 1000)
    await Promise.resolve()
    await Promise.resolve()

    tick()
    expect(fetchAgents).toHaveBeenCalledTimes(2)

    stop()
    expect(clearInterval).toHaveBeenCalledWith(7)
  })

  it('uses the real timers by default', async () => {
    vi.useFakeTimers()
    const fetchAgents = vi.fn(() => Promise.resolve({ agents: [CODEX] }))
    try {
      const stop = startAcpRegistry({ fetchAgents })
      await vi.advanceTimersByTimeAsync(ACP_REGISTRY_REFRESH_MS)
      expect(fetchAgents).toHaveBeenCalledTimes(2)
      stop()
      await vi.advanceTimersByTimeAsync(ACP_REGISTRY_REFRESH_MS)
      expect(fetchAgents).toHaveBeenCalledTimes(2)
    } finally {
      vi.useRealTimers()
    }
  })

  it('needs no arguments', () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{"agents":[]}'))
    try {
      const stop = startAcpRegistry()
      expect(fetchSpy).toHaveBeenCalledTimes(1)
      stop()
    } finally {
      fetchSpy.mockRestore()
    }
  })
})

describe('useAcpRegistry', () => {
  it('cannot be written to by a form', async () => {
    await loadAcpRegistry(() => Promise.resolve({ agents: [CODEX] }))
    const { agents } = useAcpRegistry()
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    agents.value = []
    warn.mockRestore()
    expect(useAcpRegistry().agents.value).toEqual([CODEX])
  })
})
