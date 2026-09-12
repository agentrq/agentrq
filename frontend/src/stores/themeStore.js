// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.

import { defineStore } from 'pinia'

function getStoredTheme() {
  try {
    return typeof localStorage !== 'undefined' ? localStorage?.getItem?.('theme') : null
  } catch {
    return null
  }
}

export const useThemeStore = defineStore('theme', {
  state: () => ({
    theme: getStoredTheme() || 'system'
  }),
  getters: {
    isDark(state) {
      if (state.theme === 'dark') return true
      if (state.theme === 'light') return false
      if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
        return window.matchMedia('(prefers-color-scheme: dark)').matches
      }
      return false
    }
  },
  actions: {
    setTheme(newTheme) {
      this.theme = newTheme
      try {
        if (typeof localStorage !== 'undefined') {
          localStorage?.setItem?.('theme', newTheme)
        }
      } catch {
        // ignore storage errors
      }
      this.applyTheme()
    },
    applyTheme() {
      const prefersDark = typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia('(prefers-color-scheme: dark)').matches
      const isDark = this.theme === 'dark' || (this.theme === 'system' && prefersDark)

      if (typeof document !== 'undefined') {
        if (isDark) {
          document.documentElement.classList.add('dark')
        } else {
          document.documentElement.classList.remove('dark')
        }

        const themeColorMeta = document.querySelector('meta[name="theme-color"]')
        if (themeColorMeta) themeColorMeta.setAttribute('content', isDark ? '#09090b' : '#f4f4f5')

        const statusBarMeta = document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]')
        if (statusBarMeta) statusBarMeta.setAttribute('content', isDark ? 'black' : 'default')
      }
    },
    init() {
      this.applyTheme()
      if (typeof window !== 'undefined' && typeof window.matchMedia === 'function') {
        window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
          if (this.theme === 'system') {
            this.applyTheme()
          }
        })
      }
    }
  }
})
