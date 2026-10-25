// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// CI's window-chrome harness, run in the main webview through
// AGENTRQ_E2E_INIT_SCRIPT with __THEME__ filled in. It seeds the theme the way
// a returning user has it, then does what the renderer's main.js does with it:
// asks the shell to paint the window. Until WP4 stores a server the renderer
// stops at the connection screen and never makes that call itself.
//
// After a few seconds it hides the page, so the next screenshot shows the
// window's own background (`theme_set`'s doing) and `pixel.mjs` can check it.
try {
  localStorage.setItem('theme', '__THEME__')
} catch {}
document.addEventListener('DOMContentLoaded', () => {
  const theme = localStorage.getItem('theme') || 'system'
  document.documentElement.classList.toggle('dark', theme === 'dark')
  window.agentrq.theme.set(theme)
  setTimeout(() => {
    for (const el of [document.documentElement, document.body]) {
      el.style.setProperty('background', 'transparent', 'important')
    }
    for (const el of document.body.children) el.style.setProperty('display', 'none', 'important')
  }, 14000)
})
