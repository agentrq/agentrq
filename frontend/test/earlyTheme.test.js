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

  it('is one colour on a phone, under the status bar, menu open or closed', () => {
    // The strip is set once at launch, so the page under it with the menu
    // closed and the open menu must be one colour: white in light mode, the
    // shell's zinc-950 in dark mode, where the page card is zinc-900 only on
    // wider screens.
    expect(load('light').body).toMatch(/(^| )bg-white( |$)/)
    const app = readFileSync(resolve(__dirname, '../src/App.vue'), 'utf-8')
    for (const el of ['id="app"', '<nav v-if="!isLoginPage"', '<main v-else']) {
      const tag = app.slice(app.indexOf(el), app.indexOf('>', app.indexOf(el)))
      expect(tag, el).toContain('bg-white md:bg-zinc-100 dark:bg-zinc-950')
    }
    const css = readFileSync(resolve(__dirname, '../src/style.css'), 'utf-8')
    const surface = css.slice(css.indexOf('@utility page-surface'), css.indexOf('\n}', css.indexOf('@utility page-surface')))
    expect(surface).toContain('background-color: var(--color-white)')
    expect(surface).toContain('@variant dark { background-color: var(--color-zinc-950); }')
    expect(surface).toContain('@variant md { @variant dark { background-color: var(--color-zinc-900); } }')
    const card = app.slice(app.lastIndexOf('<div', app.indexOf('scroll-smooth')), app.indexOf('>', app.indexOf('scroll-smooth')))
    expect(card).toContain('page-surface')
    // Pages that paint their own background, and their sticky headers, use
    // the card's colour too, or they show as a band on a phone.
    for (const view of ['TaskDetailView', 'TaskFormView', 'WorkspaceFormView', 'WorkspaceAnalyticsView']) {
      const src = readFileSync(resolve(__dirname, `../src/views/${view}.vue`), 'utf-8')
      const root = src.slice(src.indexOf('<div', src.indexOf('<template>')), src.indexOf('>', src.indexOf('<div', src.indexOf('<template>'))))
      expect(root, view).toContain('page-surface')
      expect(root, view).not.toContain('dark:bg-zinc-900')
    }
    for (const view of ['WorkspaceFormView', 'TaskFormView']) {
      const src = readFileSync(resolve(__dirname, `../src/views/${view}.vue`), 'utf-8')
      expect(src, view).toContain('page-surface sticky')
    }
  })
})
