// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { JSDOM } from 'jsdom'

const html = readFileSync(resolve(__dirname, '../index.html'), 'utf-8')

// Load index.html as iOS would, before any app code runs, with the given saved
// theme and OS preference.
function load(theme, osDark = false) {
  const { window } = new JSDOM(html, {
    url: 'http://localhost/',
    runScripts: 'dangerously',
    beforeParse(w) {
      if (theme) w.localStorage.setItem('theme', theme)
      w.matchMedia = (q) => ({ matches: osDark && q === '(prefers-color-scheme: dark)' })
    },
  })
  const doc = window.document
  return {
    dark: doc.documentElement.classList.contains('dark'),
    themeColor: doc.querySelector('meta[name="theme-color"]').getAttribute('content'),
    statusBar: doc.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]').getAttribute('content'),
    body: doc.body.className,
  }
}

describe('index.html applies the saved theme before first paint', () => {
  it('goes dark for a saved dark theme', () => {
    expect(load('dark')).toMatchObject({ dark: true, themeColor: '#09090b', statusBar: 'black' })
  })

  it('follows a dark OS when the theme is system or unset', () => {
    expect(load('system', true).dark).toBe(true)
    expect(load(null, true).dark).toBe(true)
  })

  it('stays light for a saved light theme, even on a dark OS', () => {
    expect(load('light', true)).toMatchObject({ dark: false, themeColor: '#ffffff', statusBar: 'default' })
  })

  it('stays light when there is no matchMedia or storage', () => {
    const { window } = new JSDOM(html, { runScripts: 'dangerously' })
    expect(window.document.documentElement.classList.contains('dark')).toBe(false)
  })

  it('gives the body the app shell background in both themes', () => {
    // iOS tints the status bar from this, so a light-only class shows as a
    // silver strip above the dark app.
    expect(load('dark').body).toContain('md:bg-zinc-100')
    expect(load('dark').body).toContain('dark:bg-zinc-950')
  })

  it('is white on a phone in light mode, like the page under the status bar', () => {
    // The strip is set once at launch and the page below it is a white card
    // with the menu closed, so the shell (and the open menu) must be white too.
    expect(load('light').body).toMatch(/(^| )bg-white( |$)/)
    const app = readFileSync(resolve(__dirname, '../src/App.vue'), 'utf-8')
    for (const el of ['id="app"', '<nav v-if="!isLoginPage"', '<main v-else']) {
      const tag = app.slice(app.indexOf(el), app.indexOf('>', app.indexOf(el)))
      expect(tag, el).toContain('bg-white md:bg-zinc-100 dark:bg-zinc-950')
    }
  })
})
