// Copyright 2026 Contextual, Inc. https://agentrq.com
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The Tauri renderer is the desktop renderer: these are desktop/'s settings
// (vite.renderer.config.mjs there explains each one), with two differences.
// The entry installs the Tauri bridge before importing desktop's main.js, and
// the target is the oldest WKWebView we run on rather than Chromium.
import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { fileURLToPath, URL } from 'node:url'

const resolvePath = (p) => fileURLToPath(new URL(p, import.meta.url))

const FRONTEND_SRC = resolvePath('../frontend/src')
const FRONTEND_ROOT = resolvePath('../frontend')
const FRONTEND_NODE_MODULES = resolvePath('../frontend/node_modules')

const { version } = JSON.parse(
  await import('node:fs/promises').then((fs) => fs.readFile(resolvePath('./package.json'), 'utf-8'))
)

export default defineConfig({
  root: resolvePath('./src/renderer'),
  base: '/',
  publicDir: resolvePath('../frontend/public'),
  define: {
    __APP_VERSION__: JSON.stringify(version),
  },
  resolve: {
    alias: [
      { find: '@app', replacement: FRONTEND_SRC },
      { find: 'virtual:pwa-register/vue', replacement: resolvePath('../frontend/src/desktop/useDesktopUpdates.js') },
      { find: /^(vue|vue-router|pinia)$/, replacement: `${FRONTEND_NODE_MODULES}/$1` },
    ],
    dedupe: ['vue', 'vue-router', 'pinia'],
  },
  // Tailwind scans from `root`, which holds no classes: frontend/src/style.css
  // keeps the frontend in the scan with `@source './'`.
  plugins: [vue(), tailwindcss()],
  optimizeDeps: {
    exclude: ['@huggingface/transformers'],
  },
  build: {
    outDir: resolvePath('./dist/renderer'),
    emptyOutDir: true,
    // WKWebView on macOS sets the floor, not Chromium.
    target: 'safari16',
  },
  server: {
    // Not desktop's 5174, so both shells can run in dev at once.
    port: 5175,
    strictPort: true,
    // Fetched by the app scheme handler in the Tauri process, not a browser tab.
    cors: true,
    fs: {
      allow: [resolvePath('./'), FRONTEND_ROOT, resolvePath('../desktop/src/renderer')],
    },
  },
})
