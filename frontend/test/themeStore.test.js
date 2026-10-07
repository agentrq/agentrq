// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { describe, it, expect, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { createPinia } from 'pinia'
import { useThemeStore } from '../src/stores/themeStore'

const themeColor = () => document.querySelector('meta[name="theme-color"]').getAttribute('content')

// The status bar takes the colour of what is right under it: the page card
// with the phone menu closed, the menu with it open.
describe('the status bar colour follows the phone menu', () => {
  let store
  beforeEach(() => {
    document.head.innerHTML = '<meta name="theme-color" content="#ffffff"><meta name="apple-mobile-web-app-status-bar-style" content="default">'
    document.body.style.backgroundColor = ''
    store = useThemeStore(createPinia())
  })

  it('is the menu colour while the menu is open in dark mode, and the page colour once it closes', () => {
    store.setTheme('dark')
    expect(themeColor()).toBe('#18181b')
    expect(document.body.style.backgroundColor).toBe('')

    store.setMenuOpen(true)
    expect(themeColor()).toBe('#09090b')
    expect(document.body.style.backgroundColor).toBe('rgb(9, 9, 11)')

    store.setMenuOpen(false)
    expect(themeColor()).toBe('#18181b')
    expect(document.body.style.backgroundColor).toBe('')
  })

  it('stays white in light mode, where the menu and the page are both white', () => {
    store.setTheme('light')
    store.setMenuOpen(true)
    expect(themeColor()).toBe('#ffffff')
    expect(document.body.style.backgroundColor).toBe('')
  })

  it('is told when App.vue opens or closes the menu', () => {
    const app = readFileSync(resolve(__dirname, '../src/App.vue'), 'utf-8')
    expect(app).toContain('watch(isMobileMenuOpen, (open) => themeStore.setMenuOpen(open))')
  })
})
