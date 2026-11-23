// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only OR LicenseRef-AgentRQ-Commercial

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'
import { readFileSync } from 'node:fs'

const { version } = JSON.parse(readFileSync('./package.json', 'utf-8'))

// The backend the dev server proxies to. The QA suite in qa/ runs its own on
// another port, so it never takes over the one a developer is using.
const apiTarget = process.env.AGENTRQ_DEV_API_TARGET || 'http://localhost:3000'

// The installed app's web manifest. No `orientation`: a lock would pin
// tablets and foldables to portrait.
export const manifest = {
  // The app's identity once installed; without it, a change to start_url
  // would make browsers treat the installed app as a different app.
  id: '/',
  name: 'AgentRQ',
  short_name: 'AgentRQ',
  description: 'Self-learning task manager and orchestrator for AI agents',
  theme_color: '#000000',
  background_color: '#fafafa',
  display: 'standalone',
  start_url: '/',
  scope: '/',
  icons: [
    {
      src: 'pwa-192x192.png',
      sizes: '192x192',
      type: 'image/png',
    },
    {
      src: 'pwa-512x512.png',
      sizes: '512x512',
      type: 'image/png',
    },
    {
      // The logo sits well inside the centre 80% circle, which is all an
      // Android launcher mask is guaranteed to keep.
      src: 'pwa-maskable-512x512.png',
      sizes: '512x512',
      type: 'image/png',
      purpose: 'maskable',
    },
  ],
  // Chrome shows its richer install sheet only with both form factors.
  screenshots: [
    {
      src: 'screenshots/narrow.png',
      sizes: '780x1688',
      type: 'image/png',
      form_factor: 'narrow',
      label: 'A task chat between you and your agent',
    },
    {
      src: 'screenshots/wide.png',
      sizes: '1280x800',
      type: 'image/png',
      form_factor: 'wide',
      label: 'A task chat between you and your agent',
    },
  ],
}

export default defineConfig({
  base: '',
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  plugins: [
    vue(),
    tailwindcss(),
    VitePWA({
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      registerType: 'prompt',
      includeAssets: ['favicon.ico', 'favicon.svg', 'agentrq.png', 'robots.txt'],
      manifest,
      injectManifest: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff2}'],
        // Only the install sheet shows these; no need to precache them.
        globIgnores: ['**/screenshots/**'],
      },
    }),
  ],
  optimizeDeps: {
    exclude: ['@huggingface/transformers'],
  },
  server: {
    port: 5173,
    proxy: {
      // ws: true is what makes the terminal work in dev, and its absence is
      // invisible: every ordinary API call proxies fine, so the configuration
      // looks correct right up until something asks for an upgrade. Without
      // it the terminal socket is never proxied at all — the browser sits on
      // "connecting" forever, with no error, because nothing ever answers the
      // handshake.
      '/api': { target: apiTarget, ws: true },
      '/mcp': apiTarget,
      '/pub': apiTarget,
      // The health probes, which would otherwise get the app and its sign-in page.
      '/healthz': apiTarget,
      '/readyz': apiTarget,
      // The operator's API and sign-in; the /admin/* pages stay with vite.
      '/admin/v1': apiTarget,
      '/auth/admin': apiTarget,
    }
  }
})
