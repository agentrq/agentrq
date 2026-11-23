// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

import { canInstall, captureInstallPrompt, promptInstall } from '../src/utils/installPrompt'

function fakeWindow({ standalone = false } = {}) {
  const target = new EventTarget()
  target.navigator = {}
  target.matchMedia = () => ({ matches: standalone })
  return target
}

function installEvent() {
  const event = new Event('beforeinstallprompt', { cancelable: true })
  event.prompt = vi.fn(async () => {})
  return event
}

describe('installPrompt', () => {
  let win
  beforeEach(() => {
    win = fakeWindow()
    captureInstallPrompt(win)
    win.dispatchEvent(new Event('appinstalled'))
  })

  it('keeps the browser prompt for the menu instead of letting it show', () => {
    const event = installEvent()
    win.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(true)
    expect(canInstall.value).toBe(true)
  })

  it('shows the prompt once, then hides the menu item', async () => {
    const event = installEvent()
    win.dispatchEvent(event)
    await promptInstall()
    expect(event.prompt).toHaveBeenCalledTimes(1)
    expect(canInstall.value).toBe(false)
    await promptInstall()
    expect(event.prompt).toHaveBeenCalledTimes(1)
  })

  it('hides the menu item once the app is installed', () => {
    win.dispatchEvent(installEvent())
    win.dispatchEvent(new Event('appinstalled'))
    expect(canInstall.value).toBe(false)
  })

  it('listens for nothing inside the installed app', () => {
    const installed = fakeWindow({ standalone: true })
    captureInstallPrompt(installed)
    const event = installEvent()
    installed.dispatchEvent(event)
    expect(event.defaultPrevented).toBe(false)
    expect(canInstall.value).toBe(false)
  })

  it('is offered only in the web build, from the account menu', () => {
    const app = readFileSync(resolve(__dirname, '../src/App.vue'), 'utf-8')
    expect(app).toMatch(/v-if="platformStore\.isWeb && canInstall" @click="installApp"/)
    const main = readFileSync(resolve(__dirname, '../src/main.js'), 'utf-8')
    expect(main).toMatch(/captureInstallPrompt\(\)\n[\s\S]*app\.mount/)
  })
})
