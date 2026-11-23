// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'

import {
  OPEN_NOTIFICATION_URL,
  followNotificationClicks,
  openNotificationUrl,
  routeForUrl,
} from '../src/composables/useNotificationClick'

function windowAt(url) {
  return { url, postMessage: vi.fn(), focus: vi.fn(async function () { return this }) }
}

function clientsWith(windows) {
  return {
    matchAll: vi.fn(async () => windows),
    openWindow: vi.fn(async (url) => ({ opened: url })),
  }
}

describe('openNotificationUrl', () => {
  it('sends an open window on another page to the task, without a second window', async () => {
    const open = windowAt('https://h/workspaces/1/tasks')
    const clients = clientsWith([open])

    expect(await openNotificationUrl(clients, '/workspaces/1/tasks/7')).toBe(open)

    expect(clients.matchAll).toHaveBeenCalledWith({ type: 'window', includeUncontrolled: true })
    expect(open.postMessage).toHaveBeenCalledWith({ type: OPEN_NOTIFICATION_URL, url: '/workspaces/1/tasks/7' })
    expect(open.focus).toHaveBeenCalled()
    expect(clients.openWindow).not.toHaveBeenCalled()
  })

  it('prefers the window already showing the task', async () => {
    const other = windowAt('https://h/workspaces/2')
    const onTask = windowAt('https://h/workspaces/1/tasks/7')
    const clients = clientsWith([other, onTask])

    expect(await openNotificationUrl(clients, '/workspaces/1/tasks/7')).toBe(onTask)
    expect(other.focus).not.toHaveBeenCalled()
    expect(other.postMessage).not.toHaveBeenCalled()
  })

  it('opens a window only when none is open', async () => {
    const clients = clientsWith([])

    expect(await openNotificationUrl(clients, '/workspaces/1')).toEqual({ opened: '/workspaces/1' })
  })
})

describe('routeForUrl', () => {
  it('leaves a URL alone with no base path', () => {
    expect(routeForUrl('/workspaces/1')).toBe('/workspaces/1')
  })

  it('strips the base path the worker URLs carry', () => {
    expect(routeForUrl('/app/workspaces/1', '/app')).toBe('/workspaces/1')
    expect(routeForUrl('/app', '/app')).toBe('/')
  })

  it('leaves a URL outside the base path alone', () => {
    expect(routeForUrl('/apple/x', '/app')).toBe('/apple/x')
  })
})

describe('followNotificationClicks', () => {
  function setup(base = '') {
    const target = new EventTarget()
    const router = { push: vi.fn(), options: { history: { base } } }
    followNotificationClicks(router, target)
    const send = (data) => target.dispatchEvent(new MessageEvent('message', { data }))
    return { router, send }
  }

  it('moves the router to the URL, under the base path', () => {
    const { router, send } = setup('/app')

    send({ type: OPEN_NOTIFICATION_URL, url: '/app/workspaces/1/tasks/7' })

    expect(router.push).toHaveBeenCalledWith('/workspaces/1/tasks/7')
  })

  it("ignores the worker's other messages", () => {
    const { router, send } = setup()

    send({ type: 'PRECACHE_PROGRESS' })
    send(null)

    expect(router.push).not.toHaveBeenCalled()
  })

  it('does nothing without a service worker', () => {
    vi.stubGlobal('navigator', undefined)
    try {
      expect(() => followNotificationClicks({ push: vi.fn() })).not.toThrow()
    } finally {
      vi.unstubAllGlobals()
    }
  })
})
