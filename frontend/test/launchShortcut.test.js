// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

// @vitest-environment node

import { describe, it, expect, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { followLaunchShortcut } from '../src/utils/launchShortcut'
import { manifest } from '../vite.config.js'

function fakeRouter() {
  const calls = []
  return {
    calls,
    replace: vi.fn(async (to) => { calls.push(['replace', to]) }),
  }
}

const route = (query) => ({ path: '/', query, hash: '' })

describe('followLaunchShortcut', () => {
  it('does nothing without a shortcut in the query', async () => {
    const router = fakeRouter()
    const actions = { 'new-task': vi.fn() }
    expect(await followLaunchShortcut(router, route({ filter: 'x' }), actions)).toBe(false)
    expect(router.replace).not.toHaveBeenCalled()
    expect(actions['new-task']).not.toHaveBeenCalled()
  })

  it('drops the query before running the action', async () => {
    const router = fakeRouter()
    const actions = { 'new-task': vi.fn(async () => { router.calls.push(['run']) }) }
    const ran = await followLaunchShortcut(router, { path: '/kanban', query: { shortcut: 'new-task', a: '1' }, hash: '#h' }, actions)
    expect(ran).toBe(true)
    expect(router.calls).toEqual([['replace', { path: '/kanban', query: { a: '1' }, hash: '#h' }], ['run']])
  })

  it('drops an unknown or repeated shortcut and runs nothing', async () => {
    for (const shortcut of ['nope', ['new-task', 'new-task'], 'toString']) {
      const router = fakeRouter()
      const actions = { 'new-task': vi.fn() }
      expect(await followLaunchShortcut(router, route({ shortcut }), actions)).toBe(false)
      expect(router.replace).toHaveBeenCalledWith({ path: '/', query: {}, hash: '' })
      expect(actions['new-task']).not.toHaveBeenCalled()
    }
  })
})

describe('the manifest shortcuts', () => {
  const app = readFileSync(resolve(__dirname, '../src/App.vue'), 'utf-8')
  const start = app.indexOf('followLaunchShortcut(router')
  const launchBlock = app.slice(start, app.indexOf('{ immediate: true }', start))

  it('each open a shortcut the shell handles, where the app is served', () => {
    expect(manifest.shortcuts.map((s) => s.name)).toEqual(['New task', 'Find task'])
    for (const { url } of manifest.shortcuts) {
      const name = new URL(url, 'https://h/agentrq/manifest.webmanifest').searchParams.get('shortcut')
      expect(new URL(url, 'https://h/agentrq/manifest.webmanifest').pathname).toBe('/agentrq/')
      expect(launchBlock).toContain(`'${name}': `)
    }
  })

  it('are followed by the shell on every route change', () => {
    expect(app).toMatch(/watch\(\n  \(\) => route\.query\.shortcut,\n  \(\) => followLaunchShortcut\(router, route,/)
  })
})
