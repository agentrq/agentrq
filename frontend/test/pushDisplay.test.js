// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'

import { requiresNotification, shouldShowPush, urlOnScreen } from '../src/composables/usePushDisplay'

const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 26_1 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.1 Mobile/15E148 Safari/604.1'
const MAC_SAFARI = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/26.1 Safari/605.1.15'
const CHROME = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Safari/537.36'
const ANDROID = 'Mozilla/5.0 (Linux; Android 16) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/142.0.0.0 Mobile Safari/537.36'
const FIREFOX = 'Mozilla/5.0 (X11; Linux x86_64; rv:145.0) Gecko/20100101 Firefox/145.0'

const TASK = '/workspaces/1/tasks/7'

const win = (url, { focused = true, visibilityState = 'visible' } = {}) => ({ url, focused, visibilityState })
const clientsWith = (windows) => ({ matchAll: vi.fn(async () => windows) })

describe('requiresNotification', () => {
  it('is true for Safari\'s engine, which cancels a subscription after silent pushes', () => {
    expect(requiresNotification(IPHONE)).toBe(true)
    expect(requiresNotification(MAC_SAFARI)).toBe(true)
  })

  it('is false for Chromium and Firefox', () => {
    expect(requiresNotification(CHROME)).toBe(false)
    expect(requiresNotification(ANDROID)).toBe(false)
    expect(requiresNotification(FIREFOX)).toBe(false)
  })

  it('is false with no user agent', () => {
    expect(requiresNotification()).toBe(false)
  })
})

describe('urlOnScreen', () => {
  it('finds a focused, visible window on the page, ignoring its query', async () => {
    const clients = clientsWith([win('https://h/workspaces/2'), win(`https://h${TASK}?tab=chat`)])

    expect(await urlOnScreen(clients, TASK)).toBe(true)
    expect(clients.matchAll).toHaveBeenCalledWith({ type: 'window', includeUncontrolled: true })
  })

  it('does not count a window in the background, or one without focus', async () => {
    expect(await urlOnScreen(clientsWith([win(`https://h${TASK}`, { visibilityState: 'hidden' })]), TASK)).toBe(false)
    expect(await urlOnScreen(clientsWith([win(`https://h${TASK}`, { focused: false })]), TASK)).toBe(false)
  })

  it('does not count another task', async () => {
    expect(await urlOnScreen(clientsWith([win('https://h/workspaces/1/tasks/8')]), TASK)).toBe(false)
  })
})

describe('shouldShowPush', () => {
  const onTask = () => clientsWith([win(`https://h${TASK}`)])

  it('skips the notification when its task is in front of the person', async () => {
    expect(await shouldShowPush(onTask(), TASK, CHROME)).toBe(false)
  })

  it('shows it when the task is not on screen', async () => {
    expect(await shouldShowPush(clientsWith([]), TASK, CHROME)).toBe(true)
  })

  it('always shows it on Safari\'s engine, without looking at the windows', async () => {
    const clients = onTask()

    expect(await shouldShowPush(clients, TASK, IPHONE)).toBe(true)
    expect(clients.matchAll).not.toHaveBeenCalled()
  })

  it('always shows a push that names no page', async () => {
    expect(await shouldShowPush(onTask(), undefined, CHROME)).toBe(true)
  })
})
