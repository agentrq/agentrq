// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { describe, it, expect, vi } from 'vitest'
import { readFileSync, existsSync } from 'node:fs'
import { resolve } from 'node:path'
import { isAppleMobile, isHomeScreenApp, needsHomeScreen, offerHomeScreen, HOME_SCREEN_HINT } from '../src/utils/iosHomeScreen'

const IPHONE = 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1'
const MAC = 'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Safari/605.1.15'

function memoryStorage() {
  const data = {}
  return { getItem: (k) => data[k] ?? null, setItem: (k, v) => { data[k] = String(v) } }
}

function fakeWindow({ userAgent = IPHONE, platform = 'iPhone', maxTouchPoints = 5, standalone, displayStandalone = false, matchMedia = true, storage = memoryStorage() } = {}) {
  return {
    navigator: { userAgent, platform, maxTouchPoints, standalone },
    matchMedia: matchMedia ? (q) => ({ matches: q === '(display-mode: standalone)' && displayStandalone }) : undefined,
    localStorage: storage,
  }
}

describe('isAppleMobile', () => {
  it('knows an iPhone by its user agent', () => {
    expect(isAppleMobile({ userAgent: IPHONE, platform: 'iPhone', maxTouchPoints: 5 })).toBe(true)
  })
  it('knows an iPad asking for the desktop site by its touch screen', () => {
    expect(isAppleMobile({ userAgent: MAC, platform: 'MacIntel', maxTouchPoints: 5 })).toBe(true)
  })
  it('leaves a Mac alone', () => {
    expect(isAppleMobile({ userAgent: MAC, platform: 'MacIntel', maxTouchPoints: 0 })).toBe(false)
  })
})

describe('isHomeScreenApp', () => {
  it("takes Safari's navigator.standalone", () => {
    expect(isHomeScreenApp(fakeWindow({ standalone: true }))).toBe(true)
  })
  it('takes the display-mode media query', () => {
    expect(isHomeScreenApp(fakeWindow({ displayStandalone: true }))).toBe(true)
  })
  it('is a browser tab otherwise, with or without matchMedia', () => {
    expect(isHomeScreenApp(fakeWindow())).toBe(false)
    expect(isHomeScreenApp(fakeWindow({ matchMedia: false }))).toBe(false)
  })
})

describe('needsHomeScreen', () => {
  it('is true for iOS Safari in a tab only', () => {
    expect(needsHomeScreen(fakeWindow())).toBe(true)
    expect(needsHomeScreen(fakeWindow({ standalone: true }))).toBe(false)
    expect(needsHomeScreen(fakeWindow({ userAgent: MAC, platform: 'MacIntel', maxTouchPoints: 0 }))).toBe(false)
  })
  it('reads the real window by default', () => {
    expect(needsHomeScreen()).toBe(false)
  })
})

describe('offerHomeScreen', () => {
  it('shows the hint once per browser', () => {
    const win = fakeWindow()
    const notify = vi.fn()
    expect(offerHomeScreen(notify, win)).toBe(true)
    expect(offerHomeScreen(notify, win)).toBe(false)
    expect(notify).toHaveBeenCalledTimes(1)
    expect(notify).toHaveBeenCalledWith(HOME_SCREEN_HINT)
  })
  it('says nothing where there is nothing to install', () => {
    const notify = vi.fn()
    expect(offerHomeScreen(notify, fakeWindow({ standalone: true }))).toBe(false)
    expect(offerHomeScreen(notify)).toBe(false)
    expect(notify).not.toHaveBeenCalled()
  })
  it('says nothing when storage is blocked, rather than on every load', () => {
    const notify = vi.fn()
    const storage = { getItem: () => { throw new Error('blocked') }, setItem: () => {} }
    expect(offerHomeScreen(notify, fakeWindow({ storage }))).toBe(false)
    expect(notify).not.toHaveBeenCalled()
  })
})

// The page runs under the notch and home indicator only with viewport-fit=cover
// (the shell pads itself out of both), and iOS wants a 180px opaque icon.
describe('index.html on iOS', () => {
  const html = readFileSync(resolve(__dirname, '../index.html'), 'utf-8')
  it('covers the whole screen', () => {
    expect(html).toMatch(/name="viewport"[^>]*viewport-fit=cover/)
  })
  it('names an apple-touch-icon that exists', () => {
    const href = html.match(/rel="apple-touch-icon" href="\/([^"]+)"/)[1]
    expect(existsSync(resolve(__dirname, '../public', href))).toBe(true)
  })
})
