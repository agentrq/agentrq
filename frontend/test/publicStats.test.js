// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The login page's public totals: folding a `/pub/stats` event into the five
 * figures, formatting them as the landing page does, and the page itself
 * showing them only once the stream has sent some.
 */

import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { createApp, h, nextTick } from 'vue'
import { publicStats, formatCount, followPublicStats, PUBLIC_STATS } from '../src/utils/publicStats'

vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ push: vi.fn() }),
}))

class FakeEventSource {
  static instances = []
  constructor(url) {
    this.url = url
    this.closed = false
    FakeEventSource.instances.push(this)
  }
  close() { this.closed = true }
  send(data) { this.onmessage({ data }) }
}

beforeEach(() => {
  FakeEventSource.instances = []
  vi.stubGlobal('EventSource', FakeEventSource)
})
afterEach(() => vi.unstubAllGlobals())

describe('publicStats', () => {
  it('maps actions to the figures and sums the three kinds of approval', () => {
    expect(publicStats([
      { action: 'task_create', total: 10 },
      { action: 'user_create', total: 2 },
      { action: 'message_create', total: 30 },
      { action: 'mcp_tool_call', total: 40 },
      { action: 'mcp_permission_manual', total: 1 },
      { action: 'mcp_permission_auto', total: 2 },
      { action: 'task_approve_manual', total: 3 },
      { action: 'workspace_create', total: 99 },
    ])).toEqual({ tasks: 10, users: 2, messages: 30, mcpCalls: 40, approvals: 6 })
  })

  it('reads an action the server has never counted as zero', () => {
    expect(publicStats([])).toEqual({ tasks: 0, users: 0, messages: 0, mcpCalls: 0, approvals: 0 })
  })

  it('names a figure for every key it returns', () => {
    expect(PUBLIC_STATS.map((s) => s.key)).toEqual(Object.keys(publicStats([])))
  })
})

describe('formatCount', () => {
  it.each([
    [0, '0'],
    [999, '999'],
    [1000, '1K'],
    [1234, '1.2K'],
    [999949, '999.9K'],
    [2000000, '2M'],
    [3450000, '3.5M'],
  ])('%i → %s', (n, s) => expect(formatCount(n)).toBe(s))
})

describe('followPublicStats', () => {
  it('follows the same-origin stream and reports each event', () => {
    const seen = []
    const stop = followPublicStats((s) => seen.push(s))
    const es = FakeEventSource.instances[0]
    expect(es.url).toBe('/pub/stats')

    es.send(JSON.stringify([{ action: 'task_create', total: 5 }]))
    expect(seen).toEqual([{ tasks: 5, users: 0, messages: 0, mcpCalls: 0, approvals: 0 }])

    stop()
    expect(es.closed).toBe(true)
  })

  it('ignores an event that is not JSON', () => {
    const onStats = vi.fn()
    followPublicStats(onStats)
    FakeEventSource.instances[0].send('not json')
    expect(onStats).not.toHaveBeenCalled()
  })
})

describe('LoginView', () => {
  it('shows the totals once the stream sends them, and stops following when it goes', async () => {
    vi.stubGlobal('fetch', () => Promise.resolve({ json: () => Promise.resolve({}) }))
    const { default: LoginView } = await import('../src/views/LoginView.vue')
    const el = document.createElement('div')
    document.body.appendChild(el)
    const app = createApp({ render: () => h(LoginView) })
    app.mount(el)
    try {
      expect(el.querySelector('dl')).toBeNull()

      const es = FakeEventSource.instances[0]
      es.send(JSON.stringify([{ action: 'task_create', total: 12345 }, { action: 'user_create', total: 7 }]))
      await nextTick()

      // One list beside the form on wide screens, a small one under it on phones.
      const lists = el.querySelectorAll('dl')
      expect(lists).toHaveLength(2)
      expect(lists[0].textContent).toContain('12.3K')
      expect(lists[0].textContent).toContain('Tasks Created')
      expect(lists[1].textContent).toContain('Users')
    } finally {
      app.unmount()
      el.remove()
    }
    expect(FakeEventSource.instances[0].closed).toBe(true)
  })
})
