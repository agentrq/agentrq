// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeAll, describe, expect, it, vi } from 'vitest'

const invoke = vi.fn(() => Promise.reject(new Error('connection_get is not implemented yet (WP4)')))
vi.mock('@tauri-apps/api/core', () => ({ invoke }))

let bridge
beforeAll(async () => {
  globalThis.window = {}
  bridge = await import('../src/bridge/index.js')
})

describe('platformOf', () => {
  it('names the OS the way process.platform does', () => {
    expect(bridge.platformOf('Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit')).toBe('darwin')
    expect(bridge.platformOf('Mozilla/5.0 (Windows NT 10.0; Win64; x64) Edg/141')).toBe('win32')
    expect(bridge.platformOf('Mozilla/5.0 (X11; Linux x86_64) AppleWebKit')).toBe('linux')
    expect(bridge.platformOf(undefined)).toBe('linux')
  })
})

describe('the stand-in window.agentrq', () => {
  it('is installed on window when the module loads', () => {
    expect(window.agentrq.isDesktop).toBe(true)
    expect(window.agentrq.platform).toBe(bridge.platformOf(navigator.userAgent))
  })

  it('asks Rust for the connection, so the renderer falls back to the connection screen', async () => {
    const agentrq = bridge.createBridge('X11; Linux')
    await expect(agentrq.connection.get()).rejects.toThrow('not implemented yet')
    expect(invoke).toHaveBeenCalledWith('connection_get')
  })

  it('rejects every other call, naming the member', async () => {
    const agentrq = bridge.createBridge('Macintosh')
    await expect(agentrq.connection.save('https://x')).rejects.toThrow('window.agentrq.connection.save is not implemented yet')
    await expect(agentrq.profiles.get()).rejects.toThrow('window.agentrq.profiles.get is not implemented yet')
  })

  it('gives every on* listener an unsubscribe function', () => {
    const agentrq = bridge.createBridge('Windows')
    const off = agentrq.navigation.onNavigate(() => {})
    expect(typeof off).toBe('function')
    expect(off()).toBeUndefined()
  })

  it('is never a thenable, so awaiting it does not hang', async () => {
    const agentrq = bridge.createBridge('')
    expect(agentrq.then).toBeUndefined()
    expect(agentrq.updates.then).toBeUndefined()
    expect(agentrq[Symbol.iterator]).toBeUndefined()
    expect(agentrq.updates[Symbol.toPrimitive]).toBeUndefined()
    expect(typeof (await agentrq.theme).set).toBe('function')
  })

  it('carries the static members desktop reads', () => {
    const agentrq = bridge.createBridge('Macintosh')
    expect(agentrq.versions).toEqual({ tauri: '', webview: 'Macintosh' })
    expect(bridge.createBridge(undefined).versions.webview).toBe('')
  })
})
