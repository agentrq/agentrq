import { defineStore } from 'pinia'

export const useThemeStore = defineStore('theme', {
  state: () => ({
    theme: localStorage.getItem('theme') || 'system'
  }),
  actions: {
    setTheme(newTheme) {
      this.theme = newTheme
      localStorage.setItem('theme', newTheme)
      this.applyTheme()
    },
    applyTheme() {
      const isDark = this.theme === 'dark' || (this.theme === 'system' && window.matchMedia('(prefers-color-scheme: dark)').matches)

      if (isDark) {
        document.documentElement.classList.add('dark')
      } else {
        document.documentElement.classList.remove('dark')
      }

      const themeColorMeta = document.querySelector('meta[name="theme-color"]')
      if (themeColorMeta) themeColorMeta.setAttribute('content', isDark ? '#09090b' : '#f4f4f5')

      const statusBarMeta = document.querySelector('meta[name="apple-mobile-web-app-status-bar-style"]')
      if (statusBarMeta) statusBarMeta.setAttribute('content', isDark ? 'black' : 'default')
    },
    init() {
      this.applyTheme()
      window.matchMedia('(prefers-color-scheme: dark)').addEventListener('change', () => {
        if (this.theme === 'system') {
          this.applyTheme()
        }
      })
    }
  }
})
