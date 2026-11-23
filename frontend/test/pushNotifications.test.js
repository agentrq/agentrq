// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('../src/api', () => ({ API_BASE_URL: '/api/v1' }))

import { usePushNotifications } from '../src/composables/usePushNotifications'

// ready never settles, as on a page with no service worker: nothing below may wait on it.
function worker(registration) {
  Object.defineProperty(navigator, 'serviceWorker', {
    value: { ready: new Promise(() => {}), getRegistration: vi.fn().mockResolvedValue(registration) },
    configurable: true,
  })
}

let fetchSpy

beforeEach(() => {
  vi.useFakeTimers()
  window.PushManager = function PushManager() {}
  fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue({ ok: true, json: () => Promise.resolve({ subscribed: true }) })
})

afterEach(() => {
  vi.useRealTimers()
  fetchSpy.mockRestore()
  delete window.PushManager
  delete navigator.serviceWorker
})

describe('usePushNotifications on sign-out', () => {
  it('returns at once when no service worker is registered', async () => {
    worker(undefined)
    await usePushNotifications().unsubscribe()
    expect(fetchSpy).not.toHaveBeenCalled()
  })

  it('deletes the subscription on the server without waiting for the push service', async () => {
    const sub = { endpoint: 'https://push.example/1', unsubscribe: vi.fn(() => new Promise(() => {})) }
    worker({ pushManager: { getSubscription: () => Promise.resolve(sub) } })
    const push = usePushNotifications()
    push.isSubscribed.value = true

    await push.unsubscribe()

    expect(fetchSpy).toHaveBeenCalledWith('/api/v1/push/subscribe', expect.objectContaining({
      method: 'DELETE',
      body: JSON.stringify({ endpoint: sub.endpoint }),
    }))
    expect(sub.unsubscribe).toHaveBeenCalled()
    expect(push.isSubscribed.value).toBe(false)
  })

  it('ignores a push service that refuses the unsubscribe', async () => {
    const sub = { endpoint: 'e', unsubscribe: vi.fn().mockRejectedValue(new Error('gone')) }
    worker({ pushManager: { getSubscription: () => Promise.resolve(sub) } })
    await expect(usePushNotifications().unsubscribe()).resolves.toBeUndefined()
  })

  it('does nothing in a browser without push', async () => {
    worker(undefined)
    delete window.PushManager
    await usePushNotifications().unsubscribe()
    expect(navigator.serviceWorker.getRegistration).not.toHaveBeenCalled()
  })

  it('returns no subscription when the registration lookup fails', async () => {
    Object.defineProperty(navigator, 'serviceWorker', {
      value: { getRegistration: vi.fn().mockRejectedValue(new Error('denied')) },
      configurable: true,
    })
    await expect(usePushNotifications().checkWorkspaceSubscription('ws1')).resolves.toBe(false)
  })

  it('reads a workspace subscription through the registration', async () => {
    worker({ pushManager: { getSubscription: () => Promise.resolve({ endpoint: 'e' }) } })
    await expect(usePushNotifications().checkWorkspaceSubscription('ws1')).resolves.toBe(true)
  })
})
